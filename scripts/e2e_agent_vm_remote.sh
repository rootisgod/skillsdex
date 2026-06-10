#!/usr/bin/env bash
set -Eeuo pipefail

remote_env="${VM_E2E_OPENROUTER_ENV:-~/.env.openrouter}"
case "$remote_env" in
  "~/"*) remote_env="$HOME/${remote_env#\~/}" ;;
esac

if [ ! -r "$remote_env" ]; then
  echo "OpenRouter env file is not readable: $remote_env" >&2
  exit 1
fi

chmod 600 "$remote_env"
set -a
# shellcheck disable=SC1090
. "$remote_env"
set +a

if [ -z "${OPENROUTER_API_KEY:-}" ]; then
  echo "OPENROUTER_API_KEY is not set after sourcing $remote_env" >&2
  exit 1
fi

mkdir -p "$HOME/.local/bin"
install -m 0755 /tmp/skillsdex-e2e "$HOME/.local/bin/skillsdex"
install -m 0755 /tmp/skillsdexd-e2e "$HOME/.local/bin/skillsdexd"

export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
SKILLSDEX_E2E_ALLOW_WRITES=1 bash /tmp/skillsdex-e2e-agent-vm.sh
