# SkillsDex Product and Technical Spec

## Summary

SkillsDex is a private hosted skill library plus CLI for syncing reusable AI-agent skills across machines. The first version targets Codex only and installs selected Markdown-based skills into the user's global Codex skills directory.

Positioning:

> Private dotfiles for AI-agent skills.

The product should not start as a public prompt marketplace, project-context manager, or multi-agent abstraction layer. The first useful version should solve one concrete problem: let a user maintain private Codex skills in one hosted place and install/sync chosen skills onto any machine.

## Goals

- Provide a private personal skill library.
- Let users create and edit skills in a hosted web app.
- Let users install selected skills locally with a Go CLI.
- Support Codex as the first target.
- Track installed files so updates are safe and overwrite behavior is predictable.
- Preserve local edits by detecting file hash changes before replacing files.
- Keep identifiers stable enough for future user/team ownership.
- Model full skill folders from the start, even though v0 only accepts and installs `SKILL.md`.
- Leave room for teams, GitHub export, and other agent targets later.

## Non-Goals for V0

- Public skill marketplace.
- Community-editable skills.
- Project-specific repo context management.
- `AGENTS.md` or `CLAUDE.md` generation.
- Bidirectional GitHub sync.
- Local CLI editing of remote skills.
- Multi-agent neutral rendering abstraction.
- Skill linting, policy checks, or secret scanning.
- Executing scripts or hooks.
- Installing supporting files beyond `SKILL.md`.
- Trust/sandbox policy for executable skill package contents.

These may become later features, but adding them now would obscure whether the core install/sync loop is valuable. The storage model should still be shaped for full skill packages so v0 does not need a disruptive migration later.

## Core Concepts

### Skill

A skill is the smallest managed unit.

Conceptually, a skill is a versioned folder. For v0, the only accepted file in that folder is a Codex-compatible `SKILL.md` document with YAML frontmatter and Markdown body.

Example:

```markdown
---
name: debugging
description: Use when investigating failing tests, runtime errors, flaky behavior, or regressions.
metadata:
  short-description: Debug failures systematically
---

# Debugging

Instructions go here.
```

The v0 canonical format should be Codex-compatible rather than "neutral". A neutral abstraction can be introduced later after real differences between targets are better understood.

Future skill folders may contain supporting files:

```text
skill-name/
  SKILL.md
  references/
  scripts/
  hooks/
  assets/
  agents/
```

V0 should plan for this shape in persistence but reject or ignore files other than `SKILL.md` at the API and CLI boundary.

### Owner

Every skill belongs to a user or, later, a team.

Canonical public identifier:

```text
owner/slug
```

Examples:

```text
iain/debugging
iain/fastmail-triage
acme/incident-response
```

The slug is immutable after creation. Display names can change.

The owner slug is a namespace label, not an authorization boundary. The server must authorize access using the authenticated user and owner membership records, not by trusting the `{owner}` segment in a URL.

## Multi-Tenancy

SkillsDex is a multi-tenant hosted app from v0.

Each user has an account and owns a private namespace. Authenticated users can list, read, create, update, archive, and install only skills they own, unless explicit team-sharing support is added later.

V0 has user-owned skills only. Team-owned skills should use the same owner model later, with team membership determining access.

All API queries must be scoped by authenticated ownership. For example, `GET /v1/skills/iain/debugging` should resolve the `iain` namespace and then verify that the authenticated user owns or can access that namespace before returning anything.

Skills are private by default. There is no public browsing, global marketplace, or unauthenticated raw skill access in v0.

### Revision

Every saved skill change creates a new revision.

The CLI installs a specific latest revision and records that revision locally. `sync` updates managed installs to newer remote revisions.

### Install

An install is a local copy of one remote skill for one target.

V0 target:

```text
codex
```

Codex global skills appear to live under:

```text
~/.codex/skills/
```

System skills use:

```text
~/.codex/skills/.system/<skill>/SKILL.md
```

SkillsDex-managed user skills should avoid `.system` and install as flat directories:

```text
~/.codex/skills/<owner>--<slug>/SKILL.md
```

Example:

```text
~/.codex/skills/iain--debugging/SKILL.md
```

The flat directory form avoids depending on nested skill discovery.

## Product Flow

### Web Flow

1. User logs in.
2. User sees a private skill browser listing their skills.
3. User creates a skill.
4. User edits frontmatter fields and Markdown body.
5. User previews the rendered Markdown.
6. User saves a revision.
7. User can inspect revision history.
8. User can view install command.

Phase 1 web pages:

- Login/logout
- Admin user list
- Admin user create/edit/reset password
- Private skill list
- Skill detail
- New skill
- Edit skill
- Revision history
- Revision diff
- Raw `SKILL.md` view

Minimum editor fields:

- Owner
- Slug
- Name
- Description
- Short description
- Markdown body
- Preview

The editor should be a real Markdown editor, not a complex block editor. It should be good enough to comfortably browse and update skills without using GitHub or the CLI as the editing surface.

Phase 1 should use separate form fields and generate `SKILL.md`, rather than requiring users to edit raw frontmatter manually. The generated preview should show the complete `SKILL.md`.

Use CodeMirror for the Markdown body if it remains straightforward. Bundle/vendor CodeMirror with the app and serve it locally, preferably embedded into the Go binary. Do not depend on CDN-hosted assets for the editor. If CodeMirror setup starts driving the implementation, fall back to a normal `<textarea>` and keep the form contract the same.

Core create/edit/save flows should work without JavaScript. CodeMirror should progressively enhance the textarea rather than replacing normal form submission.

### CLI Flow

1. User runs `skillsdex login`.
2. CLI stores auth locally.
3. User runs `skillsdex skills`.
4. CLI lists private skills available to the authenticated user.
5. User runs `skillsdex install iain/debugging --target codex`.
6. CLI downloads the skill and writes `~/.codex/skills/iain--debugging/SKILL.md`.
7. CLI records the install in its local manifest.
8. User runs `skillsdex sync` on another day/machine.
9. CLI updates installed managed skills if remote revisions changed.

## CLI Spec

Name:

```bash
skillsdex
```

Commands:

```bash
skillsdexd serve
skillsdex login
skillsdex logout
skillsdex whoami
skillsdex skills
skillsdex show <owner>/<slug>
skillsdex install <owner>/<slug> --target codex
skillsdex install-defaults --target codex
skillsdex plugins [plugin-slug] [--target laptop]
skillsdex plugin add <slug> --name <name> --url <url> --command <command> [--target laptop]
skillsdex plugin install <slug> [--dry-run] [--yes|--no]
skillsdex invites [--json]
skillsdex invites create [--json]
skillsdex invites revoke <invite-id>
skillsdex installed
skillsdex sync
skillsdex uninstall <owner>/<slug> --target codex
skillsdex doctor
```

The user CLI binary is `skillsdex`. The server binary is `skillsdexd`; `skillsdexd serve` runs the local web app and API. The CLI provides install/sync commands.

Target handling:

- `--target` should be supported from the start.
- The default target should be `codex`.
- If `--target` is omitted, use the configured default target.
- `skillsdex config set default_target codex` can be added early or deferred.

Recommended v0 command examples:

```bash
skillsdex login
skillsdex skills
skillsdex install-defaults --target codex
skillsdex install iain/debugging --target codex
skillsdex sync
```

### `login`

Authenticate with the SkillsDex server by storing an API token generated in the web app.

V0 should use API tokens for CLI auth. Tokens can be generated and viewed later in the web app. This is less secure than show-once tokens, but it is acceptable for the local-first phase 1 and avoids annoying regeneration during testing.

Store local CLI auth outside the manifest, for example in `~/.skillsdex/auth.json`.

### `serve`

Run the web app and API locally.

Default bind address:

```text
127.0.0.1:8765
```

This intentionally binds to localhost by default. Public bind addresses must not be allowed while the default admin password is still active.

On startup, `skillsdexd serve` should:

- create the SQLite database if missing
- run migrations automatically
- create the default first user if no users exist
- print the local web URL
- refuse public bind addresses while the default admin password is active

Default first user:

```text
username: admin
password: admin
```

This is acceptable for local/testing phase 1. Production hardening can come later.

### `skills`

List skills visible to the authenticated user.

Fields:

- Identifier
- Name
- Latest revision
- Updated at

### `show`

Display metadata and optionally raw Markdown.

Possible flags:

```bash
skillsdex show iain/debugging
skillsdex show iain/debugging --raw
```

### `install`

Install a skill into a target-specific destination.

Default for Codex:

```text
~/.codex/skills/<owner>--<slug>/SKILL.md
```

Behavior:

- Create destination directory if missing.
- Refuse unknown targets.
- Refuse to overwrite unmanaged files unless user confirms.
- If destination is managed and unchanged since last install, replace it.
- If destination is managed but locally modified, show a warning and require confirmation.
- Record install metadata in local manifest.

### `installed`

List local installs from the manifest.

Fields:

- Identifier
- Target
- Path
- Installed revision
- Local status

Statuses:

- `clean`
- `modified`
- `missing`
- `remote-deleted`
- `update-available`

### `sync`

Update all managed installed skills.

Behavior:

- Fetch latest metadata for all installed skills.
- For each install, compare local file hash with recorded installed hash.
- If local file is unchanged and remote revision is newer, update it.
- If local file is modified, do not overwrite without confirmation.
- If remote skill was archived or deleted, warn but do not remove the local install.
- Sync should update all managed installs, not just Codex.

### `uninstall`

Remove a managed local install.

Behavior:

- If local file is unchanged, delete the generated skill directory.
- If local file was modified, require confirmation.
- Remove install entry from manifest after successful deletion.

### `doctor`

Report environment assumptions.

For Codex:

- Detected Codex config directory.
- Detected skills directory.
- Whether skills directory is writable.
- Default target.
- Manifest path.
- Auth status.

## Local Files

SkillsDex should use its own application directory:

```text
~/.skillsdex/
  config.toml
  auth.json
  manifest.json
  cache/
    skills/
      <owner>/
        <slug>/
          SKILL.md
```

On Windows, use the appropriate user config/data directory instead of hardcoding Unix paths. The Go implementation should use OS-aware path helpers.

### Manifest

Example:

```json
{
  "version": 1,
  "installs": [
    {
      "owner": "iain",
      "slug": "debugging",
      "target": "codex",
      "path": "/home/iain/.codex/skills/iain--debugging/SKILL.md",
      "remote_revision": "rev_123",
      "installed_hash": "sha256:abc123",
      "installed_at": "2026-05-30T12:00:00Z",
      "managed": true
    }
  ]
}
```

The manifest is the source of truth for whether SkillsDex owns a local file.

## Overwrite Rules

The CLI must distinguish between unmanaged, managed clean, and managed modified files.

### Unmanaged Destination Exists

If the destination path exists but is not in the manifest:

- Do not overwrite automatically.
- Warn clearly.
- Ask for confirmation.

### Managed and Clean

If the destination path exists, is in the manifest, and its current hash matches `installed_hash`:

- It can be overwritten during install/sync.

### Managed and Modified

If the destination path exists, is in the manifest, but its current hash differs from `installed_hash`:

- Warn that the file was edited locally.
- Do not overwrite without confirmation.
- Later versions can show a diff.

### Remote Deleted or Archived

If a skill no longer exists remotely:

- Do not delete the local install automatically.
- Mark status as `remote-deleted` or `remote-archived`.
- Let the user uninstall explicitly.

## Hosted API Spec

V0 endpoints can be simple JSON over HTTPS.

Every endpoint must be authenticated except future login/OAuth endpoints. Skill reads and writes must be tenant-scoped by the authenticated user. The `{owner}` path segment is for stable identifiers and routing; it must not be treated as proof of access.

```http
GET /v1/me
GET /v1/skills
GET /v1/skills/{owner}/{slug}
GET /v1/skills/{owner}/{slug}/raw
POST /v1/skills
PUT /v1/skills/{owner}/{slug}
POST /v1/skills/{owner}/{slug}/archive
```

Optional revision endpoints:

```http
GET /v1/skills/{owner}/{slug}/revisions
GET /v1/skills/{owner}/{slug}/revisions/{revision_id}
```

The CLI needs only:

```http
GET /v1/me
GET /v1/skills
GET /v1/skills/{owner}/{slug}
GET /v1/skills/{owner}/{slug}/raw
```

## Data Model

Use SQLite for the first hosted implementation. SQLite is enough for a private single-service product, keeps deployment simple, and still gives transactions, constraints, and revision history.

The database should model immutable skill revisions as collections of files from the start. V0 only permits one file, `SKILL.md`, but the schema should not assume that a revision is a single Markdown column.

The data model must preserve tenant boundaries. A user can only access skills through direct ownership in v0. Later, teams can add membership and role checks without changing public identifiers.

### Storage Architecture

V0 should use one SQLite database for all users.

SQLite should be the source of truth for users, ownership, skills, revisions, revision files, and metadata. The filesystem should not be the primary source of truth for hosted skills.

For v0, store `SKILL.md` content directly in SQLite in `skill_revision_files.content`. Use `BLOB` so the same schema can later store non-Markdown assets without a structural migration.

Do not create server-side version folders such as:

```text
data/iain/debugging/rev-001/SKILL.md
data/iain/debugging/rev-002/SKILL.md
```

as the canonical storage model. That shape is useful as an export format, backup artifact, or local generated view, but it should not define the hosted source of truth.

A single SQLite database is expected to be sufficient for the early multi-tenant product because writes are low-volume: skill edits, revision inserts, auth/session changes, and sync metadata reads. Revisit this only when there are concrete pressure signals:

- many concurrent writes
- large binary assets bloating the database
- backups becoming slow or operationally awkward
- multiple app servers needing concurrent writes
- tenant isolation or compliance requirements that SQLite cannot satisfy

Future scaling path:

```text
SQLite:
  users, ownership, skills, revisions, file metadata

Content-addressed blob storage:
  file bytes by sha256 hash
```

The first external blob store can be local filesystem, S3, R2, or similar. Even then, SQLite should remain the authority and object storage should only hold file bytes.

### User

- `id`
- `username`
- `password_hash`
- `is_admin`
- `deactivated_at`
- `email`
- `created_at`

For phase 1, usernames and owner slugs are immutable. Display names and email can change later, but changing namespace identifiers requires a deliberate redirect/alias design and is out of scope.

Account creation is controlled by admins and invites. On first startup, if no users exist, create `admin` / `admin`. Admin users can create users directly and set/reset passwords from the web UI. Admins can also create one-time signup invites that expire after 24 hours. An invite authorizes signup by possession of the URL; the new user chooses their username and may add email as account metadata. Normal self-service password change can wait.

Do not hard-delete users in phase 1. Deactivate users instead.

### SignupInvite

- `id`
- `token_hash`
- `token_preview`
- `created_by`
- `created_at`
- `expires_at`
- `used_at`
- `used_by`
- `revoked_at`

Signup invite tokens are shown only once as part of the signup URL. The database stores only a token hash and a short preview for identification. An invite can be used once, revoked by an admin, or expire after 24 hours.

### APIToken

- `id`
- `user_id`
- `name`
- `token`
- `created_at`
- `last_used_at`
- `revoked_at`

For phase 1, API tokens may be stored retrievably so they can be viewed again in the web app. This is a deliberate local-testing tradeoff. Later, switch to storing only token hashes and showing tokens once.

### Skill

- `id`
- `owner_type` (`user` now, `team` later)
- `owner_id`
- `owner_slug`
- `slug`
- `name`
- `description`
- `short_description`
- `current_revision_id`
- `archived_at`
- `created_at`
- `updated_at`

Unique constraint:

```text
(owner_slug, slug)
```

### SkillRevision

- `id`
- `skill_id`
- `revision_number`
- `frontmatter_json`
- `manifest_json`
- `tree_hash`
- `created_at`
- `created_by`

Revisions are immutable. Editing a skill creates a new revision and updates `skills.current_revision_id`.

`frontmatter_json` stores parsed metadata from `SKILL.md` for listing and editing. `manifest_json` stores package-level metadata such as file count, total size, supported targets, and future trust flags. `tree_hash` is a hash of all files in the revision.

### SkillRevisionFile

- `id`
- `revision_id`
- `path`
- `content`
- `content_type`
- `content_hash`
- `size_bytes`
- `executable`
- `created_at`

Unique constraint:

```text
(revision_id, path)
```

For v0, every revision should have exactly one file:

```text
SKILL.md
```

The `content` column can be SQLite `TEXT` for v0, but `BLOB` is safer if the same table will later store assets. Either is acceptable as long as the application treats `SKILL.md` as UTF-8 Markdown.

Do not update revision file rows in place. File changes create a new `SkillRevision` and new `SkillRevisionFile` rows.

Suggested SQLite shape:

```sql
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0,
  deactivated_at TEXT,
  email TEXT UNIQUE,
  created_at TEXT NOT NULL
);

CREATE TABLE api_tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,
  token TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at TEXT
);

CREATE TABLE signup_invites (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  token_preview TEXT NOT NULL DEFAULT '',
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at TEXT,
  used_by TEXT REFERENCES users(id),
  revoked_at TEXT
);

CREATE TABLE skills (
  id TEXT PRIMARY KEY,
  owner_type TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  owner_slug TEXT NOT NULL,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  short_description TEXT,
  current_revision_id TEXT,
  archived_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(owner_slug, slug)
);

CREATE TABLE skill_revisions (
  id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL REFERENCES skills(id),
  revision_number INTEGER NOT NULL,
  frontmatter_json TEXT NOT NULL,
  manifest_json TEXT NOT NULL,
  tree_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL REFERENCES users(id),
  UNIQUE(skill_id, revision_number)
);

CREATE TABLE skill_revision_files (
  id TEXT PRIMARY KEY,
  revision_id TEXT NOT NULL REFERENCES skill_revisions(id),
  path TEXT NOT NULL,
  content BLOB NOT NULL,
  content_type TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  executable INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  UNIQUE(revision_id, path)
);
```

## GitHub Sync

GitHub should not be the main interface. It should be an export/backup path.

V0 can defer GitHub entirely.

First GitHub feature should be one-way export:

```text
hosted database -> GitHub repository
```

Possible future command:

```bash
skillsdex github export
```

Avoid bidirectional GitHub sync until the core product is proven. Bidirectional sync introduces conflict resolution, branch policy, deleted-file semantics, and source-of-truth ambiguity.

Future export shape:

```text
skills/
  iain/
    debugging/
      SKILL.md
    fastmail-triage/
      SKILL.md
skillsdex.json
README.md
```

## Future Full Skill Folder Support

The data model should support full skill folders from the start. V0 product behavior should still support only `SKILL.md`.

Later, the API and CLI may allow revisions containing:

```text
skill-name/
  SKILL.md
  references/
  scripts/
  hooks/
  assets/
  agents/
```

V0 behavior:

- Accept only `SKILL.md` when creating or updating a skill.
- Install only `SKILL.md` into the Codex target directory.
- Do not execute scripts or hooks.
- Do not implement script/hook trust checks yet.
- Preserve schema fields such as `executable`, `manifest_json`, and `tree_hash` for future package support.

Before enabling full package install or execution, decide:

- Whether executable scripts are allowed.
- Whether scripts require explicit trust prompts.
- Whether hooks are a first-class concept or just ordinary files.
- Whether assets count toward storage limits.
- How to render or install folders for non-Codex targets.
- How to show diffs for multi-file local modifications.
- How to block path traversal and absolute paths.
- How to display changes to executable files during sync.

## Future Multi-Target Support

Potential targets:

- Codex
- Claude Code
- Hermes
- Cursor
- Windsurf
- OpenCode
- Plain Markdown

Do not design a universal renderer too early. Start with Codex-compatible skills and add target-specific adapters when needed.

When a skill cannot cleanly map to a target, flag it to the user rather than silently degrading it.

Potential later schema:

```yaml
canonical_type: codex_skill
targets:
  codex:
    supported: true
  claude:
    supported: false
    reason: "No target-specific renderer yet"
```

## Security and Privacy

Private skills may contain sensitive operational details. Even v0 should treat privacy seriously.

Minimum requirements:

- HTTPS only.
- Private-by-default skills.
- Auth required for all skill API access.
- Do not create public raw URLs unless explicitly designed later.
- Avoid logging full skill bodies in server logs.
- Do not overwrite local files silently.
- Store local auth tokens outside the manifest.
- For v0, reject package files other than `SKILL.md`.

Later requirements:

- Secret scanning.
- Script and hook trust prompts.
- Path traversal and absolute-path validation for full packages.
- File size and package size limits.
- Team audit logs.
- Role-based access control.
- SSO for teams.
- Encryption-at-rest review.

## Suggested Implementation Stack

### CLI

- Go.
- Cross-platform path handling.
- JSON or TOML local config.
- Clear terminal prompts.
- Non-interactive mode later via `--yes` and `--no`.

### Web/API

Use a Go server-rendered web app and API.

Phase 1 should use:

- separate `skillsdex` CLI and `skillsdexd` server binaries
- SQLite
- email/password login for the web app
- session cookies for web auth
- API tokens for CLI auth
- custom CSS
- bundled local CodeMirror assets if straightforward
- no React/Next frontend
- no Docker requirement

The initial deployment target is local execution via `skillsdexd serve`. Docker should remain easy later, but should not be required for phase 1 testing.

The CLI and web app are both part of phase 1: the CLI proves install/sync, and the web app provides the primary way to browse, create, and update skills.

## V0 Acceptance Criteria

The first useful release is complete when:

1. `skillsdexd serve` starts the web app/API on `127.0.0.1:8765`.
2. First startup creates/migrates the SQLite database.
3. First startup creates `admin` / `admin` if no users exist.
4. A user can log into the web app.
5. An admin can create users and set/reset passwords in the web app.
6. An admin can create and revoke one-time signup invites from the web UI and CLI.
7. An invited user can choose a username, optionally add email metadata, and create a non-admin account before the invite expires.
8. The web app shows only the current user's private skill library.
9. A user can create a private skill in the web app.
10. The skill has a stable identifier like `iain/debugging`.
11. Usernames and owner slugs are immutable.
12. A user can edit frontmatter fields and Markdown body in the web app.
13. The app generates complete `SKILL.md` output from separate fields.
14. A user can preview the rendered skill Markdown before saving.
15. Saving creates an immutable new revision.
16. A user can inspect revision history in the web app.
17. A user can view a revision diff in the web app.
18. A user can view the raw `SKILL.md` for a skill.
19. A user can generate and later view an API token in the web app.
20. A user can authenticate the CLI with that API token.
21. `skillsdex skills` lists the user's skills.
22. `skillsdex install iain/debugging --target codex` writes a Codex skill to `~/.codex/skills/iain--debugging/SKILL.md`.
23. `skillsdex installed` shows the local install.
24. Editing the skill in the web app and running `skillsdex sync` updates the local install.
25. If the local file was manually edited, `sync` detects the hash mismatch and does not overwrite without confirmation.
26. If an unmanaged target file already exists, interactive CLI prompts before overwriting.
27. If an unmanaged target file already exists in non-interactive mode, CLI fails unless an explicit future force flag is used.
28. If the remote skill is archived, `sync` warns but leaves the local installed file in place.

## Phase 1 Decisions

- Product name: SkillsDex.
- `skillsdex` is the CLI binary and `skillsdexd` is the server binary.
- `skillsdexd serve` runs the web app and API.
- Default server bind address: `127.0.0.1:8765`.
- Local binary execution is the phase 1 deployment target.
- Docker should be easy later, but is not part of phase 1.
- Web app uses email/password login and session cookies.
- CLI uses API tokens generated in the web app.
- API tokens are viewable later in phase 1.
- First user is `admin` / `admin`, created automatically if no users exist.
- No forced password change for the default admin account in phase 1.
- Admin web UI can create users and set/reset passwords.
- Admin web UI and CLI can create one-time 24-hour signup invites.
- Invite signup lets users choose their username and optionally store email metadata.
- Normal self-service password changes can wait.
- Usernames and owner slugs are immutable in phase 1.
- Skills are edited as separate fields that generate `SKILL.md`.
- Revision diffs are included in phase 1.
- CodeMirror should be bundled locally if straightforward.
- Core form flows must work without JavaScript.
- Use custom CSS.
- Archived skills are hidden by default with a show-archived toggle.
- Do not hard-delete users or skills in phase 1; deactivate/archive instead.
- CLI installs one skill at a time and can install skills marked as recommended defaults in one command.
- CLI tracks desired plugin install commands and can run them locally with confirmation, without invoking a shell.
- Interactive CLI prompts before overwriting unmanaged files.
- Non-interactive CLI fails on unmanaged overwrite unless a future force flag is used.

## Remaining Open Questions

- Should the CLI cache remote skills even when not installed?
- Should install support `--yes` in v0 for scripting?
- How should Windows Codex skill path discovery work if Codex uses a different config directory?

## Recommended Next Step

Build the first implementation in this order:

1. Create the CLI and server binary skeletons.
2. Implement `skillsdexd serve`.
3. Create the SQLite schema and automatic migrations.
4. Create the default `admin` / `admin` user if no users exist.
5. Add web auth with sessions.
6. Build admin user management.
7. Build the private web skill browser and Markdown editor.
8. Add generated `SKILL.md` preview, revision history, and revision diffs.
9. Add API token management.
10. Add API endpoints backed by the same service and database.
11. Create CLI config/auth, `doctor`, path discovery, and manifest read/write.
12. Wire CLI auth and real API calls.
13. Implement Codex install/sync against the hosted API.
14. Dogfood with 3-5 personal Codex skills across two machines.

This builds the local hosted workflow first because the web app is the primary skill editing surface. The CLI should then prove that those web-managed skills can be installed and synced into Codex.
