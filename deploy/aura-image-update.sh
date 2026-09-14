#!/usr/bin/env bash
# Converge an edge appliance on what master published — ported from wpt-iot's
# wpt-image-update.sh. One tick, with no operator:
#   1. pulls aura and installs the payload that image carries (compose files, this script, its
#      units, sidecar config) wherever it differs from the host, so a pin or config change made
#      in the repo reaches every machine; a new updater re-executes itself before going on;
#   2. replaces aura — it runs its own migrations before its healthcheck reports healthy, so
#      health IS the bootstrap gate — and, when the payload changed, re-applies the whole stack
#      exactly as aura.service does at boot, which is how a changed pin lands on Postgres,
#      ArcadeDB or a model sidecar;
#   3. refreshes the repo-built sidecars on their moving tags and the per-user sandbox boxes.
# Third-party images never ride a moving tag here: they change when compose.yaml's pin does.
#
# Prerequisites in /opt/aura/.env (the dev default stays build-from-source, and
# an MCP left at its SHA-pinned compose default is a no-op here):
#   AURA_IMAGE=ghcr.io/chetto1983/aura:edge
#   AURA_PULL_POLICY=always
#   AURA_ARCADEDB_MCP_IMAGE=ghcr.io/chetto1983/aura-arcadedb-mcp:edge  (+ AURA_ARCADEDB_MCP_PULL_POLICY=always)
#   AURA_PIM_MCP_IMAGE=ghcr.io/chetto1983/aura-pim-mcp:sidecar
#   AURA_WHATSAPP_MCP_IMAGE=ghcr.io/chetto1983/whatsapp-mcp:latest
#   AURA_CADDY_IMAGE=ghcr.io/chetto1983/aura-caddy:edge  (+ AURA_CADDY_PULL_POLICY=always)
#   AURA_INGEST_IMAGE=ghcr.io/chetto1983/aura-ingest:edge  (+ AURA_INGEST_PULL_POLICY=always)
#   AURA_SANDBOX_IMAGE=ghcr.io/chetto1983/aura-sandbox:edge
#   AURA_SANDBOX_EGRESS_IMAGE=ghcr.io/chetto1983/aura-egress:edge

set -Eeuo pipefail

# Where docker/aura/Dockerfile's payload stage puts the files and their manifest.
PAYLOAD_IMAGE_DIR=/usr/share/aura/payload
# The manifest of the payload the running stack was last brought up with, under INSTALL_DIR.
APPLIED_MANIFEST=payload_manifest.applied
# Keys an earlier installer wrote into .env that no compose file reads any more. The image
# pins, the embedding model and the in-stack tracing endpoint moved into compose.yaml and its
# overlays on 2026-09-14, where .env cannot outrank them and freeze a host on an old value.
RETIRED_ENV_KEYS=(POSTGRES_IMAGE AURA_EMBED_IMAGE AURA_EMBED_MODEL_PATH AURA_EMBED_MODEL_URL AURA_EMBED_DIMENSIONS AURA_OTEL_ENDPOINT)

container_image_id() {
  local container_id
  container_id="$(docker compose ps -q "$1")"
  if [[ -z "${container_id}" ]]; then
    printf 'missing'
    return
  fi
  docker inspect --format '{{.Image}}' "${container_id}"
}

service_is_healthy() {
  local container_id state health
  container_id="$(docker compose ps -q "$1")"
  [[ -n "${container_id}" ]] || return 1
  state="$(docker inspect --format '{{.State.Status}}' "${container_id}")"
  health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "${container_id}")"
  [[ "${state}" == 'running' && ("${health}" == 'healthy' || "${health}" == 'none') ]]
}

wait_healthy() {
  local svc="$1" deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
  until service_is_healthy "${svc}"; do
    if ((SECONDS >= deadline)); then
      echo "${svc} did not become healthy within ${HEALTH_TIMEOUT_SECONDS}s" >&2
      docker compose ps "${svc}" >&2
      exit 1
    fi
    sleep 2
  done
}

env_value() {
  [[ -f "${INSTALL_DIR}/.env" ]] || return 0
  sed -n "s/^$1=//p" "${INSTALL_DIR}/.env" | tail -n 1 | tr -d "\"'"
}

file_sha256() {
  [[ -f "$1" ]] || return 0
  sha256sum "$1" | cut -d' ' -f1
}

# install.sh makes the same split: every script is executable, everything else is config the
# sidecars read as their own non-root users, so it has to be world-readable.
install_payload_file() {
  local mode=0644
  [[ "$2" == *.sh ]] && mode=0755
  install -D -m "${mode}" "$1" "$2"
}

# Installs the payload the aura image carries wherever the host differs from it, keeps its
# manifest at INSTALL_DIR/payload_manifest.txt, and sets UPDATER_CHANGED when the installed
# updater was replaced. Returns non-zero, having changed nothing, when the image carries no
# payload or the extracted payload does not match its manifest: an image and a compose file
# from two different commits must never meet. The manifest proves the copy is whole, not who
# made it — the trust root is the registry tag this script already pulls and runs.
#
# Changing a file also drops APPLIED_MANIFEST, the record of the payload the running stack was
# last brought up with; main re-applies the stack until that record matches again. Deciding
# on that state rather than on "this tick changed something" is what keeps a tick that dies
# between installing and applying from leaving the stack behind forever.
sync_payload() {
  local image="$1" work container rel sum stamp backup unit
  local -a changed=()
  UPDATER_CHANGED=0
  if [[ -z "${image}" ]]; then
    echo "payload: no AURA_IMAGE in .env; sync skipped."
    return 0
  fi

  work="$(mktemp -d)"
  container="$(docker create "${image}")" || {
    rm -rf "${work}"
    echo "payload: cannot create a container from ${image}" >&2
    return 1
  }
  if ! docker cp "${container}:${PAYLOAD_IMAGE_DIR}/." "${work}" >/dev/null; then
    docker rm -f "${container}" >/dev/null || true
    rm -rf "${work}"
    echo "payload: ${image} carries no payload at ${PAYLOAD_IMAGE_DIR}; nothing applied." >&2
    return 1
  fi
  docker rm -f "${container}" >/dev/null || true
  if [[ ! -f "${work}/payload_manifest.txt" ]] ||
    ! (cd "${work}" && sha256sum --check --quiet --strict payload_manifest.txt >&2); then
    rm -rf "${work}"
    echo "payload: the payload in ${image} does not match its manifest; nothing applied." >&2
    return 1
  fi

  while read -r sum rel; do
    [[ "$(file_sha256 "${INSTALL_DIR}/${rel}")" == "${sum}" ]] || changed+=("${rel}")
  done <"${work}/payload_manifest.txt"

  if ((${#changed[@]} > 0)); then
    stamp="$(date -u +%Y%m%dT%H%M%SZ)"
    backup="${INSTALL_DIR}/backups/payload-${stamp}"
    for rel in "${changed[@]}"; do
      if [[ -f "${INSTALL_DIR}/${rel}" ]]; then
        install -D -m 0600 "${INSTALL_DIR}/${rel}" "${backup}/${rel}"
      fi
      install_payload_file "${work}/${rel}" "${INSTALL_DIR}/${rel}"
      echo "payload: ${rel} updated."
    done
    rm -f "${INSTALL_DIR}/${APPLIED_MANIFEST}"
    # Only the payload this change replaced is kept: the edge channel updates many times a
    # day, and the older copies are recoverable from git anyway. A change that only added
    # files replaced nothing, wrote no backup, and leaves the previous one where it is.
    if [[ -d "${backup}" ]]; then
      find "${INSTALL_DIR}/backups" -mindepth 1 -maxdepth 1 -type d -name 'payload-*' ! -path "${backup}" \
        -exec rm -rf {} +
    fi
  fi
  if ! cmp -s "${work}/payload_manifest.txt" "${INSTALL_DIR}/payload_manifest.txt"; then
    install -m 0644 "${work}/payload_manifest.txt" "${INSTALL_DIR}/payload_manifest.txt"
  fi

  # The copies systemd actually runs are compared against the payload itself, not against
  # their /opt/aura siblings, so a host whose payload was once copied by hand still converges.
  # Only what this host already installed is replaced: syncing never enables a new surface.
  if [[ -f "${UPDATER_BIN}" && "$(file_sha256 "${UPDATER_BIN}")" != "$(file_sha256 "${work}/deploy/aura-image-update.sh")" ]]; then
    install -m 0755 "${work}/deploy/aura-image-update.sh" "${UPDATER_BIN}"
    echo "payload: ${UPDATER_BIN} updated."
    UPDATER_CHANGED=1
  fi
  local units_changed=0
  for unit in aura.service aura-image-update.service aura-image-update.timer; do
    [[ -f "${SYSTEMD_DIR}/${unit}" && -f "${work}/deploy/${unit}" ]] || continue
    [[ "$(file_sha256 "${SYSTEMD_DIR}/${unit}")" != "$(file_sha256 "${work}/deploy/${unit}")" ]] || continue
    install -m 0644 "${work}/deploy/${unit}" "${SYSTEMD_DIR}/${unit}"
    echo "payload: ${SYSTEMD_DIR}/${unit} updated."
    units_changed=1
  done
  ((units_changed == 0)) || systemctl daemon-reload

  rm -rf "${work}"
}

# Left in .env a retired key reads like a setting an operator can change, and changes nothing.
# The lines are deleted in place (GNU sed keeps the file's 0600 mode and owner) and only the key
# names are logged: .env holds the secrets, so it is neither printed nor copied to a backup.
retire_env_keys() {
  local env_file="${INSTALL_DIR}/.env" pattern found
  [[ -f "${env_file}" ]] || return 0
  pattern="^($(IFS='|' && echo "${RETIRED_ENV_KEYS[*]}"))="
  found="$(grep -oE "${pattern}" "${env_file}" | tr -d '=' | sort -u | tr '\n' ' ')" || return 0
  sed -i -E "/${pattern}/d" "${env_file}"
  echo "env: retired ${found% } removed from .env."
}

# repository[:tag][@digest] -> repository, keeping a registry port (host:5000/name) intact.
image_repository() {
  local ref="${1%%@*}"
  [[ "${ref##*/}" != *:* ]] || ref="${ref%:*}"
  printf '%s' "${ref}"
}

# A changed pin leaves the previous image TAGGED, and `docker image prune` only reclaims
# untagged ones, so every llama.cpp or SearXNG bump would leave hundreds of MB on the disk.
# An image is removed when compose still uses its repository but no pin names it any more.
# Images outside compose (the per-user sandbox boxes) are never considered, and docker itself
# refuses to remove one a container still uses.
remove_superseded_images() {
  local ref id
  local -A pinned_repos=() pinned_ids=()
  while read -r ref; do
    [[ -n "${ref}" ]] || continue
    pinned_repos["$(image_repository "${ref}")"]=1
    id="$(docker image inspect --format '{{.Id}}' "${ref}" 2>/dev/null)" && pinned_ids["${id}"]=1
  done < <(docker compose config --images)
  while read -r ref id; do
    [[ -n "${pinned_repos[$(image_repository "${ref}")]:-}" && -z "${pinned_ids[${id}]:-}" ]] || continue
    if docker rmi "${ref}" >/dev/null 2>&1; then
      echo "images: ${ref} superseded by its pin; removed."
    fi
  done < <(docker images --no-trunc --format '{{.Repository}}:{{.Tag}} {{.ID}}')
}

# MCP sidecars ride the same timer. A service whose container does not exist is
# skipped on purpose: `up -d` must never START a surface the operator has not
# enabled, only refresh one that is already running.
update_sidecar() {
  local svc="$1" svc_before svc_after
  if [[ -z "$(docker compose ps -q "${svc}")" ]]; then
    echo "${svc}: not running here; skipped."
    return 0
  fi
  svc_before="$(container_image_id "${svc}")"
  # Tolerate an unpullable pin (a :local image, or a registry blip): the timer
  # must keep refreshing everything else and retry on its next tick.
  docker compose pull "${svc}" || {
    echo "${svc}: pull failed (local-only pin or registry unreachable); skipped."
    return 0
  }
  docker compose up -d --no-deps "${svc}"
  wait_healthy "${svc}"
  svc_after="$(container_image_id "${svc}")"
  echo "${svc}: ${svc_before} -> ${svc_after}"
}

# Per-user boxes are aura's own containers, not compose services, and aura pulls a box image
# only when it is missing locally (DockerBackend.ensureImage): a tag already present is never
# refreshed, and an existing box restarts on its old image forever. So both tags are pulled
# here, and a box left on a superseded image is removed together with its egress sidecar --
# the volumes stay -- for aura to recreate on that identity's next tool call. The pair goes
# together because egress joins the box's network by container ID, and aura is restarted
# afterwards because its idle reaper still holds the removed containers' IDs.
refresh_sandbox_images() {
  local box_image egress_image box_id egress_id name egress egress_now removed=0
  box_image="$(env_value AURA_SANDBOX_IMAGE)"
  egress_image="$(env_value AURA_SANDBOX_EGRESS_IMAGE)"
  if [[ -z "${box_image}" || -z "${egress_image}" ]]; then
    echo "sandbox: no images pinned in .env; skipped."
    return 0
  fi
  if ! docker pull -q "${box_image}" >/dev/null || ! docker pull -q "${egress_image}" >/dev/null; then
    echo "sandbox: pull failed (local-only pin or registry unreachable); skipped."
    return 0
  fi
  box_id="$(docker image inspect --format '{{.Id}}' "${box_image}")"
  egress_id="$(docker image inspect --format '{{.Id}}' "${egress_image}")"
  while read -r name; do
    [[ "${name}" == aura-box-* ]] || continue
    egress="aura-egress-${name#aura-box-}"
    egress_now="$(docker inspect --format '{{.Image}}' "${egress}" 2>/dev/null || true)"
    if [[ "$(docker inspect --format '{{.Image}}' "${name}")" == "${box_id}" &&
      ("${egress_now}" == '' || "${egress_now}" == "${egress_id}") ]]; then
      continue
    fi
    [[ -z "${egress_now}" ]] || docker rm -f "${egress}" >/dev/null
    docker rm -f "${name}" >/dev/null
    echo "sandbox: ${name} ran a superseded image; removed for aura to recreate."
    removed=$((removed + 1))
  done < <(docker ps -a --filter name=aura-box- --format '{{.Names}}')
  if ((removed > 0)); then
    docker compose restart aura
    wait_healthy aura
  fi
}

main() {
  # shellcheck source=/dev/null
  [[ ! -f /etc/default/aura ]] || source /etc/default/aura
  INSTALL_DIR="${INSTALL_DIR:-/opt/aura}"
  LOCK_FILE="${AURA_IMAGE_UPDATE_LOCK:-/run/lock/aura-image-update.lock}"
  HEALTH_TIMEOUT_SECONDS="${AURA_IMAGE_UPDATE_HEALTH_TIMEOUT:-600}"
  UPDATER_BIN="${AURA_IMAGE_UPDATE_BIN:-/usr/local/sbin/aura-image-update.sh}"
  SYSTEMD_DIR="${AURA_SYSTEMD_DIR:-/etc/systemd/system}"

  exec 9>"${LOCK_FILE}"
  if ! flock -n 9; then
    echo "Another Aura image update is already running; skipping."
    exit 0
  fi

  cd "${INSTALL_DIR}"
  [[ -f compose.yaml ]] || {
    echo "Missing ${INSTALL_DIR}/compose.yaml" >&2
    exit 1
  }

  local before after
  before="$(container_image_id aura)"

  # aura, aura-migrate and garage-bootstrap share ${AURA_IMAGE}: the migrator must
  # run the binary it migrates for. `up -d aura` WITHOUT --no-deps is deliberate —
  # compose recreates the one-shot deps whose image changed and waits for their
  # service_completed_successfully before replacing aura, so new migrations land
  # first; already-healthy infra deps (postgres, garage, embed) are left alone.
  docker compose pull aura aura-migrate garage-bootstrap

  # The payload is installed BEFORE aura is replaced, so the new binary starts under the
  # compose file of its own commit. Re-executing hands the rest of this tick to the updater
  # that payload shipped; exec releases this process's lock as the new one takes it again.
  sync_payload "$(env_value AURA_IMAGE)"
  if [[ "${UPDATER_CHANGED}" == 1 && -z "${AURA_IMAGE_UPDATE_REEXEC:-}" ]]; then
    echo "payload: the updater changed; re-executing it."
    AURA_IMAGE_UPDATE_REEXEC=1 exec "${UPDATER_BIN}"
  fi
  retire_env_keys

  docker compose up -d aura
  wait_healthy aura

  after="$(container_image_id aura)"
  echo "aura: ${before} -> ${after}"
  # Provenance is checkable, not asserted: the edge image stamps VCS_REF, so the
  # running commit is read from the binary rather than trusted from the tag.
  docker compose exec -T aura aura version || true

  # A changed payload can move any service — a pin, a mount, a flag — so the whole stack is
  # brought to it the way aura.service brings it up at boot: enabled profiles only, and a
  # service is recreated only when its image or configuration differs.
  if [[ -f payload_manifest.txt ]] && ! cmp -s payload_manifest.txt "${APPLIED_MANIFEST}"; then
    docker compose up -d
    wait_healthy aura
    cp payload_manifest.txt "${APPLIED_MANIFEST}"
    echo "payload: the stack was brought up on the installed payload."
  fi

  update_sidecar arcadedb-mcp
  update_sidecar aura-pim-mcp
  update_sidecar whatsapp
  # Repo-built core services on the edge channel (published by the same workflow
  # as aura itself); on a machine pinned to :local these skip via pull tolerance.
  update_sidecar caddy
  update_sidecar aura-ingest

  refresh_sandbox_images

  # Only a tick that got this far reclaims images: tagged ones no pin names any more, then
  # the untagged ones a moving tag left behind.
  remove_superseded_images
  docker image prune --force >/dev/null
  echo "Aura image update completed; the appliance is healthy."
}

# Sourcing defines the functions and stops, so scripts/aura_image_update_test.sh can drive
# them one at a time; executing the file runs a tick.
[[ "${BASH_SOURCE[0]}" != "$0" ]] || main "$@"
