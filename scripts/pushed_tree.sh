#!/usr/bin/env bash
# pushed_tree.sh — run the pre-push gates on the commit being pushed, not on the working tree.
#
# A gate reads files, and the working tree is not what a push sends. Measured 2026-09-23,
# with two sessions sharing one working tree: knip failed a push on an untracked panel the
# other session was still writing, and the same tree would have passed a push whose commits
# exported symbols only that uncommitted panel used. Gates run on a clean checkout of the
# pushed commit instead, so what they measure is exactly what reaches the network.
#
#   pushed_tree.sh prepare      read pre-push's stdin and check the pushed commit out
#   pushed_tree.sh <cmd> [...]  run one gate inside that checkout
#
# The checkout is a detached git worktree OUTSIDE the repository (so no scanner of the working
# tree ever walks it), reused between pushes: a later push rewrites only the files that
# changed. node_modules is a symlink to the working tree's install, which is what the web
# gates need and is gitignored. AURA_PUSHED_TREE overrides the location.
#
# One push at a time per machine: two concurrent pushes would share the checkout. A gate
# refuses to run if the checkout moved off the commit it was prepared for, which is what a
# second push would do to the first.
set -euo pipefail

tree="${AURA_PUSHED_TREE:-$HOME/.cache/aura/pushed-tree}"
marker="$tree.commit"

all_zero() { case "$1" in *[!0]*) return 1 ;; *) return 0 ;; esac; }

# pushed_commit reads `<local ref> <local oid> <remote ref> <remote oid>` lines and prints the
# one commit being pushed, "none" for a push that only deletes, or HEAD when run by hand.
pushed_commit() {
  local commits="" lines=0 local_ref local_oid remote_ref remote_oid
  while read -r local_ref local_oid remote_ref remote_oid; do
    lines=$((lines + 1))
    all_zero "$local_oid" && continue
    commits="$commits $(git rev-parse "$local_oid^{commit}")"
  done
  if [ "$lines" -eq 0 ]; then
    git rev-parse HEAD
    return
  fi
  commits="$(printf '%s\n' $commits | sort -u)"
  case "$(printf '%s' "$commits" | grep -c .)" in
    0) echo none ;;
    1) echo "$commits" ;;
    *)
      echo "pushed_tree: this push sends several different commits; the gates check one." >&2
      echo "pushed_tree: push one branch or tag at a time." >&2
      return 1
      ;;
  esac
}

# is_our_worktree: the checkout exists, git can still read it (its metadata was not pruned),
# and it belongs to this repository.
is_our_worktree() {
  local ours theirs
  ours="$(git rev-parse --path-format=absolute --git-common-dir)"
  theirs="$(git -C "$tree" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" || return 1
  [ "$ours" = "$theirs" ]
}

prepare() {
  local commit main
  commit="$(pushed_commit)"
  mkdir -p "$(dirname "$tree")"
  if [ "$commit" = none ]; then
    echo none >"$marker"
    echo "pre-push gates: this push only deletes refs, nothing to check"
    return
  fi
  if is_our_worktree; then
    git -C "$tree" checkout -q --detach --force "$commit"
    git -C "$tree" clean -q -ffdx
  else
    rm -rf "$tree"
    git worktree add -q -f --detach "$tree" "$commit"
  fi
  main="$(git rev-parse --show-toplevel)"
  for dir in . web; do
    if [ -d "$main/$dir/node_modules" ]; then
      ln -sfn "$main/$dir/node_modules" "$tree/$dir/node_modules"
    fi
  done
  echo "$commit" >"$marker"
  echo "pre-push gates run on $(git log --oneline -1 "$commit") in $tree"
}

gate() {
  local commit
  commit="$(cat "$marker" 2>/dev/null || true)"
  if [ -z "$commit" ]; then
    echo "pushed_tree: no prepared checkout at $tree; the pushed-tree job must run first" >&2
    exit 1
  fi
  if [ "$commit" = none ]; then
    exit 0
  fi
  if [ "$(git -C "$tree" rev-parse HEAD)" != "$commit" ]; then
    echo "pushed_tree: $tree is no longer at $commit; another push moved it" >&2
    exit 1
  fi
  cd "$tree"
  exec "$@"
}

if [ "${1:-}" = prepare ]; then
  prepare
elif [ "$#" -gt 0 ]; then
  gate "$@"
else
  echo "usage: pushed_tree.sh prepare | pushed_tree.sh <gate command...>" >&2
  exit 2
fi
