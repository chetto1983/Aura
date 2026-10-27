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
declare -F trust_caddy_local_ca >/dev/null || { echo "FAIL: trust_caddy_local_ca undefined after source" >&2; exit 1; }

help="$(bash "$repo_root/scripts/install.sh" --help)"
printf '%s\n' "$help" | grep -q -- '--trust-local-ca' \
  || { echo "FAIL: installer help omits --trust-local-ca" >&2; exit 1; }

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

# The payload is configuration the sidecars read as their own non-root users (prometheus
# runs as nobody, grafana as 472, tempo as 10001), and the npx path runs this installer
# under a restrictive umask: measured 2026-09-10, every payload file landed 0600 and
# prometheus crash-looped on "permission denied". A re-run must also repair what an earlier
# one left behind, which cp alone never does to a file that already exists.
perm_root="$fixture_root/perm"
mkdir -p "$perm_root/payload/observability/prometheus" "$perm_root/out/observability/prometheus"
printf 'scrape_configs: []\n' > "$perm_root/payload/observability/prometheus/prometheus.yml"
printf 'stale\n' > "$perm_root/out/observability/prometheus/prometheus.yml"
chmod 0600 "$perm_root/out/observability/prometheus/prometheus.yml"
chmod 0700 "$perm_root/out/observability/prometheus" "$perm_root/out/observability"
(
  umask 077
  cd "$perm_root/out"
  AURA_PAYLOAD_DIR="$perm_root/payload" RAW_BASE="http://127.0.0.1:1/unreachable" \
    download_file observability/prometheus/prometheus.yml observability/prometheus/prometheus.yml
  for want in "644 observability/prometheus/prometheus.yml" "755 observability/prometheus" "755 observability"; do
    path="${want#* }"
    got="$(stat -c %a "$path")"
    [ "$got" = "${want%% *}" ] || { echo "FAIL: download_file left $path at $got, want ${want%% *}" >&2; exit 1; }
  done
)

echo "ok: download_file leaves the payload readable by the sidecars under any umask"

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
  [ "$(env_value AURA_CLOUDFLARED_IMAGE)" = "ghcr.io/chetto1983/aura-cloudflared:edge" ] \
    || { echo "FAIL: Cloudflare sidecar edge image missing" >&2; exit 1; }
  set_env_value AURA_CLOUDFLARED_IMAGE example.invalid/operator/cloudflared:pinned
  set_env_value AURA_CLOUDFLARED_PULL_POLICY missing
  ensure_edge_channel_env
  [ "$(env_value AURA_CLOUDFLARED_IMAGE)" = "example.invalid/operator/cloudflared:pinned" ] \
    || { echo "FAIL: explicit Cloudflare image overwritten" >&2; exit 1; }
  [ "$(env_value AURA_CLOUDFLARED_PULL_POLICY)" = "missing" ] \
    || { echo "FAIL: explicit Cloudflare pull policy overwritten" >&2; exit 1; }
  set_env_value AURA_CLOUDFLARED_PULL_POLICY always
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

# The sysfs side of the probe. A render node alone proves nothing about Vulkan; the DRM
# driver behind it is what decides, so every fixture pairs the node with a driver.
make_drm() {
  dir="$1"
  driver="$2"
  mkdir -p "$dir/renderD128/device"
  printf 'DRIVER=%s\nPCI_CLASS=30000\n' "$driver" > "$dir/renderD128/device/uevent"
}
make_drm "$fixture_root/drm-intel" i915
make_drm "$fixture_root/drm-amd" amdgpu
# VMware's virtual adapter, measured on the 26.04 guest this probe was fixed for.
make_drm "$fixture_root/drm-vmware" vmwgfx
# A render node whose driver the kernel does not name at all.
mkdir -p "$fixture_root/drm-silent/renderD128/device"

expect_backend() {
  want="$1"
  bin="$2"
  dri="$3"
  drm="${4:-$fixture_root/drm-intel}"
  got="$(PATH="$bin:/usr/bin:/bin" detect_embed_backend "$dri" "$drm")"
  [ "$got" = "$want" ] \
    || { echo "FAIL: detect_embed_backend with $(basename "$bin"), $(basename "$dri") and $(basename "$drm") gave '$got', want '$want'" >&2; exit 1; }
}
# A GPU nvidia-smi sees but Docker cannot drive is NOT CUDA: without a hook Docker has no
# `nvidia` device driver and the reservation kills `up`.
expect_backend cuda "$fixture_root/bin-hook" "$fixture_root/dri-none"
expect_backend cuda "$fixture_root/bin-cdi" "$fixture_root/dri-render"
expect_backend vulkan "$fixture_root/bin-smi" "$fixture_root/dri-render" "$fixture_root/drm-intel"
expect_backend vulkan "$fixture_root/bin-none" "$fixture_root/dri-render" "$fixture_root/drm-amd"
expect_backend cpu "$fixture_root/bin-smi" "$fixture_root/dri-none"
expect_backend cpu "$fixture_root/bin-none" "$fixture_root/dri-none"
# The regression this probe exists for: a virtual display adapter publishes a render node
# and has no Vulkan behind it, so choosing the Vulkan overlay yields a sidecar with no
# device. An unnamed driver takes the same safe road -- the CPU works, a wrong Vulkan
# guess does not start.
expect_backend cpu "$fixture_root/bin-none" "$fixture_root/dri-render" "$fixture_root/drm-vmware"
expect_backend cpu "$fixture_root/bin-none" "$fixture_root/dri-render" "$fixture_root/drm-silent"

echo "ok: detect_embed_backend picks CUDA only when Docker can drive it, then Vulkan, then CPU"

expect_posture() {
  backend="$1"
  mkdir -p "$fixture_root/posture-$backend"
  (
    cd "$fixture_root/posture-$backend"
    printf 'AURA_EMBED_BACKEND=%s\nAURA_EMBED_NGL=stale\n' "$backend" > .env
    ensure_embed_backend_env "$fixture_root/dri-render"
    shift
    for pair in "$@"; do
      [ "$(grep -c "^${pair%%=*}=" .env)" = 1 ] && grep -qx "$pair" .env \
        || { echo "FAIL: backend $backend should leave exactly $pair in .env, got: $(grep "^${pair%%=*}=" .env)" >&2; exit 1; }
    done
    # The image build is the payload's: compose.yaml and its overlays pin it, and a value in
    # .env would outrank that pin on this machine forever.
    ! grep -q '^AURA_EMBED_IMAGE=' .env \
      || { echo "FAIL: backend $backend wrote AURA_EMBED_IMAGE into .env, freezing the pinned build" >&2; exit 1; }
  )
}
# An explicit backend wins over what the host would detect: dri-render is present in every
# case below, and only the vulkan one may end up on the Vulkan overlay.
expect_posture cuda COMPOSE_FILE=compose.yaml AURA_EMBED_NGL=99
expect_posture vulkan COMPOSE_FILE=compose.yaml:compose.vulkan.yaml AURA_EMBED_NGL=99
expect_posture cpu COMPOSE_FILE=compose.yaml:compose.cpu.yaml AURA_EMBED_NGL=0

# The upgrade an existing CPU install takes: the 0.1.x wizard wrote the CPU pair and no
# backend, and the host has a render node the old probe never looked for.
mkdir -p "$fixture_root/posture-upgrade"
(
  cd "$fixture_root/posture-upgrade"
  printf 'AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server\nAURA_EMBED_NGL=0\n' > .env
  PATH="$fixture_root/bin-none:/usr/bin:/bin" ensure_embed_backend_env "$fixture_root/dri-render" "$fixture_root/drm-intel"
  for pair in AURA_EMBED_BACKEND=vulkan COMPOSE_FILE=compose.yaml:compose.vulkan.yaml AURA_EMBED_NGL=99; do
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

echo "ok: ensure_embed_backend_env derives the overlay and offload from one backend, and never the image"

# COMPOSE_FILE is only worth selecting if the overlay really clears what it claims to: the
# merged config is the proof, not the overlay's text.
if docker compose version >/dev/null 2>&1; then
  compose_out="$fixture_root/compose.out"
  # Interpolated, not --no-interpolate: Compose 2.38.2 (the CI runner's) refuses the literal
  # `./caddy/${AURA_CADDYFILE:-Caddyfile}:/etc/caddy/Caddyfile:ro` bind as "too many colons",
  # so the merge proof needs real values. Placeholders stand in for compose.yaml's required
  # secrets, and --env-file keeps a developer's real .env out of it.
  compose_env="$fixture_root/compose-config.env"
  grep -oE '\$\{[A-Z0-9_]+:\?' "$repo_root/compose.yaml" | sed -e 's/^\${//' -e 's/:?$//' | sort -u \
    | sed 's/$/=placeholder/' > "$compose_env"
  # What every install before 2026-09-14 left in .env. The pins must win over it, or a version
  # changed in compose.yaml never reaches the hosts that already exist.
  printf 'AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-vulkan\nPOSTGRES_IMAGE=postgres:stale\n' >> "$compose_env"
  # Git Bash would hand a native docker.exe the MSYS path, which Windows cannot open.
  compose_env_arg="$compose_env"
  if command -v cygpath >/dev/null 2>&1; then compose_env_arg="$(cygpath -m "$compose_env")"; fi
  compose_config() {
    if ! (cd "$repo_root" && docker compose --env-file "$compose_env_arg" "$@" config) >"$compose_out" 2>"$compose_out.err"; then
      echo "FAIL: docker compose $* config failed (compose $(docker compose version --short 2>/dev/null)): $(cat "$compose_out.err")" >&2
      exit 1
    fi
  }
  nvidia_reservations() { grep -c 'driver: nvidia' "$compose_out" || true; }
  service_image() { sed -n "/^  $1:/,/^  [a-z]/p" "$compose_out" | sed -n 's/^    image: //p' | head -n 1; }
  expect_embed_build() {
    case "$(service_image aura-llama-embed)" in
      # ggml-org ships semver releases (server-cuda-v0.4.1) as well as bNNNN CI builds,
      # and a release is the stronger pin. Both are a pin; a bare moving tag is not.
      "ghcr.io/ggml-org/llama.cpp:$1"-b[0-9]* | "ghcr.io/ggml-org/llama.cpp:$1"-v[0-9]*) ;;
      *) echo "FAIL: $2 runs embed image '$(service_image aura-llama-embed)', want the pinned $1 release or build over a stale .env" >&2; exit 1 ;;
    esac
  }
  compose_config -f compose.yaml
  expect_embed_build server-cuda compose.yaml
  case "$(service_image postgres)" in
    postgres:stale | "") echo "FAIL: postgres runs '$(service_image postgres)': a stale POSTGRES_IMAGE in .env still outranks the pin" >&2; exit 1 ;;
  esac
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
  expect_embed_build server-vulkan compose.vulkan.yaml
  grep -q '/dev/dri' "$compose_out" || { echo "FAIL: compose.vulkan.yaml does not hand /dev/dri to the embed sidecar" >&2; exit 1; }
  compose_config -f compose.yaml -f compose.cpu.yaml
  expect_embed_build server compose.cpu.yaml
  echo "ok: the CPU and Vulkan overlays each drop exactly the embed sidecar's NVIDIA reservation and pin their build over .env"
elif [ -n "${CI:-}" ]; then
  echo "FAIL: docker compose is required under CI to check the embed overlays" >&2
  exit 1
else
  echo "skip: docker compose unavailable; the embed overlay merge is checked in CI"
fi

# The updater takes a host with no record of what its stack runs for one with an update
# pending: on the lab VM's clean install (2026-10-14) the first tick re-applied the installed
# build and restarted every service that mounts a payload file, although none had changed. The
# installer records what it brought up, under the names the updater reads, and only when the
# host's payload is the one the image carries: anything else is an update the tick must apply.
updater="$repo_root/deploy/aura-image-update.sh"
applied_manifest="$(sed -n 's/^APPLIED_MANIFEST=//p' "$updater")"
payload_dir="$(sed -n 's/^PAYLOAD_IMAGE_DIR=//p' "$updater")"
applied_image="$(sed -n 's|^applied_image() { cat "$(update_dir)/\([a-z-]*\)".*|\1|p' "$repo_root/deploy/aura-update-consent.sh")"
[ -n "$applied_manifest" ] && [ -n "$payload_dir" ] && [ -n "$applied_image" ] \
  || { echo "FAIL: the updater's record names were not found" >&2; exit 1; }
image_root="$fixture_root/image"
mkdir -p "$image_root$payload_dir/deploy"
printf 'services: {}\n' > "$image_root$payload_dir/compose.yaml"
printf '[Unit]\n' > "$image_root$payload_dir/deploy/aura.service"
(cd "$image_root$payload_dir" && sha256sum compose.yaml deploy/aura.service > payload_manifest.txt)
docker() {
  case "$1" in
    create) [ "$2" = registry.example/aura:edge ] && [ -d "$image_root" ] || return 1; echo cid-1 ;;
    cp) cp "$image_root${2#cid-1:}" "$3" ;;
    rm) [ "$*" = "rm -f cid-1" ] || { echo "FAIL: unexpected docker $*" >&2; exit 1; } ;;
    image) [ "$*" = "image inspect --format {{.Id}} registry.example/aura:edge" ] \
      || { echo "FAIL: unexpected docker $*" >&2; exit 1; }
      echo sha256:installed ;;
    *) echo "FAIL: unexpected docker $*" >&2; exit 1 ;;
  esac
}
new_host() {
  rm -rf "$fixture_root/host"
  mkdir -p "$fixture_root/host/update" "$fixture_root/host/deploy"
  cp "$image_root$payload_dir/compose.yaml" "$fixture_root/host/compose.yaml"
  cp "$image_root$payload_dir/deploy/aura.service" "$fixture_root/host/deploy/aura.service"
}
(
  new_host
  cd "$fixture_root/host"
  record_applied_update registry.example/aura:edge
  [ "$(cat "update/$applied_image")" = sha256:installed ] \
    || { echo "FAIL: the installer did not record the aura image its stack runs" >&2; exit 1; }
  for record in payload_manifest.txt "$applied_manifest"; do
    cmp -s "$record" "$image_root$payload_dir/payload_manifest.txt" \
      || { echo "FAIL: the installer did not record the image's payload manifest as $record" >&2; exit 1; }
  done
)
# A payload the image does not carry -- a re-run from a newer ref, a file edited by hand --
# leaves no record, and drops the one an earlier run or tick wrote.
(
  new_host
  cd "$fixture_root/host"
  printf 'old\n' > "update/$applied_image"
  printf 'old\n' > "$applied_manifest"
  printf 'services: {edited: {}}\n' > compose.yaml
  record_applied_update registry.example/aura:edge
  [ ! -e "update/$applied_image" ] && [ ! -e "$applied_manifest" ] \
    || { echo "FAIL: the installer recorded a payload the image does not carry as applied" >&2; exit 1; }
)
# An image with no payload, or none at all, records nothing and does not stop the install.
(
  new_host
  cd "$fixture_root/host"
  record_applied_update registry.example/other:edge \
    || { echo "FAIL: an image without a payload stopped the install" >&2; exit 1; }
  [ ! -e "update/$applied_image" ] && [ ! -e "$applied_manifest" ] \
    || { echo "FAIL: the installer recorded an image it could not read" >&2; exit 1; }
)
unset -f docker
# Recorded once the stack runs that image, and before the updater's timer is enabled: an
# OnBootSec already past elapses the moment the timer starts (systemd.timer(5)).
up_line="$(grep -n '^docker compose up -d --wait' "$repo_root/scripts/install.sh" | cut -d: -f1)"
record_line="$(grep -n '^record_applied_update ' "$repo_root/scripts/install.sh" | cut -d: -f1)"
timer_line="$(grep -n '^install_systemd_unit$' "$repo_root/scripts/install.sh" | cut -d: -f1)"
[ -n "$up_line" ] && [ -n "$record_line" ] && [ -n "$timer_line" ] \
  && [ "$up_line" -lt "$record_line" ] && [ "$record_line" -lt "$timer_line" ] \
  || { echo "FAIL: the installer records its stack at line '$record_line', want after 'up' ($up_line) and before the timer ($timer_line)" >&2; exit 1; }

echo "ok: the installer records the image and payload its stack runs, and only those"

# The embedding model placed is the artifact .env names: fetched at AURA_EMBED_REVISION, not
# main, and kept only when it hashes to AURA_EMBED_FINGERPRINT. A file already in the volume
# is judged by its hash, not its size, so a same-size rebuild upstream cannot pass for it.
embed_fixture="$fixture_root/embed"
embed_case() {
  rm -rf "$embed_fixture"
  mkdir -p "$embed_fixture/scripts"
  printf 'AURA_EMBED_REVISION=%s\nAURA_EMBED_FINGERPRINT=%s\n' "$1" "$2" > "$embed_fixture/.env"
  printf '%s' "$3" > "$embed_fixture/volume-sha"
  if [ "${4:-}" = running ]; then echo embed-cid > "$embed_fixture/running"; fi
  cat > "$embed_fixture/scripts/fetch_embedding_model.sh" <<'FETCH'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$(dirname "$0")/../fetch-args"
FETCH
  chmod +x "$embed_fixture/scripts/fetch_embedding_model.sh"
}
docker() {
  case "$1 $2" in
    "compose create") ;;
    "compose ps")
      if [ "$*" = "compose ps -q --status running aura-llama-embed" ]; then
        cat "$embed_fixture/running" 2>/dev/null || true
      else
        echo embed-cid
      fi
      ;;
    "compose restart") printf '%s\n' "$*" >> "$embed_fixture/restarts" ;;
    "run --rm") cat "$embed_fixture/volume-sha" ;;
    "cp "*) printf '%s\n' "$3" > "$embed_fixture/cp-target" ;;
    *) echo "FAIL: unexpected docker $*" >&2; exit 1 ;;
  esac
}
want_fp="b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63"
(
  embed_case 0f741b5 "$want_fp" "$want_fp" running
  cd "$embed_fixture"
  ensure_embed_model >/dev/null
  [ ! -e fetch-args ] && [ ! -e cp-target ] \
    || { echo "FAIL: a model already hashing to AURA_EMBED_FINGERPRINT was fetched again" >&2; exit 1; }
  [ ! -e restarts ] || { echo "FAIL: the sidecar was restarted with its model unchanged" >&2; exit 1; }
)
(
  embed_case 0f741b5 "$(printf '%s' "$want_fp" | tr a-f A-F)" "$(printf '%064d' 7)"
  cd "$embed_fixture"
  ensure_embed_model >/dev/null
  [ "$(sed -n 2p fetch-args)" = "https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/0f741b5/embeddinggemma-300M-Q8_0.gguf" ] \
    || { echo "FAIL: the model was not fetched at AURA_EMBED_REVISION: $(sed -n 2p fetch-args)" >&2; exit 1; }
  [ "$(sed -n 3p fetch-args)" = "$want_fp" ] \
    || { echo "FAIL: the fetch was not pinned to AURA_EMBED_FINGERPRINT, lower-cased" >&2; exit 1; }
  [ "$(cat cp-target)" = "embed-cid:$EMBED_MODEL_PATH" ] \
    || { echo "FAIL: the fetched model was not placed in the sidecar volume" >&2; exit 1; }
  [ ! -e restarts ] || { echo "FAIL: a sidecar that was not running was restarted" >&2; exit 1; }
)
# llama-server loads its GGUF at start, and `compose up -d` leaves a container whose config
# did not change alone: a running sidecar would keep serving the file it replaced.
(
  embed_case 0f741b5 "$want_fp" "$(printf '%064d' 7)" running
  cd "$embed_fixture"
  ensure_embed_model >/dev/null
  [ "$(cat restarts 2>/dev/null)" = "compose restart aura-llama-embed" ] \
    || { echo "FAIL: a running sidecar was not restarted onto the model placed under it" >&2; exit 1; }
)
(
  embed_case "" "$want_fp" ""
  cd "$embed_fixture"
  if ( ensure_embed_model ) >/dev/null 2>&1; then
    echo "FAIL: ensure_embed_model placed a model with no AURA_EMBED_REVISION" >&2
    exit 1
  fi
)
unset -f docker

echo "ok: the embedding model placed is the one AURA_EMBED_REVISION and AURA_EMBED_FINGERPRINT name"

# The phase counter is hand-maintained, and the first version of it shipped as [10/9]
# because a phase was added without moving the total. An operator reading "10 of 9" learns
# that the number lying to them is the one thing they were given to trust.
declared_total="$(grep -c '^step "' "$repo_root/scripts/install.sh")"
stated_total="$(sed -n 's/^STEP_TOTAL=\([0-9]\{1,\}\)$/\1/p' "$repo_root/scripts/install.sh")"
[ -n "$stated_total" ] \
  || { echo "FAIL: install.sh declares no STEP_TOTAL for its phase counter" >&2; exit 1; }
[ "$declared_total" = "$stated_total" ] \
  || { echo "FAIL: install.sh announces $stated_total phases but calls step() $declared_total times" >&2; exit 1; }

echo "ok: the installer's phase counter counts the phases it actually has"

# The npx path runs the installer under umask 077, which left INSTALL_DIR 0700 root, and the
# wizard's re-run probe -- `test -f compose.yaml` as the login user -- could never see an existing
# install (lab VM, 2026-10-14). The directory is configuration; .env carries its own 0600.
(
  INSTALL_DIR="$fixture_root/umask-077/opt/aura"
  need_sudo() { return 1; }
  umask 077
  create_install_dirs
  mode="$(stat -c %a "$INSTALL_DIR")"
  [ "$mode" = 755 ] || { echo "FAIL: under umask 077 the install directory is $mode, want 755" >&2; exit 1; }
  chmod 0700 "$INSTALL_DIR"
  create_install_dirs
  mode="$(stat -c %a "$INSTALL_DIR")"
  [ "$mode" = 755 ] || { echo "FAIL: a re-run left an existing 0700 install directory at $mode" >&2; exit 1; }
)

echo "ok: the install directory stays readable by the wizard's re-run probe under any umask"
