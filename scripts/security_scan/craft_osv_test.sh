#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"
fixtures="$(mktemp -d)"
trap 'rm -rf -- "$fixtures"' EXIT

fail() { echo "security-scan craft test: $*" >&2; exit 1; }

# The pins are the ones the builds use: the five RUN lines of the Craft image and GridCraft's zip.
pins="$(bash "$here/craft_osv.sh" pins "$repo_root")"
[[ "$(wc -l <<<"$pins")" == 6 ]] || fail "want six apps, got: $pins"
while read -r app tag commit; do
  grep -qF "RUN /craft-web/build.sh $app $tag $commit /apps" "$repo_root/docker/aura-craft-web/Dockerfile" ||
    fail "$app $tag $commit is not a build line of docker/aura-craft-web/Dockerfile"
done < <(grep -v '^gridcraft' <<<"$pins")
grid="$(grep '^gridcraft' <<<"$pins")"
read -r _ grid_tag grid_commit <<<"$grid"
grep -qF "/gridcraft/releases/download/$grid_tag/gridcraft-web-${grid_tag#v}.zip" "$repo_root/docker/aura/Dockerfile" ||
  fail "gridcraft $grid_tag is not the zip docker/aura/Dockerfile adds"
[[ $grid_commit == - ]] || fail "gridcraft has no pinned commit, got $grid_commit"

# The filter keeps a finding only when its crate and version are in the wasm32 graph. The gate is a
# fixed version and a CVSS score of 7.0 or more, D2's fixable HIGH or CRITICAL.
printf '%s\n' zip@3.0.0 tokio@1.53.2 rsa@0.9.10 >"$fixtures/tree.txt"
finding() { jq -nc --arg n "$1" --arg v "$2" --arg id "$3" --arg sev "$4" --arg fixed "$5" '
  {package: {name: $n, version: $v}, groups: [{ids: [$id], max_severity: $sev}],
   vulnerabilities: [{id: $id, affected: [{package: {name: $n},
     ranges: [{events: ([{introduced: "0"}] + (if $fixed == "" then [] else [{fixed: $fixed}] end))}]}]}]}'; }
{
  finding zip 3.0.0 RUSTSEC-2026-0369 9.1 9.0.0
  finding tokio 1.53.2 RUSTSEC-2026-0386 "" 1.53.3
  finding rsa 0.9.10 RUSTSEC-2023-0071 5.9 ""
  finding zbus_names 4.3.4 RUSTSEC-2026-0371 9.8 4.3.5
} | jq -s '{results: [{packages: .}]}' >"$fixtures/osv.json"

out="$(bash "$here/craft_osv.sh" filter "$fixtures/tree.txt" "$fixtures/osv.json")"
grep -qP '^zip@3.0.0\tRUSTSEC-2026-0369\t9.1\t9.0.0\tFAIL$' <<<"$out" || fail "zip is not gated: $out"
grep -qP '^tokio@1.53.2\tRUSTSEC-2026-0386\t-\t1.53.3\treport$' <<<"$out" || fail "tokio is not reported: $out"
grep -qP '^rsa@0.9.10\tRUSTSEC-2023-0071\t5.9\t-\treport$' <<<"$out" || fail "rsa is not reported: $out"
if grep -q zbus_names <<<"$out"; then fail "a crate outside the graph was kept: $out"; fi

# scan, run as the workflow runs it: from the checkout, with relative paths. git, cargo and
# osv-scanner are stubs, so nothing leaves the machine. The first run on GitHub (40105988445) died
# here: git -C <clone> apply resolved ./docker/craft-web/<app>.patch inside the clone.
bin="$fixtures/bin"
mkdir -p "$bin"
printf '%s\n' "$pins" >"$fixtures/pins"
cat >"$bin/git" <<'STUB'
#!/usr/bin/env bash
dir=.
while [[ $1 == -C || $1 == -c ]]; do [[ $1 == -C ]] && dir=$2; shift 2; done
case $1 in
  clone) mkdir -p "${@: -1}"; : >"${@: -1}/Cargo.lock" ;;
  rev-parse) awk -v app="$(basename "$dir")" '$1 == app { print $3 }' "$CRAFT_TEST_PINS" ;;
  apply) [[ $2 == /* && -f $2 ]] || { echo "stub git: apply got $2" >&2; exit 128; } ;;
  ls-remote) ;;
  *) echo "stub git: unexpected $*" >&2; exit 1 ;;
esac
STUB
printf '#!/usr/bin/env bash\n' >"$bin/cargo"
cat >"$bin/osv-scanner" <<'STUB'
#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case $1 in
    --config) [[ -f $2 ]] || { echo "stub osv-scanner: no config $2" >&2; exit 2; }; shift 2 ;;
    --output-file) echo '{"results": []}' >"$2"; shift 2 ;;
    *) shift ;;
  esac
done
STUB
chmod +x "$bin/git" "$bin/cargo" "$bin/osv-scanner"
out="$(cd "$repo_root" && CRAFT_TEST_PINS="$fixtures/pins" PATH="$bin:$PATH" \
  bash scripts/security_scan/craft_osv.sh scan . .github/osv-scanner-craft.toml 2>&1)" ||
  fail "scan from the checkout with relative paths failed: $out"
[[ "$(grep -c '| ok |' <<<"$out")" == 6 ]] || fail "want six apps ok, got: $out"

echo "security-scan craft test: pass"
