#!/usr/bin/env bash
# tagged_tier_pre_push.sh — the pre-push entry point for the tagged-tier compile gate.
#
# It exists for the same reason scripts/deadcode_pre_push.sh does, and it was diagnosed the
# same way: lefthook runs a step's `run:` through `env`, and DOUBLE quotes nested inside the
# SINGLE-quoted `bash -c '…'` string are mangled before any shell sees them:
#
#   run: bash -c 'cd "$(git rev-parse --show-toplevel)" && bash scripts/tagged_tier_compile.sh --compile'
#   → exit 2 in ~0.10s, with no output at all
#
# Two further Windows-only traps were in the way before that one, both of which also failed
# CLOSED in well under a second against a real runtime of ~61s — fast enough to read as a
# cached pass in lefthook's summary rather than as a gate that never started:
#
#   1. lefthook appends the changed files to the command, so tagged_tier_compile.sh received
#      a source path as $1, hit its own usage guard and exited before compiling anything.
#      Hence the explicit --compile below.
#   2. lefthook's root arrives Windows-spelled (D:/Repo/Aura) while the script's discovery
#      paths only resolve from the MSYS form, so it reported "no Aura tier tags found".
#      Hence the cd, which lets it compute its own root exactly as a direct invocation does.
#
# CI (.github/workflows/ci.yml) and the Makefile keep calling tagged_tier_compile.sh directly;
# that contract is unchanged.
set -euo pipefail

cd "$(dirname "$0")/.."

exec bash scripts/tagged_tier_compile.sh --compile
