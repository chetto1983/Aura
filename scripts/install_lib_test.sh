#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

# Sourcing must define the functions and do nothing else. If the guard is missing the
# source runs the whole installer, which needs root and Docker and would hang CI.
# shellcheck source=/dev/null
source "$repo_root/scripts/install.sh"

declare -F download_file >/dev/null || { echo "FAIL: download_file undefined after source" >&2; exit 1; }
declare -F ensure_env_default >/dev/null || { echo "FAIL: ensure_env_default undefined after source" >&2; exit 1; }
declare -F set_env_value >/dev/null || { echo "FAIL: set_env_value undefined after source" >&2; exit 1; }

# A sourcing script is not always argument-free: the argument loop in install.sh must not
# treat the SOURCING script's arguments as its own, because a stray --help would call
# `exit 0` and a sourced exit is the CALLER's exit -- this process would end early and
# green, having asserted nothing above.
helper="$fixture_root/sourcing_with_args.sh"
cat > "$helper" <<'HELPER'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=/dev/null
source "$1"
# Reaching here at all is the assertion: install.sh must not have consumed our arguments
# nor exited on our behalf.
printf 'survived with %d args\n' "$#"
HELPER
out="$(bash "$helper" "$repo_root/scripts/install.sh" --help --appliance)"
[ "$out" = "survived with 3 args" ] || { echo "FAIL: sourcing install.sh with arguments did not survive: $out" >&2; exit 1; }

echo "ok: install.sh sources cleanly and defines its functions"

mkdir -p "$fixture_root/payload/observability/tempo" "$fixture_root/out"
printf 'compose from payload' > "$fixture_root/payload/compose.yaml"
printf 'tempo from payload' > "$fixture_root/payload/observability/tempo/tempo.yml"

# With the payload dir set, nothing may touch the network. RAW_BASE points at a port
# nothing listens on, so a curl fallback would fail the test loudly instead of silently
# passing against a real fetch.
AURA_PAYLOAD_DIR="$fixture_root/payload" \
RAW_BASE="http://127.0.0.1:1/unreachable" \
  download_file compose.yaml "$fixture_root/out/compose.yaml"
grep -q 'compose from payload' "$fixture_root/out/compose.yaml" \
  || { echo "FAIL: download_file did not copy from AURA_PAYLOAD_DIR" >&2; exit 1; }

# Nested paths must survive, and the destination directory must be created.
AURA_PAYLOAD_DIR="$fixture_root/payload" \
RAW_BASE="http://127.0.0.1:1/unreachable" \
  download_file observability/tempo/tempo.yml "$fixture_root/out/observability/tempo/tempo.yml"
grep -q 'tempo from payload' "$fixture_root/out/observability/tempo/tempo.yml" \
  || { echo "FAIL: download_file lost a nested payload path" >&2; exit 1; }

# Unset, it must still take the network branch -- this is the standalone checkout and the
# curl | bash path, and an unreachable RAW_BASE must therefore FAIL.
net_err="$fixture_root/net.err"
if ( AURA_PAYLOAD_DIR="" RAW_BASE="http://127.0.0.1:1/unreachable" \
     download_file compose.yaml "$fixture_root/out/net.yaml" ) 2>"$net_err"; then
  echo "FAIL: download_file did not fall back to the network when AURA_PAYLOAD_DIR is empty" >&2
  exit 1
fi
# Exit status alone would also be non-zero if an empty AURA_PAYLOAD_DIR were mistaken for
# a set one and cp reached for "/compose.yaml". The curl diagnostic is what distinguishes
# the two branches.
grep -q 'could not fetch http://127.0.0.1:1/unreachable/compose.yaml' "$net_err" \
  || { echo "FAIL: an empty AURA_PAYLOAD_DIR did not reach the curl branch: $(cat "$net_err")" >&2; exit 1; }

echo "ok: download_file prefers the payload and still falls back to the network"

# A compose service whose pull_policy defaults to `never` is repo-built: its `build:` context
# is not in the payload, so on an :edge install compose must pull it instead. Read from
# compose.yaml rather than listed here, so a service added later is covered the day it lands.
# Measured 2026-09-10, first npx install on a clean mini PC: arcadedb-mcp was left at
# `never`, compose fell back to building it, and `up` died on the missing context.
never_policies="$(sed -n 's/.*pull_policy: \${\([A-Z_]*\):-never}.*/\1/p' "$repo_root/compose.yaml" | sort -u)"
[ -n "$never_policies" ] || { echo "FAIL: no never-defaulted pull_policy found in compose.yaml" >&2; exit 1; }
mkdir -p "$fixture_root/edge"
(
  cd "$fixture_root/edge"
  printf 'AURA_IMAGE=ghcr.io/chetto1983/aura:edge\n' > .env
  ensure_edge_channel_env
  for policy in $never_policies; do
    [ "$(env_value "$policy")" = "always" ] \
      || { echo "FAIL: an :edge install leaves $policy at its compose default 'never'" >&2; exit 1; }
  done
)

echo "ok: an :edge install pulls every repo-built compose image"

# The embed backend is decided on the target, the only machine whose answer is true.
declare -F detect_embed_backend >/dev/null || { echo "FAIL: detect_embed_backend undefined after source" >&2; exit 1; }
declare -F ensure_embed_backend_env >/dev/null || { echo "FAIL: ensure_embed_backend_env undefined after source" >&2; exit 1; }

make_stubs() {
  dir="$1"
  shift
  mkdir -p "$dir"
  for tool in "$@"; do
    printf '#!/bin/sh\nexit 0\n' > "$dir/$tool"
    chmod +x "$dir/$tool"
  done
}
make_stubs "$fixture_root/bin-hook" nvidia-smi nvidia-container-runtime-hook
make_stubs "$fixture_root/bin-cdi" nvidia-smi nvidia-cdi-hook
make_stubs "$fixture_root/bin-smi" nvidia-smi
mkdir -p "$fixture_root/bin-none" "$fixture_root/dri-none" "$fixture_root/dri-render"
: > "$fixture_root/dri-render/renderD128"

expect_backend() {
  want="$1"
  bin="$2"
  dri="$3"
  got="$(PATH="$bin:/usr/bin:/bin" detect_embed_backend "$dri")"
  [ "$got" = "$want" ] \
    || { echo "FAIL: detect_embed_backend with $(basename "$bin") and $(basename "$dri") gave '$got', want '$want'" >&2; exit 1; }
}
# A GPU nvidia-smi sees but Docker cannot drive is NOT CUDA: without a hook Docker has no
# `nvidia` device driver and the reservation kills `up`.
expect_backend cuda "$fixture_root/bin-hook" "$fixture_root/dri-none"
expect_backend cuda "$fixture_root/bin-cdi" "$fixture_root/dri-render"
expect_backend vulkan "$fixture_root/bin-smi" "$fixture_root/dri-render"
expect_backend vulkan "$fixture_root/bin-none" "$fixture_root/dri-render"
expect_backend cpu "$fixture_root/bin-smi" "$fixture_root/dri-none"
expect_backend cpu "$fixture_root/bin-none" "$fixture_root/dri-none"

echo "ok: detect_embed_backend picks CUDA only when Docker can drive it, then Vulkan, then CPU"

expect_posture() {
  backend="$1"
  mkdir -p "$fixture_root/posture-$backend"
  (
    cd "$fixture_root/posture-$backend"
    printf 'AURA_EMBED_BACKEND=%s\nAURA_EMBED_IMAGE=stale\nAURA_EMBED_NGL=stale\n' "$backend" > .env
    ensure_embed_backend_env "$fixture_root/dri-render"
    shift
    for pair in "$@"; do
      [ "$(grep -c "^${pair%%=*}=" .env)" = 1 ] && grep -qx "$pair" .env \
        || { echo "FAIL: backend $backend should leave exactly $pair in .env, got: $(grep "^${pair%%=*}=" .env)" >&2; exit 1; }
    done
  )
}
# An explicit backend wins over what the host would detect: dri-render is present in every
# case below, and only the vulkan one may end up on the Vulkan overlay.
expect_posture cuda COMPOSE_FILE=compose.yaml \
  AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda AURA_EMBED_NGL=99
expect_posture vulkan COMPOSE_FILE=compose.yaml:compose.vulkan.yaml \
  AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-vulkan AURA_EMBED_NGL=99
expect_posture cpu COMPOSE_FILE=compose.yaml:compose.cpu.yaml \
  AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server AURA_EMBED_NGL=0

# The upgrade an existing CPU install takes: the 0.1.x wizard wrote the CPU pair and no
# backend, and the host has a render node the old probe never looked for.
mkdir -p "$fixture_root/posture-upgrade"
(
  cd "$fixture_root/posture-upgrade"
  printf 'AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server\nAURA_EMBED_NGL=0\n' > .env
  PATH="$fixture_root/bin-none:/usr/bin:/bin" ensure_embed_backend_env "$fixture_root/dri-render"
  for pair in AURA_EMBED_BACKEND=vulkan COMPOSE_FILE=compose.yaml:compose.vulkan.yaml \
      AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-vulkan AURA_EMBED_NGL=99; do
    grep -qx "$pair" .env || { echo "FAIL: upgrading a CPU install on a Vulkan host did not write $pair" >&2; exit 1; }
  done
)

mkdir -p "$fixture_root/posture-invalid"
(
  cd "$fixture_root/posture-invalid"
  printf 'AURA_EMBED_BACKEND=metal\n' > .env
  if ( ensure_embed_backend_env "$fixture_root/dri-none" ) 2>"$fixture_root/posture-invalid.err"; then
    echo "FAIL: an unknown AURA_EMBED_BACKEND was accepted" >&2
    exit 1
  fi
  grep -q "AURA_EMBED_BACKEND" "$fixture_root/posture-invalid.err" \
    || { echo "FAIL: an unknown backend was refused for the wrong reason: $(cat "$fixture_root/posture-invalid.err")" >&2; exit 1; }
)

echo "ok: ensure_embed_backend_env derives the overlay, image and offload from one backend"

# COMPOSE_FILE is only worth selecting if the overlay really clears what it claims to: the
# merged config is the proof, not the overlay's text.
if docker compose version >/dev/null 2>&1; then
  compose_out="$fixture_root/compose.out"
  compose_config() {
    if ! (cd "$repo_root" && docker compose "$@" config --no-interpolate) >"$compose_out" 2>"$compose_out.err"; then
      echo "FAIL: docker compose $* config failed (compose $(docker compose version --short 2>/dev/null)): $(cat "$compose_out.err")" >&2
      exit 1
    fi
  }
  nvidia_reservations() { grep -c 'driver: nvidia' "$compose_out" || true; }
  compose_config -f compose.yaml
  base_reservations="$(nvidia_reservations)"
  if [ "$base_reservations" -eq 0 ]; then
    echo "FAIL: compose.yaml config shows no NVIDIA reservation to clear (compose $(docker compose version --short 2>/dev/null)); embed service as merged:" >&2
    sed -n '/^  aura-llama-embed:/,/^  [a-z]/p' "$compose_out" | grep -n -A6 'deploy:' >&2 || true
    exit 1
  fi
  for posture in cpu vulkan; do
    compose_config -f compose.yaml -f "compose.$posture.yaml"
    got="$(nvidia_reservations)"
    [ "$got" = "$((base_reservations - 1))" ] \
      || { echo "FAIL: compose.$posture.yaml leaves $got NVIDIA reservations, want $((base_reservations - 1))" >&2; exit 1; }
  done
  compose_config -f compose.yaml -f compose.vulkan.yaml
  grep -q '/dev/dri' "$compose_out" || { echo "FAIL: compose.vulkan.yaml does not hand /dev/dri to the embed sidecar" >&2; exit 1; }
  echo "ok: the CPU and Vulkan overlays each drop exactly the embed sidecar's NVIDIA reservation"
elif [ -n "${CI:-}" ]; then
  echo "FAIL: docker compose is required under CI to check the embed overlays" >&2
  exit 1
else
  echo "skip: docker compose unavailable; the embed overlay merge is checked in CI"
fi
