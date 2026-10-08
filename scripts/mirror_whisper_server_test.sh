#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
script="$PWD/scripts/mirror_whisper_server.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() {
  echo "mirror-whisper-server-test: $*" >&2
  exit 1
}

# shellcheck source=scripts/mirror_whisper_server.sh
source "$script"
[ "$(mirror_tag 2026-10-08T10:56:59.551866Z)" = "2026.10.08.1056" ] || fail "tag from a Hub timestamp"
[ "$(mirror_tag 2026-01-02T03:04:05Z)" = "2026.01.02.0304" ] || fail "tag keeps leading zeros"

# Fake curl answers the Hub tag API, fake skopeo keeps the destination as one file per tag
# whose content is the raw manifest; SKOPEO_COPY_BODY lets a case corrupt the copy.
mkdir -p "$work/bin" "$work/registry"
manifest='{"schemaVersion":2,"manifests":[]}'
digest="sha256:$(printf '%s' "$manifest" | sha256sum | cut -d' ' -f1)"
cat >"$work/bin/curl" <<STUB
#!/usr/bin/env bash
printf '{"digest":"%s","last_updated":"2026-10-08T10:56:59.551866Z"}' "$digest"
STUB
cat >"$work/bin/skopeo" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$REGISTRY/calls"
ref="${*: -1}"
file="$REGISTRY/$(printf '%s' "${ref#docker://}" | tr '/:@' '___')"
case "$1" in
  inspect) [ -f "$file" ] && cat "$file" ;;
  copy) printf '%s' "${SKOPEO_COPY_BODY:-$MANIFEST}" >"$file" ;;
esac
STUB
chmod +x "$work/bin/curl" "$work/bin/skopeo"

run() {
  PATH="$work/bin:$PATH" REGISTRY="$work/registry" MANIFEST="$manifest" \
    DEST_REPO=ghcr.io/owner/aura-whisper-server bash "$script"
}

out="$(run)" || fail "first run failed"
[ "$out" = "mirror: ghcr.io/owner/aura-whisper-server:2026.10.08.1056@$digest" ] || fail "first run said: $out"
grep -q -- "copy --all --preserve-digests --retry-times 3 --src-no-creds docker://docker.io/hwdsl2/whisper-server@$digest docker://ghcr.io/owner/aura-whisper-server:2026.10.08.1056" \
  "$work/registry/calls" || fail "copy was not by digest with --all --preserve-digests"

out="$(run)" || fail "second run failed"
case "$out" in *"already holds"*) ;; *) fail "second run copied again: $out" ;; esac
[ "$(grep -c '^copy' "$work/registry/calls")" -eq 1 ] || fail "an existing tag was copied twice"

rm -f "$work/registry/"*
if SKOPEO_COPY_BODY='{"tampered":true}' run 2>/dev/null; then
  fail "a copy with another digest passed"
fi

if PATH="$work/bin:$PATH" REGISTRY="$work/registry" bash "$script" 2>/dev/null; then
  fail "a run without DEST_REPO passed"
fi

echo "mirror-whisper-server-test: pass"
