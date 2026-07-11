---
status: testing
phase: 37E-composer-model-reasoning-effort-selector-inserted
source: [37E-VERIFICATION.md]
started: 2026-07-11T06:07:47Z
updated: 2026-07-11T06:07:47Z
---

## Current Test

number: 1
name: Graduated-effort output fidelity on a real backend (D-09)
expected: |
  OFF vs ON reliably differs on both backends. The llama.cpp local budgets scale
  monotonically (low 512 < mid 2048 < high 8192 < extra 16384 < max unlimited) on the
  spike-095 pinned local model. On DeepSeek-V4-Flash, low..max may legitimately collapse
  to on/off — that is the DOCUMENTED honest-fidelity caveat (D-09), not a defect.
awaiting: user response

## Tests

### 1. Graduated-effort output fidelity on a real backend (D-09)
expected: |
  Run `scripts/deepseek_reasoning_probe.py` against OpenRouter/DeepSeek-V4-Flash (cloud
  on/off check) AND a `thinking_budget_tokens` sweep against the pinned `gemma-4-E2B-it-qat`
  spike-095 local llama-server (launched with `--jinja`, WITHOUT `--reasoning-budget`).
  Expect: OFF vs ON reliably differs on both backends; the llama.cpp local budgets scale
  monotonically (low<mid<high<extra<max). DeepSeek-V4-Flash collapsing low..max to on/off
  is the documented D-09 caveat, not a bug — but re-confirm against the live model.
why_human: |
  37E-VALIDATION.md "Manual-Only Verifications" scopes this OUT of CI ("CI must not depend
  on a live model; assert only on/off in CI"). No automated test asserts real reasoning-token
  gradation against a live backend.
result: [pending]

### 2. Live capability fetch against real OpenRouter /models
expected: |
  With a real `OPENROUTER_API_KEY` configured and `aura serve` running, hit
  `GET /api/composer/reasoning-capabilities` and confirm the response `levels` array matches
  the operator's configured `AURA_LLM_MODEL`'s actual advertised `reasoning.supported_efforts`
  from the LIVE OpenRouter API (not the 2026-07-10 fixture snapshot). Then open the Composer
  and confirm the selector renders exactly that dynamic subset (degrading to {auto,off} only
  if the live fetch fails).
why_human: |
  37E-VALIDATION.md flags this as external-dependency / CI-uses-fixtures-only. Every automated
  test exercises parse/cache/UI logic against captured fixtures; none proves the real OpenRouter
  /models shape still matches, or that the boot-warm fetch succeeds against the live network.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
