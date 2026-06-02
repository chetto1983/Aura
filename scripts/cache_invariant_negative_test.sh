#!/usr/bin/env bash
# SC#5 NEGATIVE proof for the cache invariant gate (Pitfall #3: a green gate that
# exercises nothing). Without this, SC#5 is UNPROVEN — the gate could be silently
# green and let a poisoned messages[0] through.
#
# It deliberately feeds scripts/cache_invariant_audit.sh a POISONED hash stream
# (turn 03's messages[0] hash differs) via the AURA_CACHE_AUDIT_CMD seam and
# asserts the wrapper exits NON-zero with `mutated` on stderr. If the wrapper
# instead exits 0 on a mutated prefix, THIS script fails loudly — that is the
# "gate is silently green" alarm.
#
# The canonical Go-level SC#5 proof is cmd/aura/cache_test.go
# (TestCacheAudit_Mutation_Exit1, which drives reportHashes with a poisoned
# request list). This script proves the BASH wrapper's own independent hash diff
# also catches drift — both layers of the belt-and-suspenders gate are exercised.
#
# Postgres-free: it never starts the DB stack and never runs the real subcommand.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

readonly WRAPPER="scripts/cache_invariant_audit.sh"

# --- Case 1: a mutated messages[0] MUST make the gate exit non-zero with `mutated`.
# Emit 20 `turn NN: <hex>` lines where turn 03 carries a different hash — exactly
# the prefix poisoning a future slice (1.8b/7e/10/11e) could introduce.
poisoned_stream() {
  cat <<'POISON'
echo "turn 01: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 02: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 03: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
echo "turn 04: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 05: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 06: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 07: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 08: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 09: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 10: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 11: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 12: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 13: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 14: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 15: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 16: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 17: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 18: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 19: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "turn 20: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
POISON
}

set +e
MUT_OUT="$(AURA_CACHE_AUDIT_CMD="$(poisoned_stream)" bash "$WRAPPER" 2>&1)"
MUT_CODE=$?
set -e

if [[ "$MUT_CODE" -eq 0 ]]; then
  echo "FAIL (SC#5): the gate exited 0 on a mutated messages[0] — IT IS SILENTLY GREEN." >&2
  printf '%s\n' "$MUT_OUT" >&2
  exit 1
fi
if ! printf '%s\n' "$MUT_OUT" | grep -q 'mutated'; then
  echo "FAIL (SC#5): gate exited ${MUT_CODE} but never said 'mutated' (wrong failure)." >&2
  printf '%s\n' "$MUT_OUT" >&2
  exit 1
fi
echo "ok (SC#5 case 1): mutated messages[0] -> gate exit ${MUT_CODE} with 'mutated'"

# --- Case 2: NO-SKIP-AS-GREEN — an EMPTY run (subcommand emits nothing) MUST also
# fail, never pass silently.
set +e
EMPTY_OUT="$(AURA_CACHE_AUDIT_CMD="true" bash "$WRAPPER" 2>&1)"
EMPTY_CODE=$?
set -e

if [[ "$EMPTY_CODE" -eq 0 ]]; then
  echo "FAIL (SC#5): the gate exited 0 on EMPTY output — NO-SKIP-AS-GREEN violated." >&2
  printf '%s\n' "$EMPTY_OUT" >&2
  exit 1
fi
echo "ok (SC#5 case 2): empty cache-audit output -> gate exit ${EMPTY_CODE} (no-skip-as-green)"

echo "==> cache_invariant_negative_test: PASS (the gate is NOT silently green)"
