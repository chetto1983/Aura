---
status: partial
phase: 06-kv-cache-builder
source: [06-VERIFICATION.md]
started: 2026-06-02T09:48:55Z
updated: 2026-06-02T09:48:55Z
---

## Current Test

[awaiting human testing — requires a live DeepSeek-V4 Flash session via OPENROUTER_API_KEY]

## Tests

### 1. Cache warming on DeepSeek-V4 Flash (SC#2)
expected: Run `aura chat send "hello"` 3 times in sequence on DeepSeek-V4 Flash; `usage.prompt_cache_hit_tokens` rises monotonically from turn 2 onward (the stable `messages[0]` prefix warms the provider cache). Supporting code is verified wired: `internal/llm/openai_compat/usage.go` parses `prompt_tokens_details.cached_tokens`; `internal/runner/runner_persist.go` persists it to `aura.cache_metrics`.
result: [pending]

### 2. Cache hit rate >= 80% (SC#4)
expected: After a typical session, `aura cache-stats --since=1h` reports a cache hit rate >= 80% on DeepSeek-V4 Flash (PRD performance target). `cache-stats` reads `aura.cache_metrics` via parameterized sqlc queries (verified) — this criterion validates the live provider behavior, not the read path.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
