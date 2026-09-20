#!/usr/bin/env bash
set -euo pipefail
image="${AURA_CLOUDFLARED_TEST_IMAGE:-aura-cloudflared:local}"
name="aura-cloudflared-contract-$$"
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker image inspect "$image" >/dev/null
[ "$(docker image inspect --format '{{.Config.User}}' "$image")" = '65532:65532' ]
docker run --detach --name "$name" --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true --memory 192m --cpus .5 --pids-limit 64 \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=1m,mode=0700,uid=65532,gid=65532 "$image" >/dev/null
healthy=false
for attempt in $(seq 1 30); do
  [ "$(docker inspect --format '{{.State.Running}}' "$name")" = true ] \
    || { echo 'FAIL: idle sidecar exited before healthcheck' >&2; exit 1; }
  if docker exec "$name" /usr/local/bin/aura-cloudflared-supervisor healthcheck http://127.0.0.1:8085/healthz; then healthy=true; break; fi
  sleep 1
done
[ "$healthy" = true ] || { echo 'FAIL: idle sidecar never became healthy' >&2; exit 1; }
[ "$(docker inspect --format '{{len .HostConfig.PortBindings}}' "$name")" = 0 ]
[ "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$name")" = true ]
if docker inspect --format '{{json .Config.Env}}' "$name" | grep -Ei 'TOKEN=|SECRET=|PASSWORD=|AURA_DB_|POSTGRES|DOCKER_HOST'; then
  echo 'FAIL: sidecar has forbidden credentials' >&2; exit 1
fi
if docker exec "$name" /bin/sh -c true >/dev/null 2>&1; then echo 'FAIL: shell present' >&2; exit 1; fi
if docker exec "$name" /bin/bash -c true >/dev/null 2>&1; then echo 'FAIL: bash present' >&2; exit 1; fi
docker exec "$name" /usr/local/bin/cloudflared --version | grep -F '2026.8.3'
[ -z "$(docker logs "$name" 2>&1)" ] || { echo 'FAIL: idle process emitted unexpected diagnostics' >&2; exit 1; }
echo 'ok: idle distroless sidecar is non-root, healthy, shell-free and unpublished without credentials or network'
