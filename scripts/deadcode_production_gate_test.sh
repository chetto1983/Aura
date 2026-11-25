#!/usr/bin/env bash
# Self-test for scripts/deadcode_production_gate.sh: a gate that can never fail is worthless,
# so each case points it at a throwaway module and asserts the exit code and the finding.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="$repo_root/scripts/deadcode_production_gate.sh"
bin="${DEADCODE_BIN:-$(command -v deadcode || echo "$(go env GOPATH)/bin/deadcode")}"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
deadexport="${DEADEXPORT_BIN:-}"
if [ -z "$deadexport" ]; then
  deadexport="$work/deadexport"
  go build -C "$repo_root/tools/deadexport" -o "$deadexport" .
fi

# fixture <name> <allowlist body>: a module whose main calls lib.Used; lib.TestOnly is called
# only by a test; lib.ratio, compiled everywhere, is called only from a _windows.go file
# (the shape of retention's ratioFromCapacity): dead on linux, live on windows; and
# lib.Thing reaches an interface through fmt.Println, so deadcode keeps its uncalled Spare
# alive and only deadexport reports it.
fixture() {
  local root="$work/$1"
  mkdir -p "$root/cmd/app" "$root/lib" "$root/scripts"
  printf 'module example.test/fixture\n\ngo 1.22\n' >"$root/go.mod"
  cat >"$root/cmd/app/main.go" <<'GO'
package main

import (
	"fmt"

	"example.test/fixture/lib"
)

func main() { lib.Used(); lib.Platform(); fmt.Println(lib.Thing{}) }
GO
  cat >"$root/lib/lib.go" <<'GO'
package lib

func Used() {}

func TestOnly() {}

func ratio() {}

type Thing struct{}

func (Thing) String() string { return "thing" }

func (Thing) Spare() {}
GO
  cat >"$root/lib/platform_windows.go" <<'GO'
package lib

func Platform() { ratio() }
GO
  cat >"$root/lib/platform_other.go" <<'GO'
//go:build !windows

package lib

func Platform() {}
GO
  cat >"$root/lib/lib_test.go" <<'GO'
package lib

import "testing"

func TestTestOnly(t *testing.T) { TestOnly() }
GO
  printf '%s' "$2" >"$root/scripts/deadcode_production_allowlist.txt"
  git -C "$root" init -q && git -C "$root" add -A
  echo "$root"
}

# expect <name> <want-rc> <want-stderr-substring> <allowlist body>
expect() {
  local root rc=0
  root="$(fixture "$1" "$4")"
  AURA_DEADCODE_SOURCE_ROOT="$root" bash "$gate" "$bin" "$deadexport" >"$work/$1.out" 2>"$work/$1.err" || rc=$?
  if [ "$rc" -ne "$2" ]; then
    echo "FAIL $1: rc=$rc, want $2" >&2
    cat "$work/$1.err" >&2
    exit 1
  fi
  if [ -n "$3" ] && ! grep -qF -- "$3" "$work/$1.err"; then
    echo "FAIL $1: output does not contain [$3]" >&2
    cat "$work/$1.err" >&2
    exit 1
  fi
  echo "ok   $1"
}

# A function only a test calls is dead to production: -test would have hidden it.
expect test-only-is-dead 1 "unreachable from every main package: example.test/fixture/lib.TestOnly" ""
# deadcode keeps Spare alive (Thing reaches fmt.Stringer); deadexport reports it.
expect exported-method-on-interface-type-is-dead 1 "unreachable from every main package: example.test/fixture/lib.Thing.Spare" "example.test/fixture/lib.TestOnly   called by tests only
"
# Named with a reason, they pass, and ratio (live on windows) is never reported: were it,
# the gate would fail on an unlisted finding.
expect allowlisted-passes 0 "" "example.test/fixture/lib.TestOnly   called by tests only
example.test/fixture/lib.Thing.Spare   kept for the fixture
"
# A package entry excuses everything in the package.
expect package-entry-passes 0 "" "example.test/fixture/lib   fixture package
"
# An entry that no longer matches a finding fails: the list cannot outlive the code.
expect stale-entry-fails 1 "stale allowlist entry (no longer dead): example.test/fixture/lib.Gone" "example.test/fixture/lib.TestOnly   called by tests only
example.test/fixture/lib.Thing.Spare   kept for the fixture
example.test/fixture/lib.Gone   removed long ago
"
# An entry without a reason fails.
expect reasonless-entry-fails 1 "has no reason" "example.test/fixture/lib.TestOnly
"
echo "deadcode production gate self-test: pass"
