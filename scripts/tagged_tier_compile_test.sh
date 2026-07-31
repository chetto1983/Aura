#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="$repo_root/scripts/tagged_tier_compile.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf -- "$fixture_root"' EXIT

mkdir -p "$fixture_root/good" "$fixture_root/unknown"
cat >"$fixture_root/good/db_test.go" <<'GO'
//go:build db_integration

package fixture
GO
cat >"$fixture_root/good/live_test.go" <<'GO'
//go:build paid_live

package fixture
GO
cat >"$fixture_root/good/windows.go" <<'GO'
//go:build windows

package fixture
GO
cat >"$fixture_root/good/generator.go" <<'GO'
//go:build ignore

package main
GO

actual="$(AURA_TAGGED_SOURCE_ROOT="$fixture_root/good" "$gate" --list)"
expected=$'db_integration\npaid_live'
if [[ "$actual" != "$expected" ]]; then
  printf 'tagged-tier-compile-test: unexpected tags\nexpected:\n%s\nactual:\n%s\n' \
    "$expected" "$actual" >&2
  exit 1
fi

plan="$(AURA_TAGGED_SOURCE_ROOT="$fixture_root/good" "$gate" --plan)"
if [[ "$plan" != $'.\tdb_integration,paid_live' ]]; then
  printf 'tagged-tier-compile-test: unexpected package plan: %q\n' "$plan" >&2
  exit 1
fi

cat >"$fixture_root/unknown/unknown_test.go" <<'GO'
//go:build hidden_feature

package fixture
GO
if AURA_TAGGED_SOURCE_ROOT="$fixture_root/unknown" "$gate" --list >/dev/null 2>&1; then
  echo "tagged-tier-compile-test: an unclassified build tag passed" >&2
  exit 1
fi

echo "tagged-tier-compile-test: pass"
