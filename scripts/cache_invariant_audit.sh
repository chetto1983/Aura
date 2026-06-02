#!/usr/bin/env bash
# Phase 6 cross-slice KV-cache prefix invariant gate (amendment #16, Pitfall #3 P0).
#
# This is the runtime-faithful CI gate that catches a FUTURE slice (1.8b
# microcompact, 7e, 10, 11e) mutating the assembled prefix. It drives the hidden
# `aura cache-audit` subcommand (06-04), which replays 20 deterministic fixtures
# through the REAL runner.Turn -> LlmAgent.Run -> PromptBuilder.Build path against
# an in-memory FakeClient (NO Postgres), hashes each captured messages[0] with
# prompt.PrefixHash({0}), and prints `request NN: <hex>` to stdout.
#
# Belt-and-suspenders (06-RESEARCH Pattern 4): the Go subcommand asserts the
# invariant AND exits non-zero with `messages[0] mutated at request N` on drift; THIS
# wrapper independently counts the 22 request hash lines and diffs them. Both must agree.
#
# NO-SKIP-AS-GREEN (CLAUDE.md): an EMPTY / 0-line / fewer-than-20-line run is a
# HARD failure, never a silent pass. The `| grep -c . || true` guard makes an empty
# capture abort with the hand-written diagnostic below instead of a bare pipefail.
#
# Exit codes mirror the subcommand: 0 pass / 1 messages[0] mutation / 2 fixture
# corrupt — the wrapper forwards the subcommand's exit code and stderr verbatim
# and ALSO fails 1 if its own independent hash diff disagrees.
#
# This gate is deliberately Postgres-free so it runs in any CI job with the DB
# stack down.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

readonly EXPECTED_REQUESTS=22

# AURA_CACHE_AUDIT_BIN lets the SC#5 negative test substitute an executable that
# emits a poisoned hash stream, without evaluating an environment-controlled shell
# command. In CI and locally it defaults to the real subcommand.
# Capture stdout, stderr, and the exit code separately. We must NOT let a non-zero
# subcommand exit abort the script before we can forward its diagnostic, so the
# invocation is guarded with `|| code=$?` (set -e would otherwise kill us here).
ERR_FILE="$(mktemp)"
trap 'rm -f "$ERR_FILE"' EXIT
code=0
if [[ -n "${AURA_CACHE_AUDIT_BIN:-}" ]]; then
  if [[ ! -x "$AURA_CACHE_AUDIT_BIN" ]]; then
    echo "cache-audit: AURA_CACHE_AUDIT_BIN is not executable: $AURA_CACHE_AUDIT_BIN" >"$ERR_FILE"
    OUT=""
    code=2
  else
    OUT="$("$AURA_CACHE_AUDIT_BIN" 2>"$ERR_FILE")" || code=$?
  fi
else
  OUT="$(go run ./cmd/aura cache-audit 2>"$ERR_FILE")" || code=$?
fi
ERR="$(cat "$ERR_FILE")"

# Independent line count: the `|| true` keeps the count at 0 (instead of a bare
# pipefail) when $OUT is EMPTY, so the diagnostic below — not a silent abort — is
# what the operator sees (the exact NO-SKIP-AS-GREEN failure this gate exists for).
HASH_LINES="$(printf '%s\n' "$OUT" | grep -cE '^request [0-9]{2}: [0-9a-f]+$' || true)"

fail() {
  echo "FAIL (cache invariant gate): $1" >&2
  echo "---- cache-audit stdout ----" >&2
  printf '%s\n' "$OUT" >&2
  echo "---- cache-audit stderr ----" >&2
  printf '%s\n' "$ERR" >&2
  exit 1
}

# The subcommand exited non-zero: forward its exit code + the explicit
# `messages[0] mutated at request N` (exit 1) or fixture-corrupt (exit 2) wording.
if [[ "$code" -ne 0 ]]; then
  echo "FAIL (cache invariant gate): aura cache-audit exited ${code}" >&2
  printf '%s\n' "$ERR" >&2
  printf '%s\n' "$OUT" >&2
  exit "$code"
fi

# NO-SKIP-AS-GREEN: a passing subcommand MUST have emitted exactly the expected
# request hash lines.
if [[ "${HASH_LINES:-0}" -ne "$EXPECTED_REQUESTS" ]]; then
  fail "expected exactly ${EXPECTED_REQUESTS} 'request NN: <hex>' lines, got ${HASH_LINES:-0} (empty/short output is never a silent pass)"
fi

# Independent hash diff (belt-and-suspenders over the Go assertion): every request's
# messages[0] hash must be byte-identical. The first drift is the SC#5 failure.
FIRST_HASH=""
LINE_NO=0
while IFS= read -r line; do
  # Match `request NN: <hex>` without a =~ regex (portable across shells): strip the
  # `request NN: ` prefix; a line that does not start that way leaves $hash == $line
  # and is skipped by the case guard below.
  case "$line" in
    "request "[0-9][0-9]": "*) ;;
    *) continue ;;
  esac
  LINE_NO=$((LINE_NO + 1))
  hash="${line#*: }"
  if [[ -z "$FIRST_HASH" ]]; then
    FIRST_HASH="$hash"
  elif [[ "$hash" != "$FIRST_HASH" ]]; then
    fail "messages[0] mutated at request ${LINE_NO} -- diff: ${FIRST_HASH} vs ${hash}"
  fi
done <<< "$OUT"

echo "ok (cache invariant gate): ${EXPECTED_REQUESTS} identical messages[0] request hashes (${FIRST_HASH})"
