#!/bin/sh
# Native user-systemd smoke test. Never replaces an existing installation.
set -eu
unit="$HOME/.config/systemd/user/skillsdex-agent.service"
if [ -e "$unit" ] || systemctl --user list-unit-files skillsdex-agent.service --no-legend | grep -q skillsdex-agent; then
    echo 'Existing SkillsDex service found; refusing to change it.' >&2
    exit 1
fi
test_dir=$(mktemp -d)
cleanup() {
    systemctl --user disable --now skillsdex-agent.service >/dev/null 2>&1 || true
    rm -f -- "$unit"
    systemctl --user daemon-reload
    rm -f -- "$test_dir/skillsdex" "$test_dir/device.json" "$test_dir/agent-watch.lock" "$test_dir/agent-sync.lock"
    rmdir -- "$test_dir"
}
trap cleanup EXIT
cp -- "$1" "$test_dir/skillsdex"
chmod 700 "$test_dir/skillsdex"
# Invalid credentials deliberately prevent network access or instruction writes.
printf '{}' > "$test_dir/device.json"
export SKILLSDEX_HOME="$test_dir"
"$test_dir/skillsdex" agent autostart
"$test_dir/skillsdex" agent autostart
systemctl --user is-enabled skillsdex-agent.service
systemctl --user is-active skillsdex-agent.service
echo 'Native startup, retry, and service activation passed.'
