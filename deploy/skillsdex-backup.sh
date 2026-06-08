#!/bin/sh
set -eu

db="${SKILLSDEX_DB:-/var/lib/skillsdex/skillsdex.db}"
backup_dir="${SKILLSDEX_BACKUP_DIR:-/var/backups/skillsdex}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup="${backup_dir}/skillsdex-${stamp}.db"

install -d -m 0700 "$backup_dir"
sqlite3 "$db" ".backup '${backup}'"
test -s "$backup"
printf '%s\n' "$backup"
