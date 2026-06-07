# SkillsDex

Private dotfiles for AI-agent skills.

SkillsDex is a private hosted skill library plus CLI for syncing reusable Codex skills across machines. Phase 1 is a single Go binary named `skillsdex` with a server-rendered web app, JSON API, SQLite storage, and Codex install/sync support.

## Quickstart

Build the binary:

```bash
task build
```

Start the web app and API:

```bash
./skillsdex serve
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

In the web app:

1. Log in as `admin`.
2. Open `Skills`.
3. Create a skill, import one from GitHub, or open `Top Skills` to import from agentskill.sh.
4. Open `API Tokens`.
5. Generate a token.

Authenticate the CLI:

```bash
./skillsdex login --server http://127.0.0.1:8765 --token <token>
```

List available skills:

```bash
./skillsdex skills
```

Install all recommended default skills into Codex:

```bash
./skillsdex install-defaults --target codex
```

Or install a single skill:

```bash
./skillsdex install admin/five-advisor-review --target codex
```

The Codex skill is written to:

```text
~/.codex/skills/admin--five-advisor-review/SKILL.md
```

Sync managed installs:

```bash
./skillsdex sync
```

Inspect local install status:

```bash
./skillsdex installed
```

Stop a local development server on port `8765`:

```bash
task kill
```

## Importing Skills

The web app can import a Codex-compatible `SKILL.md` from:

- A GitHub repository root, such as `https://github.com/openai/skills`.
- A GitHub skill folder, such as `https://github.com/owner/repo/tree/main/skill-name`.
- An agentskill.sh skill URL or slug, such as `https://agentskill.sh/@openclaw/openclaw-debugging` or `@openclaw/openclaw-debugging`.

The `Import` page also includes agentskill.sh search. The `Top Skills` page shows the top 100 agentskill.sh skills with sortable quality, security, and GitHub stars columns.

V0 still stores and installs only `SKILL.md`; additional files returned by external registries are ignored until full skill-folder support is added.

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

These seeded skills are marked as recommended defaults. You can edit a skill in the web app and toggle `Recommended default` to control whether `skillsdex install-defaults` includes it.

## What We Do Next

Current context:

- Phase 1 implementation is in place as one `skillsdex` binary.
- The default starter skill library is seeded into the `admin` namespace.
- The web UI has been redesigned for clearer navigation, denser skill browsing, better install guidance, and light/dark themes.
- GitHub and agentskill.sh imports are available in the web app, including agentskill.sh search and Top 100 browsing.
- The CLI install/sync loop is the core product path and should stay the main polish target.

Near-term plan:

1. Test the complete flow from a clean home directory: `serve`, web login, token generation, CLI login, `skills`, `install`, `sync`, `installed`, and modified-file protection.
2. Add a guided setup page or checklist in the web app so new users know how to generate a token and install their first skill.
3. Make the theme toggle available before login, or add an equivalent compact control on the login page.
4. Only after the install/sync loop is polished, revisit phase 2 items such as teams, GitHub export, and additional agent targets.

## Checks

```bash
task test
task build
```

Equivalent Go commands:

```bash
go test ./...
go build -o skillsdex .
```
