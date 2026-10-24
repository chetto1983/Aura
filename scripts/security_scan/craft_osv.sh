#!/usr/bin/env bash
# Watches the six Craft apps the cockpit serves (security-scan.yml, spec D6). Neither Dependabot
# nor Trivy sees them: their versions are Dockerfile arguments and their lockfiles live upstream.
#
#   craft_osv.sh [scan] [repo] [osv-config]   every app: newer tags, then the gate
#   craft_osv.sh pins [repo]                  "<app> <tag> <commit|->", one line per app
#   craft_osv.sh filter <tree> <osv.json>     the in-graph findings, tab-separated
#
# The gate reads only what ships. It resolves each app's wasm32 graph at the pinned tag, with
# Aura's patch applied. OSV-Scanner reads the whole Cargo.lock, and a finding counts only when its
# crate and version are in that graph. A fixable finding at CVSS 7.0 or more turns the run red.
# An accepted finding goes in .github/osv-scanner-craft.toml, outside docker/craft-web/: every
# file in that directory changes the Craft image pin (scripts/craft_web_inputs.sh).
set -euo pipefail

pins() {
  local repo=$1
  sed -nE 's#^RUN /craft-web/build\.sh ([a-z]+) (v[^ ]+) ([0-9a-f]{40}) /apps$#\1 \2 \3#p' \
    "$repo/docker/aura-craft-web/Dockerfile"
  # GridCraft ships its official release zip: a tag and a checksum, no commit and no patch.
  sed -nE 's#.*/storytold/gridcraft/releases/download/(v[^/]+)/gridcraft-web-.*\.zip.*#gridcraft \1 -#p' \
    "$repo/docker/aura/Dockerfile"
}

# OSV's max_severity is a CVSS base score, or empty when the advisory carries none.
filter() {
  local tree=$1 osv=$2
  jq -r --rawfile tree "$tree" '
    ($tree | split("\n") | map(select(length > 0)) | INDEX(.)) as $graph
    | .results[]?.packages[]? | . as $p
    | ($p.package.name + "@" + $p.package.version) as $crate
    | select($graph[$crate])
    | $p.vulnerabilities[] | . as $v
    | ([$p.groups[] | select(.ids | index($v.id)) | .max_severity][0] // "") as $sev
    | ([$v.affected[] | select(.package.name == $p.package.name)
        | .ranges[]?.events[]? | .fixed // empty] | join(",")) as $fixed
    | [$crate, $v.id, (if $sev == "" then "-" else $sev end), (if $fixed == "" then "-" else $fixed end),
       (if $fixed != "" and $sev != "" and ($sev | tonumber) >= 7 then "FAIL" else "report" end)]
    | @tsv' "$osv"
}

newer_tags() {
  local app=$1 tag=$2
  git ls-remote --tags --refs "https://github.com/storytold/$app" |
    sed -E 's#.*refs/tags/##' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V |
    awk -v pinned="$tag" 'seen { printf "%s ", $0 } $0 == pinned { seen = 1 }'
}

scan() {
  local repo config work status=0
  # Absolute, because git -C and cd run inside each clone, where a relative path means nothing.
  repo="$(cd "$1" && pwd)" config="$(realpath -- "$2")"
  work="$(mktemp -d)"
  trap 'rm -rf -- "$work"' RETURN
  echo "| app | pinned | newer releases | in the wasm32 graph | gate |"
  echo "|---|---|---|---|---|"
  while read -r app tag commit; do
    local src=$work/$app
    git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$tag" "https://github.com/storytold/$app" "$src"
    if [[ $commit != - && "$(git -C "$src" rev-parse HEAD)" != "$commit" ]]; then
      echo "craft: $app $tag is $(git -C "$src" rev-parse HEAD), not $commit" >&2
      return 1
    fi
    [[ $commit == - ]] || git -C "$src" apply "$repo/docker/craft-web/$app.patch"
    (cd "$src" && cargo tree --locked -p "$app-web" --target wasm32-unknown-unknown -e normal \
      --prefix none --format '{p}') | sed -E 's/^([^ ]+) v([^ ]+).*/\1@\2/' | sort -u >"$work/$app.tree"
    # --config overrides any osv-scanner.toml the upstream tree carries.
    local rc=0
    osv-scanner scan source --config "$config" --format json --output-file "$work/$app.json" \
      -L "$src/Cargo.lock" >/dev/null 2>"$work/$app.err" || rc=$?
    if (( rc > 1 )); then
      echo "craft: osv-scanner exited $rc on $app" >&2
      cat "$work/$app.err" >&2
      return 1
    fi
    filter "$work/$app.tree" "$work/$app.json" >"$work/$app.found"
    local gated verdict=ok
    gated="$(grep -c 'FAIL$' "$work/$app.found" || true)"
    if (( gated > 0 )); then verdict="FAIL ($gated)"; status=1; fi
    echo "| $app | $tag | $(newer_tags "$app" "$tag") | $(wc -l <"$work/$app.found") | $verdict |"
  done < <(pins "$repo")
  for found in "$work"/*.found; do
    sed -E "s#^#$(basename "$found" .found)\t#" "$found"
  done | awk -F'\t' 'BEGIN { print "\n| app | crate | advisory | CVSS | fixed in | gate |\n|---|---|---|---|---|---|" }
    { printf "| %s | %s | %s | %s | %s | %s |\n", $1, $2, $3, $4, $5, $6 }'
  return "$status"
}

case ${1:-scan} in
  pins) pins "${2:-.}" ;;
  filter) filter "$2" "$3" ;;
  scan) scan "${2:-.}" "${3:-${2:-.}/.github/osv-scanner-craft.toml}" ;;
  *) echo "usage: $0 [scan|pins|filter] ..." >&2; exit 2 ;;
esac
