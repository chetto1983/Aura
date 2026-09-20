#!/usr/bin/env bash
# Environment and model provisioning shared by the installer entry point.

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
# — 316 tensors including dense_2/dense_3 — so that is the build fetched here, and the check
# below is for exactly those two tensors rather than a size or a checksum, because that is
# the property that was actually wrong.
#
# Both are the release's, never .env's: compose.yaml's `-m` carries the same path, and a
# copy of either in .env froze each machine on the model it was installed with.
EMBED_MODEL_URL="https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/main/embeddinggemma-300M-Q8_0.gguf"
EMBED_MODEL_PATH="/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf"

# One HEAD, three answers: the size the fetch checks against, plus the two provenance
# values compose demands. `-L` prints the headers of EVERY hop and the CDN's final hop
# carries its own `etag:` that is NOT the artifact digest, so every reader below takes
# the FIRST match and stops.
embed_model_headers() {
  curl -fsIL "$EMBED_MODEL_URL" 2>/dev/null
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
  model_path="$EMBED_MODEL_PATH"
  model_url="$EMBED_MODEL_URL"

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
  # parse_install_config is not the only caller: any future one that hands a multi-line
  # value here would otherwise get two .env lines back with no complaint at all.
  case "$value" in
    *$'\n'*|*$'\r'*)
      echo "FAIL: set_env_value $key: value contains a line break, refusing to write it to .env" >&2
      exit 2
      ;;
  esac
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
    echo "FAIL: could not derive AURA_EMBED_REVISION/AURA_EMBED_FINGERPRINT from ${EMBED_MODEL_URL}." >&2
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
      ensure_env_default AURA_PIM_MCP_IMAGE ghcr.io/chetto1983/aura-pim-mcp:sidecar
      ensure_env_default AURA_WHATSAPP_MCP_IMAGE ghcr.io/chetto1983/whatsapp-mcp:latest
      # caddy, ingest and arcadedb-mcp are repo-built images whose compose
      # pull_policy defaults to `never`: without a published pin AND `always` a
      # fresh appliance tries to `docker build` them against a payload that has
      # no build context and dies (measured 2026-08-31 for caddy and ingest on
      # the first clean-host E2E, 2026-09-10 for arcadedb-mcp on the first npx
      # install).
      ensure_env_default AURA_CADDY_IMAGE ghcr.io/chetto1983/aura-caddy:edge
      ensure_env_default AURA_CADDY_PULL_POLICY always
      ensure_env_default AURA_CLOUDFLARED_IMAGE ghcr.io/chetto1983/aura-cloudflared:edge
      ensure_env_default AURA_CLOUDFLARED_PULL_POLICY always
      ensure_env_default AURA_INGEST_IMAGE ghcr.io/chetto1983/aura-ingest:edge
      ensure_env_default AURA_INGEST_PULL_POLICY always
      ensure_env_default AURA_ARCADEDB_MCP_IMAGE ghcr.io/chetto1983/aura-arcadedb-mcp:edge
      ensure_env_default AURA_ARCADEDB_MCP_PULL_POLICY always
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

# Decided here, on the target, the only machine whose answer is true. Docker registers its
# `nvidia` device driver only when nvidia-container-runtime-hook or nvidia-cdi-hook is on
# its PATH (moby daemon/devices_nvidia_linux.go), and without it compose.yaml's
# reservation kills `up` ("could not select device driver nvidia", measured 2026-09-10 on
# a mini PC) -- so a GPU nvidia-smi sees is CUDA only if a hook is there too. A render
# node is Intel or AMD, integrated included, which the Vulkan image drives. Metal cannot
# reach a Docker container on macOS, so a Mac lands on the CPU.
detect_embed_backend() {
  dri_dir="${1:-/dev/dri}"
  if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi >/dev/null 2>&1 &&
    { command -v nvidia-container-runtime-hook >/dev/null 2>&1 || command -v nvidia-cdi-hook >/dev/null 2>&1; }; then
    echo cuda
    return
  fi
  for node in "$dri_dir"/renderD*; do
    if [ -e "$node" ]; then
      echo vulkan
      return
    fi
  done
  echo cpu
}

# One knob: AURA_EMBED_BACKEND is detected once and is the operator's to change after that.
# The overlay and offload are derived from it on every run so they can never disagree, and
# the overlay pins the image build for its backend -- so the image is never written here: a
# value in .env would outrank compose's pin on this machine and freeze the build forever.
# docker compose reads COMPOSE_FILE from .env, so the installer, systemd and the update timer
# all resolve the same files.
ensure_embed_backend_env() {
  if [ -z "$(env_value AURA_EMBED_BACKEND)" ]; then
    set_env_value AURA_EMBED_BACKEND "$(detect_embed_backend "$@")"
  fi
  backend="$(env_value AURA_EMBED_BACKEND)"
  case "$backend" in
    cuda)
      set_env_value COMPOSE_FILE compose.yaml
      set_env_value AURA_EMBED_NGL 99
      ;;
    vulkan)
      set_env_value COMPOSE_FILE compose.yaml:compose.vulkan.yaml
      set_env_value AURA_EMBED_NGL 99
      ;;
    cpu)
      set_env_value COMPOSE_FILE compose.yaml:compose.cpu.yaml
      set_env_value AURA_EMBED_NGL 0
      ;;
    *)
      echo "FAIL: AURA_EMBED_BACKEND must be cuda, vulkan or cpu, got '$backend'." >&2
      exit 2
      ;;
  esac
}

# D-04: make the sandbox image available BEFORE `docker compose up` starts anything, so
# the documented fresh-install path never reaches the boot preflight's refusal
# (cmd/aura/serve_sandbox_preflight.go): a strict, isolation-on daemon whose box image is
# neither present nor pullable now fails loud at boot instead of denying every tool call
# silently at the operator's first shell_exec. Covers the pinned-version path
# ensure_edge_channel_env misses: that function only sets AURA_SANDBOX_IMAGE inside its
# `*:edge` branch, so a pinned install (AURA_INSTALL_REF=vX.Y.Z) never enters it and
# reaches this step with only the value the write_env_if_missing heredoc wrote --
# non-empty, because that heredoc now writes it unconditionally (D-01).
ensure_sandbox_image() {
  sandbox_image="$(env_value AURA_SANDBOX_IMAGE)"
  if [ -z "$sandbox_image" ]; then
    echo "FAIL: AURA_SANDBOX_IMAGE is unset -- cannot make the sandbox image available" >&2
    exit 1
  fi
  egress_image="$(env_value AURA_SANDBOX_EGRESS_IMAGE)"
  if [ -f docker/aura-sandbox/Dockerfile ]; then
    # A repo build context is present (a local/dev install run from a checkout) -- build
    # the same two images `make sandbox-images` builds, against the CONFIGURED refs so
    # the deployed image matches what .env actually names.
    docker build -f docker/aura-sandbox/Dockerfile -t "$sandbox_image" . || {
      echo "FAIL: could not build the sandbox box image ($sandbox_image)" >&2
      exit 1
    }
    if [ -n "$egress_image" ]; then
      docker build -f docker/aura-egress/Dockerfile -t "$egress_image" . || {
        echo "FAIL: could not build the sandbox egress image ($egress_image)" >&2
        exit 1
      }
    fi
    return 0
  fi
  # The documented path: an appliance never builds -- it pulls the published edge/pinned
  # tags scripts/install.sh already resolved into .env (Makefile's own sandbox-images
  # target comment: "An appliance never runs this -- it pulls the published edge tags").
  docker pull "$sandbox_image" || {
    echo "FAIL: could not pull the sandbox box image ($sandbox_image)" >&2
    exit 1
  }
  if [ -n "$egress_image" ]; then
    docker pull "$egress_image" || {
      echo "FAIL: could not pull the sandbox egress image ($egress_image)" >&2
      exit 1
    }
  fi
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
  ensure_env_default AURA_IMAGE "${AURA_IMAGE:-$DEFAULT_IMAGE}"
  ensure_edge_channel_env

  # Observability is an appliance default, not a hidden profile an operator must
  # remember after every reboot. Preserve additional profiles and explicit off
  # switches. The in-stack tracing endpoint is compose's, not this file's.
  profiles="$(env_value COMPOSE_PROFILES)"
  case ",${profiles}," in
    *,observability,*) ;;
    ,,) set_env_value COMPOSE_PROFILES observability ;;
    *) set_env_value COMPOSE_PROFILES "${profiles},observability" ;;
  esac
  ensure_env_default AURA_OTEL_EXPORTER otlp
  ensure_env_default AURA_OBSERVABILITY_CHECK_ENABLED true
  if [ "$APPLIANCE" -eq 1 ]; then
    bash "${AURA_PAYLOAD_DIR:-$INSTALL_DIR}/scripts/appliance_posture.sh" .env
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

  umask 077
  cat > .env <<EOF
POSTGRES_PASSWORD=${pg_pw}
POSTGRES_USER=aura
POSTGRES_DB=aura
POSTGRES_HOST=127.0.0.1
POSTGRES_PORT=5432

AURA_IMAGE=${aura_image}
AURA_ACCESS_TOKEN=${access_token}
AURA_AUTHULA_SECRET=${authula_secret}
# Appliance upgrades enforce the same strict posture as a fresh install.
AURA_PROFILE=single_user_hardened
AURA_MUSR_ISOLATION=true
AURA_SANDBOX_IMAGE=ghcr.io/chetto1983/aura-sandbox:edge
AURA_HTTPS_PORT=443
AURA_AGUI_PORT=9080
AURA_SETUP_PORT=9081
AURA_WHATSAPP_MCP_PORT=8092
AURA_ARCADEDB_MCP_PORT=8096
AURA_BACKUP_DIR=./backups
SEARXNG_SECRET=${searxng_secret}
COMPOSE_PROFILES=observability
AURA_OTEL_EXPORTER=otlp
AURA_OBSERVABILITY_CHECK_ENABLED=true

AURA_OBJECTSTORE_ACCESS_KEY=${objectstore_access_key}
AURA_OBJECTSTORE_SECRET_KEY=${objectstore_secret_key}
GARAGE_RPC_SECRET=${garage_rpc_secret}
AURA_GARAGE_ADMIN_TOKEN=${garage_admin_token}
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
