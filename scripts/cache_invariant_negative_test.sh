#!/usr/bin/env bash
# SC#5 NEGATIVE proof for the cache invariant gate (Pitfall #3: a green gate that
# exercises nothing). Without this, SC#5 is UNPROVEN — the gate could be silently
# green and let a poisoned messages[0] through.
#
# It deliberately feeds scripts/cache_invariant_audit.sh a POISONED hash stream
# (request 03 messages[0] hash differs) via the AURA_CACHE_AUDIT_CMD seam and
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
# Emit 22 `request NN: <hex>` lines where request 03 carries a different hash.
# the prefix poisoning a future slice (1.8b/7e/10/11e) could introduce.
poisoned_stream() {
  cat <<'POISON'
echo "request 01: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 02: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 03: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
echo "request 04: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 05: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 06: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 07: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 08: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 09: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 10: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 11: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 12: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 13: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 14: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 15: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 16: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 17: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 18: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 19: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 20: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 21: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
echo "request 22: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
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
