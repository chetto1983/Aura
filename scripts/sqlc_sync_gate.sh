#!/usr/bin/env bash
#
# internal/db/sqlc/ is generated FROM internal/db/migrations/ and internal/db/queries/. A
# migration that drops a table leaves a struct behind for a table the schema no longer has,
# and nothing fails to compile: a model with no query file is unreferenced Go, so vet, lint,
# build and the whole test suite stay green while the two disagree.
#
# CI's sqlc-golden job catches it — after the push, as a red build. That is exactly the
# shape the payload-manifest gate already exists to avoid, and this gate is its sibling:
# one `sqlc generate` and a diff, run before the network, telling you while it is still one
# command to fix. Measured 2026-09-06: migration 0119 dropped two tables, their models were
# not regenerated, and master went red on a commit whose own tests all passed.
#
# Self-guards on a missing sqlc: the hook must not fail a push on a machine that has not
# run `make tools`, or it gets bypassed habitually and stops protecting anything. CI always
# has it (it installs the pinned version), so the gate still holds where it is mandatory.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

sqlc_bin="$(command -v sqlc || true)"
if [ -z "$sqlc_bin" ] && [ -x "$HOME/go/bin/sqlc" ]; then
  sqlc_bin="$HOME/go/bin/sqlc"
fi
if [ -z "$sqlc_bin" ]; then
  echo "sqlc-sync: sqlc not on PATH — skipping (install with 'make tools'; CI still enforces this)" >&2
  exit 0
fi

# A dirty generated tree before we start would be reported as OUR drift, which is a
# confusing way to tell someone their working copy has uncommitted generated changes.
if ! git diff --quiet -- internal/db/sqlc/; then
  echo "sqlc-sync: internal/db/sqlc/ has uncommitted changes — commit or discard them first" >&2
  exit 1
fi

"$sqlc_bin" generate

if ! git diff --quiet -- internal/db/sqlc/; then
  echo "sqlc-sync: internal/db/sqlc/ is out of date with migrations/queries. Run 'make sqlc' and commit the result:" >&2
  git --no-pager diff --stat -- internal/db/sqlc/ >&2
  exit 1
fi

echo "ok: sqlc generate is in sync"
