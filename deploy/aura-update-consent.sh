#!/usr/bin/env bash
# The cockpit's side of deploy/aura-image-update.sh: what an appliance tick tells Aura about a
# pending update, and what it reads back before restarting anything. The updater sources this
# file from INSTALL_DIR/deploy, where the payload that carries both of them installs it, so the
# two always come from the same commit; a tick that replaces this file re-executes the updater.
#
# INSTALL_DIR/update is bind-mounted into aura (compose.yaml) and read by internal/hostupdate.
# One writer per file: this script writes status, Aura writes request (an admin's decision)
# and activity (whether anyone is using it). Every file is key=value lines read with sed and
# a regex per field, never sourced: this root script takes only digits, hex and an action word
# out of what a container wrote. applied-image is this script's own record of the aura image
# the stack was last brought up on; the payload travels inside that image.

# The systemd unit that wakes the updater the moment the cockpit writes a request.
REQUEST_PATH_UNIT=aura-update-request.path

update_dir() { printf '%s/update' "${INSTALL_DIR}"; }

kv_value() {
  [[ -f "$1" ]] || return 0
  sed -n "s/^$2=//p" "$1" | tail -n 1
}

# Echoes $1 when it matches the regex $2, otherwise the fallback $3.
valid_or() {
  if [[ "$1" =~ $2 ]]; then printf '%s' "$1"; else printf '%s' "$3"; fi
}

# The policy, overridable in /etc/default/aura (sourced by main before this file).
IDLE_SECONDS="$(valid_or "${AURA_UPDATE_IDLE_SECONDS:-}" '^[0-9]{1,9}$' 900)"
MAX_DEFER_SECONDS="$(valid_or "${AURA_UPDATE_MAX_DEFER_SECONDS:-}" '^[0-9]{1,9}$' 86400)"
ACTIVITY_STALE_SECONDS="$(valid_or "${AURA_UPDATE_ACTIVITY_STALE_SECONDS:-}" '^[0-9]{1,9}$' 180)"

# The path unit is part of the updater itself, so unlike every other surface it is enabled here
# on hosts that never installed it: without it an "update now" waits for the next timer tick.
ensure_update_channel() {
  install -d -m 0755 "$(update_dir)"
  [[ -f "deploy/${REQUEST_PATH_UNIT}" ]] || return 0
  [[ "$(file_sha256 "${SYSTEMD_DIR}/${REQUEST_PATH_UNIT}")" != "$(file_sha256 "deploy/${REQUEST_PATH_UNIT}")" ]] || return 0
  install -m 0644 "deploy/${REQUEST_PATH_UNIT}" "${SYSTEMD_DIR}/${REQUEST_PATH_UNIT}"
  systemctl daemon-reload
  systemctl enable --now "${REQUEST_PATH_UNIT}"
  echo "update: ${REQUEST_PATH_UNIT} enabled; the cockpit can ask for an update."
}

# What the last tick recorded, re-validated with the regexes the cockpit parses it with
# (internal/hostupdate/status.go).
load_status() {
  local file epoch='^[0-9]{1,12}$' hex='^[0-9a-f]{0,64}$'
  file="$(update_dir)/status"
  PREV_STATE="$(kv_value "${file}" state)"
  RUNNING_REV="$(valid_or "$(kv_value "${file}" running_rev)" "${hex}" '')"
  AVAILABLE_REV="$(valid_or "$(kv_value "${file}" available_rev)" "${hex}" '')"
  AVAILABLE_BUILT="$(valid_or "$(kv_value "${file}" available_built)" "${epoch}" 0)"
  PENDING_SINCE="$(valid_or "$(kv_value "${file}" pending_since)" "${epoch}" 0)"
  DEADLINE="$(valid_or "$(kv_value "${file}" deadline)" "${epoch}" 0)"
  DEFERRED_UNTIL="$(valid_or "$(kv_value "${file}" deferred_until)" "${epoch}" 0)"
  DEFERRED_BY="$(valid_or "$(kv_value "${file}" deferred_by)" '^[0-9a-f-]{0,36}$' '')"
  HANDLED_REQUEST="$(valid_or "$(kv_value "${file}" handled_request)" "${hex}" '')"
  UPDATE_ERROR="$(kv_value "${file}" error)"
}

record_status() {
  local dir tmp
  dir="$(update_dir)"
  install -d -m 0755 "${dir}"
  tmp="$(mktemp "${dir}/.status.XXXXXX")"
  printf '%s\n' "state=$1" "running_rev=${RUNNING_REV}" "available_rev=${AVAILABLE_REV}" \
    "available_built=${AVAILABLE_BUILT}" "pending_since=${PENDING_SINCE}" "deadline=${DEADLINE}" \
    "deferred_until=${DEFERRED_UNTIL}" "deferred_by=${DEFERRED_BY}" \
    "handled_request=${HANDLED_REQUEST}" "error=${UPDATE_ERROR//$'\n'/ }" "checked_at=$(date +%s)" >"${tmp}"
  chmod 0644 "${tmp}"
  mv -f "${tmp}" "${dir}/status"
}

# Sets REQ_* from the cockpit's latest request; fails when there is none or it is malformed.
read_request() {
  local file
  file="$(update_dir)/request"
  [[ -f "${file}" ]] || return 1
  REQ_ID="$(kv_value "${file}" id)"
  REQ_ACTION="$(kv_value "${file}" action)"
  REQ_UNTIL="$(kv_value "${file}" until)"
  REQ_BY="$(kv_value "${file}" by)"
  if [[ ! "${REQ_ID}" =~ ^[0-9a-f]{16,64}$ || ! "${REQ_ACTION}" =~ ^(apply|defer)$ ||
    ! "${REQ_UNTIL}" =~ ^[0-9]{1,12}$ || ! "${REQ_BY}" =~ ^[0-9a-f-]{0,36}$ ]]; then
    echo "update: a malformed request was ignored." >&2
    return 1
  fi
}

# Idle is nobody using Aura for IDLE_SECONDS with nothing running. A report older than
# ACTIVITY_STALE_SECONDS means Aura is down or predates this channel: nobody can be using it,
# and the update may be what brings it back. A report that cannot be read counts as busy.
aura_is_idle() {
  local file now written last live
  file="$(update_dir)/activity"
  now="$(date +%s)"
  written="$(valid_or "$(kv_value "${file}" written_at)" '^[0-9]{1,12}$' 0)"
  ((now - 10#${written} <= ACTIVITY_STALE_SECONDS)) || return 0
  last="$(valid_or "$(kv_value "${file}" last_activity_at)" '^[0-9]{1,12}$' "${now}")"
  live="$(valid_or "$(kv_value "${file}" live_runs)" '^[0-9]{1,6}$' 1)"
  ((10#${live} == 0 && now - 10#${last} >= IDLE_SECONDS))
}

image_revision() {
  docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$1" 2>/dev/null |
    tr -cd '0-9a-f' | cut -c1-64
}

image_built() {
  local created
  created="$(docker image inspect --format '{{.Created}}' "$1" 2>/dev/null)" && [[ -n "${created}" ]] &&
    date -u -d "${created}" +%s 2>/dev/null || echo 0
}

applied_image() { cat "$(update_dir)/applied-image" 2>/dev/null || true; }

# Anything downloaded that the stack does not run yet: an aura image other than the one last
# applied, a payload installed but never brought up, a sidecar's moving tag, a sandbox box.
update_pending() {
  local svc
  [[ "$(image_id "$(env_value AURA_IMAGE)")" == "$(applied_image)" ]] || return 0
  if [[ -f payload_manifest.txt ]] && ! cmp -s payload_manifest.txt "${APPLIED_MANIFEST}"; then
    return 0
  fi
  for svc in "${SIDECARS[@]}"; do
    ! service_behind "${svc}" || return 0
  done
  [[ -n "$(stale_boxes)" ]]
}

# Records what this tick found; succeeds when the update is to be applied now. The deadline
# counts from the FIRST pending build, or an edge channel publishing every hour would hold
# every machine back forever.
decide_update() {
  local now tag running reason=''
  now="$(date +%s)"
  load_status
  tag="$(image_id "$(env_value AURA_IMAGE)" || true)"
  running="$(container_image_id aura)"
  RUNNING_REV="$(image_revision "${running}")"
  if ! update_pending; then
    ! read_request 2>/dev/null || HANDLED_REQUEST="${REQ_ID}"
    AVAILABLE_REV='' AVAILABLE_BUILT=0 PENDING_SINCE=0 DEADLINE=0 DEFERRED_UNTIL=0 DEFERRED_BY='' UPDATE_ERROR=''
    record_status current
    return 1
  fi
  case "${PREV_STATE}" in
    pending | failed | applying) ((PENDING_SINCE > 0)) || PENDING_SINCE="${now}" ;;
    *) PENDING_SINCE="${now}" DEFERRED_UNTIL=0 DEFERRED_BY='' ;;
  esac
  DEADLINE=$((PENDING_SINCE + MAX_DEFER_SECONDS))
  AVAILABLE_REV="$(image_revision "${tag}")"
  AVAILABLE_BUILT="$(image_built "${tag}")"
  if read_request && [[ "${REQ_ID}" != "${HANDLED_REQUEST}" ]]; then
    HANDLED_REQUEST="${REQ_ID}"
    case "${REQ_ACTION}" in
      apply) reason="an admin asked for it" ;;
      defer)
        DEFERRED_UNTIL=$((10#${REQ_UNTIL} < DEADLINE ? 10#${REQ_UNTIL} : DEADLINE))
        DEFERRED_BY="${REQ_BY}"
        echo "update: deferred until $(date -u -d "@${DEFERRED_UNTIL}" +%FT%TZ)."
        ;;
    esac
  fi
  # Consent means nothing once aura already runs the new image: a reboot's `compose up -d`
  # pulled it under the old payload, and the two must not live apart past this tick.
  if [[ -z "${reason}" && -n "${tag}" && "${running}" == "${tag}" && "${tag}" != "$(applied_image)" ]]; then
    reason="aura already runs it (the host restarted)"
  fi
  if [[ -z "${reason}" ]] && { ((DEFERRED_UNTIL <= now)) || ((now >= DEADLINE)); } && aura_is_idle; then
    reason="nobody is using Aura"
  fi
  record_status pending
  if [[ -z "${reason}" ]]; then
    echo "update: ${AVAILABLE_REV:0:9} waits for an admin, a quiet spell or $(date -u -d "@${DEADLINE}" +%FT%TZ)."
    return 1
  fi
  echo "update: applying ${AVAILABLE_REV:0:9}: ${reason}."
}

# A failed apply leaves the cockpit an error to show; the next eligible tick retries it.
on_apply_exit() {
  (($1 != 0)) || return 0
  UPDATE_ERROR="the update stopped with exit code $1; see journalctl -u aura-image-update"
  record_status failed || true
}

# systemd folds a path trigger into a tick already running, so a request written during this
# tick started no tick of its own (measured on the lab VM, 2026-09-24): look once more.
rerun_if_asked_meanwhile() {
  read_request 2>/dev/null || return 0
  [[ "${REQ_ID}" != "$(kv_value "$(update_dir)/status" handled_request)" ]] || return 0
  ((${AURA_UPDATE_RERUNS:-0} < 3)) || return 0
  echo "update: a request arrived during this tick; running again."
  exec env -u AURA_IMAGE_UPDATE_REEXEC AURA_UPDATE_RERUNS=$((${AURA_UPDATE_RERUNS:-0} + 1)) "${UPDATER_BIN}"
}
