---
status: complete
phase: 06-kv-cache-builder
source: [06-VERIFICATION.md]
started: 2026-06-02T09:48:55Z
updated: 2026-06-02T10:20:00Z
---

## Current Test

[resolved — converted from manual operator UAT to an AUTONOMOUS real-LLM E2E test]

The two items below were originally deferred as human/operator UAT (live DeepSeek-V4
Flash session required). They are now covered by an autonomous live E2E test that
drives the REAL agent.LlmAgent over the REAL openai_compat client against
DeepSeek-V4 Flash and asserts the criteria programmatically — no human-in-the-loop.

- Test: `internal/eval/harness_kvcache_e2e_test.go` (`TestKVCacheWarmingE2E`, build tag `live_e2e`)
- Reproduce: `set -a; . ./.env; set +a; go test -tags live_e2e -run TestKVCacheWarmingE2E -timeout 600s -v ./internal/eval/`
- Gating: PAID gate behind the `live_e2e` tag; with `OPENROUTER_API_KEY` unset it t.Skips (local only). With the key present it runs and ASSERTS (no babysitting). SC#4's 80% is a hard gate — a non-caching provider route fails the build.

## Tests

### 1. Cache warming on DeepSeek-V4 Flash (SC#2)
expected: A real multi-turn session shows `usage.cached_tokens` non-decreasing once the prefix warms and ending well above the cold turn-1 value (the stable `messages[0]` prefix warming the provider cache).
result: passed — autonomous E2E run 2026-06-02 observed cached tokens 0/0/0/1152/1536/1792 across 6 turns (cold start through turn 3, then monotonic rise). Asserted by `TestKVCacheWarmingE2E`.

### 2. Cache hit rate >= 80% (SC#4)
expected: After a typical warmed session the cache hit rate (cached/prompt) reaches the PRD >= 80% target.
result: passed — autonomous E2E run 2026-06-02 observed per-turn hit rate climbing 74.7% -> 82.2% -> 91.0%, peak 91.0% >= 80% target. Asserted by `TestKVCacheWarmingE2E` (hard gate `hitRateTarget = 0.80`).

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
