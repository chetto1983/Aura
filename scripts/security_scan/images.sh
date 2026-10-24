#!/usr/bin/env bash
# Prints, as JSON for a job matrix, the images the weekly scan reads (security-scan.yml, spec
# docs/superpowers/specs/2026-10-24-weekly-security-scan-design.md, D2 and D4):
#
#   images.sh [compose.yaml] [owner]
#
# Every image compose.yaml names, in every profile. Aura's own builds appear at the `:edge` tags
# that publish-aura-edge.yml and publish-arcadedb-mcp.yml move. The two box images, which the
# daemon starts and compose does not list, are added. An image left on a local default stops the
# list rather than leaving the scan.
set -euo pipefail

compose=${1:-compose.yaml}
owner=${2:-chetto1983}
registry="ghcr.io/$owner"

# compose refuses to print a config while a `${VAR:?}` is unset, though only `up` needs the value.
while read -r name; do
  [[ -n ${!name:-} ]] || export "$name=security-scan-placeholder"
done < <(grep -oE '\$\{[A-Z0-9_]+:\?' "$compose" | sed -E 's/^\$\{([A-Z0-9_]+):\?$/\1/' | sort -u)

export AURA_IMAGE="$registry/aura:edge"
export AURA_CADDY_IMAGE="$registry/aura-caddy:edge"
export AURA_CLOUDFLARED_IMAGE="$registry/aura-cloudflared:edge"
export AURA_INGEST_IMAGE="$registry/aura-ingest:edge"
export AURA_ARCADEDB_MCP_IMAGE="$registry/aura-arcadedb-mcp:edge"

# --env-file /dev/null: a developer's .env must not change what the scan reads.
images="$(docker compose --env-file /dev/null --project-directory "$(dirname "$compose")" \
  -f "$compose" --profile '*' config --images)"
images+=$'\n'"$registry/aura-sandbox:edge"$'\n'"$registry/aura-egress:edge"

unresolved="$(grep -E '(^aura[^/]*$|^aura[^/]*:|:local$)' <<<"$images" || true)"
if [[ -n $unresolved ]]; then
  echo "security-scan images: no published image for: $(tr '\n' ' ' <<<"$unresolved")" >&2
  echo "map it to its edge tag in $0" >&2
  exit 1
fi

# The whisper server is a mirror of someone else's image, so it is held to the third-party gate.
sort -u <<<"$images" | jq -R -s -c --arg registry "$registry/" '
  [split("\n")[] | select(length > 0)
   | {image: ., name: (split("@")[0] | sub(":[^/:]+$"; ""))}
   | .kind = (if (.image | startswith($registry)) and (.name != ($registry + "aura-whisper-server"))
              then "own" else "third-party" end)]'
