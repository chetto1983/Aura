#!/usr/bin/env bash
#
# Drives deploy/aura-image-update.sh's payload sync against a stubbed docker. The payload an
# edge image carries must land on the appliance exactly where it differs, the installed copies
# of the updater and its units must follow it, and a payload that does not match its own
# manifest -- or an image that carries none -- must change nothing at all.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

calls="$fixture/calls.log"
: >"$calls"
image_payload="$fixture/image-payload"

# `docker cp <container>:<dir>/. <dst>` copies the directory's contents into dst; an image
# without that directory makes docker cp fail, which is what an image built before the
# payload stage existed does.
# shellcheck disable=SC2329 # called by the sourced updater, and redefined for the image cleanup below
docker() {
  echo "docker $*" >>"$calls"
  case "$1" in
    create) echo fake-container ;;
    cp)
      [[ -d "$image_payload" ]] || {
        echo "Error: Could not find the file /usr/share/aura/payload in container fake-container" >&2
        return 1
      }
      cp -R "$image_payload/." "$3"
      ;;
    rm) ;;
    *) fail "unexpected docker $*" ;;
  esac
}
systemctl() { echo "systemctl $*" >>"$calls"; }

# shellcheck source=/dev/null
source "$repo_root/deploy/aura-image-update.sh"
declare -F sync_payload >/dev/null || fail "sync_payload undefined after sourcing the updater"
[[ ! -s "$calls" ]] || fail "sourcing the updater ran it: $(cat "$calls")"
echo "ok: the updater sources without running"

write() {
  mkdir -p "$(dirname "$1")"
  printf '%s' "$2" >"$1"
}
content() { cat "$1"; }
mode() { stat -c %a "$1"; }
seal() {
  local sums
  sums="$(cd "$image_payload" && find . -type f ! -name payload_manifest.txt | sed 's|^\./||' | LC_ALL=C sort |
    while read -r rel; do sha256sum "$rel"; done)"
  printf '%s\n' "$sums" >"$image_payload/payload_manifest.txt"
}
backups() { find "$INSTALL_DIR/backups" -mindepth 1 -maxdepth 1 -type d -name 'payload-*' 2>/dev/null | wc -l; }

INSTALL_DIR="$fixture/opt/aura"
UPDATER_BIN="$fixture/sbin/aura-image-update.sh"
SYSTEMD_DIR="$fixture/systemd"
export TMPDIR="$fixture/tmp"
mkdir -p "$TMPDIR"

write "$image_payload/compose.yaml" "compose v2"
write "$image_payload/deploy/aura-image-update.sh" "updater v2"
write "$image_payload/deploy/aura-image-update.timer" "timer v2"
write "$image_payload/deploy/aura-image-update.service" "update service v1"
write "$image_payload/deploy/aura.service" "service v1"
write "$image_payload/observability/tempo/tempo.yml" "tempo v1"
write "$image_payload/scripts/garage_bootstrap.sh" "bootstrap v1"
seal

write "$INSTALL_DIR/compose.yaml" "compose v1"
write "$INSTALL_DIR/deploy/aura-image-update.sh" "updater v1"
write "$INSTALL_DIR/deploy/aura-image-update.timer" "timer v2"
write "$INSTALL_DIR/deploy/aura-image-update.service" "update service v1"
write "$INSTALL_DIR/deploy/aura.service" "service v1"
write "$INSTALL_DIR/scripts/garage_bootstrap.sh" "bootstrap v1"
write "$UPDATER_BIN" "updater v1"
# The installed unit can lag the /opt/aura copy (a hand-copied payload never reached
# systemd); it is compared against the payload itself, not against its sibling.
write "$SYSTEMD_DIR/aura-image-update.timer" "timer v1"
write "$SYSTEMD_DIR/aura.service" "service v1"
# The stack was last brought up on an older payload; a changed file must invalidate that
# record so the whole stack is re-applied, even if this tick dies before doing it.
write "$INSTALL_DIR/$APPLIED_MANIFEST" "manifest of the payload before"

sync_payload ghcr.io/example/aura:edge >"$fixture/sync1.out"

[[ "$UPDATER_CHANGED" == 1 ]] || fail "a new updater did not report UPDATER_CHANGED"
cmp -s "$image_payload/payload_manifest.txt" "$INSTALL_DIR/payload_manifest.txt" || fail "the payload manifest was not installed beside it"
[[ ! -e "$INSTALL_DIR/$APPLIED_MANIFEST" ]] || fail "a changed payload left the stack recorded as up to date"
[[ "$(content "$INSTALL_DIR/compose.yaml")" == "compose v2" ]] || fail "compose.yaml was not replaced"
[[ "$(content "$INSTALL_DIR/observability/tempo/tempo.yml")" == "tempo v1" ]] || fail "a payload file new to this host was not created"
[[ "$(content "$INSTALL_DIR/deploy/aura-image-update.sh")" == "updater v2" ]] || fail "the /opt/aura updater copy was not replaced"
[[ "$(content "$UPDATER_BIN")" == "updater v2" ]] || fail "the installed updater was not replaced"
[[ "$(mode "$UPDATER_BIN")" == 755 ]] || fail "the installed updater is not executable: $(mode "$UPDATER_BIN")"
[[ "$(mode "$INSTALL_DIR/deploy/aura-image-update.sh")" == 755 ]] || fail "an installed script is not 0755"
[[ "$(mode "$INSTALL_DIR/compose.yaml")" == 644 ]] || fail "an installed config file is not 0644: sidecars read it as non-root"
[[ "$(content "$SYSTEMD_DIR/aura-image-update.timer")" == "timer v2" ]] || fail "a stale installed unit was not replaced"
[[ ! -e "$SYSTEMD_DIR/aura-image-update.service" ]] || fail "a unit this host never installed was created"
grep -qx 'systemctl daemon-reload' "$calls" || fail "a replaced unit was not followed by daemon-reload"
grep -q '^docker rm ' "$calls" || fail "the extraction container was left behind"
[[ "$(backups)" == 1 ]] || fail "expected one backup directory, found $(backups)"
backup="$(find "$INSTALL_DIR/backups" -mindepth 1 -maxdepth 1 -type d)"
[[ "$(content "$backup/compose.yaml")" == "compose v1" ]] || fail "the replaced compose.yaml was not backed up"
[[ "$(content "$backup/deploy/aura-image-update.sh")" == "updater v1" ]] || fail "the replaced updater was not backed up"
[[ ! -e "$backup/scripts/garage_bootstrap.sh" ]] || fail "an unchanged file was backed up"
[[ -z "$(ls -A "$TMPDIR")" ]] || fail "the extracted payload was left in $TMPDIR"
echo "ok: a differing payload is installed, backed up, and reaches the updater and its units"

: >"$calls"
cp "$INSTALL_DIR/payload_manifest.txt" "$INSTALL_DIR/$APPLIED_MANIFEST"
sync_payload ghcr.io/example/aura:edge >"$fixture/sync2.out"
[[ "$UPDATER_CHANGED" == 0 ]] || fail "an identical payload reported a new updater"
! grep -q 'updated' "$fixture/sync2.out" || fail "an identical payload rewrote files: $(cat "$fixture/sync2.out")"
! grep -q 'systemctl' "$calls" || fail "an identical payload reloaded systemd"
[[ "$(backups)" == 1 ]] || fail "an identical payload wrote a backup"
cmp -s "$INSTALL_DIR/payload_manifest.txt" "$INSTALL_DIR/$APPLIED_MANIFEST" || fail "an identical payload invalidated the applied record"
echo "ok: an identical payload changes nothing"

write "$image_payload/compose.yaml" "compose v3"
if sync_payload ghcr.io/example/aura:edge 2>"$fixture/corrupt.err"; then
  fail "a payload that does not match its own manifest was accepted"
fi
[[ "$(content "$INSTALL_DIR/compose.yaml")" == "compose v2" ]] || fail "a refused payload still replaced compose.yaml"
[[ "$(backups)" == 1 ]] || fail "a refused payload wrote a backup"
[[ -z "$(ls -A "$TMPDIR")" ]] || fail "a refused payload was left in $TMPDIR"
echo "ok: a payload that does not match its manifest is refused and installs nothing"

rm -rf "$image_payload"
: >"$calls"
if sync_payload ghcr.io/example/aura:edge 2>"$fixture/missing.err"; then
  fail "an image without a payload was accepted"
fi
grep -q '^docker rm ' "$calls" || fail "the extraction container was left behind after a failed copy"
[[ "$(content "$INSTALL_DIR/compose.yaml")" == "compose v2" ]] || fail "an image without a payload changed compose.yaml"
echo "ok: an image without a payload is refused, never half-applied"

: >"$calls"
sync_payload ""
[[ "$UPDATER_CHANGED" == 0 ]] || fail "no configured image reported a new updater"
[[ ! -s "$calls" ]] || fail "no configured image still called docker: $(cat "$calls")"
echo "ok: a host with no AURA_IMAGE skips the sync"

# Updates land many times a day on the edge channel, so backups must not pile up: only the
# payload the latest change replaced is kept, and nothing else under backups/ is touched.
mkdir -p "$INSTALL_DIR/backups/payload-20000101T000000Z" "$INSTALL_DIR/backups/restore-drill"
write "$image_payload/compose.yaml" "compose v4"
write "$image_payload/deploy/aura-image-update.sh" "updater v2"
write "$image_payload/deploy/aura-image-update.timer" "timer v2"
write "$image_payload/deploy/aura-image-update.service" "update service v1"
write "$image_payload/deploy/aura.service" "service v1"
write "$image_payload/observability/tempo/tempo.yml" "tempo v1"
write "$image_payload/scripts/garage_bootstrap.sh" "bootstrap v1"
seal
sleep 1 # a distinct backup stamp from the first sync's
sync_payload ghcr.io/example/aura:edge >/dev/null
[[ "$(backups)" == 1 ]] || fail "expected only the latest payload backup, found $(backups)"
[[ ! -e "$INSTALL_DIR/backups/payload-20000101T000000Z" ]] || fail "an older payload backup survived"
latest="$(find "$INSTALL_DIR/backups" -mindepth 1 -maxdepth 1 -type d -name 'payload-*')"
[[ "$(content "$latest/compose.yaml")" == "compose v2" ]] || fail "the kept backup is not the payload this change replaced"
[[ -d "$INSTALL_DIR/backups/restore-drill" ]] || fail "a backup that is not a payload backup was removed"
echo "ok: only the latest payload backup is kept"

# A payload that only ADDS files replaces nothing, so no backup is written -- and a host with
# no backups/ directory at all must still converge instead of failing the tick on it.
saved_install_dir="$INSTALL_DIR"
INSTALL_DIR="$fixture/fresh-host"
mkdir -p "$INSTALL_DIR"
sync_payload ghcr.io/example/aura:edge >/dev/null
[[ "$(content "$INSTALL_DIR/compose.yaml")" == "compose v4" ]] || fail "a host with no backups/ did not receive the payload"
[[ ! -d "$INSTALL_DIR/backups" ]] || fail "a payload that replaced nothing wrote a backup"
INSTALL_DIR="$saved_install_dir"
echo "ok: a payload that only adds files converges without a backups/ directory"

# Keys an earlier installer wrote that no compose file reads any more must leave .env on their
# own: left behind they look like settings an operator can change, and change nothing.
cat >"$INSTALL_DIR/.env" <<'ENV'
POSTGRES_PASSWORD=do-not-print-me
POSTGRES_IMAGE=postgres:18.4-alpine3.24
# POSTGRES_IMAGE used to pin the database here
AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-vulkan
AURA_EMBED_IMAGE_NOTE=a longer key that only starts like a retired one
AURA_EMBED_NGL=99
ENV
chmod 600 "$INSTALL_DIR/.env"
retire_env_keys >"$fixture/retire1.out"
[[ "$(cat "$INSTALL_DIR/.env")" == "POSTGRES_PASSWORD=do-not-print-me
# POSTGRES_IMAGE used to pin the database here
AURA_EMBED_IMAGE_NOTE=a longer key that only starts like a retired one
AURA_EMBED_NGL=99" ]] || fail "retiring keys left .env as: $(cat "$INSTALL_DIR/.env")"
[[ "$(mode "$INSTALL_DIR/.env")" == 600 ]] || fail "retiring keys widened .env to $(mode "$INSTALL_DIR/.env")"
[[ "$(cat "$fixture/retire1.out")" == "env: retired AURA_EMBED_IMAGE POSTGRES_IMAGE removed from .env." ]] ||
  fail "the retired keys were not named in the log: $(cat "$fixture/retire1.out")"
! grep -q 'do-not-print-me\|server-vulkan' "$fixture/retire1.out" || fail "retiring keys printed .env values"
retire_env_keys >"$fixture/retire2.out"
[[ ! -s "$fixture/retire2.out" ]] || fail "a clean .env still reported retirements: $(cat "$fixture/retire2.out")"
echo "ok: keys no compose file reads any more are removed from .env, and nothing else is"

# A pin change leaves the previous image TAGGED, so `docker image prune` never reclaims it and
# every llama.cpp bump would leave hundreds of MB behind. Same-repository images that no
# compose pin names are removed; one a container still uses is refused by docker and kept.
declare -A local_image_ids=(
  ["ghcr.io/ggml-org/llama.cpp:server-vulkan-b10951"]=sha256:llama-new
  ["ghcr.io/ggml-org/llama.cpp:server-vulkan"]=sha256:llama-old
  ["ghcr.io/ggml-org/llama.cpp:server-cuda-b10884"]=sha256:llama-in-use
  ["searxng/searxng:2026.9.13-d4ce87c23"]=sha256:searx-new
  ["searxng/searxng:2026.7.26-b060c780d"]=sha256:searx-old
  ["hwdsl2/whisper-server:latest"]=sha256:whisper
  ["ghcr.io/chetto1983/aura:edge"]=sha256:aura
  ["ghcr.io/chetto1983/aura-sandbox:edge"]=sha256:box
  ["<none>:<none>"]=sha256:dangling
)
compose_images=(
  ghcr.io/ggml-org/llama.cpp:server-vulkan-b10951
  searxng/searxng:2026.9.13-d4ce87c23
  hwdsl2/whisper-server:latest@sha256:whisper
  ghcr.io/chetto1983/aura:edge
)
docker() {
  echo "docker $*" >>"$calls"
  case "$1 ${2:-}" in
    "compose config") printf '%s\n' "${compose_images[@]}" ;;
    "image inspect")
      # With the containerd image store an image's Id is the digest a pin names.
      local ref="${*: -1}"
      case "$ref" in
        *@sha256:*) echo "sha256:${ref##*@sha256:}" ;;
        *) [[ -n "${local_image_ids[$ref]:-}" ]] || return 1; echo "${local_image_ids[$ref]}" ;;
      esac
      ;;
    "images --no-trunc") for ref in "${!local_image_ids[@]}"; do echo "$ref ${local_image_ids[$ref]}"; done ;;
    "rmi "*)
      [[ "$2" != "ghcr.io/ggml-org/llama.cpp:server-cuda-b10884" ]] || {
        echo "Error response from daemon: conflict: image is being used by running container" >&2
        return 1
      }
      ;;
    *) fail "unexpected docker $*" ;;
  esac
}
: >"$calls"
remove_superseded_images >"$fixture/images.out" || fail "a refused removal failed the tick"
removed="$(sed -n 's/^docker rmi //p' "$calls" | LC_ALL=C sort | tr '\n' ' ')"
[[ "$removed" == "ghcr.io/ggml-org/llama.cpp:server-cuda-b10884 ghcr.io/ggml-org/llama.cpp:server-vulkan searxng/searxng:2026.7.26-b060c780d " ]] ||
  fail "removal attempts were: $removed"
grep -q 'server-vulkan superseded' "$fixture/images.out" || fail "a removed image was not reported"
! grep -q 'server-cuda-b10884 superseded' "$fixture/images.out" || fail "an image docker refused was reported as removed"
echo "ok: tagged images no pin names any more are removed, the rest are kept"
