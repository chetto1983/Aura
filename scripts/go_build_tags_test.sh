#!/usr/bin/env bash
# Self-test for scripts/go_build_tags.sh, run by scripts/run_script_tests.sh.
set -euo pipefail
script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/go_build_tags.sh"
repo="$(mktemp -d)"
trap 'rm -rf "${repo:?}"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

cd "$repo"
git init -q
mkdir -p a b c
printf '//go:build db_integration\n\npackage a\n' > a/x_test.go
printf '//go:build calendar_integration || whatsapp_integration\n\npackage a\n' > a/y_test.go
printf '//go:build !race && linux && amd64\n\npackage b\n' > b/z.go
printf '//go:build ignore\n\npackage c\n' > c/gen.go
printf '//go:build go1.27 && windows\n\npackage c\n' > c/w.go
printf '//go:build untracked_tag\n\npackage c\n' > c/u.go
git add a b c/gen.go c/w.go

got="$(bash "$script")"
want="calendar_integration,db_integration,whatsapp_integration"
[ "$got" = "$want" ] || fail "tags = '$got', want '$want' (platform, race, ignore, Go versions and untracked files left out)"

git rm -q --cached a/x_test.go a/y_test.go
if out="$(bash "$script" 2>&1)"; then
  fail "a tree with no custom tag printed '$out' instead of failing"
fi
echo "ok: go_build_tags"
