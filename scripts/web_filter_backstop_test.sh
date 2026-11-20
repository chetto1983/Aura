#!/usr/bin/env bash
# Exercises scripts/web_filter_backstop.sh in a shallow clone, as the web CI jobs run it. A PR's
# changes are counted from its merge base with the base branch, the way dorny/paths-filter counts
# them, so web commits that landed on the base branch after the PR branched are not the PR's.
set -euo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/web_filter_backstop.sh"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT

fail() { echo "web-filter-backstop-test: $*" >&2; exit 1; }

git init --quiet --initial-branch=master "$work/src"
cd "$work/src"
git config user.email test@example.invalid
git config user.name test
commit() { mkdir -p "$(dirname "$1")"; echo "$2" >>"$1"; git add -A; git commit --quiet -m "$2"; git rev-parse HEAD; }

root="$(commit web/app.ts root)"
git switch --quiet -c docker-only
docker_only="$(commit docker/Dockerfile 'a docker change')"
git switch --quiet -c web-change master
web_change="$(commit web/src/view.tsx 'a web change')"
git switch --quiet master
# The base branch moves on after both branches leave it, web included, and further than the
# backstop's first fetch depth reaches.
commit web/app.ts 'web on master' >/dev/null
for i in $(seq 1 70); do commit internal/x.go "go $i" >/dev/null; done
master="$(git rev-parse HEAD)"

git clone --quiet --bare . "$work/origin.git"
git -C "$work/origin.git" config uploadpack.allowAnySHA1InWant true

# Each case runs in a fresh one-commit clone, as actions/checkout leaves the runner.
run() {
  local dir
  dir="$(mktemp -d "$work/clone.XXXX")"
  git clone --quiet --depth 1 "file://$work/origin.git" "$dir"
  (cd "$dir" && bash "$script" "$1" "$2")
}

run "$master" "$docker_only" >/dev/null 2>&1 ||
  fail "a PR touching only docker/ failed because web/ moved on its base branch"
if run "$master" "$web_change" >/dev/null 2>&1; then
  fail "a PR touching web/ passed"
fi
run "$root" "$docker_only" >/dev/null 2>&1 || fail "a push range touching only docker/ failed"
if run "$root" "$web_change" >/dev/null 2>&1; then
  fail "a push range touching web/ passed"
fi
run 0000000000000000000000000000000000000000 "$web_change" >/dev/null 2>&1 ||
  fail "a first push (all-zero before) did not fail open"
run "$(printf 'f%.0s' $(seq 1 40))" "$web_change" >/dev/null 2>&1 ||
  fail "an unfetchable base did not fail open"

echo "web-filter-backstop test: pass"
