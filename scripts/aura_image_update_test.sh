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
# is-enabled answers "enabled" for the units ENABLED_UNITS names, as systemd would on this host.
systemctl() {
  echo "systemctl $*" >>"$calls"
  [[ "$1" != is-enabled ]] || { [[ " ${ENABLED_UNITS:-} " == *" $2 "* ]] && echo enabled || echo disabled; }
}

# shellcheck source=/dev/null
source "$repo_root/deploy/aura-image-update.sh"
declare -F sync_payload >/dev/null || fail "sync_payload undefined after sourcing the updater"
[[ ! -s "$calls" ]] || fail "sourcing the updater ran it: $(cat "$calls")"
echo "ok: the updater sources without running"

# Exercise main's sidecar dispatch, not a source-text assertion. No Docker mutation is allowed.
(
  export INSTALL_DIR="$fixture/update-dispatch" AURA_IMAGE_UPDATE_LOCK="$fixture/update.lock"
  # main sources the appliance's defaults; on an appliance they name the real INSTALL_DIR, so
  # the test hands it a file of its own, and this one moves the lock.
  export AURA_DEFAULTS_FILE="$fixture/dispatch-defaults"
  printf 'AURA_IMAGE_UPDATE_LOCK=%s\n' "$fixture/lock-from-defaults" >"$AURA_DEFAULTS_FILE"
  mkdir -p "$INSTALL_DIR"
  touch "$INSTALL_DIR/compose.yaml"
  printf 'database state' >"$INSTALL_DIR/postgres-sentinel"
  printf 'projection state' >"$INSTALL_DIR/projection-sentinel"
  # main sources the consent functions from the installed payload; this one decides to apply
  # and records nothing, so the dispatch below is all that runs (aura_update_consent_test.sh
  # drives the real ones).
  mkdir -p "$INSTALL_DIR/deploy"
  cat >"$INSTALL_DIR/deploy/aura-update-consent.sh" <<'LIB'
update_dir() { printf '%s/update' "${INSTALL_DIR}"; }
ensure_update_channel() { mkdir -p "$(update_dir)"; }
decide_update() { return 0; }
load_status() { :; }
record_status() { :; }
on_apply_exit() { :; }
image_revision() { :; }
record_mcp_sidecars() { :; }
rerun_if_asked_meanwhile() { :; }
LIB
  # aura_image_home_test.sh drives the real image-home functions.
  cat >"$INSTALL_DIR/deploy/aura-image-home.sh" <<'LIB'
fetch_image_homes() { echo fetched >>"$INSTALL_DIR/fetched"; }
move_image_registry() { :; }
LIB
  container_image_id() { echo image-id; }
  image_id() { echo image-id; }
  pull_sidecar() { :; }
  pull_sandbox_images() { :; }
  sync_payload() { UPDATER_CHANGED=0; }
  relink_enabled_units() { :; }
  retire_env_keys() { :; }
  wait_healthy() { :; }
  refresh_boxes_and_mounts() { :; }
  remove_superseded_images() { :; }
  update_sidecar() { echo "$1" >>"$fixture/updated-services"; }
  bash() { [[ "$1" == scripts/appliance_posture.sh ]] || fail 'unexpected bash'; }
  docker() {
    case "$*" in
      # A running stack: its memory MCP is up, so aura is replaced alone.
      'compose ps -q arcadedb-mcp') echo memory-id ;;
      'compose pull aura aura-migrate garage-bootstrap'|'compose up -d --pull missing aura'|'compose exec -T aura aura version'|'image prune --force') ;;
      'volume prune --force --filter label=com.docker.volume.anonymous') echo pruned >"$fixture/volume-prune" ;;
      *) fail "unexpected Docker mutation: $*" ;;
    esac
  }
  main
  [[ -e "$fixture/lock-from-defaults" ]] || fail "the updater did not read its defaults from AURA_DEFAULTS_FILE"
  grep -qx aura-cloudflared "$fixture/updated-services" || fail 'cloudflared excluded from update'
  [[ -f "$INSTALL_DIR/fetched" ]] || fail "the tick did not fetch the image homes before deciding"
  # aura.service stops with `compose down`, which orphans every volume an image declares
  # without a compose name. Only anonymous ones may go: the named per-user box workspaces a
  # refresh detaches are recreated by aura and must survive, even on an engine whose bare
  # `volume prune` would still remove named volumes.
  [[ -f "$fixture/volume-prune" ]] || fail 'a finished apply left orphaned anonymous volumes behind'
  [[ "$(cat "$INSTALL_DIR/update/applied-image")" == image-id ]] || fail 'a finished apply did not record the image it applied'
  [[ "$(cat "$INSTALL_DIR/postgres-sentinel")" == 'database state' ]] || fail 'database state changed'
  [[ "$(cat "$INSTALL_DIR/projection-sentinel")" == 'projection state' ]] || fail 'projection state changed'
)

# aura reports ready only once its memory MCP is mounted, and that sidecar starts after aura. On
# a stack that is down, aura alone never turned healthy (lab VM, 2026-10-14: 600 s, then exit 1
# before the stack came up), so the apply brings the whole stack up with it.
for memory in running stopped; do
  (
    INSTALL_DIR="$fixture/apply-$memory"
    mkdir -p "$INSTALL_DIR/update"
    cd "$INSTALL_DIR"
    record_status() { :; }
    on_apply_exit() { :; }
    update_dir() { printf '%s/update' "$INSTALL_DIR"; }
    image_revision() { :; }
    move_image_registry() { :; }
    sync_payload() { UPDATER_CHANGED=0; }
    relink_enabled_units() { echo relink >>steps; }
    retire_env_keys() { :; }
    ensure_cloudflared_edge_env() { :; }
    bash() { [[ "$1" == scripts/appliance_posture.sh ]] || fail 'unexpected bash'; }
    record_mcp_sidecars() { :; }
    container_image_id() { echo image-id; }
    image_id() { echo image-id; }
    update_sidecar() { :; }
    refresh_boxes_and_mounts() { :; }
    remove_superseded_images() { :; }
    remove_orphan_volumes() { :; }
    wait_healthy() { echo "wait_healthy $1" >>steps; }
    docker() {
      case "$*" in
        'compose ps -q arcadedb-mcp') [[ "$memory" == stopped ]] || echo memory-id ;;
        'compose up -d --pull missing aura' | 'compose up -d --pull missing') echo "docker $*" >>steps ;;
        'compose exec -T aura aura version' | 'image prune --force') ;;
        *) fail "unexpected docker $*" ;;
      esac
    }
    apply_update >/dev/null
    if [[ "$memory" == running ]]; then want='docker compose up -d --pull missing aura'; else want='docker compose up -d --pull missing'; fi
    [[ "$(cat steps)" == "relink"$'\n'"$want"$'\n'"wait_healthy aura" ]] ||
      fail "with the memory MCP $memory, the apply ran: $(tr '\n' ';' <steps)"
  )
done
echo "ok: a stack that is down comes up whole, and a running one has aura replaced alone"

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
[[ "$(LC_ALL=C sort "$INSTALL_DIR/$PENDING_PAYLOAD" | tr '\n' ' ')" == "compose.yaml deploy/aura-image-update.sh observability/tempo/tempo.yml " ]] ||
  fail "the changed paths were not recorded for the apply: $(cat "$INSTALL_DIR/$PENDING_PAYLOAD")"
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
[[ "$(wc -l <"$INSTALL_DIR/$PENDING_PAYLOAD")" == 3 ]] || fail "an identical payload recorded changed paths: $(cat "$INSTALL_DIR/$PENDING_PAYLOAD")"
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

# aura.service gained WantedBy=docker.service (2026-10-15), and on an existing host the unit is
# copied by the updater it replaces, which never re-enables it. The links are what count: an
# enabled unit missing one is re-enabled, a linked one is left alone, and a frozen host's
# disabled timer stays disabled.
(
  SYSTEMD_DIR="$fixture/relink"
  write "$SYSTEMD_DIR/aura.service" $'[Install]\nWantedBy=multi-user.target docker.service\n'
  write "$SYSTEMD_DIR/aura-image-update.timer" $'[Install]\nWantedBy=timers.target\n'
  mkdir -p "$SYSTEMD_DIR/multi-user.target.wants"
  ln -s ../aura.service "$SYSTEMD_DIR/multi-user.target.wants/aura.service"
  ENABLED_UNITS="aura.service"
  : >"$calls"
  relink_enabled_units >/dev/null
  [[ "$(grep -c '^systemctl reenable' "$calls")" == 1 ]] && grep -qx 'systemctl reenable aura.service' "$calls" ||
    fail "an enabled unit missing its docker.service link was not re-enabled alone: $(cat "$calls")"
  mkdir -p "$SYSTEMD_DIR/docker.service.wants"
  ln -s ../aura.service "$SYSTEMD_DIR/docker.service.wants/aura.service"
  : >"$calls"
  relink_enabled_units >/dev/null
  ! grep -q '^systemctl reenable' "$calls" || fail "a unit holding every link it names was re-enabled: $(cat "$calls")"
)
echo "ok: an enabled unit missing a link its WantedBy= names is re-enabled, and only that one"

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

# A home no file names, so a default written out by hand cannot pass.
(
  INSTALL_DIR="$fixture/cloudflared"
  IMAGE_HOME=registry.elsewhere/home
  mkdir -p "$INSTALL_DIR"
  printf 'AURA_IMAGE=registry.elsewhere/home/aura:edge\n' >"$INSTALL_DIR/.env"
  ensure_cloudflared_edge_env >/dev/null
  grep -qx "AURA_CLOUDFLARED_IMAGE=registry.elsewhere/home/aura-cloudflared:edge" "$INSTALL_DIR/.env" ||
    fail "the cloudflared default does not follow IMAGE_HOME: $(cat "$INSTALL_DIR/.env")"
)
echo "ok: the cloudflared default names IMAGE_HOME"

# A pin change leaves the previous image behind, and `docker image prune` never reclaims it, so
# every llama.cpp bump would leave hundreds of MB. Same-repository images that no compose pin
# names are removed; one a container still uses is refused by docker, kept and said so.
# The SearXNG pin moved from Docker Hub to ghcr.io (2026-10-07): the Docker Hub tags it
# supersedes are the same image under another registry and must go too.
# With the containerd image store a digest pin (repo:tag@sha256:...) is stored UNTAGGED, listed
# as repo <none>, and `docker rmi repo:<none>` is an invalid reference: such an image goes by its
# ID. Measured on the lab VM (2026-10-11): 8 superseded pins, about 6 GB, the disk at 99%.
local_images=(
  "ghcr.io/ggml-org/llama.cpp server-vulkan-b10951 sha256:llama-new"
  "ghcr.io/ggml-org/llama.cpp server-vulkan sha256:llama-old"
  "ghcr.io/ggml-org/llama.cpp server-cuda-b10884 sha256:llama-in-use"
  "ghcr.io/searxng/searxng 2026.10.4-d48c4b555 sha256:searx-new"
  "searxng/searxng 2026.9.13-d4ce87c23 sha256:searx-hub"
  "searxng/searxng 2026.7.26-b060c780d sha256:searx-old"
  "hwdsl2/whisper-server latest sha256:whisper"
  "ghcr.io/hwdsl2/other-whisper latest sha256:other-whisper"
  "grafana/grafana <none> sha256:grafana-new"
  "grafana/grafana <none> sha256:grafana-old"
  "ghcr.io/chetto1983/aura edge sha256:aura"
  "ghcr.io/chetto1983/aura-sandbox edge sha256:box"
  "<none> <none> sha256:dangling"
)
compose_images=(
  ghcr.io/ggml-org/llama.cpp:server-vulkan-b10951
  ghcr.io/searxng/searxng:2026.10.4-d48c4b555
  hwdsl2/whisper-server:latest@sha256:whisper
  grafana/grafana:13.2.3@sha256:grafana-new
  ghcr.io/chetto1983/aura:edge
)
local_image_id() {
  local repo tag id
  for line in "${local_images[@]}"; do
    read -r repo tag id <<<"$line"
    [[ "$repo:$tag" != "$1" ]] || { echo "$id"; return 0; }
  done
  return 1
}
docker() {
  echo "docker $*" >>"$calls"
  case "$1 ${2:-}" in
    "compose config") printf '%s\n' "${compose_images[@]}" ;;
    "image inspect")
      # With the containerd image store an image's Id is the digest a pin names.
      local ref="${*: -1}"
      case "$ref" in
        *@sha256:*) echo "sha256:${ref##*@sha256:}" ;;
        *) local_image_id "$ref" ;;
      esac
      ;;
    "images --no-trunc")
      local format="${*: -1}" line repo tag id out
      for line in "${local_images[@]}"; do
        read -r repo tag id <<<"$line"
        out="${format//'{{.Repository}}'/$repo}"
        out="${out//'{{.Tag}}'/$tag}"
        echo "${out//'{{.ID}}'/$id}"
      done
      ;;
    "rmi "*)
      [[ "$2" != *'<none>'* ]] || { echo "Error response from daemon: invalid reference format" >&2; return 1; }
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
[[ "$removed" == "ghcr.io/ggml-org/llama.cpp:server-cuda-b10884 ghcr.io/ggml-org/llama.cpp:server-vulkan searxng/searxng:2026.7.26-b060c780d searxng/searxng:2026.9.13-d4ce87c23 sha256:grafana-old " ]] ||
  fail "removal attempts were: $removed"
grep -q 'server-vulkan superseded' "$fixture/images.out" || fail "a removed image was not reported"
grep -q 'grafana/grafana sha256:grafana-old superseded' "$fixture/images.out" ||
  fail "a removed untagged image was not reported by repository and ID: $(cat "$fixture/images.out")"
! grep -q 'server-cuda-b10884 superseded' "$fixture/images.out" || fail "an image docker refused was reported as removed"
grep -q 'server-cuda-b10884 kept: .*being used by running container' "$fixture/images.out" ||
  fail "an image docker refused was not reported with docker's reason: $(cat "$fixture/images.out")"
echo "ok: images no pin names any more are removed, untagged ones by ID, and a refusal is reported"

# After the bridge moved a host, the ghcr copy of every image is superseded by its GitLab pin:
# the two homes are one repository to the cleanup. The box images live in .env, not compose,
# and are pinned the same way. A ghcr name on the same image ID as its pin (whisper keeps its
# digest) is skipped like any pinned image.
[[ "$(image_repository registry.gitlab.com/chetto1983/aura/aura-caddy:edge)" == "$(image_repository ghcr.io/chetto1983/aura-caddy:edge)" ]] ||
  fail "the two homes are two repositories: $(image_repository registry.gitlab.com/chetto1983/aura/aura-caddy:edge)"
[[ "$(image_repository registry.gitlab.com/chetto1983/aura/aura:edge)" == chetto1983/aura ]] ||
  fail "the GitLab aura is $(image_repository registry.gitlab.com/chetto1983/aura/aura:edge)"
(
  INSTALL_DIR="$fixture/bridge-images"
  mkdir -p "$INSTALL_DIR"
  printf '%s\n' AURA_SANDBOX_IMAGE=registry.gitlab.com/chetto1983/aura/aura-sandbox:edge \
    AURA_SANDBOX_EGRESS_IMAGE=registry.gitlab.com/chetto1983/aura/aura-egress:edge >"$INSTALL_DIR/.env"
  local_images=(
    "registry.gitlab.com/chetto1983/aura/aura-caddy edge sha256:caddy-new"
    "ghcr.io/chetto1983/aura-caddy edge sha256:caddy-old"
    "registry.gitlab.com/chetto1983/aura/aura-sandbox edge sha256:box-new"
    "ghcr.io/chetto1983/aura-sandbox edge sha256:box-old"
    "registry.gitlab.com/chetto1983/aura/aura-egress edge sha256:egress-new"
    "ghcr.io/chetto1983/aura-egress edge sha256:egress-old"
    "registry.gitlab.com/chetto1983/aura/aura-whisper-server <none> sha256:c6c6"
    "ghcr.io/chetto1983/aura-whisper-server <none> sha256:c6c6"
    "aura-sandbox lab-browser-sidecar-20261011-03 sha256:lab-box"
  )
  compose_images=(
    registry.gitlab.com/chetto1983/aura/aura-caddy:edge
    registry.gitlab.com/chetto1983/aura/aura-whisper-server:2026.10.08.1056@sha256:c6c6
  )
  : >"$calls"
  remove_superseded_images >"$fixture/bridge-images.out"
  removed="$(sed -n 's/^docker rmi //p' "$calls" | LC_ALL=C sort | tr '\n' ' ')"
  [[ "$removed" == "ghcr.io/chetto1983/aura-caddy:edge ghcr.io/chetto1983/aura-egress:edge ghcr.io/chetto1983/aura-sandbox:edge " ]] ||
    fail "after the bridge, removal attempts were: $removed"
)
echo "ok: after a move the ghcr copies are superseded by their GitLab pins, the box images too"

# A service reads a bind-mounted payload file only when it starts, and `compose up` does not
# recreate it for a changed file: the services mounting a changed path are restarted, a
# directory mount covers the files under it, and nothing else is touched.
readers="$fixture/readers"
mkdir -p "$readers"
docker() {
  echo "docker $*" >>"$calls"
  case "$*" in
    'compose ps -q') printf '%s\n' garage-id prometheus-id aura-id ;;
    'inspect --format '*)
      echo "garage $INSTALL_DIR/docker/garage/garage.toml"
      echo "prometheus $INSTALL_DIR/observability/prometheus/prometheus.yml $INSTALL_DIR/observability/prometheus/rules"
      echo "aura $INSTALL_DIR/update"
      ;;
    'compose restart --no-deps '*) ;;
    *) fail "unexpected docker $*" ;;
  esac
}
wait_healthy() { echo "$1" >>"$readers/healthy"; }
restarted() { sed -n 's/^docker compose restart --no-deps //p' "$calls"; }
readers_case() { # <pending path>... | none, for no record at all
  : >"$calls"
  rm -f "$readers/healthy" "$PENDING_PAYLOAD"
  [[ "$1" == none ]] || printf '%s\n' "$@" >"$PENDING_PAYLOAD"
  restart_payload_readers >"$readers/out"
}
(
  cd "$readers"
  INSTALL_DIR="$readers"
  printf '%s  %s\n' a compose.yaml b docker/garage/garage.toml c observability/prometheus/prometheus.yml >payload_manifest.txt

  readers_case docker/garage/garage.toml compose.yaml docker/garage/garage.toml
  [[ "$(restarted)" == garage ]] || fail "a changed garage.toml restarted: $(restarted)"
  [[ "$(cat "$readers/healthy")" == garage ]] || fail "the restarted service was not awaited"
  grep -q 'restarted garage, which mount' "$readers/out" || fail "the restart was not reported"

  readers_case observability/prometheus/rules/aura-alerts.yml
  [[ "$(restarted)" == prometheus ]] || fail "a file under a mounted directory restarted: $(restarted)"

  readers_case observability/prometheus/rules-old/aura-alerts.yml compose.yaml deploy/aura.service
  ! grep -q 'restart' "$calls" || fail "paths no service mounts restarted: $(restarted)"

  readers_case none
  [[ "$(restarted)" == "garage prometheus" ]] || fail "without a record, every payload reader was not restarted: $(restarted)"
)
echo "ok: services mounting a changed payload file are restarted, and only those"

# GHCR stalls one request now and then: on the lab VM, 2026-10-10, five pulls ended in
# "timeout awaiting response headers" while the requests around them took 0.3 s. A pull gets
# one more try before its image waits for the next tick.
(
  INSTALL_DIR="$fixture/stalls"
  mkdir -p "$INSTALL_DIR"
  printf '%s\n' AURA_SANDBOX_IMAGE=box:edge AURA_SANDBOX_EGRESS_IMAGE=egress:edge >"$INSTALL_DIR/.env"
  declare -A stalls_left
  docker() {
    echo "docker $*" >>"$calls"
    local ref="${*: -1}"
    case "$1 $2" in
      'pull -q' | 'compose pull')
        if ((${stalls_left[$ref]:-0} > 0)); then
          stalls_left[$ref]=$((stalls_left[$ref] - 1))
          echo "Error response from daemon: net/http: timeout awaiting response headers" >&2
          return 1
        fi
        ;;
      'compose ps') echo "$ref-id" ;;
      *) fail "unexpected docker $*" ;;
    esac
  }
  sleep() { echo "sleep $*" >>"$calls"; }
  pulls() { grep -c "^docker $1\$" "$calls" || true; }

  : >"$calls"
  stalls_left=([box:edge]=1)
  out="$(pull_sandbox_images 2>/dev/null)"
  [[ -z "$out" && "$(pulls 'pull -q box:edge')" == 2 && "$(pulls 'pull -q egress:edge')" == 1 ]] ||
    fail "a box pull that stalled once was not retried: $out $(cat "$calls")"
  grep -qx 'sleep 10' "$calls" || fail "the retry did not wait: $(cat "$calls")"

  : >"$calls"
  stalls_left=([box:edge]=2)
  out="$(pull_sandbox_images 2>/dev/null)"
  [[ "$out" == *'sandbox: pull failed'* && "$(pulls 'pull -q box:edge')" == 2 ]] ||
    fail "a box pull that stalled twice was not skipped after one retry: $out $(cat "$calls")"

  : >"$calls"
  stalls_left=([aura-ingest]=1)
  out="$(pull_sidecar aura-ingest)"
  [[ -z "$out" && "$(pulls 'compose pull aura-ingest')" == 2 ]] ||
    fail "a sidecar pull that stalled once was not retried: $out $(cat "$calls")"
)
echo "ok: a pull the registry stalls gets one more try before it waits for the next tick"
