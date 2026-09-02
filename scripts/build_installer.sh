#!/usr/bin/env bash
#
# Packs install.sh plus the 25 files it installs into one self-extracting archive.
#
# makeself rather than a hand-rolled base64 blob: it embeds a payload checksum and
# validates it on extraction, and -- the property that matters -- it runs the startup
# script with the working directory set to the extracted files. An earlier design emitted
# a download_file override ahead of install.sh verbatim; install.sh defines download_file
# itself, bash takes the last definition, and every download would have gone back to the
# network. There is no code generation here for exactly that reason.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:?usage: build_installer.sh OUTPUT.run}"

if ! command -v makeself >/dev/null 2>&1; then
  echo "FAIL: makeself is required (apt-get install -y makeself)" >&2
  exit 1
fi

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

# The payload list is derived from install.sh itself, so a file added there cannot be
# forgotten here.
while read -r rel; do
  mkdir -p "$staging/$(dirname "$rel")"
  cp "$repo_root/$rel" "$staging/$rel"
done < <(grep '^download_file ' "$repo_root/scripts/install.sh" | awk '{print $2}')

cp "$repo_root/scripts/install.sh" "$staging/install.sh"

cat > "$staging/startup.sh" <<'STARTUP'
#!/usr/bin/env bash
set -euo pipefail
# makeself runs this from the extraction directory, so the payload is right here.
export AURA_PAYLOAD_DIR="$PWD"
exec bash "$PWD/install.sh" "$@"
STARTUP
chmod +x "$staging/startup.sh"

makeself --sha256 --gzip \
  "$staging" "$output" \
  "Aura appliance installer" \
  ./startup.sh

chmod +x "$output"
echo "ok: wrote $output"
