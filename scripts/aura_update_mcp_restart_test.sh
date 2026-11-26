#!/usr/bin/env bash
#
# Drives deploy/aura-image-update.sh's apply against a stubbed docker that keeps a clock. aura
# mounts its MCP sidecars as it starts and cannot remount one whose new image advertises another
# tool set (lab VM, 2026-11-25: the PIM moved after aura, which kept 1 of its 29 tools until an
# operator restarted it). So an apply that replaces an MCP sidecar's image under a running aura
# restarts aura, once even when stale boxes need the same restart, and one that does not never
# restarts it -- after a boot too, where every sidecar is created after aura. What the sidecars
# ran before the apply outlives a tick that dies, so the next apply still decides.

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
# shellcheck source=/dev/null
source "$repo_root/deploy/aura-update-consent.sh"
eval "$(declare -f stale_boxes | sed '1s/^stale_boxes/updater_stale_boxes/')"

# Out of scope here: aura_image_update_test.sh, aura_update_consent_test.sh and
# aura_image_home_test.sh drive these.
record_status() { :; }
on_apply_exit() { :; }
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
# The real one where the sandbox is unpinned: it must not reach docker at all.
stale_boxes() { if [[ "${PINS}" == 0 ]]; then updater_stale_boxes; else echo "${STALE}"; fi; }
wait_healthy() { echo "wait_healthy $1" >>"$calls"; }

# The stack, as files in the case's directory, on a clock in tenths of a second: created-<svc>
# and started-aura hold times, absent-<svc> marks a service not running here, and blank-<svc>
# one whose inspect comes back empty (gone between ps and inspect); blank-started-aura does the same
# for aura's start time. `up -d aura` recreates aura when AURA_RECREATE=1; the whole-stack `up` recreates, in order, the services UP_RECREATES
# names; update_sidecar recreates those SIDECAR_MOVES names and dies on FAIL_AT, as a sidecar
# that never turns healthy does. `compose ps` drains its stdin, as a docker CLI may: a loop that
# feeds the record on stdin would lose the lines after the first. Applies run on /dev/null.
tick() {
  local t
  t=$(($(cat clock) + 1))
  echo "$t" >clock
  echo "$t"
}
recreate() {
  rm -f "absent-$1"
  tick >"created-$1"
  [[ "$1" != aura ]] || cp created-aura started-aura
}
# docker's RFC3339Nano drops trailing zeros: 420 is 13:00:42Z, 421 is 13:00:42.1Z.
stamp() {
  local s=$(($1 / 10)) f=$(($1 % 10))
  printf '2026-11-25T13:%02d:%02d' $((s / 60)) $((s % 60))
  ((f == 0)) || printf '.%d' "$f"
  echo Z
}
update_sidecar() {
  [[ "$1" != "${FAIL_AT}" ]] || exit 7
  [[ " ${SIDECAR_MOVES} " != *" $1 "* ]] || recreate "$1"
}
docker() {
  echo "docker $*" >>"$calls"
  local svc
  case "$*" in
    'compose ps -q '*)
      cat >/dev/null
      [[ -e "absent-$4" ]] || echo "$4-id"
      ;;
    'inspect --format {{.Image}} '*) echo "image-$(cat "created-${4%-id}")" ;;
    'inspect --format {{.Created}} '*)
      svc="${4%-id}"
      [[ ! -e "blank-$svc" ]] || return 1
      stamp "$(cat "created-$svc")"
      ;;
    'inspect --format {{.State.StartedAt}} aura-id')
      [[ ! -e blank-started-aura ]] || return 1
      stamp "$(cat started-aura)"
      ;;
    'compose up -d --pull missing aura') [[ "${AURA_RECREATE}" != 1 ]] || recreate aura ;;
    'compose up -d --pull missing') for svc in ${UP_RECREATES}; do recreate "$svc"; done ;;
    'compose restart aura') tick >started-aura ;;
    'compose exec -T aura aura version' | 'image prune --force' | 'rm -f aura-box-'* | 'rm -f aura-egress-'*) ;;
    *) fail "unexpected docker $*" ;;
  esac
}

# new_case <name>: a settled stack in a fresh install dir -- every MCP sidecar created at 80.0 s,
# aura created and started at 90.0 s, the clock at 100.0 s. PAYLOAD=1 installs a payload the
# stack has not been brought up on, PINS=0 leaves the sandbox unpinned, ABSENT names services
# not running.
new_case() {
  INSTALL_DIR="$fixture/$1"
  mkdir -p "$INSTALL_DIR/update"
  cd "$INSTALL_DIR"
  calls="$INSTALL_DIR/calls"
  : >.env
  [[ "${PINS}" == 0 ]] || printf '%s\n' AURA_SANDBOX_IMAGE=box:edge AURA_SANDBOX_EGRESS_IMAGE=egress:edge >.env
  [[ "${PAYLOAD:-0}" != 1 ]] || echo 'sum  compose.yaml' >payload_manifest.txt
  echo 1000 >clock
  echo 900 >created-aura && cp created-aura started-aura
  local svc
  for svc in "${MCP_SIDECARS[@]}"; do echo 800 >"created-$svc"; done
  for svc in ${ABSENT:-}; do touch "absent-$svc"; done
}
# booted_case <name>: as new_case, in the order a boot leaves: aura started at 90.0 s, and each
# MCP sidecar created after it (depends_on: aura, service_started), at 90.1 to 90.3 s.
booted_case() {
  new_case "$1"
  local svc t=900
  for svc in "${MCP_SIDECARS[@]}"; do
    t=$((t + 1))
    echo "$t" >"created-$svc"
  done
}
apply() {
  : >"$calls"
  apply_update >out </dev/null
}
# A tick that must die: set -e has to hold inside it, which no `if` or `||` context allows.
apply_dies() {
  local status
  : >"$calls"
  set +e
  (
    set -e
    apply_update >out 2>err </dev/null
  )
  status=$?
  set -e
  ((status != 0)) || fail "$1: the tick did not fail: $(cat out)"
}
restarts() { grep -cx 'docker compose restart aura' "$calls" || true; }
said() { grep '^aura: restarting' out || true; }
# Exactly <n> restarts of aura, each followed by waiting for it to turn healthy again.
expect_restarts() {
  [[ "$(restarts)" == "$1" ]] || fail "$2: aura restarted $(restarts) times: $(cat out)"
  [[ "$(grep -A1 -x 'docker compose restart aura' "$calls" | grep -cx 'wait_healthy aura' || true)" == "$1" ]] ||
    fail "$2: a restart of aura was not awaited: $(cat "$calls")"
}
quiet() {
  AURA_RECREATE=0 UP_RECREATES='' SIDECAR_MOVES='' FAIL_AT='' STALE=''
}
PINS=1
quiet

# (1) The incident: aura replaced alone, then the payload's whole-stack `up` moves the PIM. Once
# restarted, aura is newer than every sidecar: the next apply leaves it alone.
(
  PAYLOAD=1 && new_case pim-after-aura
  AURA_RECREATE=1 UP_RECREATES=aura-pim-mcp && apply
  expect_restarts 1 "a PIM recreated after aura started"
  [[ "$(said)" == 'aura: restarting it to remount aura-pim-mcp, replaced after it started.' ]] ||
    fail "the restart did not name the PIM alone: $(said)"
  quiet && apply
  expect_restarts 0 "the apply after that restart"
)
# A host with no sandbox pins has no boxes to refresh, and still has a PIM to remount.
(
  PINS=0 PAYLOAD=1 && new_case no-sandbox-pins
  AURA_RECREATE=1 UP_RECREATES=aura-pim-mcp && apply
  grep -qx 'sandbox: no images pinned in .env; skipped.' out || fail "the unpinned sandbox was not reported: $(cat out)"
  expect_restarts 1 "without sandbox pins, a PIM recreated after aura started"
)
echo "ok: an MCP sidecar recreated after aura started restarts aura once, and the next apply does not"

# (2) The whole-stack `up` runs when the memory MCP is down. Whether aura is recreated with it or
# not, the PIM it recreates afterwards may be a tool set aura did not mount.
(
  ABSENT=arcadedb-mcp PAYLOAD=0 && new_case stack-down-aura-recreated
  UP_RECREATES='aura arcadedb-mcp aura-pim-mcp' && apply
  grep -qx 'docker compose up -d --pull missing' "$calls" || fail "the stack was not brought up whole: $(cat "$calls")"
  expect_restarts 1 "aura recreated, then the PIM and the memory MCP"
  # The memory MCP was not running when the apply began: aura mounted the one it started.
  [[ "$(said)" == *' remount aura-pim-mcp,'* ]] || fail "the restart named: $(said)"
)
(
  ABSENT=arcadedb-mcp PAYLOAD=0 && new_case stack-down-aura-kept
  UP_RECREATES='arcadedb-mcp aura-pim-mcp' && apply
  expect_restarts 1 "aura kept, the PIM recreated by the whole-stack up"
)
echo "ok: a whole-stack up that recreates an MCP sidecar after aura started restarts aura once"

# (2b) After a boot every sidecar is younger than aura, yet aura mounted each one: an apply that
# replaces none restarts nothing, whether or not it replaces aura, and one that replaces the PIM
# still restarts aura once.
(
  PAYLOAD=1 && booted_case booted-nothing-moved
  apply
  expect_restarts 0 "the first apply after a boot, nothing replaced"
  [[ -z "$(said)" ]] || fail "an apply after a boot announced a restart: $(said)"
)
(
  PAYLOAD=1 && booted_case booted-aura-replaced
  AURA_RECREATE=1 && apply
  expect_restarts 0 "the first apply after a boot, aura replaced alone"
)
(
  PAYLOAD=0 && booted_case booted-pim-moved
  SIDECAR_MOVES=aura-pim-mcp && apply
  expect_restarts 1 "the first apply after a boot, the PIM replaced"
  [[ "$(said)" == *' remount aura-pim-mcp,'* ]] || fail "the restart named: $(said)"
)
echo "ok: after a boot, only an MCP sidecar the apply replaced restarts aura"

# (3) Nothing created after aura started: no restart, though aura itself was replaced, or was
# recreated after the PIM within the same apply, or the sidecar was created in the same second
# aura started (13:00:42Z against 13:00:42.1Z, which a comparison as text gets backwards).
(
  PAYLOAD=1 && new_case aura-replaced-alone
  AURA_RECREATE=1 && apply
  expect_restarts 0 "aura replaced, no sidecar recreated"
  [[ -z "$(said)" ]] || fail "an apply that recreated no MCP sidecar announced a restart: $(said)"
)
(
  PAYLOAD=1 && new_case aura-after-pim
  UP_RECREATES='aura-pim-mcp aura' && apply
  expect_restarts 0 "the PIM recreated, then aura after it in the same apply"
)
(
  PAYLOAD=0 && new_case same-second
  echo 421 >started-aura
  for svc in "${MCP_SIDECARS[@]}"; do echo 420 >"created-$svc"; done
  apply
  expect_restarts 0 "aura started a tenth of a second after its sidecars"
)
echo "ok: an apply that leaves every MCP sidecar older than aura never restarts aura"

# (4) Stale boxes and a recreated sidecar want the same restart: aura restarts once for both.
(
  PAYLOAD=0 && new_case boxes-and-sidecar
  SIDECAR_MOVES=arcadedb-mcp STALE=aura-box-u1 && apply
  grep -qx 'docker rm -f aura-box-u1' "$calls" || fail "the stale box was not removed: $(cat "$calls")"
  expect_restarts 1 "stale boxes and a recreated sidecar"
  [[ "$(said)" == *'remount arcadedb-mcp,'* ]] || fail "the restart did not name the memory MCP: $(said)"
)
# Stale boxes alone still restart aura once: its idle reaper holds the removed boxes' IDs.
(
  PAYLOAD=0 && new_case boxes-only
  STALE=aura-box-u1 && apply
  expect_restarts 1 "stale boxes alone"
  [[ -z "$(said)" ]] || fail "stale boxes alone named an MCP sidecar: $(said)"
)
echo "ok: stale boxes and a recreated MCP sidecar restart aura once, not twice"

# (5) A sidecar not running here is ignored, next to one that still restarts aura.
(
  ABSENT=whatsapp PAYLOAD=1 && new_case absent-sidecar
  SIDECAR_MOVES=aura-pim-mcp && apply
  expect_restarts 1 "with whatsapp absent and the PIM recreated"
  [[ "$(said)" == *aura-pim-mcp* && "$(said)" != *whatsapp* ]] || fail "the restart named the wrong sidecars: $(said)"
)
echo "ok: an MCP sidecar not running here is ignored"

# (6) A tick that dies after the PIM moved, before its restart: the next apply restarts aura,
# although that apply itself moves nothing, and the one after does not.
(
  PAYLOAD=1 && new_case dies-after-pim
  AURA_RECREATE=1 UP_RECREATES=aura-pim-mcp FAIL_AT=caddy && apply_dies "the PIM moved, then caddy never turned healthy"
  expect_restarts 0 "the tick that died"
  quiet && apply
  expect_restarts 1 "the apply after a tick that died with the PIM newer than aura"
  [[ "$(said)" == *aura-pim-mcp* ]] || fail "the retry did not name the PIM: $(said)"
  apply
  expect_restarts 0 "the second apply after the tick that died"
)
echo "ok: a tick that dies before its restart leaves it to the next apply, and only to that one"

# (7) A sidecar whose times docker cannot report fails the tick rather than guess: no restart on
# a time that is not there, and none skipped silently -- the next apply, when one runs, reads it
# and decides.
(
  PAYLOAD=0 && new_case blank-inspect
  touch blank-aura-pim-mcp
  SIDECAR_MOVES=aura-pim-mcp && apply_dies "docker reported no creation time for the PIM"
  grep -q 'docker reported no Created for aura-pim-mcp' err || fail "the failure did not say why: $(cat err)"
  expect_restarts 0 "the tick that could not read the PIM's time"
  rm blank-aura-pim-mcp
  quiet && apply
  expect_restarts 1 "the apply that reads the PIM's time again"
)
echo "ok: a time docker cannot report fails the tick, and the next apply to run decides"

# (8) aura's start time fails the tick the way a sidecar's creation time does: no restart on a
# time that is not there, and none skipped silently.
(
  PAYLOAD=0 && new_case blank-aura-start
  touch blank-started-aura
  SIDECAR_MOVES=aura-pim-mcp && apply_dies "docker reported no start time for aura"
  grep -q 'docker reported no State.StartedAt for aura' err || fail "the failure did not say why: $(cat err)"
  expect_restarts 0 "the tick that could not read aura's start time"
  rm blank-started-aura
  quiet && apply
  expect_restarts 1 "the apply that reads aura's start time again"
)
echo "ok: aura's start time that docker cannot report fails the tick, and the next apply to run decides"
