# SkillsDex

Private dotfiles for AI-agent skills.

SkillsDex is a private hosted skill library plus CLI for syncing reusable Codex skills across machines. The user CLI is `skillsdex`; the web app and JSON API run from the `skillsdexd` server binary with SQLite storage and Codex install/sync support.

## License

SkillsDex is source-available under the PolyForm Noncommercial License 1.0.0.
You may inspect, copy, modify, and share it for non-commercial purposes, but
commercial use is not licensed without separate written permission from the
copyright holder. See [LICENSE](LICENSE).

## Quickstart

Build the binary:

```bash
task build
```

Start the web app and API:

```bash
./bin/skillsdexd serve
```

Or build, start, and open the website in one step:

```bash
task launch
```

Open:

```text
http://127.0.0.1:8765
```

First startup creates:

```text
username: admin
password: admin
```

The first `admin/admin` login is redirected to `Setup > Change Password`; replace the default password before continuing.

In the web app:

1. Log in as `admin`.
2. Change the default password if prompted, then open `Setup` for the guided first-run checklist.
3. Open `Admin > Invites` if you want another user to create their own account.
4. Create a skill, import one from GitHub, or open `Top Skills` to import from agentskill.sh.
5. Open `Collections` to create a named install set for a machine, workflow, client, or project type.
6. Open `API Tokens`.
7. Generate a token and copy it before leaving the page.

To dump your library, open `Skills` and choose `Export`. The download is a zip of every owned skill package, including archived skills, plus a `skillsdex-export.json` manifest with revision and source metadata. To restore it, open `Import`, choose `Restore Backup`, and upload the zip.

The same backup path is available from the CLI:

```bash
./bin/skillsdex backup export --out ./skillsdex-backup.zip
./bin/skillsdex backup inspect --file ./skillsdex-backup.zip
./bin/skillsdex backup inspect --file ./skillsdex-backup.zip --json
./bin/skillsdex backup restore --file ./skillsdex-backup.zip --dry-run
./bin/skillsdex backup restore --file ./skillsdex-backup.zip
```

`backup inspect` and `backup restore --dry-run` validate the zip locally and summarize active, archived, pinned, recommended, source-backed, and supporting-file counts before you upload or restore it. Add `--json` to either local validation command when scripts need structured output.

For a repository-friendly one-way export, choose `GitHub Export` or run:

```bash
./bin/skillsdex github export --dir ./skills-export --yes
```

That writes a `skills/<owner>/<slug>/SKILL.md` tree plus any supporting files and a README that can be committed to GitHub. SkillsDex remains the source of truth; this is intentionally not bidirectional sync.

Authenticate the CLI:

```bash
printf '%s\n' "$SKILLSDEX_TOKEN" | ./bin/skillsdex login --server http://127.0.0.1:8765 --token-stdin
./bin/skillsdex login --server http://127.0.0.1:8765 --token <token>
```

API tokens are shown once when created. After that, the web app stores only a hash and displays a short preview so you can identify and revoke the token later.

Connect a local client to the hosted SkillsDex service:

```bash
export SKILLSDEX_TOKEN="<token from https://www.skillsdex.org API Tokens>"
printf '%s\n' "$SKILLSDEX_TOKEN" | skillsdex login --server https://www.skillsdex.org --token-stdin
skillsdex whoami
skillsdex config set default_target codex
skillsdex doctor --fix-permissions
skillsdex setup
```

Use the web app at `https://www.skillsdex.org` to log in, open `API Tokens`,
and create a token before running the CLI login command. Prefer
`--token-stdin` over `--token <token>` on shared machines because it avoids
putting the token in shell history. `login` verifies the token against
`/v1/me`, then stores the server URL and token under the local SkillsDex config
directory. Run `skillsdex doctor --fix-permissions` after login if the auth,
manifest, or config files need to be made private.

Invite another user:

```bash
./bin/skillsdex invites
./bin/skillsdex invites create
./bin/skillsdex invites create --email user@example.com
./bin/skillsdex invites create --json
./bin/skillsdex invites revoke <invite-id>
```

Admins can also create, email, and revoke invites from `Admin > Invites`. Each invite creates a one-time signup URL that expires after 24 hours. The invite authorizes signup by possession of the URL; the new user chooses their own username and may add email as account metadata. After signup, the user is sent to the login page.

Invite emails are optional and use Resend when configured on the server:

```bash
export SKILLSDEX_RESEND_API_KEY="re_xxxxxxxxx"
export SKILLSDEX_EMAIL_FROM="SkillsDex <invites@example.com>"
export SKILLSDEX_EMAIL_REPLY_TO="admin@example.com"
```

Replace `re_xxxxxxxxx` with the real Resend API key and use a sender address from a domain verified in Resend. `SKILLSDEX_EMAIL_REPLY_TO` is optional. If email is not configured, invite creation still returns the signup URL for manual sharing.

Preview the local onboarding plan:

```bash
./bin/skillsdex setup
./bin/skillsdex setup --set laptop
./bin/skillsdex setup --set laptop --json
```

`setup` checks the server, chooses a single non-empty install set when one exists, otherwise falls back to pinned skills and then recommended defaults. It previews file writes by default; add `--apply --yes` after reviewing the plan. Add `--json` for a preview-only setup plan with the selected source, apply command, and follow-up trust gates. Both preview and apply modes end with a quality gate plus backup export and restore dry-run commands so the first-run loop includes discoverability and source-of-truth checks.

Open the terminal dashboard for day-to-day install management:

```bash
./bin/skillsdex tui
```

The TUI is a non-editor surface for browsing library skills and install sets,
previewing or applying installs, running sync previews/apply, checking source
refresh, lint, and doctor status, and reviewing action output. It intentionally
does not edit `SKILL.md`, supporting files, or install set membership; those
remain web/editor or future TUI workflows.

List available skills:

```bash
./bin/skillsdex skills
./bin/skillsdex skills --json
./bin/skillsdex skills --tag testing
./bin/skillsdex show admin/five-advisor-review
./bin/skillsdex show admin/five-advisor-review --file references/guide.md
./bin/skillsdex show admin/five-advisor-review --raw
./bin/skillsdex mark admin/five-advisor-review --pinned --apply --yes
./bin/skillsdex mark admin/five-advisor-review --recommended --apply --yes
./bin/skillsdex lint --all
./bin/skillsdex lint admin/five-advisor-review
./bin/skillsdex lint --file ./my-skill
./bin/skillsdex lint --file ~/.agents/skills/admin--five-advisor-review/SKILL.md
./bin/skillsdex lint --json --strict --file ~/.agents/skills/admin--five-advisor-review/SKILL.md
```

Skills can be tagged with lightweight lowercase labels such as `go`, `python`, `testing`, or `security`. Tags are edited on the web skill form, shown on list/detail pages, included in API JSON, and preserved in SkillsDex backup export/restore. Use the web skill list tag chips, `GET /v1/skills?tag=<tag>`, or `skills --tag <tag>` to narrow list results.

Tags are filtering-only metadata. They do not install, sync, pin, recommend, trust, or group skills; use pinned installs, recommended defaults, and named install sets for those behaviors.

`show --file` prints one stored package file, including supporting files from full skill folders, without the metadata summary.

`mark` updates install flags without changing skill package content. Use `--pinned` or `--recommended` to add a skill to first-run install flows; use `--unpinned` or `--not-recommended` to remove it. It previews by default unless you add `--apply`.

Use `lint --all --json --strict` as a library-wide quality gate before exporting, syncing a new machine, or publishing a skill pack. `lint --file` accepts either a `SKILL.md` file or a full skill folder, and folder linting checks supporting files for large context, sensitive material, non-executable scripts, and non-standard package locations.

Search public agentskill.sh skills from the CLI:

```bash
./bin/skillsdex search "code review"
./bin/skillsdex top --limit 20
./bin/skillsdex import-source @openclaw/openclaw-debugging
./bin/skillsdex import-source @openclaw/openclaw-debugging --install --dry-run
./bin/skillsdex import-source @openclaw/openclaw-debugging --install --yes
```

`import-source --install` imports the source into your library, then installs the successfully imported skill package through the same managed-file protections as `install`. `--dry-run` previews only the local install step; the library import still happens.

Imported skills keep their local SkillsDex identifier, such as `admin/golang-pro`, and also store source provenance separately. For GitHub sources this includes the source author or organization, repository, package path, ref, and original URL, for example `Jeffallan/claude-skills · skills/golang-pro/SKILL.md @ main`. Existing source-backed skills with only a source URL are backfilled on startup when the URL is parseable. Skill detail pages, `show`, backup inspect, API JSON, and SkillsDex backup export/restore preserve that context without changing install commands or local ownership.

Publish a local `SKILL.md` or full skill folder into SkillsDex:

```bash
./bin/skillsdex templates
./bin/skillsdex new ./my-skill --template workflow --with-folders
./bin/skillsdex publish ./my-skill
./bin/skillsdex publish ./my-skill --apply --yes
./bin/skillsdex publish ./SKILL.md --slug local-review --strict
```

`templates` lists the reusable scaffold templates. `new` scaffolds a local skill folder from the same workflow, review, and research templates used by the web editor. `publish` previews by default, lints before writing, lists included package files, preserves supporting files from skill folders, and creates or updates the matching library skill only with `--apply`. Folder publishes skip and report common local metadata and secret-prone files such as `.git/`, `.vscode/`, `node_modules/`, `.env`, and `.DS_Store`; keep intentional assets under package paths such as `references/`, `scripts/`, `assets/`, or `templates/`.

Preview upstream changes for imported GitHub or agentskill.sh skills:

```bash
./bin/skillsdex refresh admin/five-advisor-review
./bin/skillsdex refresh admin/five-advisor-review --diff
./bin/skillsdex refresh admin/five-advisor-review --json
./bin/skillsdex refresh --all
./bin/skillsdex refresh --all --strict
./bin/skillsdex refresh --all --apply --yes
```

Install all recommended default skills into Codex:

```bash
./bin/skillsdex config set default_target codex
./bin/skillsdex install-defaults --dry-run
./bin/skillsdex install-defaults
```

Install your pinned skill set on a new machine:

```bash
./bin/skillsdex install-pinned --dry-run
./bin/skillsdex install-pinned
```

Install a named collection:

```bash
./bin/skillsdex sets
./bin/skillsdex sets laptop
./bin/skillsdex sets laptop --json
./bin/skillsdex install-set laptop --dry-run
./bin/skillsdex install-set laptop
```

Or install a single skill:

```bash
./bin/skillsdex install admin/five-advisor-review --dry-run
./bin/skillsdex install admin/five-advisor-review
```

The Codex skill is written to:

```text
~/.agents/skills/admin--five-advisor-review/SKILL.md
```

If the skill has supporting files such as `references/`, `scripts/`, `assets/`, or `templates/`, they are installed beside `SKILL.md` inside the same managed skill folder. The local manifest tracks every managed package file, so `skillsdex sync` updates supporting files and removes stale managed files as upstream revisions change.

Install dry-runs list the package files that would be written, including executable supporting files, and preview overwrite/removal diffs without prompting or writing files.

Executable flags are preserved for supporting files, but SkillsDex does not execute package scripts. New executable supporting files require confirmation unless you pass `--yes`; use `--no` to decline without prompting.

## Plugin Install Tracking

SkillsDex does not package or sync Codex plugins yet. It can track plugin install commands centrally so each machine can run the required install command locally.

```bash
./bin/skillsdex plugin add codex-autoresearch \
  --name "Codex Autoresearch" \
  --url https://github.com/TheGreenCedar/codex-autoresearch \
  --target laptop \
  --command "codex plugin install https://github.com/TheGreenCedar/codex-autoresearch"

./bin/skillsdex plugins --target laptop
./bin/skillsdex plugins codex-autoresearch
./bin/skillsdex plugin install codex-autoresearch --dry-run
./bin/skillsdex plugin install codex-autoresearch --yes
```

`plugin install` executes the stored command on the local machine without invoking a shell. It previews the exact command with `--dry-run` and otherwise requires an interactive confirmation or `--yes`. Shell wrapper commands such as `sh`, `bash`, `zsh`, `cmd`, and PowerShell are rejected when tracking a plugin.

Plugin repositories that also contain a normal Codex skill can still be imported as skills by pointing at the nested skill folder. For example, `codex-autoresearch` includes `plugins/codex-autoresearch/skills/codex-autoresearch/SKILL.md`, which can be imported separately from the plugin install tracking record.

Sync managed installs:

```bash
./bin/skillsdex sync
```

Preview sync changes without writing files:

```bash
./bin/skillsdex sync --dry-run
./bin/skillsdex sync --dry-run --strict
./bin/skillsdex sync --dry-run --json
```

Dry-run sync prints file-level added, modified, and removed package paths before any write. When changes are pending, it ends with the apply command to run after review. Add `--strict` when scripts should exit nonzero if any managed install would change. Add `--json` with `--dry-run` when automation needs per-install status, file changes, pending counts, failure counts, and next steps without parsing human output.

If a managed skill is unavailable, archived, or has an invalid package, `sync` reports every affected skill, continues checking the rest, saves any successful updates, and exits nonzero so scripts can catch the problem.

Overwrite prompts are interactive by default. For scripts, use `--yes` to allow overwrites or `--no` to decline them without prompting:

```bash
./bin/skillsdex sync --yes
./bin/skillsdex install admin/five-advisor-review --no
```

Inspect local install status:

```bash
./bin/skillsdex installed
./bin/skillsdex installed --refresh
./bin/skillsdex installed --strict
./bin/skillsdex installed --json
```

Use `installed --refresh` to also dry-run source refresh checks for managed imported skills. Use `installed --strict` in scripts to exit nonzero when there are no managed installs, when local or reachable remote install status is not clean, or when `--refresh` finds an upstream source update. Add `--json` when scripts need install rows, status counts, source-refresh counts, and issue totals without parsing the table.

Check local health and next steps:

```bash
./bin/skillsdex doctor
./bin/skillsdex doctor --strict
./bin/skillsdex doctor --refresh --strict
./bin/skillsdex doctor --fix-permissions
./bin/skillsdex doctor --json
```

`doctor` prints a user-friendly health table plus details for authentication,
server reachability, library contents, managed install status, Codex CLI
availability, Codex target writability, and whether local SkillsDex
auth/manifest files are private. Authenticated runs end with quality, sync, and
backup verification next steps. Add `--refresh` to also dry-run source refresh
checks for managed imported skills. Add `--strict` in scripts to exit nonzero
when any health check needs attention, including a machine with no managed
installs yet or an upstream source update found with `--refresh`. Add `--json`
when CI, LLM agents, or setup scripts need the detailed health report, issue
list, and next steps as structured output. Use `--fix-permissions` to chmod
existing auth and manifest files back to `0600`.

Remove a managed install:

```bash
./bin/skillsdex uninstall admin/five-advisor-review
```

Uninstall removes the tracked package files and keeps the same local-edit protection used by sync.

Stop a local development server on port `8765`:

```bash
task kill
```

## Deployment

Download the latest GitHub-built binaries for the current machine:

```bash
task update-binaries
```

That installs verified release assets to `./bin/skillsdex` and `./bin/skillsdexd`
by default. On a test machine where you want them on your PATH, choose an install
directory:

```bash
INSTALL_DIR="$HOME/.local/bin" task update-binaries
```

Set `RELEASE_TAG=v0.123` to install a specific release instead of the latest.

Install a locally built binary:

```bash
task build
scripts/install.sh ./bin/skillsdex
```

Or choose a custom install path:

```bash
PREFIX="$HOME/.local" scripts/install.sh ./bin/skillsdex
```

Docker files are retained for image build checks and future packaging work, but Docker is not the recommended local or internet-facing run path. Use the local binary for development and the systemd service plus an external Caddy reverse proxy for production.

Run under systemd on a Linux host:

```bash
id -u skillsdex >/dev/null 2>&1 || sudo useradd --system --home-dir /var/lib/skillsdex --shell /usr/sbin/nologin skillsdex
task prod:build
sudo install -m 0755 bin/skillsdex /usr/local/bin/skillsdex
sudo install -m 0755 bin/skillsdexd /usr/local/bin/skillsdexd
sudo install -m 0644 deploy/skillsdex.service /etc/systemd/system/skillsdex.service
sudo systemctl daemon-reload
sudo systemctl enable --now skillsdex
```

The unit binds to `127.0.0.1:8765`, stores data in `/var/lib/skillsdex`, and trusts only loopback proxy headers so Caddy can pass the real client IP and HTTPS state.

If you use Task, the same service path has shortcuts:

```bash
task prod:install-service
task prod:reset-admin-password
task prod:status
task prod:logs
task prod:install-backup-timer
task prod:backup-now
```

For internet-facing production, use the production plan in [PRODUCTION.md](PRODUCTION.md). The recommended default is Caddy in front of the systemd service:

```text
internet -> Caddy :443/:80 -> skillsdex 127.0.0.1:8765
```

Start from `deploy/Caddyfile`, replace the hostname, point DNS at the host, and expose only ports 80 and 443 publicly.

Do not expose a fresh instance on a public bind while the default `admin/admin` password is active. `skillsdexd serve` allows local loopback setup, but refuses public bind addresses until the admin password has been changed.

If the only admin password is lost, stop the service and reset it with local database access:

```bash
skillsdex admin reset-password --db /var/lib/skillsdex/skillsdex.db --username admin --password <new-password>
```

The reset requires an admin user, enforces the normal password length rule, revokes existing web sessions and API tokens for that user, and records an audit event.

Admins can open `Admin > Invites` to generate one-time signup links, and `Admin > Maintenance` to review database/storage size, see the largest users and skills by stored revision bytes, prune expired web sessions, revoked API token metadata, old audit events, and old skill revisions. Revision pruning keeps the newest configured revisions per skill. User deletion is separate and destructive: it removes the account, owned skills, revisions, sessions, tokens, tags, install sets, and invite references.

Build release archives:

```bash
VERSION=0.1.0 task release
```

The release task writes platform archives under `dist/` and a `dist/SHA256SUMS` manifest. Verify downloaded archives with:

```bash
cd dist && shasum -a 256 -c SHA256SUMS
```

GitHub Actions also creates an automatic release for each push to `main`. The workflow tags releases as `v0.<run_number>` and uploads paired `skillsdex-*` CLI and `skillsdexd-*` server binaries for macOS amd64/arm64, Linux amd64/arm64, and Windows amd64, plus matching `.sha256` files.

Generate a Homebrew formula from the same release artifacts:

```bash
VERSION=0.1.0 task release
VERSION=0.1.0 task homebrew-formula
```

The formula is written to `dist/homebrew/Formula/skillsdex.rb` and defaults to GitHub release URLs under `https://github.com/rootisgod/skillsdex/releases/download/v<version>/`. For forks, private taps, or alternate asset hosts, set `RELEASE_BASE_URL`, `RELEASE_TAG`, and `HOMEBREW_HOMEPAGE`.

Signed release artifacts use minisign and require a maintainer signing key:

```bash
MINISIGN_KEY_FILE=./minisign.key VERSION=0.1.0 task release-signed
```

That signs `SHA256SUMS` and each release archive beside the generated files.

## Importing Skills

The web app can import a Codex-compatible `SKILL.md` from:

- A GitHub repository root, such as `https://github.com/openai/skills`.
- A GitHub skill folder, such as `https://github.com/owner/repo/tree/main/skill-name`.
- An agentskill.sh skill URL or slug, such as `https://agentskill.sh/@openclaw/openclaw-debugging` or `@openclaw/openclaw-debugging`.

The `Import` page also includes agentskill.sh search. The `Top Skills` page shows the top 100 agentskill.sh skills with sortable quality, security, and GitHub stars columns.

The CLI can use the same authenticated server-side import path:

```bash
./bin/skillsdex search "playwright"
./bin/skillsdex top --limit 10
./bin/skillsdex import-source https://github.com/owner/repo/tree/main/skill-name
./bin/skillsdex import-source @owner/skill-name
```

Imported skill detail pages include `Refresh Source` when source metadata is available. The browser flow previews added/modified/removed package files with file diffs, then `Apply Source Update` saves a normal revision only when you confirm. Refresh pulls the latest upstream skill package and preserves local pinned/default install flags. The same path is available from the CLI with `skillsdex refresh`, which previews by default, shows added/modified/removed package files, can print diffs with `--diff`, and updates only with `--apply`.
For `refresh --all`, any per-skill refresh failure is printed and the command exits nonzero after checking the remaining skills. Add `--strict` to a preview command when scripts should also fail if any imported skill has an upstream update available. Add `--json` to a preview command when automation needs source-refresh status counts, file changes, failures, and next steps without parsing human output.

SkillsDex export zips can be restored from the same `Import` page. Restore validates every manifest path and package file path, rejects traversal or absolute paths, restores pinned/default/source metadata, and recreates archived skills as archived.

GitHub folder imports and GitHub Export preserve supporting files under the skill directory. GitHub Export is one-way: it writes active and archived skills under `skills/<owner>/<slug>/` for review, backup, or publishing, but does not import Git commits back into SkillsDex.

Skill detail pages show text previews for supporting files, and revision pages diff the full package file-by-file so upstream refreshes and folder changes can be reviewed in the browser.

## Authoring

The web editor keeps frontmatter fields separate from the Markdown body and shows the generated `SKILL.md` before save. It progressively enhances the form with local live `SKILL.md`, rendered-preview, and quality-signal updates, body/description counters, and Markdown textarea tab indentation while keeping create/edit/save usable without JavaScript. It can add, edit, remove, and mark supporting package files executable, so a full skill folder can be authored without leaving the browser. New skills can start from workflow, review, or research templates.

SkillsDex also shows local quality signals for each draft and skill detail page, including weak trigger descriptions, suggested trigger rewrites, missing short descriptions, sparse instructions, very large loaded context, possible duplicates, stale imported sources, TODO placeholders, and possible sensitive terms.

## Default Skills

Fresh installs seed useful starter skills under the `admin` namespace:

- `admin/five-advisor-review`
- `admin/systematic-debugging`
- `admin/code-review`
- `admin/test-strategy`
- `admin/implementation-plan`
- `admin/research-brief`
- `admin/documentation-update`
- `admin/security-review`
- `admin/frontend-polish`
- `admin/product-interface-redesign`
- `admin/pull-request-summary`

These seeded skills are marked as recommended defaults. You can edit a skill in the web app and toggle `Recommended default` to control whether `skillsdex install-defaults` includes it. Toggle `Pinned install` for the personal subset you want `skillsdex install-pinned` to install on a new machine.

## Collections

Collections are named install sets. Use them when pinned/default is too coarse: for example `laptop`, `workstation`, `client-acme`, `frontend`, or `incident-response`.

Create and manage collections in the web app under `Collections`, then install one from any authenticated machine:

```bash
./bin/skillsdex sets
./bin/skillsdex sets laptop
./bin/skillsdex sets laptop --json
./bin/skillsdex install-set laptop
```

## What We Do Next

Current context:

- Phase 1 implementation is in place with `skillsdex` as the CLI and `skillsdexd` as the server.
- The default starter skill library is seeded into the `admin` namespace.
- The web UI has been redesigned for clearer navigation, denser skill browsing, better install guidance, and light/dark themes.
- GitHub and agentskill.sh imports are available in the web app and CLI, including agentskill.sh search and Top 100 browsing.
- Setup guidance, `skillsdex setup`, named install sets, plugin install tracking, CLI public search/import/new/publish/mark, CLI dry-runs, overwrite flags, source refresh, `doctor` health checks, and quality signals are available.
- Quality signals are available in the web app and from `skillsdex lint`, including `--strict` and `--json` for scripts.
- SkillsDex stores full skill packages, including `SKILL.md` plus supporting files, and the web editor, API, CLI install/sync/uninstall path, web previews/diffs, backup restore, GitHub import, and GitHub export preserve those package files.
- Skill package writes are bounded: 16 MiB per request/package, 1 MiB per file, supported UTF-8 text file types only, 1,000 skills per user, 100 revisions per skill, 256 MiB stored skill bytes per user, and a 1 GiB SQLite database guardrail.
- SkillsDex export zips can be restored from the Import page or CLI with path validation and archived-skill preservation.
- One-way GitHub export is available from the web app and CLI as a repo-shaped `skills/<owner>/<slug>/` tree.
- The seeded `admin/admin` login is forced through a password-change setup screen before normal web routes are available, and public binds are refused while that default password remains active.
- Admins can create one-time 24-hour signup invites from the web UI or `skillsdex invites create`; invited users choose their username and can store email as metadata.
- API tokens and web session tokens are stored as hashes. API tokens are shown only once. Authenticated web forms use per-session CSRF tokens, including backup uploads, and session cookies are marked `Secure` when served over HTTPS directly or through a configured trusted proxy.
- Admins can review `Admin > Audit` for recent login, signup invite, password, token, skill, import/export, backup export/restore, install-set, retention, and local recovery changes.
- Admins can prune expired sessions, revoked API token metadata, old audit events, and old revisions from `Admin > Maintenance`.
- Production SQLite runs in WAL mode, and the host backup timer creates compressed SQLite backup zips for full-service recovery.
- systemd, install-script, Docker image build files, checksummed release archives, optional minisign signatures, generated Homebrew formula packaging, and legacy database migration smoke coverage are available for more repeatable local or small-host deployments.
- The CLI install/sync loop remains the core product path and should stay the main polish target.

Remaining high-level work:

1. Dogfood the live service and plan the next web UI overhaul from real use.
2. Keep expanding the clean-home e2e smoke as new CLI and web flows are added.
3. Add a disposable real-machine CLI e2e suite for the full install/sync/uninstall loop against a pre-authenticated Codex install.
4. Exercise full-folder authoring against a real Codex install with references, scripts, assets, and templates.
5. Exercise signed releases and Homebrew packaging in a real GitHub release/tap workflow.
6. Continue hosted hardening with broader audit coverage when future team features become concrete.
7. Revisit phase 2 items such as teams and additional agent targets after the single-user install/sync loop has had real use.

## Checks

Testing standards live in [`TESTING.md`](TESTING.md). Use the full gate before merging:

```bash
task verify
```

For a faster local loop while developing a slice:

```bash
go test ./...
task build
task e2e-first-run
```

`task e2e-first-run` starts SkillsDex on `127.0.0.1:18765` with temporary `HOME`, `SKILLSDEX_HOME`, cookies, and SQLite state. It exercises the default login, forced password change, setup page, one-time web token generation, CLI login, `doctor`, `doctor --fix-permissions`, `doctor --strict`, `skills`, `templates`, `mark`, named set setup preview, install dry-runs, `install-defaults`, `installed`, file-level `sync --dry-run` previews, modified-file protection with `--no`/`--yes`, and CLI backup export/inspect/restore dry-runs/restore.

The disposable real-machine e2e runner requires an explicit test-machine marker
and runs only under a temporary home on the agent VM. It exercises `login`,
`whoami`, `doctor --strict`, `setup --apply --yes`, plugin install dry-runs and
execution, installed-skill invocation through real agent CLIs, sync, and
uninstall. Codex is the first execution target, Pi is the next target because it
supports `--skill <path>` and works with OpenRouter, Claude Code is checked for
provider routing with `--bare`, and OpenCode is checked through its local DB
because headless output can omit assistant text.

The agent VM harness lives in `scripts/e2e_agent_vm.sh` and can be run from the
workstation with `task vm:e2e-agent` after creating `~/.env.openrouter` on the
VM. It creates a fresh loopback SkillsDex server and SQLite DB on the VM, seeds
deterministic skills and a harmless plugin record, installs pinned skills into a
temporary Codex home, verifies Codex and Pi can invoke the installed smoke skill
through OpenRouter, checks Claude/OpenCode provider routes, and exercises sync
plus uninstall.

`task coverage-critical` checks the critical behavior surfaces named in `TESTING.md` instead of relying on a single flat coverage percentage.

Equivalent core Go commands:

```bash
go test ./...
go vet ./...
go build -o bin/skillsdex ./cmd/skillsdex
go build -o bin/skillsdexd ./cmd/skillsdexd
```
