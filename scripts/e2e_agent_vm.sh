#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/e2e_agent_vm.sh

Run a disposable real-machine SkillsDex + agent CLI e2e on the current host.

Required:
  SKILLSDEX_E2E_ALLOW_WRITES=1
  OPENROUTER_API_KEY=sk-or-...

Optional:
  SKILLSDEX_BIN=/path/to/skillsdex
  SKILLSDEXD_BIN=/path/to/skillsdexd
  SKILLSDEX_E2E_DIR=/tmp/skillsdex-agent-e2e
  SKILLSDEX_E2E_KEEP=1
  SKILLSDEX_E2E_SKIP_AGENT_SMOKE=1
  SKILLSDEX_E2E_CLAUDE_STRICT=1
  PORT=18765
USAGE
}

log() {
  printf '\n==> %s\n' "$*"
}

fail() {
  echo "e2e_agent_vm: $*" >&2
  if [ -n "${log_file:-}" ] && [ -f "$log_file" ]; then
    echo "server log: $log_file" >&2
    tail -80 "$log_file" >&2 || true
  fi
  exit 1
}

require_contains() {
  local file="$1"
  local text="$2"
  if ! grep -Fq "$text" "$file"; then
    echo "missing expected text: $text" >&2
    sed -n '1,180p' "$file" >&2 || true
    fail "assertion failed"
  fi
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "required command missing: $1"
  fi
}

csrf_from() {
  local path="$1"
  local page="$tmp/csrf.html"
  curl -fsS -b "$cookies" -c "$cookies" "$url$path" -o "$page"
  csrf="$(sed -n 's/.*name="csrf_token" value="\([^"]*\)".*/\1/p' "$page" | head -n 1)"
  if [ -z "$csrf" ]; then
    fail "could not find csrf token on $path"
  fi
  printf '%s' "$csrf"
}

json_post() {
  local path="$1"
  local payload="$2"
  local out="$3"
  curl -fsS \
    -X POST \
    -H "Authorization: Bearer $skillsdex_token" \
    -H "Content-Type: application/json" \
    --data-binary "@$payload" \
    "$url$path" \
    -o "$out"
}

pick_port() {
  if [ -n "${PORT:-}" ]; then
    printf '%s\n' "$PORT"
    return 0
  fi
  local p
  for p in 18765 18766 18767 18768 18769 18770 18771 18772 18773 18774 18775; do
    if ! (echo >"/dev/tcp/127.0.0.1/$p") >/dev/null 2>&1; then
      printf '%s\n' "$p"
      return 0
    fi
  done
  fail "could not find a free loopback port"
}

agent_smoke() {
  if [ "${SKILLSDEX_E2E_SKIP_AGENT_SMOKE:-0}" = "1" ]; then
    log "Skipping agent OpenRouter smoke tests"
    return 0
  fi
  if [ -z "${OPENROUTER_API_KEY:-}" ]; then
    fail "OPENROUTER_API_KEY is required unless SKILLSDEX_E2E_SKIP_AGENT_SMOKE=1"
  fi

  local codex_out pi_out claude_out opencode_out
  codex_out="$tmp/codex.out"
  pi_out="$tmp/pi.out"
  claude_out="$tmp/claude.out"
  opencode_out="$tmp/opencode.out"

  mkdir -p "$HOME/.codex" "$HOME/.config/opencode" "$HOME/.local/share/opencode"
  cat >"$HOME/.codex/config.toml" <<TOML
model_provider = "openrouter"
model_reasoning_effort = "low"
model = "inclusionai/ling-2.6-flash"

[model_providers.openrouter]
name = "openrouter"
base_url = "https://openrouter.ai/api/v1"
env_key = "OPENROUTER_API_KEY"

[projects."$work"]
trust_level = "trusted"
TOML
  chmod 0600 "$HOME/.codex/config.toml"

  jq -n --arg key "$OPENROUTER_API_KEY" \
    '{openrouter:{type:"api",key:$key}}' >"$HOME/.local/share/opencode/auth.json"
  chmod 0600 "$HOME/.local/share/opencode/auth.json"
  cat >"$HOME/.config/opencode/opencode.json" <<'JSON'
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "openrouter": {
      "models": {
        "openrouter/free": {},
        "inclusionai/ling-2.6-flash": {}
      }
    }
  },
  "model": "openrouter/openrouter/free",
  "small_model": "openrouter/openrouter/free"
}
JSON
  chmod 0600 "$HOME/.config/opencode/opencode.json"

  export ANTHROPIC_BASE_URL="https://openrouter.ai/api"
  export ANTHROPIC_AUTH_TOKEN="$OPENROUTER_API_KEY"
  export ANTHROPIC_API_KEY=""
  export OPENROUTER_CLAUDE_MODEL="${OPENROUTER_CLAUDE_MODEL:-inclusionai/ling-2.6-flash}"
  export ANTHROPIC_DEFAULT_OPUS_MODEL="$OPENROUTER_CLAUDE_MODEL"
  export ANTHROPIC_DEFAULT_SONNET_MODEL="$OPENROUTER_CLAUDE_MODEL"
  export ANTHROPIC_DEFAULT_HAIKU_MODEL="$OPENROUTER_CLAUDE_MODEL"
  export CLAUDE_CODE_SUBAGENT_MODEL="$OPENROUTER_CLAUDE_MODEL"

  log "Testing Codex against installed SkillsDex skill"
  if command -v codex >/dev/null 2>&1; then
    (cd "$work" && timeout 120 codex exec -m inclusionai/ling-2.6-flash --yolo \
      "\$skillsdex-e2e-smoke-reply skillsdex-e2e-smoke-check. Run the SkillsDex E2E smoke phrase check. Return only the exact phrase required by that skill." >"$codex_out" 2>&1) || {
      sed -n '1,180p' "$codex_out" >&2 || true
      fail "codex smoke failed"
    }
    require_contains "$codex_out" "skillsdex-skill-ok"
  else
    fail "codex is not installed"
  fi

  log "Testing Pi against installed SkillsDex skill"
  if command -v pi >/dev/null 2>&1; then
    (cd "$work" && timeout 90 pi --no-session --tools read --model openrouter/openrouter/free \
      --skill "$skill_dir" \
      -p "Use the loaded SkillsDex E2E Smoke Reply skill. Run the SkillsDex E2E smoke phrase check. Output only the phrase required by the skill." >"$pi_out" 2>&1) || {
      sed -n '1,180p' "$pi_out" >&2 || true
      fail "pi smoke failed"
    }
    require_contains "$pi_out" "skillsdex-skill-ok"
  else
    fail "pi is not installed"
  fi

  log "Testing Claude provider route"
  if command -v claude >/dev/null 2>&1; then
    claude_ok=0
    if ! (cd "$work" && timeout 90 claude --bare --model "$OPENROUTER_CLAUDE_MODEL" --max-budget-usd 0.05 \
      --output-format json -p "Say ok and stop." >"$claude_out" 2>&1); then
      if [ "${SKILLSDEX_E2E_CLAUDE_STRICT:-0}" = "1" ]; then
        sed -n '1,180p' "$claude_out" >&2 || true
        fail "claude provider smoke failed"
      fi
      sed -n '1,80p' "$claude_out" >&2 || true
      echo "warning: claude provider smoke failed; continuing because SKILLSDEX_E2E_CLAUDE_STRICT is not set" >&2
      claude_ok=1
    fi
    if [ "$claude_ok" -eq 0 ]; then
      require_contains "$claude_out" '"is_error":false'
      require_contains "$claude_out" "$OPENROUTER_CLAUDE_MODEL"
    fi
  else
    echo "warning: claude is not installed; skipping" >&2
  fi

  log "Testing OpenCode provider route"
  if command -v opencode >/dev/null 2>&1; then
    (cd "$work" && timeout 90 opencode run --format json -m openrouter/openrouter/free \
      "Say ok and stop." >"$opencode_out" 2>&1) || true
    sqlite3 "$HOME/.local/share/opencode/opencode.db" \
      "select json_extract(data, '$.text') from part where json_extract(data, '$.type') = 'text' order by time_created desc limit 1;" \
      >"$tmp/opencode-db.out" 2>/dev/null || true
    tr '[:upper:]' '[:lower:]' <"$tmp/opencode-db.out" >"$tmp/opencode-db-normalized.out"
    require_contains "$tmp/opencode-db-normalized.out" "ok"
  else
    echo "warning: opencode is not installed; skipping" >&2
  fi
}

case "${1:-}" in
  -h|--help)
    usage
    exit 0
    ;;
esac

if [ "${SKILLSDEX_E2E_ALLOW_WRITES:-0}" != "1" ]; then
  fail "set SKILLSDEX_E2E_ALLOW_WRITES=1 to run this disposable machine e2e"
fi

need_cmd curl
need_cmd jq
need_cmd sqlite3

export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
skillsdex_bin="${SKILLSDEX_BIN:-skillsdex}"
skillsdexd_bin="${SKILLSDEXD_BIN:-skillsdexd}"
if ! command -v "$skillsdex_bin" >/dev/null 2>&1 && [ ! -x "$skillsdex_bin" ]; then
  fail "skillsdex binary not found: $skillsdex_bin"
fi
if ! command -v "$skillsdexd_bin" >/dev/null 2>&1 && [ ! -x "$skillsdexd_bin" ]; then
  fail "skillsdexd binary not found: $skillsdexd_bin"
fi

tmp="${SKILLSDEX_E2E_DIR:-$(mktemp -d /tmp/skillsdex-agent-e2e.XXXXXX)}"
if [ -n "${SKILLSDEX_E2E_DIR:-}" ]; then
  rm -rf "$tmp"
  mkdir -p "$tmp"
fi
home="$tmp/home"
skillsdex_home="$tmp/skillsdex-home"
db="$tmp/skillsdex.db"
cookies="$tmp/cookies.txt"
log_file="$tmp/server.log"
work="$tmp/work"
plugin_marker="$tmp/plugin-installed.txt"
mkdir -p "$home" "$skillsdex_home" "$work"

cleanup() {
  local status=$?
  trap - EXIT
  if [ -n "${server_pid:-}" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -f "$home/.local/share/opencode/auth.json" 2>/dev/null || true
  if [ "${SKILLSDEX_E2E_KEEP:-0}" != "1" ]; then
    rm -rf "$tmp"
  else
    echo "Kept e2e directory: $tmp"
  fi
  exit "$status"
}
trap cleanup EXIT

export HOME="$home"
export SKILLSDEX_HOME="$skillsdex_home"
port="$(pick_port)"
url="http://127.0.0.1:$port"

log "Starting fresh SkillsDex server on $url"
"$skillsdexd_bin" serve --addr "127.0.0.1:$port" --db "$db" >"$log_file" 2>&1 &
server_pid="$!"
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  if curl -fsS "$url/login" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done
curl -fsS "$url/login" >/dev/null 2>&1 || fail "server did not start"

log "Bootstrapping seeded admin account"
curl -fsS -c "$cookies" -d "username=admin" -d "password=admin" "$url/login" -o /dev/null
csrf="$(csrf_from /setup/password)"
curl -fsS -b "$cookies" -c "$cookies" \
  --data-urlencode "csrf_token=$csrf" \
  --data-urlencode "current_password=admin" \
  --data-urlencode "new_password=changed-password-123" \
  --data-urlencode "confirm_password=changed-password-123" \
  "$url/setup/password" -o /dev/null

csrf="$(csrf_from /tokens)"
token_page="$tmp/token.html"
curl -fsS -b "$cookies" -c "$cookies" \
  --data-urlencode "csrf_token=$csrf" \
  --data-urlencode "name=agent-vm-e2e" \
  "$url/tokens" -o "$token_page"
skillsdex_token="$(tr '\n' ' ' <"$token_page" | sed -n 's/.*class="one-time-token"[^<]*<span>New token<\/span><code>\([^<]*\)<\/code>.*/\1/p' | head -n 1)"
case "$skillsdex_token" in
  sdx_*) ;;
  *) fail "could not extract API token" ;;
esac

log "Logging local SkillsDex CLI into disposable server"
printf '%s\n' "$skillsdex_token" | "$skillsdex_bin" login --server "$url" --token-stdin >"$tmp/login.out"
require_contains "$tmp/login.out" "Logged in to $url as admin"
if grep -Fq "$skillsdex_token" "$tmp/login.out"; then
  fail "login output leaked API token"
fi
"$skillsdex_bin" whoami >"$tmp/whoami.out"
require_contains "$tmp/whoami.out" "admin"
"$skillsdex_bin" config set default_target codex >"$tmp/config.out"
require_contains "$tmp/config.out" "default_target = codex"

log "Seeding deterministic skills and plugin records through the API"
smoke_body='---
name: skillsdex-e2e-smoke-reply
description: Return the fixed SkillsDex E2E smoke phrase when asked for the smoke phrase check.
---

# SkillsDex E2E Smoke Reply

Use when the user asks to run the SkillsDex E2E smoke phrase check or mentions the activation phrase `skillsdex-e2e-smoke-check`.

When this skill is invoked, answer exactly:

skillsdex-skill-ok

Do not add punctuation, Markdown, explanation, quotes, or extra whitespace.'

jq -n \
  --arg owner "admin" \
  --arg slug "e2e-smoke-reply" \
  --arg name "skillsdex-e2e-smoke-reply" \
  --arg description "Deterministic smoke skill for real-machine SkillsDex agent CLI e2e tests." \
  --arg short "Return a fixed smoke-test phrase" \
  --arg body "$smoke_body" \
  '{
    owner: $owner,
    slug: $slug,
    name: $name,
    description: $description,
    short_description: $short,
    recommended_default: true,
    pinned_install: true,
    tags: ["e2e", "smoke"],
    body: $body
  }' >"$tmp/skill-smoke.json"
json_post "/v1/skills" "$tmp/skill-smoke.json" "$tmp/skill-smoke.out"
require_contains "$tmp/skill-smoke.out" '"slug":"e2e-smoke-reply"'

helper_body='---
name: skillsdex-e2e-helper
description: Secondary fixture skill for SkillsDex multi-skill install e2e coverage.
---

# SkillsDex E2E Helper Skill

Use when validating that SkillsDex can install more than one managed skill in a clean machine e2e.

This skill exists to make the install set non-trivial. It should not be selected for the smoke phrase check.'
jq -n \
  --arg owner "admin" \
  --arg slug "e2e-helper" \
  --arg name "skillsdex-e2e-helper" \
  --arg description "Second deterministic skill for multi-skill install coverage." \
  --arg short "Secondary e2e install fixture" \
  --arg body "$helper_body" \
  '{
    owner: $owner,
    slug: $slug,
    name: $name,
    description: $description,
    short_description: $short,
    recommended_default: false,
    pinned_install: true,
    tags: ["e2e"],
    body: $body
  }' >"$tmp/skill-helper.json"
json_post "/v1/skills" "$tmp/skill-helper.json" "$tmp/skill-helper.out"
require_contains "$tmp/skill-helper.out" '"slug":"e2e-helper"'

cat >"$tmp/e2e-plugin-installer" <<PLUGIN
#!/usr/bin/env bash
set -eu
printf '%s\n' 'skillsdex-plugin-ok' > '$plugin_marker'
PLUGIN
chmod 0700 "$tmp/e2e-plugin-installer"
jq -n \
  --arg slug "e2e-plugin-smoke" \
  --arg name "SkillsDex E2E Plugin Smoke" \
  --arg description "Harmless plugin install record with a deterministic local file side effect." \
  --arg source_url "https://example.com/skillsdex/e2e-plugin-smoke" \
  --arg target "codex" \
  --arg command "$tmp/e2e-plugin-installer" \
  '{
    slug: $slug,
    name: $name,
    description: $description,
    source_url: $source_url,
    target: $target,
    install_command: $command
  }' >"$tmp/plugin.json"
json_post "/v1/plugins" "$tmp/plugin.json" "$tmp/plugin.out"
require_contains "$tmp/plugin.out" '"slug":"e2e-plugin-smoke"'

log "Installing seeded skills into the Codex target"
"$skillsdex_bin" setup --pinned --apply --yes >"$tmp/setup.out"
require_contains "$tmp/setup.out" "Installed 2 pinned skill(s)."
"$skillsdex_bin" installed --json >"$tmp/installed.json"
require_contains "$tmp/installed.json" '"healthy": true'
require_contains "$tmp/installed.json" '"admin/e2e-smoke-reply"'
require_contains "$tmp/installed.json" '"admin/e2e-helper"'
"$skillsdex_bin" doctor --fix-permissions --strict >"$tmp/doctor.out"
require_contains "$tmp/doctor.out" "Doctor strict check: passed"

skill_file="$HOME/.agents/skills/admin--e2e-smoke-reply/SKILL.md"
skill_dir="$(dirname "$skill_file")"
test -s "$skill_file" || fail "expected installed skill at $skill_file"
require_contains "$skill_file" "skillsdex-skill-ok"

log "Testing tracked plugin install"
"$skillsdex_bin" plugin install e2e-plugin-smoke --dry-run >"$tmp/plugin-dry-run.out"
require_contains "$tmp/plugin-dry-run.out" "Mode: dry run"
"$skillsdex_bin" plugin install e2e-plugin-smoke --yes >"$tmp/plugin-install.out"
require_contains "$plugin_marker" "skillsdex-plugin-ok"

agent_smoke

log "Testing sync and uninstall loop"
"$skillsdex_bin" sync --dry-run --strict >"$tmp/sync.out"
require_contains "$tmp/sync.out" "admin/e2e-smoke-reply: clean"
require_contains "$tmp/sync.out" "admin/e2e-helper: clean"
"$skillsdex_bin" uninstall admin/e2e-helper --target codex --yes >"$tmp/uninstall.out"
require_contains "$tmp/uninstall.out" "Uninstalled admin/e2e-helper"
test ! -e "$HOME/.agents/skills/admin--e2e-helper/SKILL.md" || fail "helper skill was not removed"

log "Agent VM e2e passed"
printf 'server=%s\n' "$url"
if [ "${SKILLSDEX_E2E_KEEP:-0}" = "1" ]; then
  printf 'tmp=%s\n' "$tmp"
else
  printf 'tmp=removed on exit\n'
fi
