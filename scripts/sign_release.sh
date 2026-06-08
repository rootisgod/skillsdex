#!/usr/bin/env sh
set -eu

dist="${1:-dist}"

if [ ! -d "$dist" ]; then
  echo "Release directory not found: ${dist}" >&2
  exit 1
fi
if [ ! -f "${dist}/SHA256SUMS" ]; then
  echo "Missing ${dist}/SHA256SUMS; run task release first." >&2
  exit 1
fi
if ! command -v minisign >/dev/null 2>&1; then
  echo "minisign is required for signed releases." >&2
  exit 1
fi

cleanup_key=""
key_file="${MINISIGN_KEY_FILE:-}"
if [ -z "$key_file" ] && [ -n "${MINISIGN_KEY:-}" ]; then
  cleanup_key="$(mktemp)"
  printf '%s\n' "$MINISIGN_KEY" >"$cleanup_key"
  chmod 0600 "$cleanup_key"
  key_file="$cleanup_key"
fi
if [ -z "$key_file" ]; then
  echo "Set MINISIGN_KEY_FILE or MINISIGN_KEY before signing releases." >&2
  exit 1
fi
trap 'if [ -n "$cleanup_key" ]; then rm -f "$cleanup_key"; fi' EXIT

sign_one() {
  file="$1"
  minisign -Sm "$file" -s "$key_file"
}

sign_one "${dist}/SHA256SUMS"
for file in "${dist}"/*.tar.gz; do
  [ -e "$file" ] || continue
  sign_one "$file"
done

echo "Signed release artifacts in ${dist}"
