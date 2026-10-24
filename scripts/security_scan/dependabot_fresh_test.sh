#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"
fixtures="$(mktemp -d)"
trap 'rm -rf -- "$fixtures"' EXIT

fail() { echo "security-scan dependabot test: $*" >&2; exit 1; }
check() { DEPENDABOT_RUNS_FILE="$1" DEPENDABOT_NOW=2026-10-24T13:00:00Z \
  bash "$here/dependabot_fresh.sh" "$repo_root/.github/dependabot.yml"; }

# The titles of the Dependabot Updates runs of 2026-10-24 (runs 168-177).
run() { jq -nc --arg name "$1" --arg at "$2" --arg c "${3:-success}" \
  '{name: $name, created_at: $at, status: "completed", conclusion: $c, html_url: "https://example.invalid/run"}'; }
{
  run "go_modules in /. - Update #1649848135" 2026-10-24T10:37:19Z
  run "npm_and_yarn in /web - Update #1649848137" 2026-10-24T10:37:19Z
  run "npm_and_yarn in /packages/create-aura - Update #1649848141" 2026-10-24T10:37:20Z
  run "npm_and_yarn in /packages/create-aura for vitest - Update #1649961961" 2026-10-24T12:20:13Z
  run "pip in /docker/aura-ingest - Update #1649848146" 2026-10-24T10:37:20Z
  run "github_actions in /. - Update #1649848153" 2026-10-24T10:37:20Z
  run "docker_compose in /. - Update #1649848156" 2026-10-24T10:37:19Z
  run "docker in /docker/arcadedb-mcp, /docker/aura, /docker/aura-egress, /docker/aura-ingest, /docker/aura-sandbox, /docker/caddy, /docker/cloudflared - Update #1649848160" 2026-10-24T10:37:20Z
} | jq -s . >"$fixtures/all.json"

out="$(check "$fixtures/all.json")" || fail "every entry ran, yet the check failed: $out"
[[ "$(grep -c '| ok |' <<<"$out")" == 7 ]] || fail "want 7 entries ok, got: $out"

jq '[.[] | select(.name | startswith("go_modules") | not)]' "$fixtures/all.json" >"$fixtures/no-go.json"
if out="$(check "$fixtures/no-go.json")"; then fail "a missing gomod entry passed"; fi
grep -q '| gomod | / | 0 |' <<<"$out" || fail "the gomod row does not say 0 runs: $out"

jq --argjson r "$(run "pip in /docker/aura-ingest for pypdf - Update #1" 2026-10-22T05:53:13Z failure)" \
  '. + [$r]' "$fixtures/all.json" >"$fixtures/failed.json"
if out="$(check "$fixtures/failed.json")"; then fail "a failed run passed"; fi
grep -q '| pip | /docker/aura-ingest | 2 | .* | 1 |' <<<"$out" || fail "the pip row does not count the failure: $out"

jq '[.[] | if (.name | startswith("docker_compose")) then .created_at = "2026-10-15T03:25:00Z" else . end]' \
  "$fixtures/all.json" >"$fixtures/stale.json"
if check "$fixtures/stale.json" >/dev/null; then fail "a run nine days old counted"; fi

echo "security-scan dependabot test: pass"
