# Deployment runbook

End-to-end: **Pulumi** provisions the server + DNS, then **Ansible** hardens the host and
deploys the app as rootless Podman quadlets.

## 0. Prerequisites

- A domain delegated to **Google Cloud DNS**; a GCP project with the Cloud DNS API enabled.
- A **Hetzner Cloud** API token.
- A **GitHub App** (App ID, installation ID, private key) with *Issues: write* and
  *Projects: write*, installed on the org/repos that host your boards.
- A **Cloudflare R2** bucket + access key/secret (Pulumi state).
- Tooling on your workstation: `pulumi`, `go`, `ansible`, `rsync`, `node`/`npm`.

## 1. Configure the Pulumi state backend (Cloudflare R2)

```bash
export AWS_ACCESS_KEY_ID=<r2-access-key>
export AWS_SECRET_ACCESS_KEY=<r2-secret-key>
export PULUMI_CONFIG_PASSPHRASE=<a-strong-passphrase>
pulumi login "s3://idea-collect-pulumi-state?endpoint=<account-id>.r2.cloudflarestorage.com&region=auto&s3ForcePathStyle=true"
```

## 2. Provision infrastructure

```bash
cd infra/pulumi
pulumi stack init prod
pulumi config set --secret hcloud:token <hetzner-token>
# Edit Pulumi.prod.yaml (domain, sshPublicKey, dnsZoneName, gcp:project, sshSourceIps…)
pulumi up
```

Outputs: `serverIPv4/serverIPv6`, `domain`, and `gcpDnsSaKeyBase64` (secret — the Caddy
DNS service-account key).

## 3. Prepare Ansible vars + secrets

```bash
cd ../ansible
ansible-galaxy collection install -r requirements.yml
cp group_vars/all.example.yml  group_vars/all.yml      # edit
cp group_vars/vault.example.yml group_vars/vault.yml   # edit, then encrypt

# The GCP DNS service-account JSON comes from Pulumi:
pulumi -C ../pulumi stack output gcpDnsSaKeyBase64 --show-secrets --stack prod | base64 -d
# paste into vault.yml as vault_gcp_dns_sa_json, then:
ansible-vault encrypt group_vars/vault.yml
```

## 4. Build the frontend and generate the inventory

```bash
make -C ../../ frontend-build         # produces frontend/dist (synced to the host as the SPA)
./scripts/inventory.sh prod           # writes inventory/hosts.generated.ini from Pulumi
```

## 5. Configure + deploy the host

```bash
ansible-playbook site.yml --ask-vault-pass
```

This runs the roles in order: **base** (hardening, deploy user, unattended upgrades) →
**nftables** (default-drop firewall) → **podman** (rootless + linger + port sysctl) →
**fail2ban** (SSH + Caddy/Coraza jails) → **secrets** (env/keys from vault) → **deploy**
(build images, install quadlets, start services). The backend applies DB migrations on
startup.

## 6. Provision projects + invite codes

```bash
ssh deploy@<server>
podman exec -it backend /usr/local/bin/admin \
  project create --slug demo --name "Demo App" --owner my-org --repo demo-feedback --project-number 1
podman exec -it backend /usr/local/bin/admin \
  user create --project demo --name "Jane Doe" --email jane@example.com
# -> prints the invite code to share
```

## Verify

```bash
curl -I https://<domain>                       # valid cert via ACME DNS-01 (Google Cloud DNS)
# Submit an idea through the form, then confirm the GitHub issue + board item appear.

# WAF: a SQLi-looking probe should be blocked by Coraza (403)
curl -i "https://<domain>/api/?id=1%27%20OR%20%271%27=%271"

# fail2ban: after several bad codes the client IP is banned
sudo fail2ban-client status caddy-auth
sudo nft list ruleset | grep -A3 'set f2b'
```

## Operations

```bash
# As the deploy user:
systemctl --user status caddy backend postgres
journalctl --user -u backend -f
# Retry any submissions whose GitHub call failed:
podman exec -it backend /usr/local/bin/admin submission retry
```

## Architecture notes

- Only Caddy publishes `80/443`; backend and Postgres are reachable only on the internal
  Podman network. Rootless Caddy binds the privileged ports via
  `net.ipv4.ip_unprivileged_port_start=80`.
- Two firewall layers: the Hetzner Cloud firewall (restricts SSH/dev-port *source* to
  `sshSourceIps`) and host nftables (default-drop). In the `dev` environment both also open
  `5173/8080/5432`.
- TLS uses ACME **DNS-01** via Google Cloud DNS, so certificates issue without any inbound
  dependency and support wildcards.
