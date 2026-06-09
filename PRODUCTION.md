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

## Production Status

The first single-host production path is implemented and documented:

- Public traffic terminates at external Caddy on ports 80 and 443.
- SkillsDex runs as a systemd service on `127.0.0.1:8765`.
- Public binds are refused while the seeded `admin/admin` password remains active.
- `skillsdex admin reset-password ...` can recover or rotate an admin password from local shell access.
- `--trusted-proxy` limits forwarded client IP handling to configured proxy addresses.
- App security headers are enabled for browser-facing routes.
- Web session tokens and API tokens are stored as hashes.
- SQLite runs in WAL mode with `synchronous=NORMAL`, a busy timeout, foreign keys, and in-memory temp storage.
- Daily SQLite backups are available as compressed zip files, with SkillsDex export zips as an app-level backup and restore-dry-run path.

Keep the runbook below current whenever deployment commands, service layout, backup behavior, or recovery steps change.

## Production Runbook

### Host Assumptions

- One Linux host.
- SkillsDex runs as a systemd service.
- SkillsDex listens only on `127.0.0.1:8765`.
- Caddy is installed and managed separately by the host.
- Public firewall exposes only TCP 80 and 443.
- SQLite data lives under `/var/lib/skillsdex`.
- SQLite runs in WAL mode. Seeing `skillsdex.db-wal` and `skillsdex.db-shm`
  beside the database while the service is active is expected.
- Backups are written outside the live data directory, for example `/var/backups/skillsdex`.
- Caddy's admin API should stay on loopback, access logs should be rotated
  under `/var/log/caddy`, and direct origin requests should be blocked when the
  hostname is proxied through Cloudflare.

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

The example Caddyfile includes a Cloudflare origin gate. Keep the Cloudflare IP
ranges current from `https://www.cloudflare.com/ips/`, or remove that matcher if
the hostname is not proxied through Cloudflare. A host firewall allowlist for
Cloudflare's IP ranges is an even stronger origin-protection layer when it is
practical to automate.

The example Caddyfile only uses directives compatible with the current
production package. If the host is upgraded to Caddy v2.10 or newer, consider
adding `request_body { max_size 16MB }` to reject oversized uploads at the proxy
before they reach SkillsDex.

### Cloudflare Free Hardening

When the production hostname is proxied through Cloudflare, use these baseline
Free-plan settings:

- Keep the DNS record proxied, not DNS-only.
- Set SSL/TLS encryption mode to `Full (strict)` once the origin has a valid
  certificate for the hostname.
- Leave the Cloudflare Free Managed Ruleset enabled and review Security Events
  after changes.
- Use one conservative rate limiting rule only after observing abuse. Good first
  candidates are repeated `POST /login` attempts or invite signup abuse.
- Treat Bot Fight Mode carefully because it applies broadly on the Free plan and
  can interfere with CLI/API traffic. Enable it only after testing normal CLI
  flows, or after there is real bot traffic to mitigate.
- Keep the Caddy origin gate or, preferably, add a host firewall allowlist for
  Cloudflare IP ranges so direct-to-origin traffic cannot bypass Cloudflare.
- Do not trust `CF-Connecting-IP` or forwarded IP headers unless direct origin
  traffic is blocked by Caddy or the host firewall.

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
sudo test -s /var/backups/skillsdex/skillsdex-YYYYMMDDTHHMMSSZ.zip
skillsdex backup restore --file ./skillsdex-backup.zip --dry-run
```

### Backup

Use two backup layers:

1. **SQLite file backup** for full service recovery.
2. **SkillsDex export zip** for app-level portability and restore dry-runs.

For SQLite, prefer the SQLite online backup command when `sqlite3` is available, then zip the copied database. The packaged backup script uses `zip` when available and falls back to Python's standard `zipfile` module:

```bash
sudo install -d -m 0700 /var/backups/skillsdex
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
sudo sqlite3 /var/lib/skillsdex/skillsdex.db ".backup '/var/backups/skillsdex/skillsdex-${stamp}.db'"
sudo zip -q -9 -j "/var/backups/skillsdex/skillsdex-${stamp}.zip" "/var/backups/skillsdex/skillsdex-${stamp}.db"
sudo rm -f "/var/backups/skillsdex/skillsdex-${stamp}.db"
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
sudo test -s /var/backups/skillsdex/skillsdex-YYYYMMDDTHHMMSSZ.zip
sudo unzip -l /var/backups/skillsdex/skillsdex-YYYYMMDDTHHMMSSZ.zip
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
tmp="$(mktemp)"
sudo unzip -p /var/backups/skillsdex/skillsdex-YYYYMMDDTHHMMSSZ.zip 'skillsdex-*.db' > "$tmp"
sudo install -o skillsdex -g skillsdex -m 0600 "$tmp" /var/lib/skillsdex/skillsdex.db
rm -f "$tmp"
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
