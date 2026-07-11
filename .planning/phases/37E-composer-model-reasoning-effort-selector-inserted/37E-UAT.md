---
status: resolved
phase: 37E-composer-model-reasoning-effort-selector-inserted
source: [37E-VERIFICATION.md]
started: 2026-07-11T06:07:47Z
updated: 2026-07-11
---

## Current Test

number: 1
name: Graduated-effort output fidelity on a real backend (D-09)
expected: |
  OFF vs ON reliably differs on both backends; llama.cpp budgets scale monotonically.
awaiting: none (closed via override — deferred live-model spot-check)

## Tests

### 1. Graduated-effort output fidelity on a real backend (D-09)
expected: |
  Run scripts/deepseek_reasoning_probe.py against OpenRouter/DeepSeek-V4-Flash + a
  thinking_budget_tokens sweep against the pinned gemma-4-E2B-it-qat spike-095 local
  llama-server. OFF vs ON differs on both; local budgets scale monotonically; DeepSeek
  collapsing low..max to on/off is the documented D-09 caveat.
result: skipped — DEFERRED via operator override (2026-07-11). Explicitly Manual-Only / out-of-CI per 37E-VALIDATION.md; needs live chat models not available in this environment. Non-CI-blocking. Precedent: Phase 37 force-close with live tiers deferred.

### 2. Live capability fetch against real OpenRouter /models
expected: |
  With a real OPENROUTER_API_KEY + aura serve, GET /api/composer/reasoning-capabilities
  returns the live model's advertised levels (not the fixture snapshot) and the Composer
  selector renders exactly that dynamic subset.
result: skipped — DEFERRED via operator override (2026-07-11). External-dependency / CI-uses-fixtures-only per 37E-VALIDATION.md; needs a live OPENROUTER_API_KEY + network. Non-CI-blocking.

## Summary

total: 2
passed: 0
issues: 0
pending: 0
skipped: 2
blocked: 0

## Gaps

None CI-blocking. The 2 deferred items are live-model spot-checks (documented Manual-Only in 37E-VALIDATION.md), to be run by the operator against live DeepSeek/OpenRouter + a pinned local llama-server. All 10/10 automatable must-haves passed.
