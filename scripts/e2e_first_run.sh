#!/usr/bin/env sh
set -eu

port="${PORT:-18765}"
url="http://127.0.0.1:${port}"
tmp="$(mktemp -d)"
home="${tmp}/home"
skillsdex_home="${tmp}/skillsdex-home"
db="${tmp}/skillsdex.db"
cookies="${tmp}/cookies.txt"
log="${tmp}/server.log"
cli_bin="${tmp}/skillsdex"
daemon_bin="${tmp}/skillsdexd"

cleanup() {
  if [ -n "${server_pid:-}" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

fail() {
  echo "e2e_first_run: $*" >&2
  echo "server log: ${log}" >&2
  if [ -f "$log" ]; then
    tail -80 "$log" >&2 || true
  fi
  exit 1
}

require_contains() {
  file="$1"
  text="$2"
  if ! grep -Fq "$text" "$file"; then
    echo "missing expected text: ${text}" >&2
    sed -n '1,160p' "$file" >&2 || true
    fail "assertion failed"
  fi
}

csrf_from() {
  path="$1"
  page="${tmp}/csrf.html"
  curl -fsS -b "$cookies" -c "$cookies" "${url}${path}" -o "$page"
  csrf="$(sed -n 's/.*name="csrf_token" value="\([^"]*\)".*/\1/p' "$page" | head -n 1)"
  if [ -z "$csrf" ]; then
    fail "could not find csrf token on ${path}"
  fi
  printf '%s' "$csrf"
}

mkdir -p "$home" "$skillsdex_home"
go build -o "$cli_bin" ./cmd/skillsdex
go build -o "$daemon_bin" ./cmd/skillsdexd

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$daemon_bin" serve --addr "127.0.0.1:${port}" --db "$db" >"$log" 2>&1 &
server_pid="$!"
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  if curl -fsS "${url}/login" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done
curl -fsS "${url}/login" >/dev/null 2>&1 || fail "server did not start"

curl -fsS -c "$cookies" -d "username=admin" -d "password=admin" "${url}/login" -o /dev/null
csrf="$(csrf_from /setup/password)"
curl -fsS -b "$cookies" -c "$cookies" \
  --data-urlencode "csrf_token=${csrf}" \
  --data-urlencode "current_password=admin" \
  --data-urlencode "new_password=changed-password-123" \
  --data-urlencode "confirm_password=changed-password-123" \
  "${url}/setup/password" -o /dev/null

setup_page="${tmp}/setup.html"
curl -fsS -b "$cookies" -c "$cookies" "${url}/setup" -o "$setup_page"
require_contains "$setup_page" "Terminal Flow"
require_contains "$setup_page" "Generate a CLI token"
require_contains "$setup_page" "token-stdin"
require_contains "$setup_page" "skillsdex config set default_target codex"
require_contains "$setup_page" "skillsdex installed --strict"
require_contains "$setup_page" "skillsdex lint --all --strict"
require_contains "$setup_page" "skillsdex backup restore --file ./skillsdex-backup.zip --dry-run"

csrf="$(csrf_from /tokens)"
token_page="${tmp}/token.html"
curl -fsS -b "$cookies" -c "$cookies" \
  --data-urlencode "csrf_token=${csrf}" \
  --data-urlencode "name=e2e-cli" \
  "${url}/tokens" -o "$token_page"
token="$(tr '\n' ' ' <"$token_page" | sed -n 's/.*class="one-time-token"[^<]*<span>New token<\/span><code>\([^<]*\)<\/code>.*/\1/p' | head -n 1)"
if [ -z "$token" ]; then
  fail "could not extract one-time API token"
fi
case "$token" in
  sdx_*) ;;
  *) fail "extracted value does not look like an API token: ${token}" ;;
esac

printf '%s\n' "$token" | HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" login --server "$url" --token-stdin >"${tmp}/login.out"
require_contains "${tmp}/login.out" "Logged in to ${url} as admin"
if grep -Fq "$token" "${tmp}/login.out"; then
  fail "login output leaked API token"
fi

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" config set default_target codex >"${tmp}/config-target.out"
require_contains "${tmp}/config-target.out" "default_target = codex"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" doctor >"${tmp}/doctor.out"
require_contains "${tmp}/doctor.out" "Server: reachable as admin"
require_contains "${tmp}/doctor.out" "Managed installs: 0"
require_contains "${tmp}/doctor.out" "skillsdex lint --all --strict"
require_contains "${tmp}/doctor.out" "skillsdex backup restore --file ./skillsdex-backup.zip --dry-run"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" doctor --json >"${tmp}/doctor.json"
require_contains "${tmp}/doctor.json" '"healthy": false'
require_contains "${tmp}/doctor.json" '"server"'
require_contains "${tmp}/doctor.json" '"status": "reachable"'
require_contains "${tmp}/doctor.json" '"no managed installs"'

chmod 0644 "${skillsdex_home}/auth.json"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" doctor --fix-permissions >"${tmp}/doctor-fix-auth.out"
require_contains "${tmp}/doctor-fix-auth.out" "Fixed auth file permissions: ${skillsdex_home}/auth.json"
require_contains "${tmp}/doctor-fix-auth.out" "Auth path: ${skillsdex_home}/auth.json (private)"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" skills >"${tmp}/skills.out"
require_contains "${tmp}/skills.out" "admin/five-advisor-review"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" skills --json >"${tmp}/skills.json"
require_contains "${tmp}/skills.json" '"skills":'
require_contains "${tmp}/skills.json" '"owner": "admin"'
require_contains "${tmp}/skills.json" '"slug": "five-advisor-review"'

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" templates >"${tmp}/templates.out"
require_contains "${tmp}/templates.out" "workflow"
require_contains "${tmp}/templates.out" "Focused Review"
require_contains "${tmp}/templates.out" "research-brief"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" mark admin/five-advisor-review --pinned >"${tmp}/mark-preview.out"
require_contains "${tmp}/mark-preview.out" "Would update admin/five-advisor-review install flags"
require_contains "${tmp}/mark-preview.out" "Pinned install: no -> yes"
require_contains "${tmp}/mark-preview.out" "skillsdex mark admin/five-advisor-review --pinned --apply --yes"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" mark admin/five-advisor-review --pinned --apply --yes >"${tmp}/mark-apply.out"
require_contains "${tmp}/mark-apply.out" "Updated admin/five-advisor-review: pinned=yes"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" install-pinned --dry-run >"${tmp}/install-pinned-dry-run.out"
require_contains "${tmp}/install-pinned-dry-run.out" "Would install 1 pinned skill(s)."
require_contains "${tmp}/install-pinned-dry-run.out" "admin/five-advisor-review"

csrf="$(csrf_from /sets)"
curl -fsS -b "$cookies" -c "$cookies" \
  --data-urlencode "csrf_token=${csrf}" \
  --data-urlencode "slug=laptop" \
  --data-urlencode "name=Laptop" \
  --data-urlencode "description=Daily driver install set" \
  "${url}/sets" -o /dev/null
set_page="${tmp}/set.html"
curl -fsS -b "$cookies" -c "$cookies" "${url}/sets/laptop" -o "$set_page"
skill_id="$(tr '\n' ' ' <"$set_page" | sed -n 's/.*name="skill_id" value="\([^"]*\)"[^>]*><span><strong>admin\/five-advisor-review<\/strong>.*/\1/p' | head -n 1)"
if [ -z "$skill_id" ]; then
  fail "could not find five-advisor-review membership checkbox"
fi
csrf="$(csrf_from /sets/laptop)"
curl -fsS -b "$cookies" -c "$cookies" \
  --data-urlencode "csrf_token=${csrf}" \
  --data-urlencode "name=Laptop" \
  --data-urlencode "description=Daily driver install set" \
  --data-urlencode "skill_id=${skill_id}" \
  "${url}/sets/laptop" -o /dev/null

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sets >"${tmp}/sets.out"
require_contains "${tmp}/sets.out" "laptop"
require_contains "${tmp}/sets.out" "Laptop"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sets --json >"${tmp}/sets.json"
require_contains "${tmp}/sets.json" '"sets":'
require_contains "${tmp}/sets.json" '"slug": "laptop"'
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sets laptop >"${tmp}/set-show.out"
require_contains "${tmp}/set-show.out" "admin/five-advisor-review"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sets laptop --json >"${tmp}/set-show.json"
require_contains "${tmp}/set-show.json" '"set":'
require_contains "${tmp}/set-show.json" '"skills":'
require_contains "${tmp}/set-show.json" '"owner": "admin"'
require_contains "${tmp}/set-show.json" '"slug": "five-advisor-review"'

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" setup --set laptop >"${tmp}/setup-preview.out"
require_contains "${tmp}/setup-preview.out" "Mode: preview only"
require_contains "${tmp}/setup-preview.out" "Would install admin/five-advisor-review"
require_contains "${tmp}/setup-preview.out" "Quality gate:"
require_contains "${tmp}/setup-preview.out" "skillsdex lint --all --strict"
require_contains "${tmp}/setup-preview.out" "Backup confidence:"
require_contains "${tmp}/setup-preview.out" "skillsdex backup restore --file ./skillsdex-backup.zip --dry-run"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" setup --set laptop --json >"${tmp}/setup-preview.json"
require_contains "${tmp}/setup-preview.json" '"preview_only": true'
require_contains "${tmp}/setup-preview.json" '"mode": "set"'
require_contains "${tmp}/setup-preview.json" '"set_slug": "laptop"'
require_contains "${tmp}/setup-preview.json" '"skillsdex setup --target codex --set laptop --apply --yes"'
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" install-set laptop --dry-run >"${tmp}/install-set-dry-run.out"
require_contains "${tmp}/install-set-dry-run.out" "Would install 1 skill(s) from set laptop."
require_contains "${tmp}/install-set-dry-run.out" "Package files:"
require_contains "${tmp}/install-set-dry-run.out" "SKILL.md"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" install-set laptop --yes >"${tmp}/install-set.out"
require_contains "${tmp}/install-set.out" "Installed 1 skill(s) from set laptop."

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" install-defaults --yes >"${tmp}/install-defaults.out"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" installed >"${tmp}/installed.out"
require_contains "${tmp}/installed.out" "admin/five-advisor-review"
require_contains "${tmp}/installed.out" "clean"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" installed --json >"${tmp}/installed.json"
require_contains "${tmp}/installed.json" '"healthy": true'
require_contains "${tmp}/installed.json" '"install_count":'
require_contains "${tmp}/installed.json" '"admin/five-advisor-review"'
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" installed --refresh >"${tmp}/installed-refresh.out"
require_contains "${tmp}/installed-refresh.out" "SOURCE"
require_contains "${tmp}/installed-refresh.out" "admin/five-advisor-review"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" lint admin/five-advisor-review >"${tmp}/lint-remote.out"
require_contains "${tmp}/lint-remote.out" "admin/five-advisor-review:"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" lint --all >"${tmp}/lint-all.out"
require_contains "${tmp}/lint-all.out" "Linted "
require_contains "${tmp}/lint-all.out" "active skill(s):"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" lint --all --json >"${tmp}/lint-all.json"
require_contains "${tmp}/lint-all.json" '"skill_count":'
require_contains "${tmp}/lint-all.json" '"reports":'
skill_file="${home}/.agents/skills/admin--five-advisor-review/SKILL.md"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" lint --file "$skill_file" >"${tmp}/lint-file.out"
require_contains "${tmp}/lint-file.out" "${skill_file}:"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sync --dry-run --strict >"${tmp}/sync-clean.out"
require_contains "${tmp}/sync-clean.out" "admin/five-advisor-review: clean"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sync --dry-run --json >"${tmp}/sync-clean.json"
require_contains "${tmp}/sync-clean.json" '"healthy": true'
require_contains "${tmp}/sync-clean.json" '"clean_count":'
require_contains "${tmp}/sync-clean.json" '"admin/five-advisor-review"'

chmod 0644 "${skillsdex_home}/manifest.json"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" doctor --fix-permissions --strict >"${tmp}/doctor-strict.out"
require_contains "${tmp}/doctor-strict.out" "Fixed manifest file permissions: ${skillsdex_home}/manifest.json"
require_contains "${tmp}/doctor-strict.out" "Doctor strict check: passed"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" doctor --refresh --strict >"${tmp}/doctor-refresh-strict.out"
require_contains "${tmp}/doctor-refresh-strict.out" "Source refresh:"
require_contains "${tmp}/doctor-refresh-strict.out" "Doctor strict check: passed"

payload="${tmp}/update.json"
cat >"$payload" <<'JSON'
{
  "owner": "admin",
  "slug": "five-advisor-review",
  "name": "Five Advisor Review",
  "description": "Use when you need a decision, plan, proposal, or idea stress-tested from multiple viewpoints before choosing a concrete next step.",
  "short_description": "Stress-test decisions with five shuffled advisors",
  "recommended_default": true,
  "pinned_install": false,
  "body": "# Five Advisor Review\n\nRemote update from e2e first-run smoke.\n"
}
JSON
curl -fsS -X PUT -H "Authorization: Bearer ${token}" -H "Content-Type: application/json" --data-binary "@${payload}" "${url}/v1/skills/admin/five-advisor-review" -o "${tmp}/update.out"

printf '# Local edit\n' >"$skill_file"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sync --dry-run >"${tmp}/sync-dry-run.out" 2>"${tmp}/sync-dry-run.err"
require_contains "${tmp}/sync-dry-run.out" "local file modified; would require confirmation"
require_contains "${tmp}/sync-dry-run.out" "modified	SKILL.md"
require_contains "${tmp}/sync-dry-run.out" "skillsdex sync --yes"
set +e
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sync --dry-run --strict --json >"${tmp}/sync-dry-run.json" 2>"${tmp}/sync-dry-run-json.err"
sync_json_status=$?
set -e
if [ "$sync_json_status" -eq 0 ]; then
  fail "sync --dry-run --strict --json should report pending updates"
fi
require_contains "${tmp}/sync-dry-run.json" '"healthy": false'
require_contains "${tmp}/sync-dry-run.json" '"pending_count": 1'
require_contains "${tmp}/sync-dry-run.json" '"status": "local-modified"'
require_contains "${tmp}/sync-dry-run.json" '"skillsdex sync --yes"'
grep -Fq "# Local edit" "$skill_file" || fail "dry-run changed local edit"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sync --no >"${tmp}/sync-no.out" 2>"${tmp}/sync-no.err"
grep -Fq "# Local edit" "$skill_file" || fail "--no changed local edit"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" sync --yes >"${tmp}/sync-yes.out" 2>"${tmp}/sync-yes.err"
require_contains "${tmp}/sync-yes.out" "updated to revision"
grep -Fq "Remote update from e2e first-run smoke" "$skill_file" || fail "--yes did not apply remote update"

backup="${tmp}/skillsdex-backup.zip"
HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" backup export --out "$backup" >"${tmp}/backup.out"
test -s "$backup" || fail "backup export did not write data"
require_contains "${tmp}/backup.out" "Exported backup to ${backup}"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" backup inspect --file "$backup" >"${tmp}/backup-inspect.out"
require_contains "${tmp}/backup-inspect.out" "Backup is valid: ${backup}"
require_contains "${tmp}/backup-inspect.out" "admin/five-advisor-review"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" backup inspect --file "$backup" --json >"${tmp}/backup-inspect.json"
require_contains "${tmp}/backup-inspect.json" '"valid": true'
require_contains "${tmp}/backup-inspect.json" '"skill_count":'
require_contains "${tmp}/backup-inspect.json" '"admin/five-advisor-review"'

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" backup restore --file "$backup" --dry-run >"${tmp}/restore-dry-run.out"
require_contains "${tmp}/restore-dry-run.out" "Restore dry-run: backup was not uploaded."
require_contains "${tmp}/restore-dry-run.out" "admin/five-advisor-review"

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" backup restore --file "$backup" --dry-run --json >"${tmp}/restore-dry-run.json"
require_contains "${tmp}/restore-dry-run.json" '"restore_dry_run": true'
require_contains "${tmp}/restore-dry-run.json" '"admin/five-advisor-review"'

HOME="$home" SKILLSDEX_HOME="$skillsdex_home" "$cli_bin" backup restore --file "$backup" >"${tmp}/restore.out"
require_contains "${tmp}/restore.out" "admin/five-advisor-review"

echo "First-run e2e smoke passed on ${url}"
