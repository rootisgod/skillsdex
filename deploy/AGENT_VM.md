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

`opencode` and `agentskill` can be added to `VM_COMPONENTS` for that task only
after Node/npm already exist on the VM.

Verify afterwards:

```bash
task vm:agent-verify
```

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
- agentskill.sh CLI through npm, without running `ags setup`
- OpenRouter provider settings in `~/.codex/config.toml`, defaulting to
  `openrouter/free` for low-cost smoke tests
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
export OPENROUTER_CLAUDE_MODEL="openrouter/free"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$OPENROUTER_CLAUDE_MODEL"
export CLAUDE_CODE_SUBAGENT_MODEL="$OPENROUTER_CLAUDE_MODEL"
```

Codex is configured for OpenRouter in `~/.codex/config.toml`. The default model
is `openrouter/free`; override it for one run when needed:

```bash
codex exec -m openrouter/free "Say ok and stop."
codex exec -m qwen/qwen3-coder:free "Say ok and stop."
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

Claude Code can also route through OpenRouter. OpenRouter documents that Claude
Code is only guaranteed to work with Anthropic first-party provider models, but
for a zero-cost compatibility probe use the free router:

```bash
timeout 30 claude --model "$OPENROUTER_CLAUDE_MODEL" -p "Say ok and stop."
```

If that hangs or fails because Claude Code expects Anthropic-compatible
responses, fall back to the cheap Anthropic-backed model:

```bash
export OPENROUTER_CLAUDE_MODEL="~anthropic/claude-haiku-latest"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$OPENROUTER_CLAUDE_MODEL"
export CLAUDE_CODE_SUBAGENT_MODEL="$OPENROUTER_CLAUDE_MODEL"
claude --model "$OPENROUTER_CLAUDE_MODEL" -p "Say ok and stop."
```

`ags` and `skillsdex` do not call an LLM directly in this setup. They can import,
install, and sync skills without model-provider credentials.
