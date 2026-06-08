#!/usr/bin/env sh
set -eu

prefix="${PREFIX:-/usr/local}"
bindir="${BINDIR:-${prefix}/bin}"
src="${1:-./skillsdex}"

if [ ! -f "$src" ]; then
  echo "skillsdex binary not found at: $src" >&2
  echo "Build it first with: go build -o skillsdex ." >&2
  exit 1
fi

mkdir -p "$bindir"
install -m 0755 "$src" "$bindir/skillsdex"
echo "Installed skillsdex to $bindir/skillsdex"
