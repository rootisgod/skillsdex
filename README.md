# SkillsDex

Private dotfiles for AI-agent skills.

SkillsDex is a private hosted skill library plus CLI for syncing reusable Codex skills across machines. Phase 1 is a single Go binary named `skillsdex` with a server-rendered web app, JSON API, SQLite storage, and Codex install/sync support.

## Quickstart

Build the binary:

```bash
go build -o skillsdex .
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
2. Open `API Tokens`.
3. Generate a token.

Authenticate the CLI:

```bash
./skillsdex login --server http://127.0.0.1:8765 --token <token>
```

List available skills:

```bash
./skillsdex skills
```

Install the default Five Advisor Review skill into Codex:

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

## What We Do Next

The next project work should stay focused on making the phase 1 loop obvious and reliable:

1. Commit the seeded default skills and this quickstart.
2. Test the complete flow from a clean home directory: `serve`, web login, token generation, CLI login, `skills`, `install`, `sync`, `installed`, and modified-file protection.
3. Add a guided setup page or checklist in the web app so new users know how to generate a token and install their first skill.
4. Add a first-class “install recommended defaults” flow after CLI auth.
5. Only after the install/sync loop is polished, revisit phase 2 items such as teams, GitHub export, and additional agent targets.

## Checks

```bash
go test ./...
go build -o skillsdex .
```
