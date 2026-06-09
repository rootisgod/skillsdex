#!/bin/sh
set -eu

db="${SKILLSDEX_DB:-/var/lib/skillsdex/skillsdex.db}"
backup_dir="${SKILLSDEX_BACKUP_DIR:-/var/backups/skillsdex}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
tmp_db="${backup_dir}/skillsdex-${stamp}.db"
backup="${backup_dir}/skillsdex-${stamp}.zip"

command -v sqlite3 >/dev/null

install -d -m 0700 "$backup_dir"
trap 'rm -f "$tmp_db"' EXIT INT TERM
sqlite3 "$db" ".backup '${tmp_db}'"
test -s "$tmp_db"
TMPDIR="$backup_dir"
export TMPDIR
if command -v zip >/dev/null; then
	zip -q -9 -j "$backup" "$tmp_db"
else
	command -v python3 >/dev/null
	python3 - "$backup" "$tmp_db" "$(basename "$tmp_db")" <<'PY'
import sys
import zipfile

backup, tmp_db, member = sys.argv[1:4]
with zipfile.ZipFile(backup, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    archive.write(tmp_db, member)
PY
fi
test -s "$backup"
printf '%s\n' "$backup"
