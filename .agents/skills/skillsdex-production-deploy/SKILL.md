---
name: skillsdex-production-deploy
description: Use when deploying, upgrading, recovering, or checking this SkillsDex repository on a single Linux server over SSH using systemd, SQLite, and external Caddy. Covers safe bootstrap, trusted proxy setup, admin password reset, backups, verification, and rollback for this project only.
metadata:
  short-description: Deploy SkillsDex over SSH with systemd and Caddy
---

# SkillsDex Production Deploy

Use this skill only for this repository's production path:

```text
internet -> external Caddy :443/:80 -> skillsdex systemd service on 127.0.0.1:8765 -> SQLite /var/lib/skillsdex/skillsdex.db
```

Do not use Docker as the live deployment path.

## Safety Rules

- Never ask the user to paste SSH private keys, server passwords, or admin passwords into chat.
- Prefer an existing SSH config alias or `ssh-agent`.
- If an admin password is needed, ask the user to set it locally in an environment variable such as `PASSWORD` before running commands, or let the remote CLI prompt interactively.
- Always verify the SSH target before changing it: hostname, OS, architecture, current user, and whether `sudo` works.
- Stop on host mismatch, unexpected architecture, missing sudo, dirty local build state that matters, or commands that would expose port `8765` publicly.
- Treat Caddy as external host infrastructure. This repo may install an example Caddyfile, but SkillsDex must not manage Caddy as an app dependency.
- Use non-interactive commands where possible. Avoid interactive shells and editors.

## Required Inputs

Before deploying, establish:

- SSH target, preferably a host alias such as `skillsdex-prod`.
- Production domain.
- Target architecture from `ssh <host> 'uname -m'`.
- Whether Caddy is installed and managed by the host.
- Whether the user wants password reset to be interactive or via `PASSWORD=...`.

## Local Preflight

From the repo root:

```bash
git status --short --branch
go test -count=1 ./...
task --list
```

Read these files before changing deployment behavior:

- `PRODUCTION.md`
- `deploy/skillsdex.service`
- `deploy/Caddyfile`
- `Taskfile.yml`

## Remote Preflight

Use SSH to confirm the target:

```bash
ssh <host> 'hostname; uname -a; id; command -v sudo; command -v systemctl; command -v caddy || true; ss -ltnp | sed -n "1,40p"'
```

Confirm:

- Linux host.
- `systemctl` is available.
- `sudo` is available for the SSH user.
- Caddy is installed or the user will install it.
- Port `8765` is not already publicly bound.

## Deployment Workflow

1. Build a Linux binary locally for the remote architecture.
2. Upload binary and deploy files.
3. Create the `skillsdex` system user if missing.
4. Install `/usr/local/bin/skillsdex` and `/usr/local/bin/skillsdexd`.
5. Install `deploy/skillsdex.service`.
6. Reload systemd and start the service.
7. Reset the admin password before opening public traffic.
8. Install or update Caddy config externally using `deploy/Caddyfile` as the example.
9. Install backup timer if requested.
10. Run verification.

Prefer repo tasks when operating directly on the server from a checked-out repo:

```bash
task prod:install-service
PASSWORD='<new-long-password>' task prod:reset-admin-password
task prod:install-backup-timer
task prod:backup-now
task prod:status
```

When deploying over SSH from a local checkout, use the same underlying commands from `PRODUCTION.md` and upload artifacts with `scp` or `rsync`.

## Verification

After deployment:

```bash
ssh <host> 'sudo systemctl status skillsdex --no-pager'
ssh <host> 'sudo journalctl -u skillsdex -n 100 --no-pager'
ssh <host> 'ss -ltnp | grep ":8765"'
curl -I https://<domain>
```

Expected:

- SkillsDex listens on `127.0.0.1:8765`, not public interfaces.
- Public firewall exposes only `80/tcp` and `443/tcp`.
- Caddy proxies HTTPS to local SkillsDex.
- Headers include `Strict-Transport-Security`, `Content-Security-Policy`, `X-Frame-Options`, and `X-Content-Type-Options`.
- Login works with the rotated admin password.

From an authenticated client machine:

```bash
skillsdex whoami
skillsdex doctor --strict
skillsdex backup export --out ./skillsdex-backup.zip
skillsdex backup restore --file ./skillsdex-backup.zip --dry-run
```

## Backup Workflow

Install the backup timer:

```bash
ssh <host> 'sudo install -d -m 0700 /var/backups/skillsdex'
ssh <host> 'sudo install -m 0755 deploy/skillsdex-backup.sh /usr/local/bin/skillsdex-backup'
ssh <host> 'sudo install -m 0644 deploy/skillsdex-backup.service /etc/systemd/system/skillsdex-backup.service'
ssh <host> 'sudo install -m 0644 deploy/skillsdex-backup.timer /etc/systemd/system/skillsdex-backup.timer'
ssh <host> 'sudo systemctl daemon-reload && sudo systemctl enable --now skillsdex-backup.timer'
```

Run a backup once and verify:

```bash
ssh <host> 'sudo systemctl start skillsdex-backup.service && sudo systemctl status skillsdex-backup.service --no-pager'
ssh <host> 'sudo find /var/backups/skillsdex -type f -name "skillsdex-*.db" -size +0 -print | tail -5'
```

## Upgrade And Rollback

Before replacing the binary:

```bash
ssh <host> 'sudo cp /usr/local/bin/skillsdex /usr/local/bin/skillsdex.previous'
ssh <host> 'sudo cp /usr/local/bin/skillsdexd /usr/local/bin/skillsdexd.previous'
```

After upload:

```bash
ssh <host> 'sudo install -m 0755 ./skillsdex /usr/local/bin/skillsdex && sudo install -m 0755 ./skillsdexd /usr/local/bin/skillsdexd && sudo systemctl restart skillsdex && sudo systemctl status skillsdex --no-pager'
```

From this repository, use the remote deployment task for the current production
host:

```bash
task prod:deploy-binary
```

Rollback:

```bash
ssh <host> 'sudo install -m 0755 /usr/local/bin/skillsdex.previous /usr/local/bin/skillsdex && sudo install -m 0755 /usr/local/bin/skillsdexd.previous /usr/local/bin/skillsdexd && sudo systemctl restart skillsdex'
```

## Recovery

Admin password recovery:

```bash
ssh <host> 'sudo systemctl stop skillsdex'
ssh <host> 'sudo -u skillsdex /usr/local/bin/skillsdex admin reset-password --db /var/lib/skillsdex/skillsdex.db --username admin'
ssh <host> 'sudo systemctl start skillsdex'
```

Temporary login lockout recovery:

```bash
ssh <host> 'sudo systemctl restart skillsdex'
```

Use lockout recovery only after confirming the attempts were expected or after rotating the affected password.
