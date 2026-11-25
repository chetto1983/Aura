#!/usr/bin/env bash
#
# Drives deploy/aura-image-update.sh's apply against a stubbed docker. aura mounts its MCP
# sidecars when it turns healthy and cannot remount one whose new image advertises another tool
# set (lab VM, 2026-11-25: the PIM moved after aura, which kept 1 of its 29 tools until an
# operator restarted it). An apply that moves an MCP sidecar's image after aura started must
# restart aura, once even when stale boxes need the same restart; one that moves none must not.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# shellcheck source=/dev/null
source "$repo_root/deploy/aura-image-update.sh"

# Out of scope here: aura_image_update_test.sh, aura_update_consent_test.sh and
# aura_image_home_test.sh drive these.
record_status() { :; }
on_apply_exit() { :; }
update_dir() { printf '%s/update' "$INSTALL_DIR"; }
image_revision() { :; }
move_image_registry() { :; }
# shellcheck disable=SC2034 # read by the sourced apply_update
sync_payload() { UPDATER_CHANGED=0; }
relink_enabled_units() { :; }
retire_env_keys() { :; }
ensure_cloudflared_edge_env() { :; }
bash() { [[ "$1" == scripts/appliance_posture.sh ]] || fail "unexpected bash $*"; }
restart_payload_readers() { :; }
remove_superseded_images() { :; }
remove_orphan_volumes() { :; }
image_id() { echo image-id; }
stale_boxes() { echo "${STALE}"; }
wait_healthy() { echo "wait_healthy $1" >>"$calls"; }

# The stack, as files under the case's directory: absent-<svc> is a service not running here,
# moved-<svc> one whose container now runs its new image. A whole-stack `up` moves the services
# UP_MOVES names and starts those UP_STARTS names; update_sidecar moves those SIDECAR_MOVES names.
update_sidecar() { [[ " ${SIDECAR_MOVES} " != *" $1 "* ]] || touch "moved-$1"; }
docker() {
  echo "docker $*" >>"$calls"
  local svc
  case "$*" in
    'compose ps -q '*) [[ -e "absent-$4" ]] || echo "$4-id" ;;
    'inspect --format {{.Image}} '*)
      svc="${4%-id}"
      if [[ -e "moved-$svc" ]]; then echo "$svc-new"; else echo "$svc-old"; fi
      ;;
    'compose up -d --pull missing')
      for svc in ${UP_MOVES}; do touch "moved-$svc"; done
      for svc in ${UP_STARTS}; do rm -f "absent-$svc"; done
      ;;
    'compose up -d --pull missing aura' | 'compose exec -T aura aura version' | 'image prune --force') ;;
    'compose restart aura' | 'rm -f aura-box-'* | 'rm -f aura-egress-'*) ;;
    *) fail "unexpected docker $*" ;;
  esac
}

# apply_case <name>: one apply in a fresh install dir, its output in out and docker calls in
# calls there. PAYLOAD=1 installs a payload the stack has not been brought up on, PINS=0 leaves
# the sandbox images unpinned, ABSENT names the services not running when the tick starts.
apply_case() {
  INSTALL_DIR="$fixture/$1"
  mkdir -p "$INSTALL_DIR/update"
  cd "$INSTALL_DIR"
  calls="$INSTALL_DIR/calls"
  : >"$calls"
  : >.env
  [[ "${PINS:-1}" == 0 ]] || printf '%s\n' AURA_SANDBOX_IMAGE=box:edge AURA_SANDBOX_EGRESS_IMAGE=egress:edge >.env
  [[ "${PAYLOAD:-0}" != 1 ]] || echo 'sum  compose.yaml' >payload_manifest.txt
  local svc
  for svc in ${ABSENT:-}; do touch "absent-$svc"; done
  apply_update >out
}
restarts() { grep -c '^docker compose restart aura$' "$calls" || true; }
# The restart of aura is followed by waiting for it to turn healthy again.
awaited() { [[ "$(grep -A1 -x 'docker compose restart aura' "$calls" | tail -n 1)" == 'wait_healthy aura' ]]; }
said() { grep 'restart' out | grep -v '^sandbox:' || true; }

# (1) The incident: the payload's whole-stack `up` moved the PIM after aura had mounted it.
(
  PAYLOAD=1 UP_MOVES=aura-pim-mcp UP_STARTS='' SIDECAR_MOVES='' STALE=''
  apply_case pim-after-aura
  [[ "$(restarts)" == 1 ]] || fail "a PIM moved after aura mounted it restarted aura $(restarts) times: $(cat out)"
  awaited || fail "the restart of aura was not awaited: $(cat "$calls")"
  [[ "$(said)" == *aura-pim-mcp* ]] || fail "the restart did not name the PIM: $(cat out)"
)
# A host with no sandbox pins has no boxes to refresh, and still has a PIM to remount.
(
  PINS=0 PAYLOAD=1 UP_MOVES=aura-pim-mcp UP_STARTS='' SIDECAR_MOVES='' STALE=''
  apply_case no-sandbox-pins
  grep -qx 'sandbox: no images pinned in .env; skipped.' out || fail "the unpinned sandbox was not reported: $(cat out)"
  [[ "$(restarts)" == 1 ]] || fail "without sandbox pins a moved PIM restarted aura $(restarts) times: $(cat out)"
)
echo "ok: an MCP sidecar moved after aura mounted it restarts aura once"

# (2) Nothing moved: no restart, however many sidecars the apply touched.
(
  PAYLOAD=1 UP_MOVES='' UP_STARTS='' SIDECAR_MOVES='' STALE=''
  apply_case nothing-moved
  [[ "$(restarts)" == 0 ]] || fail "an apply that moved no MCP sidecar restarted aura: $(cat out)"
  [[ -z "$(said)" ]] || fail "an apply that moved no MCP sidecar announced a restart: $(said)"
)
# With its memory MCP down the stack comes up whole, the PIM recreated on its new image
# together with aura: aura mounted that image, so there is nothing to remount. What aura
# mounted is read once it is healthy, not when the tick starts.
(
  PAYLOAD=0 ABSENT=arcadedb-mcp UP_MOVES=aura-pim-mcp UP_STARTS=arcadedb-mcp SIDECAR_MOVES='' STALE=''
  apply_case stack-down
  grep -qx 'docker compose up -d --pull missing' "$calls" || fail "the stack-down case did not bring the stack up: $(cat "$calls")"
  [[ "$(restarts)" == 0 ]] || fail "a PIM that moved before aura mounted it restarted aura: $(cat out)"
)
echo "ok: an apply that moves no MCP sidecar after aura mounted it never restarts aura"

# (3) Stale boxes and a moved sidecar want the same restart: aura restarts once for both.
(
  PAYLOAD=0 UP_MOVES='' UP_STARTS='' SIDECAR_MOVES=arcadedb-mcp STALE=aura-box-u1
  apply_case boxes-and-sidecar
  grep -qx 'docker rm -f aura-box-u1' "$calls" || fail "the stale box was not removed: $(cat "$calls")"
  [[ "$(restarts)" == 1 ]] || fail "stale boxes and a moved sidecar restarted aura $(restarts) times: $(cat out)"
  awaited || fail "the restart of aura was not awaited: $(cat "$calls")"
  [[ "$(said)" == *arcadedb-mcp* ]] || fail "the restart did not name the moved sidecar: $(cat out)"
)
# Stale boxes alone still restart aura once: its idle reaper holds the removed boxes' IDs.
(
  PAYLOAD=0 UP_MOVES='' UP_STARTS='' SIDECAR_MOVES='' STALE=aura-box-u1
  apply_case boxes-only
  [[ "$(restarts)" == 1 ]] || fail "stale boxes alone restarted aura $(restarts) times: $(cat out)"
  [[ -z "$(said)" ]] || fail "stale boxes alone named an MCP sidecar: $(said)"
)
echo "ok: stale boxes and a moved MCP sidecar restart aura once, not twice"

# (4) A sidecar not running when aura started was never mounted: starting it now (the payload
# `up` enabling it) is no reason to restart, while the PIM moving next to it still is.
(
  PAYLOAD=1 ABSENT=whatsapp UP_MOVES='' UP_STARTS=whatsapp SIDECAR_MOVES=aura-pim-mcp STALE=''
  apply_case absent-sidecar
  [[ "$(restarts)" == 1 ]] || fail "with whatsapp absent and the PIM moved, aura restarted $(restarts) times: $(cat out)"
  [[ "$(said)" == *aura-pim-mcp* && "$(said)" != *whatsapp* ]] ||
    fail "the restart named the wrong sidecars: $(said)"
)
echo "ok: an MCP sidecar not running when aura started is ignored"
