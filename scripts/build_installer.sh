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

# shellcheck source=scripts/payload_files.sh
source "$repo_root/scripts/payload_files.sh"

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

# The payload list is derived from install.sh itself, so a file added there cannot be
# forgotten here; see payload_files.sh for the $-literal guard and why this must be a
# command substitution rather than `< <(payload_files ...)`.
payload_rels="$(payload_files "$repo_root")" || exit 1
while read -r rel; do
  if [ -z "$rel" ]; then continue; fi
  mkdir -p "$staging/$(dirname "$rel")"
  cp "$repo_root/$rel" "$staging/$rel"
done <<< "$payload_rels"

cp "$repo_root/scripts/install.sh" "$staging/install.sh"

# makeself's own runtime header parses argv for ITS flags (--target, --keep, ...) before it
# execs this script, and rejects anything it does not recognize -- so a plain
# "./install-appliance.run --config X" dies as "Unrecognized flag" and install.sh never
# runs. '--' is makeself's own escape hatch: everything after it is handed to us verbatim.
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
echo "  usage: $output -- --config /path/install.conf   ('--' is required)"
