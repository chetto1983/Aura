#!/usr/bin/env bash
set -euo pipefail
if (set +H) 2>/dev/null; then
  set +H
fi

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
if command -v git >/dev/null 2>&1 && git rev-parse --show-toplevel >/dev/null 2>&1; then
  cd "$(git rev-parse --show-toplevel)"
else
  cd "$(dirname "$script_dir")"
fi

bucket="${AURA_OBJECTSTORE_BUCKET:-aura-assets}"
access_key="${AURA_OBJECTSTORE_ACCESS_KEY:-GK000000000000000000000000}"
secret_key="${AURA_OBJECTSTORE_SECRET_KEY:-0000000000000000000000000000000000000000000000000000000000000000}"
key_name="${AURA_GARAGE_KEY_NAME:-aura-assets-local}"
zone="${AURA_GARAGE_ZONE:-dc1}"
capacity="${AURA_GARAGE_CAPACITY:-1G}"

compose() {
  if [ -n "${AURA_COMPOSE_ENV_FILE:-}" ]; then
    docker compose --env-file "$AURA_COMPOSE_ENV_FILE" "$@"
    return
  fi
  docker compose "$@"
}

garage() {
  compose exec -T garage /garage "$@"
}

compose up -d garage >/dev/null

node_id="$(garage status | awk '/NO ROLE ASSIGNED|pending\.\.\./ { print $1; exit }')"
if [ -n "$node_id" ]; then
  garage layout assign -z "$zone" -c "$capacity" "$node_id" >/dev/null
  version="$(garage layout show | awk '/garage layout apply --version/ { print $NF; exit }')"
  if [ -n "$version" ]; then
    garage layout apply --version "$version" >/dev/null
  fi
fi

if ! garage bucket info "$bucket" >/dev/null 2>&1; then
  garage bucket create "$bucket" >/dev/null
fi

if ! garage key info "$access_key" >/dev/null 2>&1; then
  garage key import --yes -n "$key_name" "$access_key" "$secret_key" >/dev/null
fi

garage bucket allow --read --write --owner "$bucket" --key "$access_key" >/dev/null
if [ "${AURA_GARAGE_BOOTSTRAP_BUILD:-0}" = "1" ]; then
  compose run --rm --build --no-deps --entrypoint aura \
    -e AURA_OBJECTSTORE_ENDPOINT=http://garage:3900 \
    aura objectstore bootstrap >/dev/null
else
  compose run --rm --no-deps --entrypoint aura \
    -e AURA_OBJECTSTORE_ENDPOINT=http://garage:3900 \
    aura objectstore bootstrap >/dev/null
fi
garage bucket info "$bucket"
