#!/usr/bin/env bash
#
# Publishes every appliance image from the operator's machine. It was the image home's only
# publisher while GitHub ran no workflow (docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md);
# since 2026-10-21 GitHub Actions publishes ghcr again, and this script serves the GitLab copy
# that bridged hosts still read: run it with AURA_PUBLISH_REGISTRY=registry.gitlab.com/chetto1983/aura
# until none is left (PRD §17, "Return to ghcr"). It builds
# what .github/workflows/publish-aura-edge.yml and publish-arcadedb-mcp.yml build, the same way,
# plus the two forks; nothing is pushed until every build and image contract passed, and the
# moving tags move only once every per-commit tag is in the registry, aura:edge last.
#
#   scripts/publish_images.sh [<aura commit>]        (default HEAD)
#
# The commit must already be on the remote's master: appliances run whatever aura:edge names,
# and its master-<sha> tag says the commit is there for anyone to read.
# Docker must already reach the registry (`glab auth configure-docker`).
#   AURA_PUBLISH_REMOTE       default: gitlab
#   AURA_PUBLISH_REGISTRY     default: IMAGE_HOME in the published commit's deploy/aura-image-update.sh
#   AURA_PUBLISH_PIM_GIT      default: https://gitlab.com/Chetto1983/aura-pim-mcp.git
#   AURA_PUBLISH_WHATSAPP_GIT default: https://gitlab.com/Chetto1983/whatsapp-mcp.git
#   AURA_PUBLISH_FORK_REF     default: main

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)"
AURA_SOURCE=https://gitlab.com/Chetto1983/aura

# name:dockerfile, in the order publish-aura-edge.yml moves the edge tags: aura last.
EDGE_IMAGES=(
  aura-caddy:docker/caddy/Dockerfile
  aura-cloudflared:docker/cloudflared/Dockerfile
  aura-ingest:docker/aura-ingest/Dockerfile
  aura-sandbox:docker/aura-sandbox/Dockerfile
  aura-egress:docker/aura-egress/Dockerfile
  aura:docker/aura/Dockerfile
)
ARCADEDB_MCP=aura-arcadedb-mcp:docker/arcadedb-mcp/Dockerfile
# Each fork builds its root Dockerfile and carries both moving tags appliances follow.
FORK_NAMES=(aura-pim-mcp whatsapp-mcp)
FORK_MOVING_TAGS=(sidecar latest)
# image, the variable its contract test reads the image from, the test.
CONTRACTS=(
  "aura-cloudflared AURA_CLOUDFLARED_TEST_IMAGE scripts/cloudflared_image_contract_test.sh"
  "aura-ingest AURA_INGEST_IMAGE scripts/ingest_image_contract_test.sh"
  "aura-sandbox AURA_SANDBOX_IMAGE scripts/sandbox_image_contract_test.sh"
)

fork_git() {
  case "$1" in
    aura-pim-mcp) echo "${AURA_PUBLISH_PIM_GIT:-https://gitlab.com/Chetto1983/aura-pim-mcp.git}" ;;
    whatsapp-mcp) echo "${AURA_PUBLISH_WHATSAPP_GIT:-https://gitlab.com/Chetto1983/whatsapp-mcp.git}" ;;
  esac
}

# <tag> <context dir> <dockerfile relative to it> <revision> <source url> [docker build args]
build_image() {
  local tag="$1" ctx="$2" file="$3" rev="$4" src="$5"
  shift 5
  docker build --platform linux/amd64 -f "${ctx}/${file}" \
    --label "org.opencontainers.image.source=${src}" \
    --label "org.opencontainers.image.revision=${rev}" "$@" -t "${tag}" "${ctx}"
}

# A push the registry drops gets one more try, and the second push skips every layer the first
# stored. On 2026-10-14 three of five publishes lost one upload: twice a broken pipe through
# Docker Desktop's proxy, once a response-header timeout.
push_retrying() {
  docker push "$1" && return 0
  sleep 10
  docker push "$1"
}

# A run's images are the registry's once pushed, and a rebuild comes from the build cache, so the
# local copies go when main returns, whatever it returns. Kept, five publishes left 38 tags and
# 14.35 GB on the operator's Docker (2026-10-14). The build cache stays.
remove_built_images() {
  local tag
  for tag in "$@"; do
    if docker image rm "${tag}" >/dev/null; then
      echo "removed ${tag}"
    else
      echo "publish: ${tag} stays on this machine" >&2
    fi
  done
}

# Every step that can fail returns explicitly: errexit is suspended for a function called as a
# condition (`if main …`, `main || …`), and a build that failed there must still push nothing.
main() {
  local registry remote sha work entry name file fork_sha tag image var test week
  local -a pushes=() moves=() args=()
  remote="${AURA_PUBLISH_REMOTE:-gitlab}"
  sha="$(git -C "${repo_root}" rev-parse "${1:-HEAD}^{commit}")" || return 1
  git -C "${repo_root}" fetch --quiet "${remote}" master || return 1
  git -C "${repo_root}" merge-base --is-ancestor "${sha}" "${remote}/master" ||
    { echo "publish: ${sha} is not on ${remote}/master; push it first" >&2; return 1; }
  work="$(mktemp -d)"
  trap 'rm -rf "${work}"; remove_built_images "${pushes[@]}"' RETURN
  mkdir -p "${work}/aura"
  git -C "${repo_root}" archive "${sha}" | tar -x -C "${work}/aura" || return 1
  # The home the published commit's updater follows, not the working tree's: on the return trip
  # the two differ.
  registry="${AURA_PUBLISH_REGISTRY:-$(sed -n 's/^IMAGE_HOME=//p' "${work}/aura/deploy/aura-image-update.sh")}"
  [[ -n "${registry}" ]] || { echo "publish: no registry (set AURA_PUBLISH_REGISTRY)" >&2; return 1; }

  week="$(bash "${repo_root}/scripts/apt_week.sh")" || return 1
  for entry in "${EDGE_IMAGES[@]}"; do
    name="${entry%%:*}" file="${entry#*:}" tag="${registry}/${name}:master-${sha}"
    case "${name}" in
      aura) args=(--build-arg "VCS_REF=${sha}" --build-arg "APT_WEEK=${week}") ;;
      aura-caddy | aura-ingest | aura-sandbox | aura-egress) args=(--build-arg "APT_WEEK=${week}") ;;
      *) args=() ;;
    esac
    build_image "${tag}" "${work}/aura" "${file}" "${sha}" "${AURA_SOURCE}" "${args[@]}" || return 1
    pushes+=("${tag}")
  done
  name="${ARCADEDB_MCP%%:*}" file="${ARCADEDB_MCP#*:}" tag="${registry}/${name}:${sha}"
  build_image "${tag}" "${work}/aura" "${file}" "${sha}" "${AURA_SOURCE}" || return 1
  pushes+=("${tag}")
  moves+=("${registry}/${name}:edge ${tag}")

  for name in "${FORK_NAMES[@]}"; do
    git clone --quiet "$(fork_git "${name}")" "${work}/${name}" || return 1
    git -C "${work}/${name}" checkout --quiet "${AURA_PUBLISH_FORK_REF:-main}" || return 1
    fork_sha="$(git -C "${work}/${name}" rev-parse HEAD)" || return 1
    tag="${registry}/${name}:${fork_sha}"
    build_image "${tag}" "${work}/${name}" Dockerfile "${fork_sha}" "$(fork_git "${name}" | sed 's/\.git$//')" || return 1
    pushes+=("${tag}")
    for image in "${FORK_MOVING_TAGS[@]}"; do moves+=("${registry}/${name}:${image} ${tag}"); done
  done

  for entry in "${CONTRACTS[@]}"; do
    read -r name var test <<<"${entry}"
    (export "${var}=${registry}/${name}:master-${sha}" && bash "${work}/aura/${test}") || return 1
  done

  for tag in "${pushes[@]}"; do
    push_retrying "${tag}" || return 1
    echo "published ${tag}"
  done
  # The edge set moves last and together, aura:edge the very last, as publish-aura-edge.yml does.
  for entry in "${EDGE_IMAGES[@]}"; do
    name="${entry%%:*}"
    moves+=("${registry}/${name}:edge ${registry}/${name}:master-${sha}")
  done
  for entry in "${moves[@]}"; do
    read -r tag image <<<"${entry}"
    docker buildx imagetools create --prefer-index=false --tag "${tag}" "${image}" || return 1
    echo "moved ${tag}"
  done
}

[[ "${BASH_SOURCE[0]:-$0}" != "$0" ]] || main "$@"
