#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
script="$PWD/docker/aura/aura-tempo-blocks-repair.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() {
  echo "tempo-blocks-repair-test: $*" >&2
  exit 1
}

blocks="$work/blocks"
tenant="$blocks/single-tenant"
old="$(date -d '-20 minutes' '+%Y%m%d%H%M.%S')"

# block <name> <meta file> <content> <age>: a block directory as Tempo's local backend lays it out.
block() {
  mkdir -p "$tenant/$1"
  printf 'parquet' >"$tenant/$1/data.parquet"
  printf '%s' "$3" >"$tenant/$1/$2"
  [ "$4" = fresh ] || touch -t "$old" "$tenant/$1/$2"
}

block healthy meta.json '{"blockID":"healthy"}' old
block compacted meta.compacted.json '{"blockID":"compacted"}' old
block empty-meta meta.json '' old
block empty-compacted meta.compacted.json '' old
block being-written meta.json '' fresh
mkdir -p "$tenant/no-meta"
printf 'parquet' >"$tenant/no-meta/data.parquet"

out="$(sh "$script" "$blocks" 2>&1)" || fail "exited non-zero: $out"

for kept in healthy compacted being-written no-meta; do
  [ -d "$tenant/$kept" ] || fail "removed $kept"
done
for removed in empty-meta empty-compacted; do
  [ ! -e "$tenant/$removed" ] || fail "kept $removed"
  grep -q "removing $tenant/$removed, its meta" <<<"$out" || fail "did not log $removed"
done

out="$(sh "$script" "$work/missing" 2>&1)" || fail "a fresh volume with no blocks directory failed: $out"
[ -z "$out" ] || fail "a fresh volume logged: $out"

echo "tempo-blocks-repair-test: ok"
