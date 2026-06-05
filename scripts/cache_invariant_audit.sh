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
# Phase 11 (D-06/D-07) extends the audit to THREE byte-stable streams over the same
# 20-turn replay with a FIXED skill set loaded:
#   - `request NN:`   — messages[0] (the system prompt; the CAP-04 invariant)
#   - `messages1 NN:` — messages[1] (the always-on skill block; D-07)
#   - `skillman NN:`  — the non-deferred skill tool's manifest-in-Description (D-06)
# A turn-stable runner keeps all three byte-identical across every turn. The wrapper
# independently counts + diffs each stream; the Go subcommand asserts the same.
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
fail() {
  echo "FAIL (cache invariant gate): $1" >&2
  echo "---- cache-audit stdout ----" >&2
  printf '%s\n' "$OUT" >&2
  echo "---- cache-audit stderr ----" >&2
  printf '%s\n' "$ERR" >&2
  exit 1
}

# The subcommand exited non-zero: forward its exit code + the explicit
# `messages[0] mutated at request N` / `messages[1] ... mutated` / `skill manifest ...
# mutated` (exit 1) or fixture-corrupt (exit 2) wording.
if [[ "$code" -ne 0 ]]; then
  echo "FAIL (cache invariant gate): aura cache-audit exited ${code}" >&2
  printf '%s\n' "$ERR" >&2
  printf '%s\n' "$OUT" >&2
  exit "$code"
fi

# assert_stream <line-prefix> <human-label>: independently counts the EXPECTED_REQUESTS
# `<prefix> NN: <hex>` lines and asserts every hash in the stream is byte-identical
# (belt-and-suspenders over the Go assertion). NO-SKIP-AS-GREEN: an empty/short stream
# is a HARD failure. Prints the stable hash on success.
#
# Implemented WITHOUT a shell function + WITHOUT `$(printf | grep)` command
# substitution: a `local var="$(cmd)"` inside a function forks a subshell that aborts
# with exit 127 under the w64devkit MSYS bash this gate must also run on locally. A
# single pass over the captured lines (case-guarded, no per-stream subshell) is portable
# across the CI Linux bash AND the local w64devkit bash.
count0=0; count1=0; countMan=0
first0=""; first1=""; firstMan=""
no0=0; no1=0; noMan=0
while IFS= read -r line; do
  case "$line" in
    "request "[0-9][0-9]": "[0-9a-f]*)
      no0=$((no0 + 1)); count0=$((count0 + 1)); hash="${line#*: }"
      if [[ -z "$first0" ]]; then first0="$hash"
      elif [[ "$hash" != "$first0" ]]; then fail "messages[0] mutated at request ${no0} -- diff: ${first0} vs ${hash}"; fi
      ;;
    "messages1 "[0-9][0-9]": "[0-9a-f]*)
      no1=$((no1 + 1)); count1=$((count1 + 1)); hash="${line#*: }"
      if [[ -z "$first1" ]]; then first1="$hash"
      elif [[ "$hash" != "$first1" ]]; then fail "messages[1] always-block mutated at request ${no1} -- diff: ${first1} vs ${hash}"; fi
      ;;
    "skillman "[0-9][0-9]": "[0-9a-f]*)
      noMan=$((noMan + 1)); countMan=$((countMan + 1)); hash="${line#*: }"
      if [[ -z "$firstMan" ]]; then firstMan="$hash"
      elif [[ "$hash" != "$firstMan" ]]; then fail "skill manifest-in-Description mutated at request ${noMan} -- diff: ${firstMan} vs ${hash}"; fi
      ;;
  esac
done <<< "$OUT"

# NO-SKIP-AS-GREEN: each stream MUST have emitted exactly EXPECTED_REQUESTS lines.
[[ "$count0"   -eq "$EXPECTED_REQUESTS" ]] || fail "expected ${EXPECTED_REQUESTS} 'request NN: <hex>' lines (messages[0]), got ${count0} (empty/short output is never a silent pass)"
[[ "$count1"   -eq "$EXPECTED_REQUESTS" ]] || fail "expected ${EXPECTED_REQUESTS} 'messages1 NN: <hex>' lines (messages[1] always-block), got ${count1} (empty/short output is never a silent pass)"
[[ "$countMan" -eq "$EXPECTED_REQUESTS" ]] || fail "expected ${EXPECTED_REQUESTS} 'skillman NN: <hex>' lines (skill manifest-in-Description), got ${countMan} (empty/short output is never a silent pass)"

echo "ok (cache invariant gate): ${EXPECTED_REQUESTS} identical messages[0] hashes (${first0})"
echo "ok (cache invariant gate): ${EXPECTED_REQUESTS} identical messages[1] always-block hashes (${first1})"
echo "ok (cache invariant gate): ${EXPECTED_REQUESTS} identical skill manifest-in-Description hashes (${firstMan})"
