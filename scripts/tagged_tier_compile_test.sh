#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="$repo_root/scripts/tagged_tier_compile.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf -- "$fixture_root"' EXIT

mkdir -p \
  "$fixture_root/good/internal/fixture" \
  "$fixture_root/good/.planning/spikes/example" \
  "$fixture_root/unknown/internal/fixture"
cat >"$fixture_root/good/internal/fixture/db_test.go" <<'GO'
//go:build db_integration

package fixture
GO
cat >"$fixture_root/good/internal/fixture/live_test.go" <<'GO'
//go:build paid_live

package fixture
GO
cat >"$fixture_root/good/internal/fixture/measure_test.go" <<'GO'
//go:build measure

package fixture
GO
cat >"$fixture_root/good/internal/fixture/windows.go" <<'GO'
//go:build windows

package fixture
GO
cat >"$fixture_root/good/internal/fixture/generator.go" <<'GO'
//go:build ignore

package main
GO
cat >"$fixture_root/good/.planning/spikes/example/main.go" <<'GO'
//go:build spike_example

package main
GO

actual="$(AURA_TAGGED_SOURCE_ROOT="$fixture_root/good" bash "$gate" --list)"
expected=$'db_integration\nmeasure\npaid_live'
if [[ "$actual" != "$expected" ]]; then
  printf 'tagged-tier-compile-test: unexpected tags\nexpected:\n%s\nactual:\n%s\n' \
    "$expected" "$actual" >&2
  exit 1
fi

plan="$(AURA_TAGGED_SOURCE_ROOT="$fixture_root/good" bash "$gate" --plan)"
if [[ "$plan" != $'internal/fixture\tdb_integration,measure,paid_live' ]]; then
  printf 'tagged-tier-compile-test: unexpected package plan: %q\n' "$plan" >&2
  exit 1
fi

cat >"$fixture_root/unknown/internal/fixture/unknown_test.go" <<'GO'
//go:build hidden_feature

package fixture
GO
if AURA_TAGGED_SOURCE_ROOT="$fixture_root/unknown" bash "$gate" --list >/dev/null 2>&1; then
  echo "tagged-tier-compile-test: an unclassified build tag passed" >&2
  exit 1
fi

echo "tagged-tier-compile-test: pass"
