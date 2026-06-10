#!/usr/bin/env bash
set -Eeuo pipefail

default_components="core,codex,claude,opencode,pi,openrouter,agentskill"
components="$default_components"
dry_run=0
verify_only=0

usage() {
  cat <<'USAGE'
Usage: scripts/provision_agent_vm.sh [--dry-run] [--verify-only] [--components LIST]

Provision an Ubuntu test machine with agent CLIs and non-secret provider config.

Components:
  core        apt packages, Node/npm, Go, task, local PATH setup
  codex      OpenAI Codex CLI standalone installer
  claude     Claude Code apt repository and package
  opencode   OpenCode npm package
  pi         Pi coding agent npm package
  openrouter Codex OpenRouter provider config and env example only
  agentskill agentskill.sh CLI npm package, without running skill setup

Defaults to:
  core,codex,claude,opencode,pi,openrouter,agentskill
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run)
      dry_run=1
      ;;
    --verify-only)
      verify_only=1
      ;;
    --components)
      if [ "$#" -lt 2 ]; then
        echo "missing value for --components" >&2
        exit 2
      fi
      components="$2"
      shift
      ;;
    --components=*)
      components="${1#--components=}"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

log() {
  printf '\n==> %s\n' "$*"
}

say() {
  printf '%s\n' "$*"
}

run() {
  if [ "$dry_run" -eq 1 ]; then
    printf '+'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

need_cmd() {
  if ! have "$1"; then
    echo "required command missing: $1" >&2
    return 1
  fi
}

sudo_cmd=()
if [ "$(id -u)" -ne 0 ]; then
  if ! have sudo; then
    echo "sudo is required when not running as root" >&2
    exit 1
  fi
  sudo_cmd=(sudo)
fi

run_sudo() {
  run "${sudo_cmd[@]}" "$@"
}

require_sudo() {
  if [ "$(id -u)" -eq 0 ] || [ "$dry_run" -eq 1 ]; then
    return 0
  fi
  if sudo -n true 2>/dev/null; then
    return 0
  fi
  if [ -t 0 ]; then
    say "sudo requires authentication; you may be prompted by sudo."
    return 0
  fi
  cat >&2 <<'EOF'
sudo requires interactive authentication on this host.

Run this script from an interactive SSH session, or configure passwordless sudo
for the provisioning user before using the non-interactive Taskfile wrapper.
EOF
  exit 1
}

validate_components() {
  local IFS=,
  local component
  for component in $components; do
    case "$component" in
      core|codex|claude|opencode|pi|openrouter|agentskill) ;;
      "")
        echo "empty component in --components" >&2
        exit 2
        ;;
      *)
        echo "unknown component: $component" >&2
        echo "allowed components: $default_components" >&2
        exit 2
        ;;
    esac
  done
}

want() {
  case ",$components," in
    *",$1,"*) return 0 ;;
    *) return 1 ;;
  esac
}

append_once() {
  local file="$1"
  local marker="$2"
  local line="$3"

  if [ "$dry_run" -eq 1 ]; then
    say "+ ensure ${file} contains ${marker}"
    return 0
  fi

  mkdir -p "$(dirname "$file")"
  touch "$file"
  if ! grep -Fq "$marker" "$file"; then
    printf '\n%s\n' "$line" >>"$file"
  fi
}

ensure_user_path() {
  mkdir -p "$HOME/.local/bin"
  export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
  append_once "$HOME/.profile" "SkillsDex agent VM PATH" '# SkillsDex agent VM PATH
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) PATH="$HOME/.local/bin:$PATH" ;;
esac
case ":$PATH:" in
  *":$HOME/go/bin:"*) ;;
  *) PATH="$HOME/go/bin:$PATH" ;;
esac
export PATH'
}

node_major() {
  if ! have node; then
    echo 0
    return 0
  fi
  node --version | sed 's/^v//' | cut -d. -f1
}

ensure_npm_prefix() {
  mkdir -p "$HOME/.local"
  export npm_config_prefix="$HOME/.local"
  if [ "$dry_run" -eq 1 ]; then
    say "+ npm config set prefix $HOME/.local"
    return 0
  fi
  npm config set prefix "$HOME/.local" >/dev/null
}

install_core() {
  log "Installing base packages"
  require_sudo
  run_sudo env DEBIAN_FRONTEND=noninteractive apt-get update
  run_sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y \
    build-essential \
    ca-certificates \
    curl \
    git \
    gnupg \
    golang-go \
    jq \
    lsb-release \
    nodejs \
    npm \
    pkg-config \
    sqlite3 \
    tar \
    unzip

  ensure_user_path
  ensure_npm_prefix

  if [ "$dry_run" -eq 1 ]; then
    say "+ verify Node.js major version is at least 18 after apt install"
    if ! have task; then
      say "+ env GOBIN=$HOME/go/bin go install github.com/go-task/task/v3/cmd/task@latest"
    fi
    return 0
  fi

  local major
  major="$(node_major)"
  if [ "$major" -lt 18 ]; then
    echo "Node.js 18+ is required; apt installed node major version ${major}" >&2
    echo "Install a newer Node.js package and rerun this script." >&2
    exit 1
  fi

  if ! have task; then
    log "Installing go-task into GOPATH"
    run env GOBIN="$HOME/go/bin" go install github.com/go-task/task/v3/cmd/task@latest
  fi
}

install_codex() {
  log "Installing Codex CLI"
  if have codex; then
    say "codex already present: $(command -v codex)"
    return 0
  fi
  need_cmd curl

  local tmp
  tmp="$(mktemp)"
  if [ "$dry_run" -eq 1 ]; then
    say "+ curl -fsSL https://chatgpt.com/codex/install.sh -o $tmp"
    say "+ CODEX_NON_INTERACTIVE=1 sh $tmp"
    return 0
  fi
  trap 'rm -f "$tmp"' RETURN
  curl -fsSL https://chatgpt.com/codex/install.sh -o "$tmp"
  CODEX_NON_INTERACTIVE=1 sh "$tmp"
  rm -f "$tmp"
  trap - RETURN
}

install_claude() {
  log "Installing Claude Code"
  if have claude; then
    say "claude already present: $(command -v claude)"
    return 0
  fi
  require_sudo

  local key_tmp list_tmp actual expected
  expected="31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"
  key_tmp="$(mktemp)"
  list_tmp="$(mktemp)"

  if [ "$dry_run" -eq 1 ]; then
    say "+ curl -fsSL https://downloads.claude.ai/keys/claude-code.asc -o $key_tmp"
    say "+ verify Claude Code apt key fingerprint $expected"
    say "+ install Claude Code apt source"
    say "+ apt-get update && apt-get install -y claude-code"
    return 0
  fi

  trap 'rm -f "$key_tmp" "$list_tmp"' RETURN
  curl -fsSL https://downloads.claude.ai/keys/claude-code.asc -o "$key_tmp"
  actual="$(gpg --show-keys --with-colons "$key_tmp" | awk -F: '$1 == "fpr" { print $10; exit }')"
  if [ "$actual" != "$expected" ]; then
    echo "unexpected Claude Code apt key fingerprint: $actual" >&2
    exit 1
  fi

  run_sudo install -d -m 0755 /etc/apt/keyrings
  run_sudo install -m 0644 "$key_tmp" /etc/apt/keyrings/claude-code.asc
  printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/stable stable main' >"$list_tmp"
  run_sudo install -m 0644 "$list_tmp" /etc/apt/sources.list.d/claude-code.list
  run_sudo env DEBIAN_FRONTEND=noninteractive apt-get update
  run_sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y claude-code
  rm -f "$key_tmp" "$list_tmp"
  trap - RETURN
}

npm_global_install() {
  local package="$1"
  need_cmd npm
  ensure_user_path
  ensure_npm_prefix
  run npm install -g "$package"
}

install_opencode() {
  log "Installing OpenCode"
  if have opencode; then
    say "opencode already present: $(command -v opencode)"
    return 0
  fi
  npm_global_install opencode-ai@latest
}

install_pi() {
  log "Installing Pi coding agent"
  if have pi; then
    say "pi already present: $(command -v pi)"
    return 0
  fi
  npm_global_install @earendil-works/pi-coding-agent@latest
}

install_agentskill() {
  log "Installing agentskill.sh CLI"
  if have ags; then
    say "ags already present: $(command -v ags)"
    return 0
  fi
  npm_global_install @agentskill.sh/cli@latest
  say "Installed ags only. Run 'ags setup' later if you want it to write /learn skills."
}

configure_openrouter() {
  log "Configuring OpenRouter placeholders"
  local codex_dir codex_config tmp env_file opencode_dir opencode_config opencode_auth_example codex_model
  codex_model="${OPENROUTER_CODEX_MODEL:-openrouter/free}"
  codex_dir="$HOME/.codex"
  codex_config="$codex_dir/config.toml"
  opencode_dir="$HOME/.config/opencode"
  opencode_config="$opencode_dir/opencode.json"
  opencode_auth_example="$HOME/.local/share/opencode/auth.example.json"
  env_file="$HOME/.config/skillsdex-agent/env.example"

  if [ "$dry_run" -eq 1 ]; then
    say "+ ensure $codex_config uses OpenRouter with model $codex_model"
    say "+ write $opencode_config with cheap/free OpenRouter model entries"
    say "+ write $opencode_auth_example with placeholder OpenRouter credentials"
    say "+ write $env_file with placeholder environment variables"
    return 0
  fi

  mkdir -p "$codex_dir" "$opencode_dir" "$(dirname "$opencode_auth_example")" "$(dirname "$env_file")"
  touch "$codex_config"
  chmod 0600 "$codex_config"

  if grep -Eq '^[[:space:]]*model[[:space:]]*=' "$codex_config"; then
    tmp="$(mktemp)"
    awk -v model="$codex_model" '
      /^[[:space:]]*model[[:space:]]*=/ && !done { print "model = \"" model "\""; done=1; next }
      { print }
    ' "$codex_config" >"$tmp"
    mv "$tmp" "$codex_config"
  else
    tmp="$(mktemp)"
    {
      printf '%s\n' "# Added by SkillsDex agent VM provisioner."
      printf 'model = "%s"\n' "$codex_model"
      cat "$codex_config"
    } >"$tmp"
    mv "$tmp" "$codex_config"
  fi

  if grep -Eq '^[[:space:]]*model_provider[[:space:]]*=' "$codex_config"; then
    tmp="$(mktemp)"
    awk '
      /^[[:space:]]*model_provider[[:space:]]*=/ && !done { print "model_provider = \"openrouter\""; done=1; next }
      { print }
    ' "$codex_config" >"$tmp"
    mv "$tmp" "$codex_config"
  else
    tmp="$(mktemp)"
    {
      printf '%s\n' '# Added by SkillsDex agent VM provisioner.'
      printf '%s\n' 'model_provider = "openrouter"'
      cat "$codex_config"
    } >"$tmp"
    mv "$tmp" "$codex_config"
  fi

  if grep -Eq '^[[:space:]]*model_reasoning_effort[[:space:]]*=' "$codex_config"; then
    tmp="$(mktemp)"
    awk '
      /^[[:space:]]*model_reasoning_effort[[:space:]]*=/ && !done { print "model_reasoning_effort = \"low\""; done=1; next }
      { print }
    ' "$codex_config" >"$tmp"
    mv "$tmp" "$codex_config"
  else
    tmp="$(mktemp)"
    {
      printf '%s\n' 'model_reasoning_effort = "low"'
      cat "$codex_config"
    } >"$tmp"
    mv "$tmp" "$codex_config"
  fi

  if ! grep -Eq '^[[:space:]]*\[model_providers\.openrouter\]' "$codex_config"; then
    cat >>"$codex_config" <<'TOML'

[model_providers.openrouter]
name = "openrouter"
base_url = "https://openrouter.ai/api/v1"
env_key = "OPENROUTER_API_KEY"
TOML
  fi
  chmod 0600 "$codex_config"

  cat >"$opencode_config" <<'JSON'
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "openrouter": {
      "models": {
        "openrouter/free": {},
        "qwen/qwen3-coder:free": {},
        "~anthropic/claude-haiku-latest": {}
      }
    }
  },
  "model": "openrouter/openrouter/free",
  "small_model": "openrouter/openrouter/free"
}
JSON

  cat >"$opencode_auth_example" <<'JSON'
{
  "openrouter": {
    "type": "api",
    "key": "sk-or-your-key-here"
  }
}
JSON
  chmod 0600 "$opencode_config" "$opencode_auth_example"

  cat >"$env_file" <<'ENV'
# Copy the exports you want into your shell profile after creating real keys.
# This file intentionally contains no secrets.
export OPENROUTER_API_KEY="sk-or-..."

# Claude Code can be pointed at OpenRouter later if you choose that route.
export ANTHROPIC_AUTH_TOKEN="$OPENROUTER_API_KEY"
export ANTHROPIC_BASE_URL="https://openrouter.ai/api"
export ANTHROPIC_API_KEY=""

# Cheap/free smoke-test models. Claude Code needs an Anthropic-backed
# OpenRouter model for reliable non-interactive output.
export OPENROUTER_CODEX_MODEL="openrouter/free"
export OPENROUTER_OPENCODE_MODEL="openrouter/openrouter/free"
export OPENROUTER_PI_MODEL="openrouter/openrouter/free"
export OPENROUTER_CLAUDE_MODEL="~anthropic/claude-sonnet-latest"
export ANTHROPIC_DEFAULT_OPUS_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_SONNET_MODEL="$OPENROUTER_CLAUDE_MODEL"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="$OPENROUTER_CLAUDE_MODEL"
export CLAUDE_CODE_SUBAGENT_MODEL="$OPENROUTER_CLAUDE_MODEL"
ENV
  chmod 0600 "$env_file"
}

verify_install() {
  log "Verification"
  export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
  say "host: $(hostname)"
  say "kernel: $(uname -a)"
  if [ -r /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    say "os: ${PRETTY_NAME:-unknown}"
  fi
  say "user: $(id)"

  local cmd
  for cmd in node npm go task codex claude opencode pi ags; do
    if have "$cmd"; then
      printf '%-10s %s\n' "$cmd" "$(command -v "$cmd")"
      case "$cmd" in
        node|npm|go|task|codex|claude|opencode|pi|ags)
          "$cmd" --version 2>/dev/null | sed "s/^/  /" || true
          ;;
      esac
    else
      printf '%-10s missing\n' "$cmd"
    fi
  done

  if [ -f "$HOME/.codex/config.toml" ]; then
    say "codex config: $HOME/.codex/config.toml"
  fi
  if [ -f "$HOME/.config/skillsdex-agent/env.example" ]; then
    say "env example: $HOME/.config/skillsdex-agent/env.example"
  fi
}

validate_components

if [ "$verify_only" -eq 0 ]; then
  want core && install_core
  want codex && install_codex
  want claude && install_claude
  want opencode && install_opencode
  want pi && install_pi
  want openrouter && configure_openrouter
  want agentskill && install_agentskill
fi

verify_install
