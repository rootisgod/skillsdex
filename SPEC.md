# Skilldex Product and Technical Spec

## Summary

Skilldex is a private hosted skill library plus CLI for syncing reusable AI-agent skills across machines. The first version targets Codex only and installs selected Markdown-based skills into the user's global Codex skills directory.

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
- Leave room for full skill folders, teams, GitHub export, and other agent targets later.

## Non-Goals for V0

- Public skill marketplace.
- Community-editable skills.
- Project-specific repo context management.
- `AGENTS.md` or `CLAUDE.md` generation.
- Bidirectional GitHub sync.
- Local CLI editing of remote skills.
- Multi-agent neutral rendering abstraction.
- Skill linting, policy checks, or secret scanning.
- Installing executable scripts or supporting files.

These may become later features, but adding them now would obscure whether the core install/sync loop is valuable.

## Core Concepts

### Skill

A skill is the smallest managed unit.

For v0, a skill is a Codex-compatible `SKILL.md` document with YAML frontmatter and Markdown body.

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

Skilldex-managed user skills should avoid `.system` and install as flat directories:

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
2. User creates a skill.
3. User edits frontmatter fields and Markdown body.
4. User saves a revision.
5. User can view install command.

Minimum editor fields:

- Owner
- Slug
- Name
- Description
- Short description
- Markdown body
- Preview

The editor should be a real Markdown editor, not a complex block editor.

### CLI Flow

1. User runs `skilldex login`.
2. CLI stores auth locally.
3. User runs `skilldex skills`.
4. CLI lists private skills available to the authenticated user.
5. User runs `skilldex install iain/debugging --target codex`.
6. CLI downloads the skill and writes `~/.codex/skills/iain--debugging/SKILL.md`.
7. CLI records the install in its local manifest.
8. User runs `skilldex sync` on another day/machine.
9. CLI updates installed managed skills if remote revisions changed.

## CLI Spec

Name:

```bash
skilldex
```

Commands:

```bash
skilldex login
skilldex logout
skilldex whoami
skilldex skills
skilldex show <owner>/<slug>
skilldex install <owner>/<slug> --target codex
skilldex installed
skilldex sync
skilldex uninstall <owner>/<slug> --target codex
skilldex doctor
```

Target handling:

- `--target` should be supported from the start.
- The default target should be `codex`.
- If `--target` is omitted, use the configured default target.
- `skilldex config set default_target codex` can be added early or deferred.

Recommended v0 command examples:

```bash
skilldex login
skilldex skills
skilldex install iain/debugging --target codex
skilldex sync
```

### `login`

Authenticate with the hosted service.

Implementation options:

- Device code flow if available.
- Browser-based OAuth callback if simple enough.
- Temporary personal access token for earliest prototype.

V0 can use token auth to keep implementation small.

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
skilldex show iain/debugging
skilldex show iain/debugging --raw
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

Skilldex should use its own application directory:

```text
~/.skilldex/
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

The manifest is the source of truth for whether Skilldex owns a local file.

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

### User

- `id`
- `username`
- `email`
- `created_at`

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
- `body_markdown`
- `frontmatter_json`
- `content_hash`
- `created_at`
- `created_by`

For v0, `body_markdown` can contain the full `SKILL.md` contents. Storing parsed frontmatter separately is useful for listing and editing fields.

## GitHub Sync

GitHub should not be the main interface. It should be an export/backup path.

V0 can defer GitHub entirely.

First GitHub feature should be one-way export:

```text
hosted database -> GitHub repository
```

Possible future command:

```bash
skilldex github export
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
skilldex.json
README.md
```

## Future Full Skill Folder Support

V0 supports only `SKILL.md`.

Later, skills may include:

```text
skill-name/
  SKILL.md
  references/
  scripts/
  assets/
  agents/
```

Before supporting this, decide:

- Whether executable scripts are allowed.
- Whether scripts require explicit trust prompts.
- Whether assets count toward storage limits.
- How to render or install folders for non-Codex targets.
- How to show diffs for multi-file local modifications.

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

Later requirements:

- Secret scanning.
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

Reasonable options:

- Go API plus server-rendered web UI.
- Go API plus simple React/Next frontend.
- Rails/Laravel/Django if speed of building hosted CRUD matters more than single-language purity.

For the first implementation, choose whatever makes auth, forms, and persistence quickest. The CLI is the product differentiator; the web app is mostly private CRUD at v0.

## V0 Acceptance Criteria

The first useful release is complete when:

1. A user can create a private skill in the web app.
2. The skill has a stable identifier like `iain/debugging`.
3. The user can edit and save the skill Markdown.
4. The user can authenticate the CLI.
5. `skilldex skills` lists the user's skills.
6. `skilldex install iain/debugging --target codex` writes a Codex skill to `~/.codex/skills/iain--debugging/SKILL.md`.
7. `skilldex installed` shows the local install.
8. Editing the skill in the web app and running `skilldex sync` updates the local install.
9. If the local file was manually edited, `sync` detects the hash mismatch and does not overwrite without confirmation.
10. If the remote skill is archived, `sync` warns but leaves the local installed file in place.

## Open Questions

- Should the earliest prototype use hosted auth or a local/dev token?
- Should the web app and API be one service or separate apps?
- Should skill body storage be full `SKILL.md` text only, or parsed frontmatter plus body from the start?
- Should the CLI cache remote skills even when not installed?
- Should install support `--yes` in v0 for scripting?
- How should Windows Codex skill path discovery work if Codex uses a different config directory?
- What is the product name: `skilldex`, `agentskills`, or something else?

## Recommended Next Step

Build the first implementation in this order:

1. Create Go CLI skeleton with `doctor`, config path discovery, and manifest read/write.
2. Implement local install/sync behavior against a local fixture API or static JSON file.
3. Add hosted API and database.
4. Add web editor.
5. Wire CLI auth and real API calls.
6. Dogfood with 3-5 personal Codex skills across two machines.

This keeps the riskiest local behavior testable before spending too much time on hosted product polish.
