#!/bin/sh
set -eu

secrets_dir="${AURA_SECRETS_DIR:-/secrets}"
searxng_dir="$secrets_dir/searxng"
mkdir -p "$secrets_dir"
mkdir -p "$searxng_dir"

# Earlier Compose revisions mounted generated configs as single files. If
# Docker created host-side directories for those missing file mounts, replace
# them with the generated files this script owns.
for stale_path in "$secrets_dir/garage.toml" "$secrets_dir/searxng-settings.yml"; do
	if [ -d "$stale_path" ]; then
		rm -rf "$stale_path"
	fi
done

random_hex() {
	bytes="$1"
	od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
}

ensure_secret() {
	path="$1"
	bytes="$2"
	prefix="${3:-}"
	if [ ! -s "$path" ]; then
		printf '%s%s\n' "$prefix" "$(random_hex "$bytes")" > "$path"
	fi
	chmod 0644 "$path"
}

ensure_secret "$secrets_dir/garage_s3_access_key" 16 "GK"
ensure_secret "$secrets_dir/garage_s3_secret_key" 32
ensure_secret "$secrets_dir/garage_rpc_secret" 32
ensure_secret "$secrets_dir/garage_admin_token" 32
ensure_secret "$secrets_dir/garage_metrics_token" 32
ensure_secret "$secrets_dir/searxng_secret_key" 32

garage_access="$(cat "$secrets_dir/garage_s3_access_key")"
garage_secret="$(cat "$secrets_dir/garage_s3_secret_key")"
garage_rpc="$(cat "$secrets_dir/garage_rpc_secret")"
garage_admin="$(cat "$secrets_dir/garage_admin_token")"
garage_metrics="$(cat "$secrets_dir/garage_metrics_token")"
searxng_secret="$(cat "$secrets_dir/searxng_secret_key")"

cat > "$secrets_dir/aura.env.tmp" <<EOF
GARAGE_S3_ACCESS_KEY=$garage_access
GARAGE_S3_SECRET_KEY=$garage_secret
EOF
mv "$secrets_dir/aura.env.tmp" "$secrets_dir/aura.env"
chmod 0644 "$secrets_dir/aura.env"

cat > "$secrets_dir/garage-default.env.tmp" <<EOF
GARAGE_DEFAULT_ACCESS_KEY=$garage_access
GARAGE_DEFAULT_SECRET_KEY=$garage_secret
GARAGE_DEFAULT_BUCKET=aura-artifacts
EOF
mv "$secrets_dir/garage-default.env.tmp" "$secrets_dir/garage-default.env"
chmod 0644 "$secrets_dir/garage-default.env"

cat > "$secrets_dir/garage.toml.tmp" <<EOF
metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"

replication_factor = 1
rpc_bind_addr = "[::]:3901"
rpc_public_addr = "garage:3901"
rpc_secret = "$garage_rpc"

[s3_api]
s3_region = "garage"
api_bind_addr = "[::]:3900"
root_domain = ".s3.garage.localhost"

[s3_web]
bind_addr = "[::]:3902"
root_domain = ".web.garage.localhost"
index = "index.html"

[admin]
api_bind_addr = "[::]:3903"
admin_token = "$garage_admin"
metrics_token = "$garage_metrics"
EOF
mv "$secrets_dir/garage.toml.tmp" "$secrets_dir/garage.toml"
chmod 0644 "$secrets_dir/garage.toml"

cat > "$searxng_dir/settings.yml.tmp" <<EOF
use_default_settings: true

server:
  secret_key: "$searxng_secret"
  limiter: false

search:
  formats:
    - html
    - json
EOF
mv "$searxng_dir/settings.yml.tmp" "$searxng_dir/settings.yml"
chmod 0644 "$searxng_dir/settings.yml"

# The aura container runs as uid 10001 (see Dockerfile). On a fresh checkout
# Docker auto-creates the host bind-mount sources for /data and /workspace as
# root, leaving the aura user unable to write aura.db, logs, or the runtime
# workspace. Reclaim ownership here so the first boot succeeds. Mounts are
# optional so missing paths are not fatal.
for owned_path in /data /workspace; do
	if [ -d "$owned_path" ]; then
		chown -R 10001:10001 "$owned_path" 2>/dev/null || true
	fi
done

echo "Aura local service secrets ready in $secrets_dir"
