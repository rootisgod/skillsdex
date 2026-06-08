#!/bin/sh
set -eu

profile="${TMPDIR:-/tmp}/skillsdex-critical-cover.out"
report="${TMPDIR:-/tmp}/skillsdex-critical-cover.txt"

go test -coverprofile="$profile" ./... >/dev/null
go tool cover -func="$profile" >"$report"

check_func() {
  name="$1"
  min="$2"
  label="$3"
  value="$(awk -v fn="$name" '$2 == fn { gsub("%", "", $3); print $3 }' "$report" | tail -n 1)"
  if [ -z "$value" ]; then
    echo "coverage-critical: missing function ${name} (${label})" >&2
    return 1
  fi
  awk -v got="$value" -v want="$min" -v fn="$name" -v label="$label" 'BEGIN {
    if (got + 0 < want + 0) {
      printf "coverage-critical: %s (%s) coverage %.1f%% below %.1f%%\n", fn, label, got, want > "/dev/stderr"
      exit 1
    }
  }'
  printf "%-32s %6.1f%% >= %5.1f%%  %s\n" "$name" "$value" "$min" "$label"
}

check_func userForAPIToken 90 "auth token lookup"
check_func validCSRF 80 "web CSRF gate"
check_func importSkillInputsFromGitHub 80 "GitHub folder import"
check_func refreshImportedSkill 80 "source refresh"
check_func inspectSkillsDexBackup 90 "backup inspection"
check_func runBackup 70 "CLI backup flow"
check_func installSkillResponse 80 "install and overwrite protection"
check_func runSync 80 "managed sync"
check_func syncPackageChanges 90 "full-folder sync diffing"
check_func skillPackageFiles 75 "package file normalization"
check_func runSetup 70 "first-run setup"
check_func printSetupPlanJSON 90 "CLI setup JSON"
check_func runRefreshAllJSON 55 "CLI refresh JSON"
check_func safeZipDestination 70 "zip traversal protection"

echo "Critical coverage gates passed."
