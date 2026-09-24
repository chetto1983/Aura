#!/usr/bin/env bash
#
# Drives deploy/aura-update-consent.sh against a stubbed docker: when a tick applies a pending
# update on its own, when it waits for the cockpit, and what it records for the cockpit to read.
# The status it writes is parsed by internal/hostupdate (status_test.go pins the same fields).

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

INSTALL_DIR="$fixture/opt/aura"
SYSTEMD_DIR="$fixture/systemd"
UPDATER_BIN="$fixture/sbin/updater"
mkdir -p "$INSTALL_DIR/update" "$SYSTEMD_DIR" "$(dirname "$UPDATER_BIN")"
printf 'AURA_IMAGE=ghcr.io/example/aura:edge\n' >"$INSTALL_DIR/.env"
cd "$INSTALL_DIR"

# shellcheck source=deploy/aura-image-update.sh
source "$repo_root/deploy/aura-image-update.sh"
# shellcheck source=deploy/aura-update-consent.sh
source "$repo_root/deploy/aura-update-consent.sh"
[[ "$IDLE_SECONDS" == 900 && "$MAX_DEFER_SECONDS" == 86400 && "$ACTIVITY_STALE_SECONDS" == 180 ]] ||
  fail "default policy is $IDLE_SECONDS/$MAX_DEFER_SECONDS/$ACTIVITY_STALE_SECONDS"
set +e # the updater enables errexit when sourced; the cases below test return codes

OLD=sha256:0ld
NEW=sha256:e3e
declare -A RUNNING CONTAINER_IMAGE CONTAINER_REF TAG
declare -A REV=([sha256:0ld]=4de507676c6b234470d59ce57ce9bfbe61233cfe [sha256:e3e]=7886200e539046d3b7dc47176facddb74f98ce6b)
reset_host() {
  RUNNING=([aura]=aura-id)
  CONTAINER_IMAGE=([aura-id]="$OLD")
  CONTAINER_REF=([aura-id]=ghcr.io/example/aura:edge)
  TAG=([ghcr.io/example/aura:edge]="$OLD")
  rm -f "$INSTALL_DIR"/update/* "$INSTALL_DIR/payload_manifest.txt" "$INSTALL_DIR/$APPLIED_MANIFEST"
  printf '%s' "$OLD" >"$INSTALL_DIR/update/applied-image"
}
docker() {
  case "$1 ${2:-}" in
    "compose ps") echo "${RUNNING[$4]:-}" ;;
    "inspect --format")
      case "$3" in
        '{{.Image}}') echo "${CONTAINER_IMAGE[$4]}" ;;
        '{{.Config.Image}}') echo "${CONTAINER_REF[$4]}" ;;
        *) fail "unexpected docker $*" ;;
      esac
      ;;
    "image inspect")
      case "$4" in
        '{{.Id}}') [[ -n "${TAG[$5]:-}" ]] || return 1; echo "${TAG[$5]}" ;;
        *Labels*) echo "${REV[$5]:-}" ;;
        '{{.Created}}') echo 2026-09-24T06:47:13Z ;;
        *) fail "unexpected docker $*" ;;
      esac
      ;;
    "ps -a") ;;
    *) fail "unexpected docker $*" ;;
  esac
}
systemctl() { echo "systemctl $*" >>"$fixture/systemctl.log"; }

status() { kv_value "$INSTALL_DIR/update/status" "$1"; }
activity() { # seconds since the report, seconds since the last use, live runs
  local now
  now="$(date +%s)"
  printf 'written_at=%s\nlast_activity_at=%s\nlive_runs=%s\n' $((now - $1)) $((now - $2)) "$3" >"$INSTALL_DIR/update/activity"
}
request() { printf 'id=%s\naction=%s\nuntil=%s\nby=448ddbe1-96ea-405d-8219-4a3d52a425c0\n' "$1" "$2" "$3" >"$INSTALL_DIR/update/request"; }
decides() { # expected: apply | wait
  local got=wait
  decide_update >"$fixture/decide.out" 2>&1 && got=apply
  [[ "$got" == "$1" ]] || fail "$2: decided to $got, want $1 ($(cat "$fixture/decide.out"))"
}
publish_new_build() { TAG=([ghcr.io/example/aura:edge]="$NEW"); }

reset_host
activity 30 30 0
decides wait "nothing new"
[[ "$(status state)" == current && "$(status running_rev)" == "${REV[$OLD]}" && "$(status pending_since)" == 0 ]] ||
  fail "an up-to-date host recorded $(cat "$INSTALL_DIR/update/status")"
for key in state running_rev available_rev available_built pending_since deadline deferred_until deferred_by handled_request error checked_at; do
  grep -q "^$key=" "$INSTALL_DIR/update/status" || fail "status carries no $key"
done
echo "ok: an up-to-date host records current and applies nothing"

reset_host
publish_new_build
activity 30 30 0
before="$(date +%s)"
decides wait "someone used Aura 30 s ago"
[[ "$(status state)" == pending && "$(status available_rev)" == "${REV[$NEW]}" ]] || fail "pending not recorded: $(cat "$INSTALL_DIR/update/status")"
since="$(status pending_since)"
((since >= before && $(status deadline) == since + 86400)) || fail "pending_since=$since deadline=$(status deadline)"
[[ "$(status available_built)" == "$(date -u -d 2026-09-24T06:47:13Z +%s)" ]] || fail "build time $(status available_built)"
echo "ok: a new build while someone works waits, with a deadline 24 h from now"

TAG=([ghcr.io/example/aura:edge]=sha256:n3wer)
REV[sha256:n3wer]=0123456789abcdef0123456789abcdef01234567
sleep 1
decides wait "a newer build while still waiting"
[[ "$(status pending_since)" == "$since" && "$(status available_rev)" == "${REV[sha256:n3wer]}" ]] ||
  fail "a newer build moved the deadline: $(cat "$INSTALL_DIR/update/status")"
echo "ok: a newer build shows the newer revision but keeps the first build's deadline"

reset_host
publish_new_build
activity 30 1200 0
decides apply "idle for 20 minutes"
activity 30 1200 1
decides wait "idle for 20 minutes but a tool call still running"
activity 600 30 3
decides apply "an activity report 10 minutes old (Aura down)"
rm -f "$INSTALL_DIR/update/activity"
decides apply "no activity report at all (Aura predates the channel)"
printf 'written_at=%s\nlast_activity_at=soon\nlive_runs=x\n' "$(date +%s)" >"$INSTALL_DIR/update/activity"
decides wait "an unreadable activity report"
echo "ok: idle, down or silent Aura applies; busy or unreadable waits"

reset_host
publish_new_build
activity 30 30 2
request 0123456789abcdef0123456789abcdef apply 0
decides apply "an admin asked while two runs were live"
[[ "$(status handled_request)" == 0123456789abcdef0123456789abcdef ]] || fail "the request was not recorded as handled"
decides wait "the same request read again"
request 'abc;reboot' apply 0
decides wait "a malformed id"
request fedcba9876543210fedcba9876543210 reboot 0
decides wait "an unknown action"
echo "ok: a new apply request applies once; a handled or malformed one changes nothing"

reset_host
publish_new_build
activity 30 1200 0
request 11112222333344445555666677778888 defer $(($(date +%s) + 3600))
decides wait "deferred an hour while idle"
[[ "$(status deferred_by)" == 448ddbe1-96ea-405d-8219-4a3d52a425c0 ]] || fail "deferred_by not recorded"
request 22223333444455556666777788889999 defer $(($(date +%s) + 999999))
decides wait "deferred past the deadline"
[[ "$(status deferred_until)" == "$(status deadline)" ]] || fail "deferral not clamped: $(status deferred_until) vs $(status deadline)"
sed -i "s/^pending_since=.*/pending_since=$(($(date +%s) - 86401))/" "$INSTALL_DIR/update/status"
decides apply "the deadline passed while idle, deferral or not"
echo "ok: a deferral holds an idle host until the deadline, and is clamped to it"

reset_host
publish_new_build
CONTAINER_IMAGE=([aura-id]="$NEW")
activity 30 30 4
decides apply "aura already runs the new image after a reboot"
grep -q 'host restarted' "$fixture/decide.out" || fail "the reboot case was not named: $(cat "$fixture/decide.out")"
echo "ok: once a reboot started the new image, the rest of the update applies without asking"

reset_host
RUNNING[caddy]=caddy-id
CONTAINER_IMAGE[caddy-id]=sha256:c0ddy
CONTAINER_REF[caddy-id]=ghcr.io/example/aura-caddy:edge
TAG[ghcr.io/example/aura-caddy:edge]=sha256:c0ddy2
activity 30 30 0
decides wait "only caddy's tag moved, someone active"
[[ "$(status state)" == pending ]] || fail "a moved sidecar tag was not pending"
printf 'm1\n' >"$INSTALL_DIR/payload_manifest.txt"
TAG[ghcr.io/example/aura-caddy:edge]=sha256:c0ddy
decides wait "a payload installed but never applied, someone active"
[[ "$(status state)" == pending ]] || fail "an unapplied payload was not pending"
echo "ok: a moved sidecar tag or an unapplied payload is pending too"

reset_host
publish_new_build
activity 30 30 0
decides wait "pending before an apply"
(
  trap 'on_apply_exit $?' EXIT
  record_status applying
  exit 7
)
[[ "$(status state)" == failed && "$(status error)" == *"exit code 7"* ]] || fail "a failed apply recorded $(cat "$INSTALL_DIR/update/status")"
( on_apply_exit 0 )
[[ "$(status state)" == failed ]] || fail "a clean exit overwrote the failure"
echo "ok: an apply that dies leaves the cockpit a failed state with the reason"

reset_host
# shellcheck disable=SC2016 # the stub expands them when the updater execs it, not here
printf '#!/usr/bin/env bash\necho "rerun reexec=${AURA_IMAGE_UPDATE_REEXEC:-} reruns=${AURA_UPDATE_RERUNS:-}" >%q\n' "$fixture/rerun.out" >"$UPDATER_BIN"
chmod +x "$UPDATER_BIN"
activity 30 30 0
decides wait "nothing to do"
request 99998888777766665555444433332222 apply 0
(AURA_IMAGE_UPDATE_REEXEC=1 rerun_if_asked_meanwhile)
[[ "$(cat "$fixture/rerun.out" 2>/dev/null)" == "rerun reexec= reruns=1" ]] || fail "a request left during the tick did not rerun cleanly: $(cat "$fixture/rerun.out" 2>/dev/null)"
rm -f "$fixture/rerun.out"
(AURA_UPDATE_RERUNS=3 rerun_if_asked_meanwhile)
[[ ! -e "$fixture/rerun.out" ]] || fail "reruns are not bounded"
decides wait "the request, handled on the rerun"
(rerun_if_asked_meanwhile)
[[ ! -e "$fixture/rerun.out" ]] || fail "a handled request reran the updater"
echo "ok: a request written during a tick reruns it once more, with a bound"

mkdir -p deploy
printf '[Path]\nPathChanged=/opt/aura/update/request\n' >"deploy/$REQUEST_PATH_UNIT"
: >"$fixture/systemctl.log"
ensure_update_channel >/dev/null
cmp -s "deploy/$REQUEST_PATH_UNIT" "$SYSTEMD_DIR/$REQUEST_PATH_UNIT" || fail "the path unit was not installed"
grep -qx "systemctl enable --now $REQUEST_PATH_UNIT" "$fixture/systemctl.log" || fail "the path unit was not enabled"
: >"$fixture/systemctl.log"
ensure_update_channel >/dev/null
[[ ! -s "$fixture/systemctl.log" ]] || fail "an installed path unit was reinstalled"
echo "ok: the updater installs and enables its own request trigger once"
