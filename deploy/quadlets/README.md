# Podman quadlets (rootless)

These units run as the unprivileged `deploy` user. The Ansible `deploy` role copies
them to `~/.config/containers/systemd/` and runs `systemctl --user daemon-reload`.

## Runtime layout (on the host, under the deploy user's home)

```
~/idea-collect/
├── Caddyfile                     # bind-mounted into caddy
├── www/                          # built frontend (dist) bind-mounted into caddy
├── logs/                         # Caddy access + Coraza audit logs (fail2ban reads these)
└── secrets/
    ├── postgres.env
    ├── backend.env
    ├── caddy.env
    ├── github-app.private-key.pem
    └── gcp-dns-sa.json
```

## Prerequisites (handled by the Ansible `podman` role)

- Rootless Podman installed; `loginctl enable-linger deploy` so units start at boot.
- `sysctl net.ipv4.ip_unprivileged_port_start=80` so rootless Caddy can bind 80/443.
- Images built/loaded: `localhost/idea-collect-backend:latest`,
  `localhost/idea-collect-caddy:latest`.

## Manual control (as the deploy user)

```bash
systemctl --user daemon-reload
systemctl --user start caddy.service        # pulls in backend -> postgres via Requires=
systemctl --user status caddy backend postgres
journalctl --user -u backend -f
```

## First-run migrations

The backend applies embedded migrations automatically on startup. To provision projects
and invite codes, run the admin CLI inside the backend container:

```bash
podman exec -it backend /usr/local/bin/admin project list
```
