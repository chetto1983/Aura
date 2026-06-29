#!/bin/sh
set -eu

bucket="${AURA_OBJECTSTORE_BUCKET:-aura-assets}"
access_key="${AURA_OBJECTSTORE_ACCESS_KEY:?AURA_OBJECTSTORE_ACCESS_KEY required in .env}"
secret_key="${AURA_OBJECTSTORE_SECRET_KEY:?AURA_OBJECTSTORE_SECRET_KEY required in .env}"
key_name="${AURA_GARAGE_KEY_NAME:-aura-assets-local}"
zone="${AURA_GARAGE_ZONE:-dc1}"
capacity="${AURA_GARAGE_CAPACITY:-1G}"

log() {
  printf '%s\n' "aura garage bootstrap: $*" >&2
}

garage() {
  /usr/local/bin/garage "$@"
}

status=""
attempt=0
while :; do
  attempt=$((attempt + 1))
  if status="$(garage status 2>&1)"; then
    break
  fi
  if [ "$attempt" -ge 60 ]; then
    printf '%s\n' "$status" >&2
    log "garage RPC did not become reachable"
    exit 1
  fi
  sleep 1
done

node_id="$(printf '%s\n' "$status" | awk '/NO ROLE ASSIGNED|pending\.\.\./ { print $1; exit }')"
if [ -n "$node_id" ]; then
  log "assigning node $node_id to zone $zone with capacity $capacity"
  garage layout assign -z "$zone" -c "$capacity" "$node_id" >/dev/null
  version="$(garage layout show | awk '/garage layout apply --version/ { print $NF; exit }')"
  if [ -n "$version" ]; then
    garage layout apply --version "$version" >/dev/null
  fi
fi

if ! garage bucket info "$bucket" >/dev/null 2>&1; then
  log "creating bucket $bucket"
  garage bucket create "$bucket" >/dev/null
fi

if ! garage key info "$access_key" >/dev/null 2>&1; then
  log "importing key $key_name"
  garage key import --yes -n "$key_name" "$access_key" "$secret_key" >/dev/null
fi

garage bucket allow --read --write --owner "$bucket" --key "$access_key" >/dev/null
log "bucket $bucket key is ready"
