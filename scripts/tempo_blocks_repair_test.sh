#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
script="$PWD/docker/aura/aura-tempo-blocks-repair.sh"
work="$(mktemp -d)"
trap 'chmod -R u+rwx "$work"; rm -rf "$work"' EXIT

fail() {
  echo "tempo-blocks-repair-test: $*" >&2
  exit 1
}

# The scan-failure and removal-failure cases rely on permissions that root ignores.
[ "$(id -u)" -ne 0 ] || fail "run as a non-root user"

now="$(date +%s)"
stamp() { date -d "@$1" '+%Y%m%d%H%M.%S'; }

# block <tenant dir> <name> <meta file> <content> <mtime epoch>: a block as Tempo's local backend lays it out.
block() {
  mkdir -p "$1/$2"
  printf 'parquet' >"$1/$2/data.parquet"
  printf '%s' "$4" >"$1/$2/$3"
  touch -t "$(stamp "$5")" "$1/$2/$3"
}

# repair <blocks dir> <boot epoch>: runs the script, which must succeed whatever it meets.
repair() {
  out="$(sh "$script" "$1" "$2" 2>&1)" || fail "exited non-zero: $out"
}

# Ten minutes guard a meta that a running Tempo may still be writing.
blocks="$work/age/blocks"
tenant="$blocks/single-tenant"
block "$tenant" healthy meta.json '{"blockID":"healthy"}' $((now - 1200))
block "$tenant" compacted meta.compacted.json '{"blockID":"compacted"}' $((now - 1200))
block "$tenant" empty-meta meta.json '' $((now - 1200))
block "$tenant" empty-compacted meta.compacted.json '' $((now - 1200))
block "$tenant" being-written meta.json '' "$now"
mkdir -p "$tenant/no-meta"
printf 'parquet' >"$tenant/no-meta/data.parquet"
repair "$blocks" 1
for kept in healthy compacted being-written no-meta; do
  [ -d "$tenant/$kept" ] || fail "removed $kept"
done
for removed in empty-meta empty-compacted; do
  [ ! -e "$tenant/$removed" ] || fail "kept $removed"
  grep -q "removed $tenant/$removed, its meta" <<<"$out" || fail "did not log $removed"
done

# A meta written before this boot lost its writer with the power, however young it is.
blocks="$work/boot/blocks"
tenant="$blocks/single-tenant"
block "$tenant" before-boot meta.json '' $((now - 120))
block "$tenant" after-boot meta.json '' $((now - 30))
repair "$blocks" $((now - 60))
[ ! -e "$tenant/before-boot" ] || fail "kept an empty meta written before the boot"
[ -d "$tenant/after-boot" ] || fail "removed an empty meta written after the boot"

# A scan that cannot read part of the volume still repairs the rest, and says what it skipped.
blocks="$work/scan/blocks"
block "$blocks/readable" empty meta.json '' $((now - 1200))
mkdir -p "$blocks/unreadable"
chmod 000 "$blocks/unreadable"
repair "$blocks" 1
[ ! -e "$blocks/readable/empty" ] || fail "a failed scan stopped the repair of a readable tenant"
grep -q "scanning $blocks failed" <<<"$out" || fail "a failed scan was not logged: $out"

# A block it cannot remove is logged, and does not fail the start.
blocks="$work/remove/blocks"
block "$blocks/single-tenant" stuck meta.json '' $((now - 1200))
chmod 555 "$blocks/single-tenant"
repair "$blocks" 1
[ -d "$blocks/single-tenant/stuck" ] || fail "removed a block from a read-only tenant directory"
grep -q "could not remove $blocks/single-tenant/stuck" <<<"$out" || fail "a failed removal was not logged: $out"

repair "$work/missing" 1
[ -z "$out" ] || fail "a fresh volume with no blocks directory logged: $out"

echo "tempo-blocks-repair-test: ok"
