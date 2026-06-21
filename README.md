# idea-collect

A lightweight idea/feedback collection tool for non-technical audiences. An invited person
enters an invite/auth code, fills in a simple form, and their submission lands on a GitHub
Projects v2 Kanban board as an Issue with their name in the body.

## Stack

| Layer        | Choice |
|--------------|--------|
| Frontend     | Vite + Svelte + TypeScript (static SPA) |
| Backend      | Go (`net/http`, `pgx`) |
| Database     | PostgreSQL 18 |
| GitHub       | GitHub App → Issues + Projects v2 (GraphQL) |
| Runtime      | Rootless Podman quadlets (Caddy → backend → Postgres) |
| TLS / WAF    | Caddy (Coraza WAF + Google Cloud DNS ACME DNS-01) |
| Firewall     | Hetzner Cloud firewall + host nftables + fail2ban |
| IaC          | Pulumi (Go) — Hetzner Cloud + Google Cloud DNS, state in Cloudflare R2 |
| Config mgmt  | Ansible |

## Layout

```
frontend/   Vite + Svelte + TS SPA
backend/    Go API server (cmd/server) + admin CLI (cmd/admin)
infra/
  pulumi/   Pulumi Go program (hcloud + gcp dns)
  ansible/  roles + playbooks (host hardening + deploy)
deploy/
  caddy/    custom Caddy (xcaddy) image + Caddyfile + CRS
  quadlets/ Podman *.container / *.network / *.volume units
  fail2ban/ jails + filters
  nftables/ ruleset
```

## Quick start (local dev)

Prerequisites: Go 1.26+, Node 20+, Podman or Docker.

```bash
# 1. Start Postgres 18
make db-up

# 2. Run the backend (applies migrations on startup)
make backend-run            # listens on :8080

# 3. Create a project + a user (prints an auth code)
make admin ARGS="project create --slug demo --name 'Demo App' \
    --owner my-org --repo demo-feedback --project-number 1"
make admin ARGS="user create --project demo --name 'Jane Doe' --email jane@example.com"

# 4. Run the frontend (proxies /api -> :8080)
make frontend-dev           # http://localhost:5173
```

See `make help` for all targets. Configuration is via environment variables — copy
`backend/.env.example` to `backend/.env`.

## Deployment

1. **Provision** infrastructure with Pulumi (`infra/pulumi`) — creates the Hetzner server,
   firewall, Google Cloud DNS zone/records and a DNS service account. State lives in
   Cloudflare R2.
2. **Configure** the host with Ansible (`infra/ansible`) — hardening, nftables, rootless
   Podman, fail2ban, secrets, then deploys the quadlet units and runs migrations.

Full runbook: [`infra/README.md`](infra/README.md).
