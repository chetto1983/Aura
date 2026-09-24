#!/usr/bin/env bash
# Actual updater, payload extraction and self-reexec, with a strict fake Docker control plane.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
export INSTALL_DIR="$fixture/install" AURA_IMAGE_UPDATE_BIN="$fixture/sbin/updater"
export AURA_SYSTEMD_DIR="$fixture/systemd" AURA_IMAGE_UPDATE_LOCK="$fixture/lock"
export CF_FIXTURE="$fixture"
mkdir -p "$INSTALL_DIR" "$fixture/bin" "$fixture/sbin" "$fixture/payload/deploy" "$fixture/payload/scripts"
printf 'AURA_IMAGE=ghcr.io/chetto1983/aura:edge\nPOSTGRES_PASSWORD=synthetic-preserve\n' >"$INSTALL_DIR/.env"
chmod 600 "$INSTALL_DIR/.env"
env_metadata="$(stat -c '%a:%u:%g' "$INSTALL_DIR/.env")"
printf 'old compose' >"$INSTALL_DIR/compose.yaml"
printf '#!/usr/bin/env bash\nexit 33\n' >"$AURA_IMAGE_UPDATE_BIN"
chmod +x "$AURA_IMAGE_UPDATE_BIN"
cp "$repo_root/deploy/aura-image-update.sh" "$fixture/payload/deploy/aura-image-update.sh"
cp "$repo_root/deploy/aura-update-consent.sh" "$fixture/payload/deploy/aura-update-consent.sh"
cp "$repo_root/scripts/appliance_posture.sh" "$fixture/payload/scripts/appliance_posture.sh"
cp "$repo_root/compose.yaml" "$fixture/payload/compose.yaml"
(cd "$fixture/payload" && sha256sum compose.yaml deploy/aura-image-update.sh deploy/aura-update-consent.sh \
  scripts/appliance_posture.sh >payload_manifest.txt)
# The updater sources its consent functions from the payload an earlier tick installed.
install -D -m 0755 "$repo_root/deploy/aura-update-consent.sh" "$INSTALL_DIR/deploy/aura-update-consent.sh"
printf 'persisted PostgreSQL settings' >"$fixture/postgres-volume"
printf 'derived runtime projection' >"$fixture/aura-cloudflared-state"
cat >"$fixture/bin/docker" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"$CF_FIXTURE/calls"
case "$*" in
  create\ *) echo payload-container ;;
  cp\ payload-container:*) cp -R "$CF_FIXTURE/payload/." "$3" ;;
  'rm -f payload-container') ;;
  'compose pull aura aura-migrate garage-bootstrap'|'compose up -d aura'|'compose exec -T aura aura version') ;;
  'compose up -d')
    grep -qx 'AURA_CLOUDFLARED_IMAGE=ghcr.io/chetto1983/aura-cloudflared:edge' "$INSTALL_DIR/.env"
    grep -qx 'AURA_CLOUDFLARED_PULL_POLICY=always' "$INSTALL_DIR/.env"
    touch "$CF_FIXTURE/started" ;;
  'compose ps -q aura') echo aura-id ;;
  'compose ps -q aura-cloudflared') [[ ! -f "$CF_FIXTURE/started" ]] || echo cloudflared-id ;;
  'compose ps -q '* ) ;;
  'compose pull aura-cloudflared'|'compose up -d --no-deps aura-cloudflared') ;;
  inspect\ --format\ * )
    case "$3" in
      '{{.Image}}') echo image-id ;;
      '{{.Config.Image}}') echo ghcr.io/chetto1983/aura-cloudflared:edge ;;
      '{{.State.Status}}') echo running ;;
      '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}') echo healthy ;;
      *) exit 1 ;;
    esac ;;
  image\ inspect\ --format\ * )
    case "$4" in
      '{{.Id}}') echo image-id ;;
      '{{.Created}}') echo 2026-09-24T06:47:13Z ;;
      *Labels*) echo 7886200e539046d3b7dc47176facddb74f98ce6b ;;
      *) exit 1 ;;
    esac ;;
  'compose config --images'|'images --no-trunc --format {{.Repository}}:{{.Tag}} {{.ID}}'|'image prune --force') ;;
  *) echo 'Unexpected Docker mutation' >&2; exit 1 ;;
esac
STUB
chmod +x "$fixture/bin/docker"
export PATH="$fixture/bin:$PATH"
bash "$repo_root/deploy/aura-image-update.sh" >"$fixture/output"
grep -q 're-executing it' "$fixture/output"
grep -qx 'compose pull aura-cloudflared' "$fixture/calls"
grep -qx 'compose up -d --no-deps aura-cloudflared' "$fixture/calls"
grep -q 'healthy' "$fixture/output"
[[ "$(stat -c '%a:%u:%g' "$INSTALL_DIR/.env")" == "$env_metadata" ]]
grep -qx 'POSTGRES_PASSWORD=synthetic-preserve' "$INSTALL_DIR/.env"
! grep -q 'synthetic-preserve' "$fixture/output"
[[ "$(cat "$fixture/postgres-volume")" == 'persisted PostgreSQL settings' ]]
[[ "$(cat "$fixture/aura-cloudflared-state")" == 'derived runtime projection' ]]
cp "$INSTALL_DIR/.env" "$fixture/first.env"
bash "$repo_root/deploy/aura-image-update.sh" >"$fixture/rerun-output"
cmp "$INSTALL_DIR/.env" "$fixture/first.env"
# Exercise the migration itself on custom and pinned installs without advancing their stack.
source "$repo_root/deploy/aura-image-update.sh"
printf 'AURA_IMAGE=ghcr.io/example/aura:edge\nAURA_CLOUDFLARED_IMAGE=custom/image:pinned\nAURA_CLOUDFLARED_PULL_POLICY=missing\n' >"$INSTALL_DIR/.env"
cp "$INSTALL_DIR/.env" "$fixture/custom.env"
ensure_cloudflared_edge_env
cmp "$INSTALL_DIR/.env" "$fixture/custom.env"
printf 'AURA_IMAGE=ghcr.io/example/aura:v1\n' >"$INSTALL_DIR/.env"
cp "$INSTALL_DIR/.env" "$fixture/pinned.env"
ensure_cloudflared_edge_env
cmp "$INSTALL_DIR/.env" "$fixture/pinned.env"
echo 'ok: old edge appliance migrates after payload reexec, pulls/starts healthy sidecar; rerun, volumes, metadata and explicit pins preserved'
