#!/usr/bin/env bash
# Copies hwdsl2/whisper-server:latest to GHCR under a dated tag that is never collected.
#
# hwdsl2 publishes only :latest, and Docker Hub collects the previous manifest within hours of
# a republish (2026-10-08: republished 10:56 UTC, gone before 18:56), so a digest pin on Hub
# breaks CI and every fresh install. compose.yaml pins this copy instead.
#
# The tag is the upstream push time, YYYY.MM.DD.HHMM: the same :latest always maps to the same
# tag, so a run that finds the tag already present does nothing, and Dependabot reads the
# format as a dated version it can order.
#
# Needs DEST_REPO (ghcr.io/<owner>/aura-whisper-server) and a prior `skopeo login ghcr.io`.
set -euo pipefail

SRC_REPO="${SRC_REPO:-hwdsl2/whisper-server}"
HUB_API="${HUB_API:-https://hub.docker.com/v2/repositories}"

mirror_tag() {
  date -u -d "$1" +%Y.%m.%d.%H%M
}

# Prints "<digest> <last_updated>" for the upstream :latest.
upstream_latest() {
  curl -fsS --retry 3 "${HUB_API}/${SRC_REPO}/tags/latest" |
    python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["digest"], d["last_updated"])'
}

manifest_digest() {
  echo "sha256:$(skopeo inspect --raw "$1" | sha256sum | cut -d' ' -f1)"
}

main() {
  : "${DEST_REPO:?DEST_REPO must name the GHCR repository}"
  local digest updated tag dest
  read -r digest updated < <(upstream_latest)
  [[ "${digest}" == sha256:* && -n "${updated}" ]] || {
    echo "mirror: Docker Hub returned no digest for ${SRC_REPO}:latest" >&2
    exit 1
  }
  tag="$(mirror_tag "${updated}")"
  dest="docker://${DEST_REPO}:${tag}"

  if skopeo inspect --raw "${dest}" >/dev/null 2>&1; then
    require_digest "${dest}" "${digest}" "${tag}"
    echo "mirror: ${DEST_REPO}:${tag} already holds ${SRC_REPO}@${digest}"
    return 0
  fi

  skopeo copy --all --preserve-digests --retry-times 3 --src-no-creds \
    "docker://docker.io/${SRC_REPO}@${digest}" "${dest}"
  require_digest "${dest}" "${digest}" "${tag}"
  echo "mirror: ${DEST_REPO}:${tag}@${digest}"
}

# A dated tag must hold the upstream digest it is named after, whether this run wrote it or
# an earlier one did: anything else is a tag someone moved, and compose would pin the wrong image.
require_digest() {
  local actual
  actual="$(manifest_digest "$1")"
  if [[ "${actual}" != "$2" ]]; then
    echo "mirror: ${DEST_REPO}:$3 is ${actual}, expected $2" >&2
    exit 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
