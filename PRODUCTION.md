# SkillsDex Production Plan

This is the production deployment plan for a single SkillsDex instance exposed to the public internet.

The default deployment shape is:

```text
internet -> Caddy :443/:80 -> skillsdex 127.0.0.1:8765 -> SQLite /var/lib/skillsdex/skillsdex.db
```

Caddy is external deployment infrastructure. SkillsDex does not vendor, launch, or require Caddy at runtime; this repository only provides a known-good example config.

## Proxy Choice

Use Caddy by default.

Caddy is the best fit for SkillsDex because the first production target is a single host, a single app process, and a single public hostname. It gives us automatic HTTPS and HTTP-to-HTTPS redirects with a small config surface.

Use nginx instead if the server already has an nginx operational standard, shared configs, log shipping, and certificate automation.

Use Traefik instead if SkillsDex becomes one service among many container-platform workloads that need dynamic routing from labels or ingress resources.

For now, avoid Traefik as the default. It is strong for dynamic platforms, but that is unnecessary for one systemd service. Avoid nginx as the default because it usually adds more TLS/certbot configuration than this app needs.

## Production Milestones

### 1. Safe Bootstrap

Goal: a public instance must not be claimable through `admin/admin`.

Implementation tasks:

- Add a startup guard that refuses public binds when the default admin password is still active.
- Keep local-only first run available on `127.0.0.1`.
- Add or extend local admin CLI commands so an operator can set the initial admin password before opening the firewall.
- Document the bootstrap sequence.

Acceptance checks:

- `skillsdexd serve --addr 0.0.0.0:8765` fails when the default admin password is active.
- `skillsdexd serve --addr 127.0.0.1:8765` still works for local setup.
- `skillsdex admin reset-password ...` sets a valid password and revokes prior access.

### 2. Reverse Proxy And TLS

Goal: expose only Caddy publicly and keep SkillsDex bound to localhost.

Implementation tasks:

- Ship a Caddyfile example in `deploy/Caddyfile`.
- Document DNS, firewall, and systemd assumptions.
- Configure the systemd service with `--trusted-proxy 127.0.0.1` so loopback Caddy can pass `X-Forwarded-Proto` and `X-Forwarded-For`.
- Keep app-level security headers enabled.

Acceptance checks:

- Public ports: 80 and 443 only.
- SkillsDex listens on `127.0.0.1:8765`.
- The systemd unit includes `--trusted-proxy 127.0.0.1`.
- `curl -I https://example.com` shows HSTS, CSP, frame denial, and no-sniff headers.

### 3. Trusted Proxy Handling

Goal: rate limiting and audit logs should use the real client IP only when the proxy is trusted.

Implementation tasks:

- Add a `--trusted-proxy` config option.
- Only trust forwarded client-IP headers from configured proxy addresses.
- Keep `RemoteAddr` as the fallback.

Acceptance checks:

- Direct requests cannot spoof audit/rate-limit IPs through `X-Forwarded-For`.
- Requests through Caddy can record the actual client IP when configured.

### 4. Session Storage Hardening

Goal: database read access should not expose usable web sessions.

Implementation tasks:

- Store web session token hashes rather than raw session tokens.
- Migrate existing session rows safely or invalidate them.
- Keep CSRF tokens separate from session tokens.

Acceptance checks:

- Session lookup works with a presented cookie.
- Raw cookie values are not stored in `web_sessions`.
- Existing users can recover by logging in again if old sessions are invalidated.

### 5. Backups And Restore

Goal: production data can be recovered predictably.

Implementation tasks:

- Document SQLite file backup and SkillsDex backup export.
- Add a backup timer example or script.
- Document restore and verification steps.

Acceptance checks:

- A backup can be produced without stopping the service.
- A restore dry run succeeds.
- A fresh host can be restored from the documented artifacts.

### 6. Operations Runbook

Goal: one operator can safely install, update, recover, and inspect the service.

Implementation tasks:

- Document install, upgrade, rollback, logs, backups, password reset, and account lockout handling.
- Document post-deploy checks for auth, backups, local DB path, and proxy expectations.

Acceptance checks:

- A new host can be provisioned from the runbook.
- An admin password loss can be recovered with local shell access.
- A failed upgrade can be rolled back.

## Initial Implementation Order

1. Add Caddy production docs and example config.
2. Add the public-bind/default-password guard.
3. Add trusted proxy configuration.
4. Hash web session tokens.
5. Add backup timer/runbook.

## Production Runbook

### Host Assumptions

- One Linux host.
- SkillsDex runs as a systemd service.
- SkillsDex listens only on `127.0.0.1:8765`.
- Caddy is installed and managed separately by the host.
- Public firewall exposes only TCP 80 and 443.
- SQLite data lives under `/var/lib/skillsdex`.
- Backups are written outside the live data directory, for example `/var/backups/skillsdex`.

### Initial Install

Build and install the binary:

```bash
go build -o skillsdex ./cmd/skillsdex
go build -o skillsdexd ./cmd/skillsdexd
id -u skillsdex >/dev/null 2>&1 || sudo useradd --system --home-dir /var/lib/skillsdex --shell /usr/sbin/nologin skillsdex
sudo install -m 0755 skillsdex /usr/local/bin/skillsdex
sudo install -m 0755 skillsdexd /usr/local/bin/skillsdexd
sudo install -m 0644 deploy/skillsdex.service /etc/systemd/system/skillsdex.service
sudo systemctl daemon-reload
```

Or use the Taskfile helper:

```bash
task prod:install-service
```

From your workstation, build a Linux binary, upload it to the production server,
restart the service, and verify the local HTTP listener:

```bash
task prod:deploy-binary
```

The remote deployment task defaults to `root@178.104.5.217` with
`~/.ssh/ib_ssh`. Override those defaults when needed:

```bash
PROD_HOST=root@your-host PROD_SSH_KEY=~/.ssh/your-key task prod:deploy-binary
```

Start the app on loopback only:

```bash
sudo systemctl enable --now skillsdex
```

Before opening public traffic, set a non-default admin password from local shell access:

```bash
sudo systemctl stop skillsdex
sudo -u skillsdex /usr/local/bin/skillsdex admin reset-password \
  --db /var/lib/skillsdex/skillsdex.db \
  --username admin \
  --password '<new-long-password>'
sudo systemctl start skillsdex
```

Or use:

```bash
task prod:reset-admin-password
```

For non-interactive use:

```bash
PASSWORD='<new-long-password>' task prod:reset-admin-password
```

Optional invite emails use Resend. Configure them with a systemd override instead of committing secrets:

```bash
sudo systemctl edit skillsdex
```

```ini
[Service]
Environment="SKILLSDEX_RESEND_API_KEY=re_xxxxxxxxx"
Environment="SKILLSDEX_EMAIL_FROM=SkillsDex <invites@example.com>"
Environment="SKILLSDEX_EMAIL_REPLY_TO=admin@example.com"
```

Replace `re_xxxxxxxxx` with the real Resend API key and use a sender address from a domain verified in Resend. `SKILLSDEX_EMAIL_REPLY_TO` is optional. Then reload and restart:

```bash
sudo systemctl daemon-reload
sudo systemctl restart skillsdex
```

Install and configure Caddy outside this project. Use `deploy/Caddyfile` as the starting point, replacing `skills.example.com` with the production hostname. Caddy should proxy to `127.0.0.1:8765`.

### Firewall

Expose only:

```text
80/tcp
443/tcp
```

Do not expose `8765/tcp` publicly. SkillsDex should be reachable only from local Caddy.

### Logs

Inspect SkillsDex logs:

```bash
sudo journalctl -u skillsdex -f
```

Or:

```bash
task prod:logs
```

Inspect Caddy logs using the host's Caddy packaging defaults, commonly:

```bash
sudo journalctl -u caddy -f
```

### Post-Deploy Checks

Confirm the app is only listening on loopback:

```bash
ss -ltnp | grep ':8765'
```

Confirm only the proxy is public by checking the host firewall or cloud firewall for ports 80 and 443 only.

Confirm browser-facing security headers through Caddy:

```bash
curl -I https://skills.example.com
```

The response should include `Strict-Transport-Security`, `Content-Security-Policy`, `X-Frame-Options`, and `X-Content-Type-Options`.

From an authenticated admin machine, confirm API auth and local install health:

```bash
skillsdex whoami
skillsdex doctor --strict
```

After configuring backups, confirm at least one SQLite backup exists and run an app-level restore dry-run:

```bash
sudo test -s /var/backups/skillsdex/skillsdex-YYYYMMDDTHHMMSSZ.db
skillsdex backup restore --file ./skillsdex-backup.zip --dry-run
```

### Backup

Use two backup layers:

1. **SQLite file backup** for full service recovery.
2. **SkillsDex export zip** for app-level portability and restore dry-runs.

For SQLite, prefer the SQLite online backup command when `sqlite3` is available:

```bash
sudo install -d -m 0700 /var/backups/skillsdex
sudo sqlite3 /var/lib/skillsdex/skillsdex.db \
  ".backup '/var/backups/skillsdex/skillsdex-$(date -u +%Y%m%dT%H%M%SZ).db'"
```

An optional systemd timer example is included:

```bash
sudo install -d -m 0700 /var/backups/skillsdex
sudo install -m 0755 deploy/skillsdex-backup.sh /usr/local/bin/skillsdex-backup
sudo install -m 0644 deploy/skillsdex-backup.service /etc/systemd/system/skillsdex-backup.service
sudo install -m 0644 deploy/skillsdex-backup.timer /etc/systemd/system/skillsdex-backup.timer
sudo systemctl daemon-reload
sudo systemctl enable --now skillsdex-backup.timer
```

Or:

```bash
task prod:install-backup-timer
task prod:backup-now
```

Keep copies off-host. At minimum, verify that the backup file exists and is non-empty:

```bash
sudo test -s /var/backups/skillsdex/skillsdex-YYYYMMDDTHHMMSSZ.db
```

For app-level backup, create an API token in the web UI and run from a trusted admin machine:

```bash
skillsdex login --server https://skills.example.com --token-stdin
skillsdex backup export --out ./skillsdex-backup.zip
skillsdex backup inspect --file ./skillsdex-backup.zip
skillsdex backup restore --file ./skillsdex-backup.zip --dry-run
```

### Restore

For full host recovery:

1. Stop SkillsDex.
2. Move the damaged database aside.
3. Restore the SQLite backup.
4. Start SkillsDex.
5. Confirm login and run `skillsdex doctor` from an authenticated machine.

Example:

```bash
sudo systemctl stop skillsdex
sudo install -o skillsdex -g skillsdex -m 0600 /var/backups/skillsdex/skillsdex-YYYYMMDDTHHMMSSZ.db /var/lib/skillsdex/skillsdex.db
sudo systemctl start skillsdex
sudo journalctl -u skillsdex -n 100 --no-pager
```

For app-level recovery into an existing instance, use:

```bash
skillsdex backup restore --file ./skillsdex-backup.zip --dry-run
skillsdex backup restore --file ./skillsdex-backup.zip
```

### Upgrade

Build or download the new binaries, then replace them with quick rollback copies:

```bash
sudo cp /usr/local/bin/skillsdex /usr/local/bin/skillsdex.previous
sudo cp /usr/local/bin/skillsdexd /usr/local/bin/skillsdexd.previous
sudo install -m 0755 ./skillsdex /usr/local/bin/skillsdex
sudo install -m 0755 ./skillsdexd /usr/local/bin/skillsdexd
sudo systemctl restart skillsdex
sudo systemctl status skillsdex --no-pager
```

Or:

```bash
task prod:deploy-binary
task prod:restart
```

Check logs and the web login page. If the upgrade fails:

```bash
sudo install -m 0755 /usr/local/bin/skillsdex.previous /usr/local/bin/skillsdex
sudo install -m 0755 /usr/local/bin/skillsdexd.previous /usr/local/bin/skillsdexd
sudo systemctl restart skillsdex
```

Production task shortcuts:

```bash
task prod:install-service
task prod:deploy-binary
task prod:status
task prod:logs
task prod:reset-admin-password
task prod:install-backup-timer
task prod:backup-now
task prod:caddyfile-check
```

### Storage Guardrails

Application writes enforce these defaults:

- `16 MiB` maximum request body and skill package/backup restore upload.
- `1 MiB` maximum per skill file.
- Supporting files must be supported UTF-8 text types.
- `1,000` skills per user.
- `100` revisions per skill.
- `256 MiB` stored skill file bytes per user.
- `1 GiB` SQLite database size guardrail.

Use `Admin > Maintenance` to review database size, largest users/skills, prune
expired sessions, revoked token metadata, old audit events, and older skill
revisions while keeping the newest configured revisions per skill. Hard-deleting
a user removes the account and owned skills, revisions, sessions, tokens, tags,
and install sets; deactivation remains the reversible lockout option.

### Account Recovery

If the only admin password is lost, use local shell access:

```bash
sudo systemctl stop skillsdex
sudo -u skillsdex /usr/local/bin/skillsdex admin reset-password \
  --db /var/lib/skillsdex/skillsdex.db \
  --username admin \
  --password '<new-long-password>'
sudo systemctl start skillsdex
```

The reset revokes existing sessions and API tokens for that admin.

### Lockout Recovery

Login throttling is in-memory. Restarting the service clears temporary lockouts:

```bash
sudo systemctl restart skillsdex
```

Use this only after confirming the login attempts were expected or after rotating the affected password.
