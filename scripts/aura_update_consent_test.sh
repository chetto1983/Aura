#!/usr/bin/env bash
#
# Drives deploy/aura-update-consent.sh against a stubbed docker: when a tick applies a pending
# update on its own (at night, with nothing running), when it waits for the cockpit, and what it
# records for the cockpit to read.
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
# shellcheck source=deploy/aura-image-home.sh
source "$repo_root/deploy/aura-image-home.sh"
[[ "$UPDATE_AT" == 03:00 && "$NIGHT_SECONDS" == 10800 && "$MAX_DEFER_SECONDS" == 86400 && "$ACTIVITY_STALE_SECONDS" == 180 ]] ||
  fail "default policy is $UPDATE_AT/$NIGHT_SECONDS/$MAX_DEFER_SECONDS/$ACTIVITY_STALE_SECONDS"
for given in 25:00 3:00 '03:00;reboot' ''; do
  got="$(AURA_UPDATE_AT="$given" bash -c 'source "$1"; echo "$UPDATE_AT"' _ "$repo_root/deploy/aura-update-consent.sh")"
  [[ "$got" == 03:00 ]] || fail "AURA_UPDATE_AT=$given gave $got, want the 03:00 default"
done
got="$(AURA_UPDATE_AT=04:30 bash -c 'source "$1"; echo "$UPDATE_AT"' _ "$repo_root/deploy/aura-update-consent.sh")"
[[ "$got" == 04:30 ]] || fail "AURA_UPDATE_AT=04:30 gave $got"
# Host local time, across tonight's change from CEST to CET.
for at in '2026-10-24 12:00|2026-10-25 03:00 CET' '2026-10-25 02:30|2026-10-25 03:00 CET' \
  '2026-10-25 03:00|2026-10-26 03:00 CET' '2026-03-29 01:00|2026-03-29 03:00 CEST'; do
  from="$(TZ=Europe/Rome date -d "${at%%|*}" +%s)"
  got="$(TZ=Europe/Rome date -d "@$(TZ=Europe/Rome next_apply_at "$from")" '+%F %H:%M %Z')"
  [[ "$got" == "${at#*|}" ]] || fail "the first 03:00 after ${at%%|*} is $got, want ${at#*|}"
done
echo "ok: the night time defaults to 03:00 local, refuses a malformed value, and survives DST"
set +e # the updater enables errexit when sourced; the cases below test return codes

OLD=sha256:0ld
NEW=sha256:e3e
# CONTAINER_MANIFEST and PLATFORM_TAG model a multi-platform (containerd) image store: the
# platform manifest a container was created from, and the one a tag names for linux/amd64.
declare -A RUNNING CONTAINER_IMAGE CONTAINER_REF CONTAINER_MANIFEST TAG PLATFORM_TAG
declare -A REV=([sha256:0ld]=4de507676c6b234470d59ce57ce9bfbe61233cfe [sha256:e3e]=7886200e539046d3b7dc47176facddb74f98ce6b)
BOXES=''
reset_host() {
  RUNNING=([aura]=aura-id)
  CONTAINER_IMAGE=([aura-id]="$OLD")
  CONTAINER_REF=([aura-id]=ghcr.io/example/aura:edge)
  CONTAINER_MANIFEST=()
  TAG=([ghcr.io/example/aura:edge]="$OLD")
  PLATFORM_TAG=()
  BOXES=''
  rm -f "$INSTALL_DIR"/update/* "$INSTALL_DIR/payload_manifest.txt" "$INSTALL_DIR/$APPLIED_MANIFEST"
  printf '%s' "$OLD" >"$INSTALL_DIR/update/applied-image"
}
docker() {
  case "$1 ${2:-}" in
    "compose ps") echo "${RUNNING[$4]:-}" ;;
    "inspect --format")
      [[ -n "${CONTAINER_IMAGE[$4]:-}" ]] || return 1
      case "$3" in
        '{{.Image}}' | '{{.Id}}') echo "${CONTAINER_IMAGE[$4]}" ;;
        '{{.Config.Image}}') echo "${CONTAINER_REF[$4]}" ;;
        *ImageManifestDescriptor*) [[ -z "${CONTAINER_MANIFEST[$4]:-}" ]] || echo "${CONTAINER_MANIFEST[$4]} linux/amd64" ;;
        *) fail "unexpected docker $*" ;;
      esac
      ;;
    "image inspect")
      if [[ "$3 $4 $5 ${6:-}" == '--platform linux/amd64 --format {{.Id}}' ]]; then
        [[ -n "${PLATFORM_TAG[$7]:-}" ]] || return 1
        echo "${PLATFORM_TAG[$7]}"
        return 0
      fi
      case "$4" in
        '{{.Id}}') [[ -n "${TAG[$5]:-}" ]] || return 1; echo "${TAG[$5]}" ;;
        *Labels*) echo "${REV[$5]:-}" ;;
        '{{.Created}}') echo 2026-09-24T06:47:13Z ;;
        *) fail "unexpected docker $*" ;;
      esac
      ;;
    "ps -a") [[ -z "$BOXES" ]] || printf '%s\n' $BOXES ;;
    *) fail "unexpected docker $*" ;;
  esac
}
systemctl() { echo "systemctl $*" >>"$fixture/systemctl.log"; }

status() { kv_value "$INSTALL_DIR/update/status" "$1"; }
# Whether epoch $1 is a 03:00 on the local calendar day of epoch $2 or the next one, after $2. A
# calendar day, not 86400 s: the day of a DST change lasts 23 or 25 hours.
is_next_night() {
  local day
  day="$(date -d "@$2" +%F)"
  (($1 > $2)) && [[ "$(date -d "@$1" +%H:%M)" == 03:00 ]] &&
    [[ "$(date -d "@$1" +%F)" == "$day" || "$(date -d "@$1" +%F)" == "$(date -d "$day +1 day" +%F)" ]]
}
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
for key in state running_rev available_rev available_built pending_since apply_at deadline deferred_until deferred_by handled_request error checked_at; do
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
apply_at="$(status apply_at)"
is_next_night "$apply_at" "$since" || fail "apply_at=$apply_at is not the first 03:00 after $since"
[[ "$(status available_built)" == "$(date -u -d 2026-09-24T06:47:13Z +%s)" ]] || fail "build time $(status available_built)"
echo "ok: a new build waits for the next 03:00, with a deadline 24 h from now"

TAG=([ghcr.io/example/aura:edge]=sha256:n3wer)
REV[sha256:n3wer]=0123456789abcdef0123456789abcdef01234567
sleep 1
decides wait "a newer build while still waiting"
[[ "$(status pending_since)" == "$since" && "$(status apply_at)" == "$apply_at" && "$(status available_rev)" == "${REV[sha256:n3wer]}" ]] ||
  fail "a newer build moved the deadline: $(cat "$INSTALL_DIR/update/status")"
echo "ok: a newer build shows the newer revision but keeps the first build's deadline"

reset_host
publish_new_build
activity 30 3600 0
decides wait "nobody used Aura for an hour, in the day"
night_came() { sed -i "s/^apply_at=.*/apply_at=$(($(date +%s) - 60))/" "$INSTALL_DIR/update/status"; }
night_ended() { sed -i "s/^apply_at=.*/apply_at=$(($(date +%s) - NIGHT_SECONDS - 60))/" "$INSTALL_DIR/update/status"; }
night_came
activity 30 30 0
decides apply "the night came, someone chatted a minute ago"
grep -q '03:00' "$fixture/decide.out" || fail "the night case was not named: $(cat "$fixture/decide.out")"
activity 30 30 1
decides wait "the night came, a tool call still running"
activity 600 30 3
decides apply "the night came, an activity report 10 minutes old (Aura down)"
rm -f "$INSTALL_DIR/update/activity"
decides apply "the night came, no activity report at all (Aura predates the channel)"
printf 'written_at=%s\nlast_activity_at=soon\nlive_runs=x\n' "$(date +%s)" >"$INSTALL_DIR/update/activity"
decides wait "the night came, an unreadable activity report"
activity 30 30 1
night_ended
decides wait "a run held the whole night"
is_next_night "$(status apply_at)" "$(date +%s)" || fail "a missed night did not move to the next one: apply_at=$(status apply_at)"
activity 30 3600 0
decides wait "the morning after a missed night, nothing running"
echo "ok: only the night applies on its own, unless a run is live or the report is unreadable; a missed night waits for the next"

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
decides wait "deferred an hour"
night_came
decides wait "the night came while the deferral still holds"
night_ended
decides wait "a postponement outlasted the night"
is_next_night "$(status apply_at)" "$(date +%s)" || fail "an outlasted night did not move to the next one: apply_at=$(status apply_at)"
[[ "$(status deferred_by)" == 448ddbe1-96ea-405d-8219-4a3d52a425c0 ]] || fail "deferred_by not recorded"
request 22223333444455556666777788889999 defer $(($(date +%s) + 999999))
decides wait "deferred past the deadline"
[[ "$(status deferred_until)" == "$(status deadline)" ]] || fail "deferral not clamped: $(status deferred_until) vs $(status deadline)"
sed -i "s/^pending_since=.*/pending_since=$(($(date +%s) - 86401))/" "$INSTALL_DIR/update/status"
night_came
decides apply "the night came and the deadline passed, deferral or not"
echo "ok: a deferral holds the night until the deadline, and is clamped to it"

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

# On a multi-platform image store a republish of unchanged content moves the tag's index digest
# but not its platform manifest, which is what compose compares, so compose keeps the container.
# Lab VM, 2026-10-14: whatsapp-mcp rebuilt from the same commit, and every idle tick re-applied
# the update for a container compose would never recreate.
reset_host
RUNNING[whatsapp]=wa-id
CONTAINER_IMAGE[wa-id]=sha256:1ndex1
CONTAINER_MANIFEST[wa-id]=sha256:m4nifest
CONTAINER_REF[wa-id]=ghcr.io/example/whatsapp-mcp:latest
TAG[ghcr.io/example/whatsapp-mcp:latest]=sha256:1ndex2
PLATFORM_TAG[ghcr.io/example/whatsapp-mcp:latest]=sha256:m4nifest
activity 30 1200 0
decides wait "an attested republish of whatsapp's unchanged image, idle"
[[ "$(status state)" == current ]] || fail "an unchanged republish was pending: $(cat "$INSTALL_DIR/update/status")"
PLATFORM_TAG[ghcr.io/example/whatsapp-mcp:latest]=sha256:m4nifest2
decides wait "whatsapp's content changed, in the day"
[[ "$(status state)" == pending ]] || fail "a changed sidecar was not pending: $(cat "$INSTALL_DIR/update/status")"
echo "ok: a sidecar is behind when its content changed, not when only its index did"

reset_host
cp "$INSTALL_DIR/.env" "$fixture/env.consent"
printf 'AURA_SANDBOX_IMAGE=box:edge\nAURA_SANDBOX_EGRESS_IMAGE=egress:edge\n' >>"$INSTALL_DIR/.env"
BOXES='aura-box-u1'
CONTAINER_IMAGE[aura-box-u1]=sha256:b0x1 CONTAINER_MANIFEST[aura-box-u1]=sha256:bm1
CONTAINER_IMAGE[aura-egress-u1]=sha256:egr1 CONTAINER_MANIFEST[aura-egress-u1]=sha256:em1
TAG[box:edge]=sha256:b0x2 PLATFORM_TAG[box:edge]=sha256:bm1
TAG[egress:edge]=sha256:egr2 PLATFORM_TAG[egress:edge]=sha256:em1
[[ -z "$(stale_boxes)" ]] || fail "a box on unchanged content was stale: $(stale_boxes)"
PLATFORM_TAG[egress:edge]=sha256:em2
[[ "$(stale_boxes)" == aura-box-u1 ]] || fail "a box whose egress content changed was not stale"
PLATFORM_TAG[egress:edge]=sha256:em1 PLATFORM_TAG[box:edge]=sha256:bm2
[[ "$(stale_boxes)" == aura-box-u1 ]] || fail "a box whose content changed was not stale"
unset 'CONTAINER_IMAGE[aura-egress-u1]'
PLATFORM_TAG[box:edge]=sha256:bm1
[[ -z "$(stale_boxes)" ]] || fail "a box without its egress sidecar was stale"
cp "$fixture/env.consent" "$INSTALL_DIR/.env"
echo "ok: a box is stale when its content or its egress's changed, not when only an index did"

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
retry="$(status apply_at)"
is_next_night "$retry" "$(date +%s)" || fail "a failure did not move the retry to the next 03:00: apply_at=$retry"
( on_apply_exit 0 )
[[ "$(status state)" == failed ]] || fail "a clean exit overwrote the failure"
activity 30 3600 0
decides wait "failed earlier, nobody around, in the day"
[[ "$(status apply_at)" == "$retry" ]] || fail "the retry night was lost: $(cat "$INSTALL_DIR/update/status")"
echo "ok: an apply that dies leaves the cockpit a failed state, and retries the next night"

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
