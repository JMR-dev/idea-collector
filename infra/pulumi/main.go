// Pulumi program: provisions a Hetzner Cloud server (with an environment-aware cloud
// firewall) and Google Cloud DNS records for idea-collect, plus a least-privilege DNS
// service account used by Caddy for ACME DNS-01. Outputs feed the Ansible inventory.
package main

import (
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/dns"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-hcloud/sdk/go/hcloud"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// cloud-init: make sure Python is present so Ansible can manage the host.
const userData = `#cloud-config
package_update: true
packages:
  - python3
`

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		environment := getOr(cfg, "environment", "prod")
		domain := cfg.Require("domain")
		serverType := getOr(cfg, "hcloudServerType", "cax11")
		location := getOr(cfg, "hcloudLocation", "fsn1")
		image := getOr(cfg, "hcloudImage", "debian-12")
		sshPublicKey := cfg.Require("sshPublicKey")
		dnsZoneName := cfg.Require("dnsZoneName")
		createZone := cfg.GetBool("createDnsZone")

		// SSH (and dev ports) are restricted to these CIDRs; default open if unset.
		sshSourceIps := []string{"0.0.0.0/0", "::/0"}
		_ = cfg.GetObject("sshSourceIps", &sshSourceIps)

		gcpProject := config.New(ctx, "gcp").Require("project")

		// --- Hetzner: SSH key, firewall, server ---
		sshKey, err := hcloud.NewSshKey(ctx, "idea-collect", &hcloud.SshKeyArgs{
			Name:      pulumi.String("idea-collect"),
			PublicKey: pulumi.String(sshPublicKey),
		})
		if err != nil {
			return err
		}

		rules := publicRules()
		if environment == "dev" {
			// Expose higher dev ports, but only to the admin source IPs.
			rules = append(rules, devRules(toStringArray(sshSourceIps))...)
		}
		rules = append(rules, &hcloud.FirewallRuleArgs{ // SSH, restricted
			Direction: pulumi.String("in"),
			Protocol:  pulumi.String("tcp"),
			Port:      pulumi.String("22"),
			SourceIps: toStringArray(sshSourceIps),
		})

		firewall, err := hcloud.NewFirewall(ctx, "idea-collect", &hcloud.FirewallArgs{
			Name:  pulumi.String("idea-collect"),
			Rules: rules,
			ApplyTos: hcloud.FirewallApplyToArray{
				&hcloud.FirewallApplyToArgs{LabelSelector: pulumi.String("app=idea-collect")},
			},
		})
		if err != nil {
			return err
		}

		server, err := hcloud.NewServer(ctx, "idea-collect", &hcloud.ServerArgs{
			Name:       pulumi.String("idea-collect-" + environment),
			ServerType: pulumi.String(serverType),
			Image:      pulumi.String(image),
			Location:   pulumi.String(location),
			SshKeys:    pulumi.StringArray{sshKey.Name},
			UserData:   pulumi.String(userData),
			Labels:     pulumi.StringMap{"app": pulumi.String("idea-collect"), "env": pulumi.String(environment)},
		}, pulumi.DependsOn([]pulumi.Resource{firewall}))
		if err != nil {
			return err
		}

		// --- Google Cloud DNS: optional zone + A/AAAA records ---
		zoneRef := pulumi.String(dnsZoneName).ToStringOutput()
		if createZone {
			zone, err := dns.NewManagedZone(ctx, "idea-collect", &dns.ManagedZoneArgs{
				Name:        pulumi.String(dnsZoneName),
				DnsName:     pulumi.String(cfg.Require("dnsZoneDomain")), // e.g. example.com.
				Description: pulumi.String("idea-collect"),
				Project:     pulumi.String(gcpProject),
			})
			if err != nil {
				return err
			}
			zoneRef = zone.Name
		}

		fqdn := domain + "."
		if _, err := dns.NewRecordSet(ctx, "idea-collect-a", &dns.RecordSetArgs{
			Name:        pulumi.String(fqdn),
			ManagedZone: zoneRef,
			Type:        pulumi.String("A"),
			Ttl:         pulumi.Int(300),
			Rrdatas:     pulumi.StringArray{server.Ipv4Address},
			Project:     pulumi.String(gcpProject),
		}); err != nil {
			return err
		}
		if _, err := dns.NewRecordSet(ctx, "idea-collect-aaaa", &dns.RecordSetArgs{
			Name:        pulumi.String(fqdn),
			ManagedZone: zoneRef,
			Type:        pulumi.String("AAAA"),
			Ttl:         pulumi.Int(300),
			Rrdatas:     pulumi.StringArray{server.Ipv6Address},
			Project:     pulumi.String(gcpProject),
		}); err != nil {
			return err
		}

		// --- DNS service account for Caddy ACME DNS-01 (scoped to this zone) ---
		sa, err := serviceaccount.NewAccount(ctx, "caddy-dns", &serviceaccount.AccountArgs{
			AccountId:   pulumi.String("idea-collect-caddy-dns"),
			DisplayName: pulumi.String("idea-collect Caddy DNS-01"),
			Project:     pulumi.String(gcpProject),
		})
		if err != nil {
			return err
		}
		if _, err := dns.NewDnsManagedZoneIamMember(ctx, "caddy-dns-admin", &dns.DnsManagedZoneIamMemberArgs{
			ManagedZone: zoneRef,
			Role:        pulumi.String("roles/dns.admin"),
			Member:      pulumi.Sprintf("serviceAccount:%s", sa.Email),
			Project:     pulumi.String(gcpProject),
		}); err != nil {
			return err
		}
		saKey, err := serviceaccount.NewKey(ctx, "caddy-dns", &serviceaccount.KeyArgs{
			ServiceAccountId: sa.Name,
		})
		if err != nil {
			return err
		}

		// --- Outputs (consumed by infra/ansible/scripts/inventory.sh) ---
		ctx.Export("environment", pulumi.String(environment))
		ctx.Export("serverIPv4", server.Ipv4Address)
		ctx.Export("serverIPv6", server.Ipv6Address)
		ctx.Export("domain", pulumi.String(domain))
		ctx.Export("sshUser", pulumi.String("root"))
		// Base64-encoded service-account JSON; Ansible decodes it to gcp-dns-sa.json.
		ctx.Export("gcpDnsSaKeyBase64", pulumi.ToSecret(saKey.PrivateKey))
		return nil
	})
}

// publicRules returns the always-on inbound rules (HTTP, HTTPS, QUIC) open to the world.
func publicRules() hcloud.FirewallRuleArray {
	all := pulumi.StringArray{pulumi.String("0.0.0.0/0"), pulumi.String("::/0")}
	return hcloud.FirewallRuleArray{
		&hcloud.FirewallRuleArgs{Direction: pulumi.String("in"), Protocol: pulumi.String("tcp"), Port: pulumi.String("80"), SourceIps: all},
		&hcloud.FirewallRuleArgs{Direction: pulumi.String("in"), Protocol: pulumi.String("tcp"), Port: pulumi.String("443"), SourceIps: all},
		&hcloud.FirewallRuleArgs{Direction: pulumi.String("in"), Protocol: pulumi.String("udp"), Port: pulumi.String("443"), SourceIps: all},
	}
}

// devRules opens the higher development ports (Vite, backend, Postgres) to admin IPs only.
func devRules(src pulumi.StringArrayInput) hcloud.FirewallRuleArray {
	return hcloud.FirewallRuleArray{
		&hcloud.FirewallRuleArgs{Direction: pulumi.String("in"), Protocol: pulumi.String("tcp"), Port: pulumi.String("5173"), SourceIps: src},
		&hcloud.FirewallRuleArgs{Direction: pulumi.String("in"), Protocol: pulumi.String("tcp"), Port: pulumi.String("8080"), SourceIps: src},
		&hcloud.FirewallRuleArgs{Direction: pulumi.String("in"), Protocol: pulumi.String("tcp"), Port: pulumi.String("5432"), SourceIps: src},
	}
}

func toStringArray(in []string) pulumi.StringArray {
	out := make(pulumi.StringArray, len(in))
	for i, s := range in {
		out[i] = pulumi.String(s)
	}
	return out
}

func getOr(cfg *config.Config, key, def string) string {
	if v := cfg.Get(key); v != "" {
		return v
	}
	return def
}
