#!/usr/bin/env bash
# Pull and apply the latest Aura appliance images on an edge installation —
# ported from wpt-iot's wpt-image-update.sh. Only the aura service and the MCP
# sidecars are ever replaced: Postgres, ArcadeDB, Garage and the llama.cpp
# sidecars are never touched here. Aura runs its own migrations before its
# healthcheck reports healthy, so health IS the bootstrap gate.
#
# Prerequisites in /opt/aura/.env (the dev default stays build-from-source, and
# an MCP left at its SHA-pinned compose default is a no-op here):
#   AURA_IMAGE=ghcr.io/chetto1983/aura:edge
#   AURA_PULL_POLICY=always
#   AURA_ARCADEDB_MCP_IMAGE=ghcr.io/chetto1983/aura-arcadedb-mcp:edge
#   AURA_PIM_MCP_IMAGE=ghcr.io/chetto1983/aura-pim-mcp:sidecar
#   AURA_WHATSAPP_MCP_IMAGE=ghcr.io/chetto1983/whatsapp-mcp:latest
#   AURA_CADDY_IMAGE=ghcr.io/chetto1983/aura-caddy:edge  (+ AURA_CADDY_PULL_POLICY=always)
#   AURA_INGEST_IMAGE=ghcr.io/chetto1983/aura-ingest:edge  (+ AURA_INGEST_PULL_POLICY=always)

set -Eeuo pipefail

[[ -f /etc/default/aura ]] && source /etc/default/aura

INSTALL_DIR="${INSTALL_DIR:-/opt/aura}"
LOCK_FILE="${AURA_IMAGE_UPDATE_LOCK:-/run/lock/aura-image-update.lock}"
HEALTH_TIMEOUT_SECONDS="${AURA_IMAGE_UPDATE_HEALTH_TIMEOUT:-600}"

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

before="$(container_image_id aura)"

# aura, aura-migrate and garage-bootstrap share ${AURA_IMAGE}: the migrator must
# run the binary it migrates for. `up -d aura` WITHOUT --no-deps is deliberate —
# compose recreates the one-shot deps whose image changed and waits for their
# service_completed_successfully before replacing aura, so new migrations land
# first; already-healthy infra deps (postgres, garage, embed) are left alone.
docker compose pull aura aura-migrate garage-bootstrap
docker compose up -d aura

deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
until service_is_healthy aura; do
  if ((SECONDS >= deadline)); then
    echo "Aura did not become healthy within ${HEALTH_TIMEOUT_SECONDS}s" >&2
    docker compose ps aura >&2
    exit 1
  fi
  sleep 2
done

after="$(container_image_id aura)"
echo "aura: ${before} -> ${after}"
# Provenance is checkable, not asserted: the edge image stamps VCS_REF, so the
# running commit is read from the binary rather than trusted from the tag.
docker compose exec -T aura aura version || true

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
  deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
  until service_is_healthy "${svc}"; do
    if ((SECONDS >= deadline)); then
      echo "${svc} did not become healthy within ${HEALTH_TIMEOUT_SECONDS}s" >&2
      docker compose ps "${svc}" >&2
      exit 1
    fi
    sleep 2
  done
  svc_after="$(container_image_id "${svc}")"
  echo "${svc}: ${svc_before} -> ${svc_after}"
}

update_sidecar arcadedb-mcp
update_sidecar aura-pim-mcp
update_sidecar whatsapp
# Repo-built core services on the edge channel (published by the same workflow
# as aura itself); on a machine pinned to :local these skip via pull tolerance.
update_sidecar caddy
update_sidecar aura-ingest

# Remove only untagged images left behind by a successful replacement.
docker image prune --force >/dev/null
echo "Aura image update completed; the appliance is healthy."
