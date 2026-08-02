#!/usr/bin/env bash
# Aura appliance installer. Intended usage:
#   curl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/<tag>/scripts/install.sh | bash
#
# Optional env:
#   AURA_INSTALL_REF=vX.Y.Z
#   AURA_IMAGE=ghcr.io/chetto1983/aura:vX.Y.Z
#   POSTGRES_IMAGE=postgres:18.4-alpine3.24
#   AURA_INSTALL_DIR=/opt/aura

set -euo pipefail

export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

APPLIANCE=0
GVISOR=0
INSTALL_DIR="${AURA_INSTALL_DIR:-}"

usage() {
  cat <<'EOF'
usage: install.sh [--appliance] [--gvisor] [--dir PATH]

  --appliance  install and enable the systemd aura.service unit
  --gvisor     provision runsc and set AURA_RUNTIME=runsc in .env
  --dir PATH   installation directory (default: /opt/aura on Linux)
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --appliance) APPLIANCE=1 ;;
    --gvisor) APPLIANCE=1; GVISOR=1 ;;
    --dir)
      [ "$#" -ge 2 ] || { echo "FAIL: --dir requires a path" >&2; exit 2; }
      INSTALL_DIR="$2"
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

OS="$(uname -s)"
if [ -z "$INSTALL_DIR" ]; then
  if [ "$OS" = "Darwin" ]; then
    INSTALL_DIR="$HOME/.aura/appliance"
  else
    INSTALL_DIR="/opt/aura"
  fi
fi

RAW_REF="${AURA_INSTALL_REF:-main}"
RAW_BASE="${AURA_INSTALL_BASE_URL:-https://raw.githubusercontent.com/chetto1983/Aura/${RAW_REF}}"
IMAGE_TAG="${AURA_IMAGE_TAG:-${RAW_REF}}"
DEFAULT_IMAGE="ghcr.io/chetto1983/aura:${IMAGE_TAG}"

if [ "$IMAGE_TAG" = "main" ] && [ -z "${AURA_IMAGE:-}" ]; then
  echo "WARN: AURA_INSTALL_REF is main; set AURA_IMAGE or AURA_INSTALL_REF to a release tag for a clean-host install." >&2
fi

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

download_file() {
  src="$1"
  dst="$2"
  mkdir -p "$(dirname "$dst")"
  curl -fsSL "${RAW_BASE}/${src}" -o "$dst" || {
    echo "FAIL: could not fetch ${RAW_BASE}/${src}" >&2
    exit 1
  }
}

env_value() {
  key="$1"
  awk -F= -v k="$key" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' .env
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
  ensure_generated_env_secret AURA_PIM_MCP_ADMIN_TOKEN 32
  ensure_generated_env_secret SEARXNG_SECRET 32
  ensure_objectstore_env_secrets
  ensure_env_default ARCADEDB_APP_USER "aura_memory"
  ensure_env_default ARCADEDB_DATABASE "aura_memory"
  ensure_env_default POSTGRES_IMAGE "${POSTGRES_IMAGE:-postgres:18.4-alpine3.24}"
  ensure_env_default AURA_IMAGE "${AURA_IMAGE:-$DEFAULT_IMAGE}"
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
  pim_admin_token="$(openssl rand -hex 32)"
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
AURA_PIM_MCP_ADMIN_TOKEN=${pim_admin_token}
AURA_BACKUP_DIR=./backups
SEARXNG_SECRET=${searxng_secret}

AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda
AURA_EMBED_MODEL_PATH=/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf
AURA_EMBED_POOLING=last
AURA_EMBED_NGL=99
AURA_EMBED_DIMENSIONS=768

AURA_OBJECTSTORE_ACCESS_KEY=${objectstore_access_key}
AURA_OBJECTSTORE_SECRET_KEY=${objectstore_secret_key}
GARAGE_RPC_SECRET=${garage_rpc_secret}
AURA_GARAGE_ADMIN_TOKEN=${garage_admin_token}

OPENROUTER_API_KEY=${openrouter_key}
EOF
  # The heredoc above is the fresh-install template and it WILL drift: compose
  # fail-fasts on twelve `:?` variables and interpolates the whole file before it
  # selects a service, so one missing name aborts every compose invocation. Rather
  # than list them twice, hand the file to the same idempotent filler the
  # already-have-a-.env path uses — it only writes keys that are absent.
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
  [ "$GVISOR" -eq 1 ] || return
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
  [ "$APPLIANCE" -eq 1 ] || return
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
  as_root systemctl daemon-reload
  as_root systemctl enable --now aura.service
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

preflight_hw
install_docker
provision_gvisor

as_root mkdir -p "$INSTALL_DIR" "$INSTALL_DIR/caddy" "$INSTALL_DIR/deploy" "$INSTALL_DIR/backups" "$INSTALL_DIR/scripts" "$INSTALL_DIR/searxng"
if need_sudo; then
  as_root chown -R "$(id -u):$(id -g)" "$INSTALL_DIR"
fi

cd "$INSTALL_DIR"
download_file compose.yaml compose.yaml
download_file compose.minipc.yaml compose.minipc.yaml
download_file caddy/Caddyfile caddy/Caddyfile
download_file deploy/aura.service deploy/aura.service
download_file searxng/settings.yml searxng/settings.yml
download_file searxng/limiter.toml searxng/limiter.toml
download_file scripts/garage_bootstrap.sh scripts/garage_bootstrap.sh
chmod +x scripts/garage_bootstrap.sh

write_env_if_missing

ensure_objectstore_public_endpoint

# Quale stack e quale runtime: entrambi finiscono in .env, non in un -f. Cosi' ogni
# comando successivo — il nostro, quello di systemd, quello che l'operatore digita a mano
# sei mesi dopo — vede la stessa configurazione senza doversi ricordare niente.
if [ "$APPLIANCE" -eq 1 ]; then
  set_env_value COMPOSE_FILE compose.yaml:compose.minipc.yaml
  set_env_value AURA_EMBED_NGL 0
fi
if [ "$GVISOR" -eq 1 ]; then
  set_env_value AURA_RUNTIME runsc
fi

aura_image="$(env_value AURA_IMAGE)"
if [ "${aura_image}" != "aura:local" ]; then
  docker pull "$aura_image"
fi

docker compose up -d
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

Re-running this installer will keep ${INSTALL_DIR}/.env byte-identical.
EOF
