#!/usr/bin/env bash
#
# Drives scripts/publish_images.sh against a stubbed docker and git. The images it builds must
# be the ones the GitHub workflows build, nothing may be pushed when a build or an image
# contract fails, and no moving tag may move before every per-commit tag is in the registry --
# with aura:edge last, as publish-aura-edge.yml does since 2026-09-27.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# shellcheck source=/dev/null
source "$repo_root/scripts/publish_images.sh"
declare -F main >/dev/null || fail "main undefined after sourcing the publisher"

# The workflows are the reference while GitHub runs them: same Dockerfiles, same edge order.
edge_workflow="$repo_root/.github/workflows/publish-aura-edge.yml"
workflow_files="$(sed -n 's/^ *file: *//p' "$edge_workflow" | sort -u | tr '\n' ' ')"
script_files="$(for e in "${EDGE_IMAGES[@]}"; do echo "${e#*:}"; done | sort -u | tr '\n' ' ')"
[[ "$workflow_files" == "$script_files" ]] || fail "edge Dockerfiles differ: workflow [$workflow_files] script [$script_files]"
workflow_order="$(sed -n 's/^ *for image in \(.*\); do$/\1/p' "$edge_workflow")"
script_order="$(for e in "${EDGE_IMAGES[@]}"; do printf '%s ' "${e%%:*}"; done)"
[[ "$workflow_order " == "$script_order" ]] || fail "edge tag order differs: workflow [$workflow_order] script [$script_order]"
[[ "$(sed -n 's/^ *file: *//p' "$repo_root/.github/workflows/publish-arcadedb-mcp.yml")" == "${ARCADEDB_MCP#*:}" ]] ||
  fail "arcadedb-mcp Dockerfile differs from its workflow"
echo "ok: the publisher builds what the GitHub workflows build, in the edge workflow's order"

calls="$fixture/calls.log"
src="$fixture/src"
mkdir -p "$src/scripts" "$src/fork"
for s in cloudflared_image_contract_test.sh ingest_image_contract_test.sh sandbox_image_contract_test.sh; do
  printf '#!/usr/bin/env bash\n' >"$src/scripts/$s"
done
touch "$src/fork/Dockerfile"
# The commit being published names its own home, unlike the working tree's.
mkdir -p "$src/deploy"
printf 'IMAGE_HOME=registry.fromcommit/aura\n' >"$src/deploy/aura-image-update.sh"
failing_build=""
failing_contract=""
failing_push=""
flaky_push=""
on_master=1
sleep() { :; }
git() {
  echo "git $*" >>"$calls"
  case "$*" in
    *' fetch --quiet gitlab master') ;;
    *' merge-base --is-ancestor 0123456789abcdef0123456789abcdef01234567 gitlab/master') ((on_master)) ;;
    *' rev-parse '*) echo 0123456789abcdef0123456789abcdef01234567 ;;
    *' archive '*) tar -c -C "$src" . ;;
    'clone --quiet '*) mkdir -p "${*: -1}" && cp -R "$src/fork/." "${*: -1}" ;;
    *' checkout --quiet '*) ;;
    *) fail "unexpected git $*" ;;
  esac
}
docker() {
  echo "docker $*" >>"$calls"
  if [[ "$1" == build && -n "$failing_build" && "$*" == *"/$failing_build:"* ]]; then return 1; fi
  if [[ "$1" == push && -n "$failing_push" && "$2" == *"/$failing_push:"* ]]; then return 1; fi
  if [[ "$1" == push && -n "$flaky_push" && "$2" == *"/$flaky_push:"* && ! -e "$fixture/flaked" ]]; then
    touch "$fixture/flaked"
    return 1
  fi
  case "$1 ${2:-}" in
    'build '* | 'push '* | 'buildx imagetools' | 'image rm') ;;
    *) fail "unexpected docker $*" ;;
  esac
}
bash() {
  # The week is real: an empty one would let a missing --build-arg value pass unseen.
  [[ "$1" != */scripts/apt_week.sh ]] || { command bash "$@"; return; }
  # Each contract reads its image from a variable of its own; only that one is set for it.
  echo "contract $* image=${AURA_CLOUDFLARED_TEST_IMAGE:-}${AURA_INGEST_IMAGE:-}${AURA_SANDBOX_IMAGE:-}" >>"$calls"
  [[ -z "$failing_contract" || "$1" != *"$failing_contract"* ]]
}
export AURA_PUBLISH_REGISTRY=registry.example/aura

# The tags a run built, and the tags it removed from this machine, one per line, sorted.
built_tags() { sed -n 's/^docker build .* -t \([^ ]*\) [^ ]*$/\1/p' "$calls" | grep -v "/${1:-none}:" | sort; }
removed_tags() { sed -n 's/^docker image rm \([^ ]*\)$/\1/p' "$calls" | sort; }

: >"$calls"
main HEAD >"$fixture/out" || fail "a clean publish failed: $(tail -5 "$calls")"
pushes="$(grep -c '^docker push ' "$calls")"
[[ "$pushes" == 9 ]] || fail "expected 9 per-commit pushes, saw $pushes"
last_push="$(grep -n '^docker push ' "$calls" | tail -1 | cut -d: -f1)"
first_move="$(grep -n '^docker buildx imagetools create' "$calls" | head -1 | cut -d: -f1)"
((last_push < first_move)) || fail "a moving tag moved before every per-commit tag was pushed"
grep '^docker buildx imagetools create' "$calls" | tail -1 | grep -q -- '--tag registry.example/aura/aura:edge ' ||
  fail "aura:edge did not move last: $(grep '^docker buildx' "$calls" | tail -1)"
grep -q '^docker build .*--build-arg VCS_REF=0123456789abcdef0123456789abcdef01234567' "$calls" ||
  fail "aura was not stamped with its commit"
week="$(command bash "$repo_root/scripts/apt_week.sh")"
[[ "$week" =~ ^[0-9]{4}-W[0-9]{2}$ ]] || fail "scripts/apt_week.sh printed $week, not an ISO week"
for name in aura aura-ingest; do
  grep -q "^docker build .*/docker/${name}/Dockerfile .*--build-arg APT_WEEK=${week} -t " "$calls" ||
    fail "${name} was not built with this week's apt refresh"
done
grep -q -- '--tag registry.example/aura/aura-pim-mcp:sidecar ' "$calls" || fail "the pim fork's sidecar tag did not move"
grep -q -- '--tag registry.example/aura/whatsapp-mcp:latest ' "$calls" || fail "the whatsapp fork's latest tag did not move"
echo "ok: every per-commit tag is pushed before any moving tag moves, and aura:edge moves last"

# A misspelt variable would leave a contract testing its default image instead of this commit's.
for name in cloudflared ingest sandbox; do
  grep -qx "contract .*/scripts/${name}_image_contract_test.sh image=registry.example/aura/aura-${name}:master-0123456789abcdef0123456789abcdef01234567" "$calls" ||
    fail "the ${name} contract did not test this commit's aura-${name}: $(grep "${name}_image_contract" "$calls")"
done
echo "ok: each image contract tests this commit's image"

# Measured 2026-10-14: five publishes left 38 tags, 14.35 GB, on the operator's Docker.
[[ -n "$(built_tags)" && "$(built_tags)" == "$(removed_tags)" ]] ||
  fail "the images this publish built are still on the machine: built [$(built_tags | tr '\n' ' ')] removed [$(removed_tags | tr '\n' ' ')]"
last_move="$(grep -n '^docker buildx imagetools create' "$calls" | tail -1 | cut -d: -f1)"
first_remove="$(grep -n '^docker image rm ' "$calls" | head -1 | cut -d: -f1)"
((last_move < first_remove)) || fail "an image was removed before every moving tag had moved"
echo "ok: a publish removes the images it built, once every tag has moved"

for case in build contract; do
  : >"$calls"
  if [[ "$case" == build ]]; then failing_build=aura-ingest; else failing_contract=sandbox_image_contract_test.sh; fi
  if main HEAD >"$fixture/out" 2>&1; then fail "a failed $case still published"; fi
  ! grep -q '^docker push ' "$calls" || fail "a failed $case pushed: $(grep '^docker push ' "$calls")"
  [[ "$(built_tags "$failing_build")" == "$(removed_tags)" ]] ||
    fail "a failed $case left its images behind: built [$(built_tags "$failing_build" | tr '\n' ' ')] removed [$(removed_tags | tr '\n' ' ')]"
  failing_build="" failing_contract=""
done
echo "ok: a failed build or image contract pushes nothing and leaves no image behind"

: >"$calls"
failing_push=aura-ingest
if main HEAD >"$fixture/out" 2>&1; then fail "a failed push still published"; fi
! grep -q '^docker buildx imagetools create' "$calls" || fail "a failed push moved a tag: $(grep '^docker buildx' "$calls" | head -1)"
[[ "$(built_tags)" == "$(removed_tags)" ]] || fail "a failed push left its images behind"
failing_push=""
echo "ok: a failed push moves no tag and leaves no image behind"

# Measured 2026-10-14: three of five publishes lost one upload, twice to a broken pipe through
# Docker Desktop's proxy and once to a response-header timeout.
: >"$calls"
flaky_push=aura-ingest
main HEAD >"$fixture/out" 2>&1 || fail "a push the registry dropped once failed the publish: $(tail -3 "$fixture/out")"
[[ "$(grep -c '^docker push .*/aura-ingest:' "$calls")" == 2 ]] || fail "the dropped push was not retried: $(grep '^docker push' "$calls")"
grep '^docker buildx imagetools create' "$calls" | tail -1 | grep -q -- '--tag registry.example/aura/aura:edge ' ||
  fail "the tags did not move after the retried push"
flaky_push=""
echo "ok: a push the registry drops gets one more try"

# Appliances update to whatever aura:edge names, and master-<sha> claims the commit is there.
: >"$calls"
on_master=0
if main HEAD >"$fixture/out" 2>&1; then fail "a commit that is not on master was published"; fi
grep -q 'not on gitlab/master' "$fixture/out" || fail "refused for another reason: $(cat "$fixture/out")"
! grep -q '^docker ' "$calls" || fail "a commit that is not on master was built: $(grep '^docker ' "$calls" | head -1)"
on_master=1
echo "ok: only a commit already on the remote's master is built and published"

# The registry is the published commit's IMAGE_HOME. On the return trip the working tree may name
# one home while the commit being published names the other.
: >"$calls"
(unset AURA_PUBLISH_REGISTRY && main HEAD >"$fixture/out" 2>&1) || fail "a publish with no AURA_PUBLISH_REGISTRY failed: $(tail -3 "$fixture/out")"
grep -q -- ' -t registry.fromcommit/aura/aura:master-' "$calls" ||
  fail "the publish did not use the commit's IMAGE_HOME: $(grep '^docker build' "$calls" | head -1)"
! grep -q 'registry.gitlab.com' "$calls" || fail "the working tree's IMAGE_HOME reached the publish"
echo "ok: the registry is the published commit's IMAGE_HOME"
