#!/usr/bin/env bash
# Executes both installer entry points with external commands replaced by strict fixtures.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
export FIXTURE_REPO="$repo_root" FIXTURE_CALLS="$fixture/calls"
mkdir -p "$fixture/bin"
cat >"$fixture/bin/docker" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"$FIXTURE_CALLS"
case "$*" in
  'info'|'compose version'|pull\ *|'compose create aura-llama-embed'|'compose pull aura-cloudflared'|compose\ up\ *) ;;
  'compose ps -aq aura-llama-embed') echo fixture-embed ;;
  run\ --rm\ --volumes-from\ fixture-embed\ *) echo 123 ;;
  compose\ exec\ -T\ grafana\ *) echo '{"value":[0,"1"]}' ;;
  *) echo "Unexpected Docker operation: $*" >&2; exit 1 ;;
esac
STUB
cat >"$fixture/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == -fsIL ]]; then
  printf 'x-linked-size: 123\nx-repo-commit: abc123\nx-linked-etag: abc456\n'
elif [[ "$1" == -fsSL && "$3" == -o && "$2" == https://fixture.invalid/* ]]; then
  cp "$FIXTURE_REPO/${2#https://fixture.invalid/}" "$4"
else
  echo 'Unexpected network request' >&2; exit 1
fi
STUB
chmod +x "$fixture/bin/"*
export PATH="$fixture/bin:$PATH" AURA_INSTALL_SKIP_HW=1 AURA_INSTALL_BASE_URL=https://fixture.invalid
unset AURA_PAYLOAD_DIR AURA_IMAGE AURA_INSTALL_REF AURA_IMAGE_TAG
fail() { echo "FAIL: $*" >&2; exit 1; }
run_install() {
  if [[ "${1:-}" == --artifact ]]; then
    "$2" -- --dir "$fixture/install" >"$fixture/output"
  else
    bash "$repo_root/scripts/install.sh" --dir "$fixture/install" >"$fixture/output"
  fi
}
run_install "$@"
grep -qx 'AURA_CLOUDFLARED_IMAGE=ghcr.io/chetto1983/aura-cloudflared:edge' "$fixture/install/.env" || fail 'published image missing'
grep -qx 'AURA_CLOUDFLARED_PULL_POLICY=always' "$fixture/install/.env" || fail 'pull policy missing'
grep -qx 'compose pull aura-cloudflared' "$FIXTURE_CALLS" || fail 'sidecar was not explicitly pulled'
grep -qx 'compose up -d --wait --wait-timeout 300' "$FIXTURE_CALLS" || fail 'stack health was not awaited'
grep -q 'Settings.*Remote access' "$fixture/output" || fail 'cockpit handoff missing'
! grep -qE '^(CLOUDFLARE_API_TOKEN|CLOUDFLARE_TUNNEL_TOKEN|TUNNEL_TOKEN)=' "$fixture/install/.env" || fail 'installer wrote a Cloudflare secret'
cmp "$repo_root/compose.yaml" "$fixture/install/compose.yaml"
cmp "$repo_root/caddy/Caddyfile" "$fixture/install/caddy/Caddyfile"
cp "$fixture/install/.env" "$fixture/before.env"
# Represent existing durable volumes outside the installation tree. The strict Docker stub
# rejects all volume/down/removal calls; these sentinels additionally detect filesystem loss.
mkdir -p "$fixture/volumes/postgres" "$fixture/volumes/aura-cloudflared-state"
printf 'encrypted database state' >"$fixture/volumes/postgres/state"
printf 'derived projection' >"$fixture/volumes/aura-cloudflared-state/state"
run_install "$@"
cmp "$fixture/before.env" "$fixture/install/.env"
[[ "$(cat "$fixture/volumes/postgres/state")" == 'encrypted database state' ]] || fail 'database lost'
[[ "$(cat "$fixture/volumes/aura-cloudflared-state/state")" == 'derived projection' ]] || fail 'projection lost'
echo 'ok: fresh install and rerun ship remote access, pull/await idle sidecar, preserve secrets and volumes'
