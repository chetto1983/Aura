#!/bin/sh
# Tempo's local backend writes a block's meta file without fsync or an atomic rename, so a power
# loss can leave it empty. One unreadable meta aborts the poll of the whole tenant (2.9.4 and
# 3.1.0 alike, tempodb/blocklist/poller.go): no stored trace is found and retention stops. This
# removes such blocks before Tempo starts. An empty meta younger than ten minutes may still be
# being written by a running Tempo, so it is left alone.
set -eu

blocks="${1:-/var/tempo/blocks}"
[ -d "$blocks" ] || exit 0

find "$blocks" -mindepth 3 -maxdepth 3 -type f \( -name meta.json -o -name meta.compacted.json \) \
  -empty -mmin +10 |
  while IFS= read -r meta; do
    block="$(dirname "$meta")"
    [ -d "$block" ] || continue
    printf '%s\n' "aura tempo repair: removing $block, its $(basename "$meta") is empty" >&2
    rm -rf "$block"
  done
