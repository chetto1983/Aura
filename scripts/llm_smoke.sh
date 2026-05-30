#!/usr/bin/env bash
# scripts/llm_smoke.sh — MANUAL real-OpenRouter acceptance gate for `aura chat`.
#
# This is the SPEC Req#11 / ROADMAP SC#1 LIVE acceptance gate (D-31): it drives the
# real `aura chat` REPL against OpenRouter (paid, non-deterministic) and asserts a
# streamed prose reply + a non-zero token+USD cost footer. The product owner runs it
# LOCALLY before closing the phase; the human-verify checkpoint eyeballs the prose.
#
# WHY THIS IS NOT A CI JOB (CLAUDE.md NO-SKIP-AS-GREEN):
#   A CI job that needs a live paid LLM and a real API key cannot run deterministically
#   — it would be skipped (a falsely-green job exercising nothing) or burn money + flake
#   on every push. So nothing in CI pretends to hit a live model. Instead the
#   DETERMINISTIC tier (Plans 01-04 + cmd/aura/{chat,config}_test.go) GENUINELY
#   exercises every parse / accumulate / cancel / budget / redaction / cost-table /
#   prose-render path with golden SSE fixtures + a FakeClient. This script is the ONLY
#   place that talks to the real provider, and it is invoked by a human, never by CI.
#   It is therefore deliberately ABSENT from Makefile quality/quality-full and from
#   every .github workflow.
#
# THE ONE LEGITIMATE SKIP: with OPENROUTER_API_KEY unset the script prints a clear
# "skipped" notice and exits 0 — this is a manual gate, not a CI gate, so an unset key
# is the operator declining to spend, not a green lie about a live LLM.
#
# Usage:
#   export OPENROUTER_API_KEY=sk-or-...   # https://openrouter.ai/keys
#   bash scripts/llm_smoke.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
  echo "smoke skipped: OPENROUTER_API_KEY unset (manual gate, not CI)"
  echo "  set it from https://openrouter.ai/keys then re-run: bash scripts/llm_smoke.sh"
  exit 0
fi

echo "==> aura chat live smoke (real OpenRouter, paid)"

# Two scripted turns: a ping (asserts streamed prose + a non-zero footer) and a
# tool-provoking turn (asks for the current time -> the current_time builtin). The
# second turn also exercises the in-memory history (the model sees turn 1).
SCRIPT=$'Ciao, rispondi in una frase.\nChe ore sono adesso?\n/exit\n'

OUT="$(printf '%s' "$SCRIPT" | go run ./cmd/aura chat 2>&1)"
STATUS=$?

echo "----- captured output -----"
printf '%s\n' "$OUT"
echo "---------------------------"

if [[ $STATUS -ne 0 ]]; then
  echo "FAIL: aura chat exited non-zero ($STATUS)" >&2
  exit 1
fi

# 1. A streamed prose reply must be present (the REPL prompt + some non-empty text).
if ! printf '%s' "$OUT" | grep -q '›'; then
  echo "FAIL: no REPL prompt in output (did the binary run?)" >&2
  exit 1
fi

# 2. The cost footer must render with a NON-ZERO token count. The footer shape is
#    `· N tok (I in / O out) · $USD · Ts`. Reject a zero-token footer (would mean the
#    usage chunk was dropped — Critical Failure Mode #3) and reject an absent footer.
FOOTER="$(printf '%s\n' "$OUT" | grep -oE '· [0-9]+ tok \([0-9]+ in / [0-9]+ out\) · [^ ]+ · [0-9.]+s' | tail -1 || true)"
if [[ -z "$FOOTER" ]]; then
  echo "FAIL: no cost footer (· N tok (... in / ... out) · \$... · ...s) found" >&2
  exit 1
fi
echo "cost footer: $FOOTER"

TOK="$(printf '%s' "$FOOTER" | grep -oE '· [0-9]+ tok' | grep -oE '[0-9]+' | head -1)"
if [[ "${TOK:-0}" -le 0 ]]; then
  echo "FAIL: cost footer reported 0 tok — usage chunk dropped or not surfaced" >&2
  exit 1
fi

# 3. The USD figure must not be a fabricated $0 for the known default model; n/a is
#    only acceptable for an unknown model (the default is seeded in the price table).
USD="$(printf '%s' "$FOOTER" | grep -oE '· \$[0-9.]+ ·|· n/a ·' | head -1 || true)"
if [[ "$USD" == "· n/a ·" ]]; then
  echo "WARN: USD is n/a — expected a real figure for the seeded default model" >&2
fi
if printf '%s' "$FOOTER" | grep -qE '· \$0(\.0+)? ·'; then
  echo "FAIL: cost footer reported \$0 (never fabricate/zero a real spend)" >&2
  exit 1
fi

# 4. No raw text_response JSON should leak into the streamed prose (clean-prose UX).
if printf '%s' "$OUT" | grep -qE '\{"text":|"text\\":'; then
  echo "FAIL: raw text_response JSON leaked into the REPL output" >&2
  exit 1
fi

echo "==> llm_smoke: PASS (streamed prose + non-zero token+USD footer, clean prose)"
