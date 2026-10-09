#!/bin/sh
# Tempo's local backend writes a block's meta file without fsync or an atomic rename, so a power
# loss can leave it empty. One unreadable meta aborts the poll of the whole tenant (2.9.4 and
# 3.1.0 alike, tempodb/blocklist/poller.go): no stored trace is found and retention stops. This
# removes such blocks; a running Tempo lists them again on its next poll, without a restart.
#
# It runs on every `compose up`, at boot too, where Docker's restart policy may already have
# started Tempo. An empty meta written before this boot lost its writer with the power; a newer
# one younger than ten minutes may still be being written, so it is left alone.
#
# A failure is logged, never returned: Compose would otherwise refuse to start Tempo, failing the
# `up` that aura.service runs at boot and the one the updater runs on every update.
set -u

blocks="${1:-/var/tempo/blocks}"
boot="${2:-$(awk '/^btime/ { print $2 }' /proc/stat)}"
[ -d "$blocks" ] || exit 0

log() {
  printf '%s\n' "aura tempo repair: $*" >&2
}

stale=$(($(date +%s) - 600))
metas="$(find "$blocks" -mindepth 3 -maxdepth 3 -type f \
  \( -name meta.json -o -name meta.compacted.json \) -empty)" ||
  log "scanning $blocks failed; blocks it could not read were not checked"

printf '%s\n' "$metas" | while IFS= read -r meta; do
  [ -n "$meta" ] || continue
  modified="$(stat -c %Y "$meta" 2>/dev/null)" || continue
  [ "$modified" -lt "$boot" ] || [ "$modified" -lt "$stale" ] || continue
  block="$(dirname "$meta")"
  if rm -rf "$block"; then
    log "removed $block, its $(basename "$meta") was empty"
  else
    log "could not remove $block, whose $(basename "$meta") is empty"
  fi
done
exit 0
