#!/usr/bin/env bash
# Aura appliance installer. Intended usage (equivalent — the npx bin is a veneer
# over this script, see scripts/create-aura.mjs):
#   sudo npx github:chetto1983/Aura -- --appliance
#   curl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/<tag>/scripts/install.sh | bash
#
# Without AURA_INSTALL_REF the install tracks master and defaults the image to
# the ghcr :edge moving tag (published on every master push); --appliance then
# also enables the aura-image-update timer so the machine self-updates.
#
# Optional env:
#   AURA_INSTALL_REF=vX.Y.Z
#   AURA_IMAGE=ghcr.io/chetto1983/aura:vX.Y.Z
#   POSTGRES_IMAGE=postgres:18.4-alpine3.24
#   AURA_INSTALL_DIR=/opt/aura

set -euo pipefail

export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

# Sourced or executed, decided once because it is needed twice. The tests source this file
# to reach its functions: the argument loop below must not eat the SOURCING script's
# arguments, because a stray --help would call `exit 0` and a sourced exit is the CALLER's
# exit -- the test process would end early and green, having asserted nothing.
# The `:-$0` default is load-bearing, not decorative: under `set -u` (above) a bare
# `${BASH_SOURCE[0]}` aborts in the curl | bash case, where that variable is unset. That
# same unset-defaults-to-$0 behaviour is also why curl | bash still installs at all -- the
# comparison holds, AURA_EXECUTED=1, and the script runs.
if [ "${BASH_SOURCE[0]:-$0}" = "$0" ]; then AURA_EXECUTED=1; else AURA_EXECUTED=0; fi

APPLIANCE=0
GVISOR=0
INSTALL_DIR="${AURA_INSTALL_DIR:-}"
CONFIG_FILE=""
CFG_INSTALL_DIR=""; CFG_APPLIANCE=""; CFG_GVISOR=""
CFG_LLM_PROVIDER=""; CFG_LLM_BASE_URL=""; CFG_LLM_MODEL=""
CFG_OPENROUTER_API_KEY=""; CFG_EMBED_IMAGE=""; CFG_EMBED_NGL=""

usage() {
  cat <<'EOF'
usage: install.sh [--appliance] [--gvisor] [--dir PATH] [--config PATH]

  --appliance    install and enable the systemd aura.service unit
  --gvisor       provision runsc and set AURA_RUNTIME=runsc in .env
  --dir PATH     installation directory (default: /opt/aura on Linux)
  --config PATH  read answers from an absolute-path config file (see the spec)
EOF
}

if [ "$AURA_EXECUTED" = 1 ]; then
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --appliance) APPLIANCE=1 ;;
      --gvisor) APPLIANCE=1; GVISOR=1 ;;
      --dir)
        [ "$#" -ge 2 ] || { echo "FAIL: --dir requires a path" >&2; exit 2; }
        INSTALL_DIR="$2"
        shift
        ;;
      --config)
        [ "$#" -ge 2 ] || { echo "FAIL: --config requires a file" >&2; exit 2; }
        [ -z "$CONFIG_FILE" ] || { echo "FAIL: --config may only be supplied once" >&2; exit 2; }
        CONFIG_FILE="$2"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "FAIL: unknown argument: $1" >&2
        usage >&2
        exit 2
        ;;
    esac
    shift
  done
fi

OS="$(uname -s)"
if [ -z "$INSTALL_DIR" ]; then
  if [ "$OS" = "Darwin" ]; then
    INSTALL_DIR="$HOME/.aura/appliance"
  else
    INSTALL_DIR="/opt/aura"
  fi
fi

RAW_REF="${AURA_INSTALL_REF:-master}"
RAW_BASE="${AURA_INSTALL_BASE_URL:-https://raw.githubusercontent.com/chetto1983/Aura/${RAW_REF}}"
IMAGE_TAG="${AURA_IMAGE_TAG:-${RAW_REF}}"
# A master install rides the continuous-delivery channel: every master push
# publishes ghcr.io/chetto1983/aura:edge (there is no :master image tag), and
# the aura-image-update timer keeps the appliance tracking it. Pinned installs
# (AURA_INSTALL_REF=vX.Y.Z) keep the exact-tag image and skip the timer.
if [ "$IMAGE_TAG" = "master" ]; then
  IMAGE_TAG="edge"
fi
DEFAULT_IMAGE="ghcr.io/chetto1983/aura:${IMAGE_TAG}"

need_sudo() {
  [ "$(id -u)" -ne 0 ]
}

as_root() {
  if need_sudo; then
    command -v sudo >/dev/null 2>&1 || {
      echo "FAIL: root privileges required; re-run as root or install sudo." >&2
      exit 1
    }
    sudo "$@"
  else
    "$@"
  fi
}

cpu_count() {
  if command -v getconf >/dev/null 2>&1; then
    getconf _NPROCESSORS_ONLN 2>/dev/null || true
  elif [ "$OS" = "Darwin" ]; then
    sysctl -n hw.ncpu 2>/dev/null || true
  fi
}

ram_kib() {
  if [ "$OS" = "Linux" ] && [ -r /proc/meminfo ]; then
    awk '/MemTotal:/ {print $2}' /proc/meminfo
  elif [ "$OS" = "Darwin" ]; then
    bytes="$(sysctl -n hw.memsize 2>/dev/null || echo 0)"
    echo $(( bytes / 1024 ))
  else
    echo 0
  fi
}

disk_free_kib() {
  probe="$1"
  while [ ! -e "$probe" ] && [ "$probe" != "/" ]; do
    probe="$(dirname "$probe")"
  done
  df -Pk "$probe" | awk 'NR==2 {print $4}'
}

preflight_hw() {
  if [ "${AURA_INSTALL_SKIP_HW:-0}" = "1" ]; then
    echo "WARN: skipping hardware preflight because AURA_INSTALL_SKIP_HW=1" >&2
    return
  fi

  cpus="$(cpu_count)"
  cpus="${cpus:-0}"
  mem_kib="$(ram_kib)"
  free_kib="$(disk_free_kib "$INSTALL_DIR")"

  hard_mem=$((16 * 1024 * 1024))
  warn_mem=$((32 * 1024 * 1024))
  hard_disk=$((20 * 1024 * 1024))
  warn_disk=$((50 * 1024 * 1024))

  failed=0
  if [ "$cpus" -lt 4 ]; then
    echo "FAIL: Aura requires at least 4 CPU cores; found ${cpus}." >&2
    failed=1
  fi
  if [ "$mem_kib" -lt "$hard_mem" ]; then
    echo "FAIL: Aura requires at least 16 GiB RAM; found $((mem_kib / 1024 / 1024)) GiB." >&2
    failed=1
  elif [ "$mem_kib" -lt "$warn_mem" ]; then
    echo "WARN: 32 GiB RAM is recommended; found $((mem_kib / 1024 / 1024)) GiB." >&2
  fi
  if [ "$free_kib" -lt "$hard_disk" ]; then
    echo "FAIL: Aura requires at least 20 GiB free disk; found $((free_kib / 1024 / 1024)) GiB." >&2
    failed=1
  elif [ "$free_kib" -lt "$warn_disk" ]; then
    echo "WARN: 50 GiB free disk is recommended; found $((free_kib / 1024 / 1024)) GiB." >&2
  fi

  if [ "$failed" -ne 0 ]; then
    exit 1
  fi
}

docker_ready() {
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1
}

install_docker() {
  if docker_ready; then
    return
  fi

  echo "==> Docker not ready; attempting best-effort install"
  if [ "$OS" = "Linux" ]; then
    command -v curl >/dev/null 2>&1 || { echo "FAIL: curl is required to install Docker." >&2; exit 1; }
    tmp="$(mktemp)"
    curl -fsSL https://get.docker.com -o "$tmp" || {
      echo "FAIL: could not download https://get.docker.com" >&2
      exit 1
    }
    as_root sh "$tmp" || {
      echo "FAIL: Docker install failed. Install Docker Engine manually: https://docs.docker.com/engine/install/" >&2
      exit 1
    }
    rm -f "$tmp"
  elif [ "$OS" = "Darwin" ]; then
    command -v brew >/dev/null 2>&1 || {
      echo "FAIL: Docker Desktop is required on macOS. Install Homebrew or install Docker Desktop manually: https://docs.docker.com/desktop/setup/install/mac-install/" >&2
      exit 1
    }
    brew install --cask docker || {
      echo "FAIL: Docker Desktop install failed. Install manually: https://docs.docker.com/desktop/setup/install/mac-install/" >&2
      exit 1
    }
    echo "FAIL: start Docker Desktop, then re-run this installer." >&2
    exit 1
  else
    echo "FAIL: unsupported OS ${OS}. Install Docker manually: https://docs.docker.com/engine/install/" >&2
    exit 1
  fi

  docker_ready || {
    echo "FAIL: Docker is installed but 'docker compose' is not ready. Log out/in or start the Docker daemon, then re-run." >&2
    exit 1
  }
}

# The artifact (scripts/build_installer.sh) unpacks the payload beside this script and
# points AURA_PAYLOAD_DIR at it, so an appliance install fetches nothing: the 25 files it
# used to pull from a MOVING git ref, as root, travel with the installer instead. Unset --
# a repo checkout, or curl | bash -- keeps the network branch, so both entry points still
# work from one implementation.
download_file() {
  src="$1"
  dst="$2"
  mkdir -p "$(dirname "$dst")"
  if [ -n "${AURA_PAYLOAD_DIR:-}" ]; then
    cp "$AURA_PAYLOAD_DIR/$src" "$dst" || {
      echo "FAIL: payload is missing ${src}" >&2
      exit 1
    }
    return 0
  fi
  curl -fsSL "${RAW_BASE}/${src}" -o "$dst" || {
    echo "FAIL: could not fetch ${RAW_BASE}/${src}" >&2
    exit 1
  }
}

# The wizard hands its answers over in a file, never in argv: an API key on a command line
# is readable in the process table and lands in shell history. Values are base64 so a
# newline or a shell metacharacter in a model name cannot break the format.
parse_install_config() {
  path="$1"
  case "$path" in
    /*) ;;
    *) echo "FAIL: --config requires an absolute path" >&2; exit 2 ;;
  esac
  [ -f "$path" ] || { echo "FAIL: config not found: $path" >&2; exit 2; }
  head -n 1 "$path" | grep -qx 'format=1' || {
    echo "FAIL: unsupported config format in $path" >&2
    exit 2
  }
  # `IFS='=' read -r key value` looks equivalent but is not: bash drops a lone trailing
  # delimiter when reassembling the overflow into $value, which silently eats exactly the
  # single '=' padding byte base64 appends to any value whose length is 2 mod 3, and
  # corrupts it. A front-anchored split on the FIRST '=' only can't lose bytes after it.
  while IFS= read -r line || [ -n "$line" ]; do
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      install_dir_base64) CFG_INSTALL_DIR="$(config_decode "$value")" ;;
      appliance) CFG_APPLIANCE="$value" ;;
      gvisor) CFG_GVISOR="$value" ;;
      llm_provider_base64) CFG_LLM_PROVIDER="$(config_decode "$value")" ;;
      llm_base_url_base64) CFG_LLM_BASE_URL="$(config_decode "$value")" ;;
      llm_model_base64) CFG_LLM_MODEL="$(config_decode "$value")" ;;
      openrouter_api_key_base64) CFG_OPENROUTER_API_KEY="$(config_decode "$value")" ;;
      embed_image_base64) CFG_EMBED_IMAGE="$(config_decode "$value")" ;;
      embed_ngl_base64) CFG_EMBED_NGL="$(config_decode "$value")" ;;
      # format=1 is what buys forward compatibility -- a future wizard bumps to
      # format=2 and the check above already refuses that loudly -- so within
      # format=1 a key naming nothing above can only be a typo or corruption.
      format|'') ;;
      '#'*) ;;
      *) echo "FAIL: unknown key '$key' in $path" >&2; exit 2 ;;
    esac
  done < "$path"
}

config_decode() {
  [ -n "$1" ] || { printf ''; return 0; }
  printf '%s' "$1" | base64 -d
}

# Only non-empty values are written. An absent key means "the wizard did not ask", not
# "clear it": clearing is how a re-run would wipe a key the operator set by hand, and
# install.sh's whole contract is that re-running preserves explicit settings.
apply_install_config() {
  if [ -n "$CFG_LLM_PROVIDER" ]; then set_env_value AURA_LLM_PROVIDER "$CFG_LLM_PROVIDER"; fi
  if [ -n "$CFG_LLM_BASE_URL" ]; then set_env_value AURA_LLM_BASE_URL "$CFG_LLM_BASE_URL"; fi
  if [ -n "$CFG_LLM_MODEL" ]; then set_env_value AURA_LLM_MODEL "$CFG_LLM_MODEL"; fi
  if [ -n "$CFG_OPENROUTER_API_KEY" ]; then set_env_value OPENROUTER_API_KEY "$CFG_OPENROUTER_API_KEY"; fi
  if [ -n "$CFG_EMBED_IMAGE" ]; then set_env_value AURA_EMBED_IMAGE "$CFG_EMBED_IMAGE"; fi
  if [ -n "$CFG_EMBED_NGL" ]; then set_env_value AURA_EMBED_NGL "$CFG_EMBED_NGL"; fi
}

env_value() {
  key="$1"
  awk -F= -v k="$key" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' .env
}

# ensure_embed_model puts the embedding GGUF where the sidecar expects it.
#
# The sidecar is started with a LOCAL `-m <path>`, not `--hf-repo`, because it has no
# egress: a first boot that tries to reach HuggingFace fails with "Could not establish
# connection" and restart-loops. That made the model a manual prerequisite an operator had
# no way to discover — the install left a path pointing at a file that had to appear by
# magic. This function is that missing step.
#
# It also fixes something the manual copy hid for months. The appliance was running
# unsloth's `embeddinggemma-300M-Q8_0.gguf` (328,577,056 bytes, 314 tensors), which OMITS
# EmbeddingGemma's two sentence-transformers dense projections. Without them llama.cpp
# returns the raw backbone output: still 768-wide, still no error, just not the model's
# embeddings. `convert_hf_to_gguf.py` drops them unless `--sentence-transformers-dense-modules`
# is passed, and Google's own maintainer confirms the projections are part of the model
# (huggingface.co/google/embeddinggemma-300m/discussions/22). ggml-org's build carries them
# — 316 tensors including dense_2/dense_3 — so that is the default here, and the check
# below is for exactly those two tensors rather than a size or a checksum, because that is
# the property that was actually wrong.
EMBED_MODEL_URL_DEFAULT="https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/main/embeddinggemma-300M-Q8_0.gguf"

EMBED_MODEL_PATH_DEFAULT="/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf"

embed_model_url() {
  url="$(env_value AURA_EMBED_MODEL_URL)"
  [ -n "$url" ] || url="$EMBED_MODEL_URL_DEFAULT"
  printf '%s\n' "$url"
}

# One HEAD, three answers: the size the fetch checks against, plus the two provenance
# values compose demands. `-L` prints the headers of EVERY hop and the CDN's final hop
# carries its own `etag:` that is NOT the artifact digest, so every reader below takes
# the FIRST match and stops.
embed_model_headers() {
  curl -fsIL "$(embed_model_url)" 2>/dev/null
}

# Reads one header from stdin. `strip` is a character class dropped from the value,
# which is how the quotes come off an ETag and stray CR off a header line.
embed_header_value() {
  awk -v want="$1" -v strip="$2" '
    BEGIN { IGNORECASE = 1; want = tolower(want) ":" }
    tolower($1) == want { gsub(strip, "", $2); print $2; exit }
  '
}

ensure_embed_model() {
  # Both values are ALSO defaulted in compose.yaml, so an .env that omits them still
  # boots a sidecar pointing at this path. Returning early on an absent key would skip
  # the fetch silently and leave that exact install broken — the failure this function
  # exists to prevent. Mirror the compose default instead.
  model_path="$(env_value AURA_EMBED_MODEL_PATH)"
  [ -n "$model_path" ] || model_path="$EMBED_MODEL_PATH_DEFAULT"
  model_url="$(embed_model_url)"

  # Upstream is the size authority: pinning one here would turn a legitimate upstream
  # rebuild into a failed install, while asking the server costs one HEAD request.
  want_bytes="$(embed_model_headers | embed_header_value x-linked-size '[^0-9]')"

  docker compose create aura-llama-embed >/dev/null 2>&1 || true
  embed_cid="$(docker compose ps -aq aura-llama-embed 2>/dev/null | head -1)"
  if [ -z "$embed_cid" ]; then
    echo "FAIL: could not materialise the aura-llama-embed container to place the model" >&2
    exit 1
  fi
  have_bytes="$(docker run --rm --volumes-from "$embed_cid" alpine \
    sh -c "stat -c %s '$model_path' 2>/dev/null || echo 0" 2>/dev/null | tr -d '\r')"

  if [ -n "$want_bytes" ] && [ "$have_bytes" = "$want_bytes" ]; then
    echo "embedding model already present (${have_bytes} bytes)"
    return 0
  fi
  if [ "$have_bytes" != "0" ]; then
    echo "embedding model differs from upstream (${have_bytes} vs ${want_bytes:-unknown} bytes) — refetching"
  fi

  tmp_model="$(mktemp -t aura-embed-model.XXXXXX)"
  trap 'rm -f "$tmp_model"' EXIT
  scripts/fetch_embedding_model.sh "$tmp_model" "$model_url"

  docker cp "$tmp_model" "${embed_cid}:${model_path}" || {
    echo "FAIL: could not place the embedding model into the sidecar volume" >&2
    exit 1
  }
  rm -f "$tmp_model"
  trap - EXIT
  echo "embedding model installed at ${model_path}"
}

set_env_value() {
  key="$1"
  value="$2"
  tmp="$(mktemp)"
  awk -F= -v k="$key" -v v="$value" '
    BEGIN { done = 0 }
    $1 == k && done == 0 { print k "=" v; done = 1; next }
    { print }
    END { if (done == 0) print k "=" v }
  ' .env > "$tmp"
  cat "$tmp" > .env
  rm -f "$tmp"
}

ensure_env_default() {
  key="$1"
  value="$2"
  if [ -n "$(env_value "$key")" ]; then
    return
  fi
  set_env_value "$key" "$value"
}

ensure_generated_env_secret() {
  key="$1"
  bytes="$2"
  prefix="${3:-}"
  if [ -n "$(env_value "$key")" ]; then
    return
  fi
  command -v openssl >/dev/null 2>&1 || {
    echo "FAIL: openssl is required to generate ${key}." >&2
    exit 1
  }
  set_env_value "$key" "${prefix}$(openssl rand -hex "$bytes")"
}

ensure_objectstore_env_secrets() {
  ensure_generated_env_secret AURA_OBJECTSTORE_ACCESS_KEY 12 GK
  ensure_generated_env_secret AURA_OBJECTSTORE_SECRET_KEY 32
  ensure_generated_env_secret GARAGE_RPC_SECRET 32
  # Phase 36 D-08: bearer token for the internal-only Garage Admin API v2 (:3903).
  ensure_generated_env_secret AURA_GARAGE_ADMIN_TOKEN 32
}

# AURA_EMBED_REVISION/AURA_EMBED_FINGERPRINT are not secrets to invent: they name the
# exact artifact this install serves, and a pair that does not match it defeats the whole
# point — the mismatch that is supposed to stop vectors from two different models being
# compared. HuggingFace answers both in the HEAD already made for the size: X-Repo-Commit
# is the revision, and for an LFS object X-Linked-ETag IS the file's SHA-256. Derive them.
ensure_embed_provenance() {
  if [ -n "$(env_value AURA_EMBED_REVISION)" ] && [ -n "$(env_value AURA_EMBED_FINGERPRINT)" ]; then
    return
  fi
  headers="$(embed_model_headers)"
  revision="$(printf '%s\n' "$headers" | embed_header_value x-repo-commit '[^0-9a-fA-F]')"
  fingerprint="$(printf '%s\n' "$headers" | embed_header_value x-linked-etag '[^0-9a-fA-F]')"
  if [ -z "$revision" ] || [ -z "$fingerprint" ]; then
    echo "FAIL: could not derive AURA_EMBED_REVISION/AURA_EMBED_FINGERPRINT from $(embed_model_url)." >&2
    echo "      compose requires both. A non-HuggingFace mirror does not serve those headers;" >&2
    echo "      set the pair in .env from the artifact you actually serve." >&2
    exit 1
  fi
  ensure_env_default AURA_EMBED_REVISION "$revision"
  ensure_env_default AURA_EMBED_FINGERPRINT "$fingerprint"
}

# An :edge install tracks GHCR continuously (deploy/aura-image-update.*): compose
# must re-pull the moving tag on every up, and each MCP sidecar the machine runs
# needs its own moving tag — a SHA-pinned default would make the update timer a
# no-op. Idempotent like the rest: explicit operator choices are preserved.
ensure_edge_channel_env() {
  case "$(env_value AURA_IMAGE)" in
    *:edge)
      ensure_env_default AURA_PULL_POLICY always
      ensure_env_default AURA_ARCADEDB_MCP_IMAGE ghcr.io/chetto1983/aura-arcadedb-mcp:edge
      ensure_env_default AURA_PIM_MCP_IMAGE ghcr.io/chetto1983/aura-pim-mcp:sidecar
      ensure_env_default AURA_WHATSAPP_MCP_IMAGE ghcr.io/chetto1983/whatsapp-mcp:latest
      # caddy and ingest are repo-built images: without a published pin a fresh
      # appliance tries to `docker build` them against a payload that has no
      # build context and dies (measured 2026-08-31, first clean-host E2E).
      ensure_env_default AURA_CADDY_IMAGE ghcr.io/chetto1983/aura-caddy:edge
      ensure_env_default AURA_CADDY_PULL_POLICY always
      ensure_env_default AURA_INGEST_IMAGE ghcr.io/chetto1983/aura-ingest:edge
      ensure_env_default AURA_INGEST_PULL_POLICY always
      # The per-identity box and its egress sidecar are repo-built too, but they
      # are NOT compose services -- the daemon creates them per identity -- so
      # they escaped the pin above and defaulted to the bare local names
      # aura-sandbox:latest / aura-egress:latest, which no appliance has ever
      # built and no registry serves. The daemon's ensureImage then cannot find
      # them, Route denies, and EVERY box-capable tool denies with it while the
      # sandbox readiness probe holds the machine unhealthy. Pointing them at
      # the edge tags makes ensureImage pull on first box creation, which is the
      # whole install: there is nothing to build here.
      ensure_env_default AURA_SANDBOX_IMAGE ghcr.io/chetto1983/aura-sandbox:edge
      ensure_env_default AURA_SANDBOX_EGRESS_IMAGE ghcr.io/chetto1983/aura-egress:edge
      ;;
  esac
}

ensure_internal_env_secrets() {
  command -v openssl >/dev/null 2>&1 || {
    echo "FAIL: openssl is required to generate Aura internal secrets." >&2
    exit 1
  }
  ensure_generated_env_secret POSTGRES_PASSWORD 32
  ensure_generated_env_secret AURA_ACCESS_TOKEN 32
  ensure_generated_env_secret AURA_AUTHULA_SECRET 32
  # ArcadeDB holds the memory, one database per identity. compose fail-fasts on
  # all three, and it interpolates the whole file before selecting a service, so
  # a missing one aborts every compose invocation.
  ensure_generated_env_secret ARCADEDB_PASSWORD 32
  ensure_generated_env_secret ARCADEDB_APP_PASSWORD 32
  ensure_generated_env_secret AURA_ARCADEDB_TENANT_SECRET 32
  ensure_generated_env_secret SEARXNG_SECRET 32
  ensure_objectstore_env_secrets
  ensure_embed_provenance
  ensure_env_default ARCADEDB_APP_USER "aura_memory"
  ensure_env_default ARCADEDB_DATABASE "aura_memory"
  ensure_env_default POSTGRES_IMAGE "${POSTGRES_IMAGE:-postgres:18.4-alpine3.24}"
  ensure_env_default AURA_IMAGE "${AURA_IMAGE:-$DEFAULT_IMAGE}"
  ensure_edge_channel_env

  # Observability is an appliance default, not a hidden profile an operator must
  # remember after every reboot. Preserve additional profiles and explicit off
  # switches, but migrate the one known-stale endpoint from the old shared-network
  # topology.
  profiles="$(env_value COMPOSE_PROFILES)"
  case ",${profiles}," in
    *,observability,*) ;;
    ,,) set_env_value COMPOSE_PROFILES observability ;;
    *) set_env_value COMPOSE_PROFILES "${profiles},observability" ;;
  esac
  ensure_env_default AURA_OTEL_EXPORTER otlp
  ensure_env_default AURA_OBSERVABILITY_CHECK_ENABLED true
  ensure_env_default AURA_OTEL_ENDPOINT tempo:4317
  if [ "$(env_value AURA_OTEL_ENDPOINT)" = "localhost:4317" ]; then
    set_env_value AURA_OTEL_ENDPOINT tempo:4317
  fi
}

ensure_objectstore_public_endpoint() {
  if [ -n "$(env_value AURA_OBJECTSTORE_PUBLIC_ENDPOINT)" ]; then
    return
  fi
  set_env_value AURA_OBJECTSTORE_PUBLIC_ENDPOINT "https://$(host_for_summary)"
}

write_env_if_missing() {
  if [ -f .env ]; then
    ensure_internal_env_secrets
    chmod 600 .env
    return
  fi

  command -v openssl >/dev/null 2>&1 || {
    echo "FAIL: openssl is required for secret generation." >&2
    exit 1
  }

  pg_pw="$(openssl rand -hex 32)"
  access_token="$(openssl rand -hex 32)"
  objectstore_access_key="GK$(openssl rand -hex 12)"
  objectstore_secret_key="$(openssl rand -hex 32)"
  garage_rpc_secret="$(openssl rand -hex 32)"
  garage_admin_token="$(openssl rand -hex 32)"
  authula_secret="$(openssl rand -hex 32)"
  searxng_secret="$(openssl rand -hex 32)"
  aura_image="${AURA_IMAGE:-$DEFAULT_IMAGE}"
  openrouter_key="${OPENROUTER_API_KEY:-}"

  umask 077
  cat > .env <<EOF
POSTGRES_PASSWORD=${pg_pw}
POSTGRES_IMAGE=${POSTGRES_IMAGE:-postgres:18.4-alpine3.24}
POSTGRES_USER=aura
POSTGRES_DB=aura
POSTGRES_HOST=127.0.0.1
POSTGRES_PORT=5432

AURA_IMAGE=${aura_image}
AURA_ACCESS_TOKEN=${access_token}
AURA_AUTHULA_SECRET=${authula_secret}
AURA_HTTPS_PORT=443
AURA_AGUI_PORT=9080
AURA_SETUP_PORT=9081
AURA_WHATSAPP_MCP_PORT=8092
AURA_ARCADEDB_MCP_PORT=8096
AURA_BACKUP_DIR=./backups
SEARXNG_SECRET=${searxng_secret}
COMPOSE_PROFILES=observability
AURA_OTEL_EXPORTER=otlp
AURA_OTEL_ENDPOINT=tempo:4317
AURA_OBSERVABILITY_CHECK_ENABLED=true

AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda
AURA_EMBED_MODEL_PATH=/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf
# Where the installer fetches that file from when it is missing or differs from upstream.
# ggml-org's build and NOT unsloth's: unsloth's Q8_0 omits the two sentence-transformers
# dense projections, which makes llama.cpp return backbone-only vectors at the correct
# width with no error at all. The installer refuses a model without them.
AURA_EMBED_MODEL_URL=https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/main/embeddinggemma-300M-Q8_0.gguf
AURA_EMBED_NGL=99
AURA_EMBED_DIMENSIONS=768

AURA_OBJECTSTORE_ACCESS_KEY=${objectstore_access_key}
AURA_OBJECTSTORE_SECRET_KEY=${objectstore_secret_key}
GARAGE_RPC_SECRET=${garage_rpc_secret}
AURA_GARAGE_ADMIN_TOKEN=${garage_admin_token}

OPENROUTER_API_KEY=${openrouter_key}
EOF
  # The heredoc above is the fresh-install template and it WILL drift: compose
  # fail-fasts on every `:?` variable and interpolates the whole file before it
  # selects a service, so one missing name aborts every compose invocation — including
  # `ps`. Rather than list them twice, hand the file to the same idempotent filler the
  # already-have-a-.env path uses; it only writes keys that are absent. Counting them
  # here is what rotted last time: `grep -oE '\$\{[A-Z_]+:\?' compose.yaml | sort -u`
  # is the live count, and this installer must cover all of it.
  ensure_internal_env_secrets
  chmod 600 .env
}

native_linux_docker() {
  [ "$OS" = "Linux" ] || return 1
  docker_os="$(docker info --format '{{.OperatingSystem}}' 2>/dev/null || true)"
  case "$docker_os" in
    *"Docker Desktop"*) return 1 ;;
  esac
  return 0
}

provision_gvisor() {
  # return 0, not bare return: bare return propagates the failed test's status 1,
  # and under top-level `set -e` that silently kills every install without --gvisor.
  [ "$GVISOR" -eq 1 ] || return 0
  native_linux_docker || {
    echo "FAIL: --gvisor is only supported on native Linux Docker, not Docker Desktop or ${OS}." >&2
    exit 1
  }
  command -v dpkg >/dev/null 2>&1 || {
    echo "FAIL: --gvisor provisioning currently requires a Debian/Ubuntu-style host with dpkg." >&2
    exit 1
  }
  as_root apt-get update
  as_root apt-get install -y apt-transport-https ca-certificates curl gnupg
  curl -fsSL https://gvisor.dev/archive.key \
    | as_root gpg --dearmor -o /usr/share/keyrings/gvisor-archive-keyring.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" \
    | as_root tee /etc/apt/sources.list.d/gvisor.list >/dev/null
  as_root apt-get update
  as_root apt-get install -y runsc
  as_root runsc install || true
  as_root systemctl reload docker
}

install_systemd_unit() {
  [ "$APPLIANCE" -eq 1 ] || return 0
  if [ "$OS" != "Linux" ]; then
    echo "WARN: --appliance systemd autostart is Linux-only; skipping unit install." >&2
    return
  fi
  if [ "$INSTALL_DIR" != "/opt/aura" ]; then
    echo "FAIL: deploy/aura.service expects /opt/aura; use --dir /opt/aura for --appliance." >&2
    exit 1
  fi

  as_root cp deploy/aura.service /etc/systemd/system/aura.service
  # Nessun drop-in per gVisor: il runtime e' AURA_RUNTIME in .env, quindi l'unit di base
  # va bene identica in entrambi i casi.
  # Edge appliances also get the self-update timer, so the machine keeps tracking
  # GHCR without an operator; a version-pinned install deliberately does not.
  case "$(env_value AURA_IMAGE)" in
    *:edge)
      as_root install -m 0755 deploy/aura-image-update.sh /usr/local/sbin/aura-image-update.sh
      as_root cp deploy/aura-image-update.service /etc/systemd/system/aura-image-update.service
      as_root cp deploy/aura-image-update.timer /etc/systemd/system/aura-image-update.timer
      ;;
  esac
  as_root systemctl daemon-reload
  as_root systemctl enable --now aura.service
  case "$(env_value AURA_IMAGE)" in
    *:edge) as_root systemctl enable --now aura-image-update.timer ;;
  esac
}

host_for_summary() {
  if command -v hostname >/dev/null 2>&1; then
    if hostname -I >/dev/null 2>&1; then
      first_ip="$(hostname -I | awk '{print $1}')"
      [ -n "$first_ip" ] && { echo "$first_ip"; return; }
    fi
    hostname
  else
    echo "localhost"
  fi
}

# Sourcing this script defines its functions and stops here; executing it falls through and
# installs. The tests drive one function at a time, which is impossible while the file is
# imperative top-to-bottom.
if [ "$AURA_EXECUTED" = 0 ]; then
  return 0
fi

# Parsed before any side effect (hardware checks, Docker, mkdir) so a bad --config fails
# fast instead of after a preflight the operator has no reason to have run at all.
if [ -n "$CONFIG_FILE" ]; then
  parse_install_config "$CONFIG_FILE"
  if [ -n "$CFG_INSTALL_DIR" ]; then INSTALL_DIR="$CFG_INSTALL_DIR"; fi
  if [ "$CFG_APPLIANCE" = "true" ]; then APPLIANCE=1; fi
  if [ "$CFG_GVISOR" = "true" ]; then APPLIANCE=1; GVISOR=1; fi
fi

preflight_hw
install_docker
provision_gvisor

as_root mkdir -p "$INSTALL_DIR" "$INSTALL_DIR/caddy" "$INSTALL_DIR/deploy" "$INSTALL_DIR/backups" "$INSTALL_DIR/scripts" "$INSTALL_DIR/searxng" \
  "$INSTALL_DIR/observability/grafana/dashboards" \
  "$INSTALL_DIR/observability/grafana/provisioning/dashboards" \
  "$INSTALL_DIR/observability/grafana/provisioning/datasources" \
  "$INSTALL_DIR/observability/grafana/provisioning/alerting" \
  "$INSTALL_DIR/observability/grafana/provisioning/plugins" \
  "$INSTALL_DIR/observability/prometheus/rules" \
  "$INSTALL_DIR/observability/prometheus/tests" \
  "$INSTALL_DIR/observability/tempo"
if need_sudo; then
  as_root chown -R "$(id -u):$(id -g)" "$INSTALL_DIR"
fi

cd "$INSTALL_DIR"
download_file compose.yaml compose.yaml
download_file caddy/Caddyfile caddy/Caddyfile
# The other value compose.yaml's ${AURA_CADDYFILE:-Caddyfile} mount can take
# (AURA_CADDYFILE=Caddyfile.domain in .env) -- see the garage.toml/backup.json
# comment below for what an unshipped bind-mount source does to the container.
download_file caddy/Caddyfile.domain caddy/Caddyfile.domain
# compose bind-mounts these FILEs into containers that have no `profiles:` key
# and so always start; without either, Docker fabricates a directory at the
# mount source instead. garage crash-loops on "IO error: Is a directory"
# (measured 2026-08-31, clean-host E2E kill #3) -- loud. backup.json is quiet:
# it enables ArcadeDB's AutoBackupSchedulerPlugin for every mem_<uuid> database,
# so its absence just disables hourly memory backups while the healthcheck (which
# never checks backup state) stays green.
download_file docker/garage/garage.toml docker/garage/garage.toml
download_file docker/arcadedb/backup.json docker/arcadedb/backup.json
download_file deploy/aura.service deploy/aura.service
download_file deploy/aura-image-update.sh deploy/aura-image-update.sh
download_file deploy/aura-image-update.service deploy/aura-image-update.service
download_file deploy/aura-image-update.timer deploy/aura-image-update.timer
download_file searxng/settings.yml searxng/settings.yml
download_file searxng/limiter.toml searxng/limiter.toml
download_file scripts/garage_bootstrap.sh scripts/garage_bootstrap.sh
download_file scripts/fetch_embedding_model.sh scripts/fetch_embedding_model.sh
download_file scripts/observability_sidecar_check.sh scripts/observability_sidecar_check.sh
download_file observability/grafana/provisioning/alerting/aura.yml observability/grafana/provisioning/alerting/aura.yml
download_file observability/grafana/provisioning/dashboards/aura.yml observability/grafana/provisioning/dashboards/aura.yml
download_file observability/grafana/provisioning/datasources/aura.yml observability/grafana/provisioning/datasources/aura.yml
download_file observability/grafana/provisioning/plugins/aura.yml observability/grafana/provisioning/plugins/aura.yml
download_file observability/grafana/dashboards/aura-agents.json observability/grafana/dashboards/aura-agents.json
download_file observability/grafana/dashboards/aura-data-retention.json observability/grafana/dashboards/aura-data-retention.json
download_file observability/grafana/dashboards/aura-overview.json observability/grafana/dashboards/aura-overview.json
download_file observability/grafana/dashboards/aura-tools-mcp.json observability/grafana/dashboards/aura-tools-mcp.json
download_file observability/prometheus/prometheus.yml observability/prometheus/prometheus.yml
download_file observability/prometheus/rules/aura-alerts.yml observability/prometheus/rules/aura-alerts.yml
download_file observability/prometheus/rules/aura-recording.yml observability/prometheus/rules/aura-recording.yml
download_file observability/prometheus/tests/aura-rules.test.yml observability/prometheus/tests/aura-rules.test.yml
download_file observability/tempo/tempo.yml observability/tempo/tempo.yml
chmod +x scripts/garage_bootstrap.sh scripts/fetch_embedding_model.sh scripts/observability_sidecar_check.sh deploy/aura-image-update.sh

write_env_if_missing
apply_install_config

ensure_objectstore_public_endpoint

# The optional container runtime travels in .env so installer, systemd and manual
# compose commands all resolve the same sandbox posture.
if [ "$GVISOR" -eq 1 ]; then
  set_env_value AURA_RUNTIME runsc
fi

aura_image="$(env_value AURA_IMAGE)"
if [ "${aura_image}" != "aura:local" ]; then
  docker pull "$aura_image"
fi

ensure_embed_model
docker compose up -d --wait --wait-timeout 300
scripts/observability_sidecar_check.sh
install_systemd_unit

token="$(env_value AURA_ACCESS_TOKEN)"
host="$(host_for_summary)"

cat <<EOF

Aura is starting.

Wizard:
  https://${host}/setup/?token=${token}

Next steps:
  docker compose -f ${INSTALL_DIR}/compose.yaml ps
  docker compose -f ${INSTALL_DIR}/compose.yaml logs -f aura
  Import Caddy's local CA from the caddy-data volume on LAN clients if the browser warns.

Re-running preserves secrets and explicit settings; known-safe deployment defaults may be migrated.
EOF
