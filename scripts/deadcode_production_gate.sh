#!/usr/bin/env bash
# deadcode_production_gate.sh <deadcode-binary> <deadexport-binary> — what no production
# binary can reach.
#
# deadcode_gate.sh runs deadcode with -test, so every test is a root and a function only a
# test calls reads as live. That run stays (it catches code nothing calls at all); this one
# runs WITHOUT -test, so the roots are the module's main packages only, and reports what
# nothing shipped can reach. Measured 2026-11-24: the -test gate printed nothing while this
# run found production functions kept alive by their own tests alone.
#
# deadcode alone still cannot see an exported method nobody calls on a type that reaches an
# interface: its RTA keeps every exported method of such a type alive, since reflection
# might call it (go/callgraph/rta). tools/deadexport counts type-checked references from
# non-test code instead and covers that half; on 2026-11-24 it found 49 of the 55
# functions removed that day, none of which deadcode reported. This gate takes the union.
#
# A function is reported only when it is dead on BOTH linux and windows: a helper called
# only from a _windows.go file is live there (retention ratioFromCapacity), not dead.
#
# Every finding must be named in scripts/deadcode_production_allowlist.txt with a reason,
# either by symbol (<package path>.<Func> or <package path>.<Type>.<Method>) or by package
# path (a test-support package, live only for tests by design). The list fails closed both
# ways: an unlisted finding fails, and so does a listed symbol or package that no longer
# produces a finding, so the list cannot outlive the code it excuses. A method called only
# by reflection (a template, MethodByName) would be a finding too, and belongs on the list.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <deadcode-binary> <deadexport-binary>" >&2
  exit 2
fi
bin="$1"
deadexport="$2"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# The self-test (deadcode_production_gate_test.sh) points the gate at a fixture module.
source_root="${AURA_DEADCODE_SOURCE_ROOT:-$repo_root}"
cd "$source_root"
allowlist="scripts/deadcode_production_allowlist.txt"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mapfile -t packages < <(bash "$repo_root/scripts/go_packages.sh")
# scan <goos> writes $work/dead.<goos>: the union of both tools' findings for that GOOS.
# A tool that fails, or a deadcode that prints load errors, fails the scan: a partial
# program would make every function whose callers did not load read as dead.
scan() {
  local goos="$1"
  local format='{{range .Funcs}}{{$.Path}}.{{.Name}}{{"\n"}}{{end}}'
  if ! GOOS="$goos" "$bin" -f "$format" "${packages[@]}" >"$work/raw.$goos" 2>"$work/err.$goos" ||
    [ -s "$work/err.$goos" ]; then
    cat "$work/err.$goos" >&2
    echo "FATAL: deadcode failed or reported load errors for GOOS=$goos" >&2
    return 2
  fi
  if ! GOOS="$goos" "$deadexport" "${packages[@]}" >"$work/exported.$goos"; then
    echo "FATAL: deadexport failed for GOOS=$goos" >&2
    return 2
  fi
  cut -f1 "$work/exported.$goos" >>"$work/raw.$goos"
  grep -v '^$' "$work/raw.$goos" | sort -u >"$work/dead.$goos" || true
}
scan linux &
linux_scan=$!
scan windows &
windows_scan=$!
scan_status=0
wait "$linux_scan" || scan_status=$?
wait "$windows_scan" || scan_status=$?
[ "$scan_status" -eq 0 ] || exit "$scan_status"
comm -12 "$work/dead.linux" "$work/dead.windows" >"$work/findings"

awk -v file="$allowlist" '
  /^[[:space:]]*(#|$)/ { next }
  NF < 2 { printf "FATAL: %s:%d: entry %s has no reason\n", file, NR, $1 > "/dev/stderr"; bad = 1; next }
  { print $1 }
  END { exit bad }
' "$allowlist" | sort -u >"$work/allowed"

# A finding is excused by its exact symbol or by its package path: the package is everything
# up to the last slash plus the next segment's name before its first dot.
status=0
awk -v allowed="$work/allowed" -v used="$work/used" '
  BEGIN { while ((getline line < allowed) > 0) ok[line] = 1 }
  {
    n = split($0, parts, "/")
    last = parts[n]
    pkg = substr($0, 1, length($0) - length(last)) substr(last, 1, index(last, ".") - 1)
    if ($0 in ok) { print $0 > used; next }
    if (pkg in ok) { print pkg > used; next }
    print "unreachable from every main package: " $0 > "/dev/stderr"
    failed = 1
  }
  END { exit failed }
' "$work/findings" || status=1

touch "$work/used"
sort -u "$work/used" -o "$work/used"
while IFS= read -r stale; do
  echo "stale allowlist entry (no longer dead): $stale" >&2
  status=1
done < <(comm -23 "$work/allowed" "$work/used")

if [ "$status" -ne 0 ]; then
  echo "FATAL: production reachability gate failed; remove the dead code, or name it in $allowlist with the reason it stays" >&2
fi
exit "$status"
