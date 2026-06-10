# Agent VM Provisioning

This repo includes a disposable Ubuntu VM bootstrap for validating the real
machine SkillsDex CLI loop before installing the SkillsDex service itself.

Default VM:

```bash
VM_HOST=iain@192.168.1.45
VM_SSH_KEY=$HOME/.ssh/ib_ssh
```

Preflight:

```bash
task vm:agent-preflight
```

The non-interactive Taskfile installer needs passwordless sudo for components
that install apt packages. If preflight reports `sudo: interactive
authentication required`, either enable passwordless sudo for this disposable
VM or SSH in and run `/tmp/skillsdex-provision-agent-vm.sh` interactively after
copying it.

Preview the provisioning run:

```bash
task vm:provision-agents-dry-run
```

Apply it:

```bash
task vm:provision-agents
```

Install only the user-level Codex/OpenRouter pieces that do not use apt:

```bash
task vm:provision-agents-user
```

`opencode`, `pi`, and `agentskill` can be added to `VM_COMPONENTS` for that task only
after Node/npm already exist on the VM.

Verify afterwards:

```bash
task vm:agent-verify
```

Run the full disposable server and real-agent e2e after putting an OpenRouter
key on the VM at `~/.env.openrouter`:

```bash
task vm:e2e-agent
```

This task builds Linux `skillsdex` and `skillsdexd` binaries, copies them to the
VM, starts a fresh loopback SkillsDex server from a new SQLite DB, bootstraps the
seeded admin account, creates deterministic e2e skills and a harmless plugin
record, installs the pinned skills into a temporary Codex home, and smoke-tests
Codex and Pi against the installed skill. Claude and OpenCode are checked for
provider connectivity, with OpenCode verified through its local DB because
headless output may omit assistant text.

The remote env file should contain:

```bash
export OPENROUTER_API_KEY="sk-or-..."
```

Use `VM_E2E_OPENROUTER_ENV=/path/to/env task vm:e2e-agent` to source a different
key file on the VM.

Point the SkillsDex client at production after you have a web API token:

```bash
ssh -i ~/.ssh/ib_ssh iain@192.168.1.45
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export SKILLSDEX_TOKEN="<token from https://www.skillsdex.org API Tokens>"
printf '%s\n' "$SKILLSDEX_TOKEN" | skillsdex login --server https://www.skillsdex.org --token-stdin
skillsdex whoami
skillsdex config set default_target codex
skillsdex doctor --fix-permissions
skillsdex setup
```

Do not paste the API token into chat or commit it to the repo. `--token-stdin`
keeps it out of shell history; unset it from the current shell with
`unset SKILLSDEX_TOKEN` after login if you do not need it anymore.

Limit components when testing a smaller slice:

```bash
VM_COMPONENTS=core,codex,openrouter task vm:provision-agents
```

The provisioning script installs:

- base Ubuntu build and CLI packages needed for SkillsDex development
- Go `task` via `go install`
- Codex CLI using the official standalone installer
- Claude Code through Anthropic's signed apt repository
- OpenCode through npm
- Pi coding agent through npm
- agentskill.sh CLI through npm, without running `ags setup`
- OpenRouter provider settings in `~/.codex/config.toml`, defaulting to
  `inclusionai/ling-2.6-flash` for reliable low-cost skill smoke tests
- OpenCode OpenRouter model entries in `~/.config/opencode/opencode.json`
- a secret-free env example at `~/.config/skillsdex-agent/env.example`

It intentionally does not write API keys, log into any tool, or install skills
from public registries. Run those steps manually after reviewing the machine.

## OpenRouter Smoke Tests

OpenRouter can cover most of the installed agent tools with one API key. Create
an OpenRouter key with a low credit limit, then SSH into the VM and export it:

```bash
ssh -i ~/.ssh/ib_ssh iain@192.168.1.45
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export OPENROUTER_API_KEY="sk-or-..."
export ANTHROPIC_BASE_URL="https://openrouter.ai/api"
export ANTHROPIC_AUTH_TOKEN="$OPENROUTER_API_KEY"
export ANTHROPIC_API_KEY=""
export OPENROUTER_CLAUDE_MODEL="inclusionai/ling-2.6-flash"
export OPENROUTER_PI_MODEL="openrouter/openrouter/free"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$OPENROUTER_CLAUDE_MODEL"
export CLAUDE_CODE_SUBAGENT_MODEL="$OPENROUTER_CLAUDE_MODEL"
```

The values tested on the disposable VM were:

| Tool | Model or provider value | Status |
| --- | --- | --- |
| Raw OpenRouter API | `openrouter/free` | returned `ok` |
| Codex CLI | `inclusionai/ling-2.6-flash` | returned `skillsdex-skill-ok` from the installed SkillsDex skill |
| Claude Code | `inclusionai/ling-2.6-flash` | returned `ok` with `--bare`; full VM e2e passed |
| OpenCode | `openrouter/openrouter/free` | returned `ok`, but only in the local DB because OpenCode 1.16.2 did not print assistant text in headless mode |
| Pi CLI | `openrouter/openrouter/free` | returned `ok` |

Claude Code model notes from the VM:

| Model | OpenRouter price | VM result |
| --- | --- | --- |
| `google/gemma-4-26b-a4b-it:free` | free | returned `ok` once, then timed out at 90s; not stable enough for the default |
| `openrouter/free` | free | provider route succeeded, but Claude Code returned empty final text |
| `nvidia/nemotron-3-nano-30b-a3b:free` | free | provider route succeeded, but Claude Code returned empty final text |
| `deepseek/deepseek-v4-flash` | `$0.0983/M` input, `$0.1966/M` output | provider route succeeded, but Claude Code returned empty final text |
| `inclusionai/ling-2.6-flash` | `$0.01/M` input, `$0.03/M` output | returned `ok` and passed the full VM e2e |
| `mistralai/ministral-3b-2512` | `$0.10/M` input, `$0.10/M` output | returned `Ok.` in direct Claude Code smoke testing |
| `anthropic/claude-haiku-4.5` | `$1/M` input, `$5/M` output | provider route succeeded, but Claude Code returned empty final text |

Codex is configured for OpenRouter in `~/.codex/config.toml`. The e2e harness
uses `inclusionai/ling-2.6-flash` for the installed-skill assertion because
`openrouter/free` returned arbitrary text in Codex 0.139.0 on one VM run. Codex
0.139.0 also requires installed skills to include YAML frontmatter:

```bash
codex exec -m openrouter/free "Say ok and stop."
codex exec -m inclusionai/ling-2.6-flash '$skillsdex-e2e-smoke-reply skillsdex-e2e-smoke-check. Run the SkillsDex E2E smoke phrase check.'
```

OpenCode has an OpenRouter provider config in `~/.config/opencode/opencode.json`.
Authenticate it interactively with `/connect`, or write the auth file locally:

```bash
mkdir -p ~/.local/share/opencode
jq -n --arg key "$OPENROUTER_API_KEY" \
  '{openrouter:{type:"api",key:$key}}' > ~/.local/share/opencode/auth.json
chmod 0600 ~/.local/share/opencode/auth.json
opencode run -m openrouter/openrouter/free "Say ok and stop."
```

OpenCode 1.16.2 may print only a status line in headless mode even when the
model response succeeds. Verify the most recent text response from its local DB:

```bash
sqlite3 ~/.local/share/opencode/opencode.db \
  "select json_extract(data, '$.text') from part where json_extract(data, '$.type') = 'text' order by time_created desc limit 1;"
```

Pi uses `OPENROUTER_API_KEY` directly and accepts provider-prefixed model names:

```bash
pi --no-session --no-tools --model "$OPENROUTER_PI_MODEL" -p "Say ok and stop."
```

When testing an installed skill with Pi, allow only the read tool so Pi can load
the skill package without write access:

```bash
pi --no-session --tools read --model "$OPENROUTER_PI_MODEL" \
  --skill ~/.agents/skills/admin--e2e-smoke-reply \
  -p "Use the loaded skill and run the SkillsDex E2E smoke phrase check."
```

Claude Code can also route through OpenRouter. Use `--bare` so Claude Code does
not wait on cached OAuth/keychain state:

```bash
export OPENROUTER_CLAUDE_MODEL="inclusionai/ling-2.6-flash"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$OPENROUTER_CLAUDE_MODEL"
export CLAUDE_CODE_SUBAGENT_MODEL="$OPENROUTER_CLAUDE_MODEL"
claude --bare --model "$OPENROUTER_CLAUDE_MODEL" --max-budget-usd 0.05 --output-format json -p "Say ok and stop."
```

Claude Code 2.1.153 can route several OpenRouter models successfully, but many
non-Anthropic and Haiku responses returned empty final text. `inclusionai/ling-2.6-flash`
is the cheapest tested model that returned usable text and passed the full VM
e2e.

`ags` and `skillsdex` do not call an LLM directly in this setup. They can import,
install, and sync skills without model-provider credentials.

## Disposable Skills and Plugin E2E

The agent VM is a disposable real-machine e2e target for SkillsDex-managed skill
and plugin installs. The harness flow is:

1. Start from a snapshotted or freshly provisioned VM.
2. Log the SkillsDex CLI into `https://www.skillsdex.org` with a throwaway API
   token.
3. Install a tiny deterministic smoke-test skill, for example one that says to
   reply exactly `skillsdex-skill-ok` when asked for the SkillsDex smoke phrase.
4. Run the installed agent CLI with a prompt that should trigger that skill.
5. Assert the exact response or the expected plugin side effect.
6. Run sync/update/uninstall and verify the managed files are added, changed, or
   removed as expected.

Codex should be the first target because SkillsDex already writes Codex skills
under the native user skill path `~/.agents/skills`. Pi is the next useful
execution target because it can load skills directly with `--skill <path>` and
already works with OpenRouter.
Claude Code can be tested after confirming `--bare` still loads the installed
skill path needed for the scenario. OpenCode is useful for install-location
checks, but its current headless output behavior makes it a weaker command-line
assertion target.

Plugin tests should start with dry-run or harmless plugin install commands and
then graduate to a small known plugin that has a deterministic command or file
side effect. Keep the e2e gated behind explicit environment variables such as
`SKILLSDEX_E2E_SERVER`, `SKILLSDEX_E2E_TOKEN`, and
`SKILLSDEX_E2E_ALLOW_WRITES=1` so it cannot run accidentally on a real home
directory.
