---
phase: 06-kv-cache-builder
verified: 2026-06-02T18:00:00Z
status: human_needed
score: 3/5 must-haves verified (2 require live API — classified as human_needed, not failed)
overrides_applied: 0
human_verification:
  - test: "Run `aura chat send 'hello'` three times sequentially on a live DeepSeek-V4 Flash session and observe per-turn usage output"
    expected: "usage.prompt_cache_hit_tokens (CachedTokens) is 0 on turn 1 (cold), rises monotonically from turn 2 onward as the cache warms"
    why_human: "Provider-side cache warming is non-deterministic in CI. PRD OQ4 explicitly states the 80% hit-rate is measured, not CI-gated. The supporting code is wired (CachedTokens parsed in openai_compat/usage.go, aura cache-stats CLI reads the DB) but the assertion requires a live DeepSeek-V4 Flash session with a real API key."
  - test: "With Postgres up and a session that has run >= 5 turns, run `aura cache-stats --since=1h`"
    expected: "Command exits 0, prints a tabwriter table with per-turn rows and a TOTAL summary line including a hit-rate percentage. For a typical session on DeepSeek-V4 Flash the hit-rate should be >= 80%."
    why_human: "The SQL query side is integration-tested (TestCacheMetrics_WindowAndAggregate PASS 0.27s with Postgres up), but the 80% target itself is provider-dependent and not CI-gated (PRD OQ4). Confirming the pipeline end-to-end (real DeepSeek turns -> DB insert -> stats readout) requires a live session."
---

# Phase 6: KV Cache Builder Verification Report

**Phase Goal:** PromptBuilder with stable-prefix discipline. Two system messages invariant: messages[0] byte-identical turn-on-turn (system + tool manifest, alphabetically sorted); messages[1] mutable. Provider-aware cache_control injection (Anthropic ephemeral; DeepSeek auto + parse usage.prompt_cache_hit_tokens; OpenRouter prefix-only). Cross-slice CI job scripts/cache_invariant_audit.sh runs from this phase onward and gates every subsequent merge.
**Verified:** 2026-06-02T18:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #  | Truth                                                                                                                                                         | Status          | Evidence                                                                                                                                                                                                                                                       |
|----|---------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1  | A 20-turn replay via scripts/cache_invariant_audit.sh shows SHA-256(messages[0]) constant across all 20 turns (printed to stdout, asserted by the script)    | ✓ VERIFIED      | `bash scripts/cache_invariant_audit.sh` exits 0; all 20 lines print `d69144fde595...` (single identical hash). Both the bash wrapper's independent diff AND the Go subcommand's internal assertion agree.                                                      |
| 2  | [OPERATOR/LIVE] aura chat send "hello" 3x shows usage.prompt_cache_hit_tokens rising monotonically from turn 2 (cache warming)                               | ? UNCERTAIN     | Supporting code is wired: `openai_compat/usage.go` parses `prompt_tokens_details.cached_tokens` -> `CachedTokens`; `runner_persist.go` inserts it per turn; `aura cache-stats` reads it. Live DeepSeek-V4 Flash session required to observe cache warming.    |
| 3  | Generated prompt for an Anthropic-direct provider has cache_control {"type":"ephemeral"} on system+tools blocks, NOT on history                               | ✓ VERIFIED      | `go test ./internal/agent/prompt/ -run TestCacheControlSeam` PASS. `injectCacheControl` sets `req.ToolsCacheControl = "ephemeral"` under `anthropic`, is pure no-op under `openrouter`; no history message is marked. Dormant seam confirmed correct.          |
| 4  | [OPERATOR/LIVE] aura cache-stats --since=1h shows cache hit rate >= 80%                                                                                      | ? UNCERTAIN     | SQL query side VERIFIED (parameterized `sqlc.arg(since)::timestamptz`, no string concat, `time.ParseDuration` validates input). Integration tests pass with Postgres up. The 80% figure itself requires a live DeepSeek-V4 Flash session (PRD OQ4 / provider-dependent). |
| 5  | CI gate: a PR that breaks scripts/cache_invariant_audit.sh fails the build with an explicit "messages[0] mutated at <site>" error                             | ✓ VERIFIED      | `bash scripts/cache_invariant_negative_test.sh` exits 0 (PASS): Case 1 feeds a poisoned hash stream (turn 03 differs) -> gate exits 1 with "mutated". Case 2 feeds empty output -> gate exits 1 (no-skip-as-green). ci.yml has a dedicated `cache-invariant` job (Postgres-free, CI=true, no services). |

**Score:** 3/5 truths verified (2 require live API — human verification items, not failures)

### Required Artifacts

| Artifact                                          | Expected                                                      | Status     | Details                                                                                                             |
|---------------------------------------------------|---------------------------------------------------------------|------------|---------------------------------------------------------------------------------------------------------------------|
| `internal/agent/prompt/builder.go`                | PromptBuilder.Build chokepoint (D-01)                         | ✓ VERIFIED | 36 LOC. Reproduces inline construction exactly: `Messages: history`, `Tools: reg.RenderToolDefs()`, scalars from cfg. Calls `injectCacheControl` last. LlmAgent.Run confirmed routing through `a.builder.Build(...)` at llm_agent.go:137. |
| `internal/agent/prompt/hash.go`                   | PrefixHash index-set fingerprint (D-06a)                      | ✓ VERIFIED | 42 LOC. Uses `canonicaljson.Marshal` + `sha256.New`. Skips indices >= len(msgs) for forward compat. Table-driven tests cover 6 behaviors.                                                                                      |
| `internal/agent/prompt/cache_anthropic.go`        | injectCacheControl dormant seam (D-03)                        | ✓ VERIFIED | 28 LOC. Pure no-op unless `provider == "anthropic"`. Never touches history messages. Documented as dormant until Slice 13.                                                                                                       |
| `internal/llm/client.go`                          | Request.ToolsCacheControl field + corrected comment (D-03a)   | ✓ VERIFIED | Field `ToolsCacheControl string` present. Comment correctly states injection decision lives in prompt builder; wire layer serializes but never decides.                                                                          |
| `internal/db/migrations/0007_cache_metrics.up.sql`| cache_metrics DDL + ts index + grants                         | ✓ VERIFIED | Exact DDL: `numeric(10,4)` cost, `timestamptz DEFAULT now()`, FK ON DELETE CASCADE. CONCURRENTLY absent (Pitfall 4). `GRANT SELECT, INSERT ON aura.cache_metrics TO aura_app` — append-only (T-06-04).                          |
| `internal/db/queries/cache_metrics.sql`           | InsertCacheMetric + ListCacheMetricsSince + AggregateCacheMetricsSince | ✓ VERIFIED | All three queries present. Uses `sqlc.arg(since)::timestamptz` (parameterized). No string concatenation.                                                                                                         |
| `internal/cachemetrics/store.go`                  | sqlc-backed Store with Insert + window reads                  | ✓ VERIFIED | 102 LOC. `New(pool)`, `Insert`, `ListSince`, `AggregateSince` all present and substantive. Calls generated sqlc queries with pgtype boundary conversion.                                                                         |
| `internal/runner/interfaces.go`                   | CacheMetricStore narrow interface                             | ✓ VERIFIED | `type CacheMetricStore interface { Insert(ctx, sqlc.InsertCacheMetricParams) error }` — Insert-only surface, mirrors PauseStore pattern.                                                                                         |
| `cmd/aura/cache_audit.go`                         | runCacheAudit — 20-turn replay + PrefixHash + exit codes      | ✓ VERIFIED | 252 LOC. Drives real `runner.Turn -> LlmAgent.Run -> PromptBuilder.Build` path (not synthetic). Reads `client.LastRequest().Messages[0]`. Exit codes 0/1/2 with SC#5 wording on mutation.                                       |
| `cmd/aura/cache_stats.go`                         | runCacheStats — --since window query + tabwriter              | ✓ VERIFIED | 119 LOC. `time.ParseDuration` validation (exits 64 on bogus input). Windowed read via AggregateSince/ListSince. `total_prompt==0` guard for hit-rate ratio.                                                                     |
| `cmd/aura/cachefakes.go`                          | Importable no-op CacheMetricStore + in-memory Stores          | ✓ VERIFIED | 281 LOC. Non-test file (no `_test.go` suffix). Implements all four narrow runner Store interfaces. `memCacheMetricStore.Insert` is a no-op.                                                                                      |
| `scripts/fixtures/cache_invariant/turn-01.json` (×20) | Deterministic replay fixtures including tool-call turns   | ✓ VERIFIED | All 20 fixture files present. turn-05 scripts `current_time` tool call, turn-12 scripts `tool_search` tool call (both followed by text_response). Deterministic — no clock/UUID in fixture text.                                |
| `scripts/cache_invariant_audit.sh`                | Thin wrapper driving aura cache-audit + hash diff + explicit failure | ✓ VERIFIED | 95 LOC. `set -euo pipefail`. Counts exactly 20 `turn NN:` hash lines with `grep -c . || true` guard. Independent hash diff (belt-and-suspenders over Go assertion). Exits 1 with "messages[0] mutated at turn N" on drift.   |
| `scripts/cache_invariant_negative_test.sh`        | SC#5 negative proof                                           | ✓ VERIFIED | 83 LOC. Case 1: poisoned stream (turn 03 differs) -> gate exits non-zero with "mutated". Case 2: empty output -> gate exits non-zero (no-skip-as-green). Both cases pass.                                                       |
| `.github/workflows/ci.yml`                        | cache-invariant CI job (Postgres-free, gates every merge)     | ✓ VERIFIED | Dedicated `cache-invariant` job with `runs-on: ubuntu-latest`, `env.CI: "true"`, no `services` key (Postgres-free). Steps: `cache invariant gate` (wrapper) + `cache invariant gate (negative)` (SC#5 proof).                  |

### Key Link Verification

| From                              | To                                          | Via                                             | Status     | Details                                                                                          |
|-----------------------------------|---------------------------------------------|-------------------------------------------------|------------|--------------------------------------------------------------------------------------------------|
| `internal/agent/llm_agent.go:137` | `internal/agent/prompt.PromptBuilder.Build` | `a.builder.Build(a.history, a.registry, ...)`   | ✓ WIRED    | `grep -n "prompt\." llm_agent.go` confirms builder field + constructor + Build call at line 137. Inline `llm.Request{}` removed. |
| `internal/agent/prompt/hash.go`   | `internal/canonicaljson.Marshal`            | deterministic sha256 serialization              | ✓ WIRED    | `import canonicaljson` confirmed; `canonicaljson.Marshal(msgs[i])` called per index.             |
| `cmd/aura/cache_audit.go`         | `agenttest.FakeClient.Requests` / `LastRequest()` | reads `client.LastRequest()` per turn     | ✓ WIRED    | `reqs = append(reqs, client.LastRequest())` after each `Runner.Turn` call. Not synthetic.        |
| `cmd/aura/cache_audit.go`         | `internal/agent/prompt.PrefixHash`          | `hashMessages0 -> prompt.PrefixHash(req.Messages, []int{0})` | ✓ WIRED | Function `hashMessages0` explicitly calls `prompt.PrefixHash` with index set `{0}`.             |
| `cmd/aura/main.go`                | `runCacheStats` / `runCacheAudit`           | two switch cases                                | ✓ WIRED    | `case "cache-stats":` and `case "cache-audit":` (hidden) both present at main.go:52-54.         |
| `scripts/cache_invariant_audit.sh`| `cmd/aura cache-audit`                      | `go run ./cmd/aura cache-audit`                 | ✓ WIRED    | `AUDIT_CMD="${AURA_CACHE_AUDIT_CMD:-go run ./cmd/aura cache-audit}"` — the real subcommand is default. |
| `.github/workflows/ci.yml`        | `scripts/cache_invariant_audit.sh`          | named CI step invoking the wrapper              | ✓ WIRED    | Step `cache invariant gate` runs `bash scripts/cache_invariant_audit.sh` in the `cache-invariant` job. |
| `internal/runner/runner_persist.go:persistAssistantAnswer` | `cachemetrics.Store.Insert` | sibling INSERT via `r.persistCacheMetric(ctx, convID, seq, u, cost)` | ✓ WIRED | One metric row written per completed assistant turn using already-computed `u` (llm.Usage) + `cost`. Nil cacheMetrics fails loud (no silent skip). |
| `internal/llm/openai_compat/usage.go` | `CachedTokens` field | `PromptTokensDetails.CachedTokens` -> `llm.Usage.CachedTokens` | ✓ WIRED | `toUsage()` maps `w.PromptTokensDetails.CachedTokens` -> `CachedTokens`. DeepSeek `prompt_cache_hit_tokens` parsing exists and flows to runner persist seam. |

### Data-Flow Trace (Level 4)

| Artifact                           | Data Variable     | Source                              | Produces Real Data       | Status      |
|------------------------------------|-------------------|-------------------------------------|--------------------------|-------------|
| `cmd/aura/cache_stats.go`          | `rows`, `agg`     | `cachemetrics.Store.ListSince/AggregateSince` -> `sqlc` -> `aura.cache_metrics` | Yes — real SQL window query on DB rows inserted per turn | ✓ FLOWING (SQL path; requires Postgres up for live run) |
| `internal/agent/prompt/builder.go` | `Messages`        | `history []llm.Message` passed from caller | Yes — caller's real in-memory conversation history | ✓ FLOWING |
| `cmd/aura/cache_audit.go`          | `reqs`            | `client.LastRequest()` after real `runner.Turn` | Yes — FakeClient captures real Build() output per turn | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior                                                 | Command                                          | Result                                                    | Status  |
|----------------------------------------------------------|--------------------------------------------------|-----------------------------------------------------------|---------|
| 20-turn replay exits 0 with 20 identical SHA-256 hashes  | `bash scripts/cache_invariant_audit.sh`          | `ok (cache invariant gate): 20 identical messages[0] hashes (d69144...)`, exit 0 | ✓ PASS  |
| Negative test proves gate is not silently green          | `bash scripts/cache_invariant_negative_test.sh`  | `PASS (the gate is NOT silently green)`, exit 0           | ✓ PASS  |
| `go build ./...` succeeds (no import cycle)              | `go build ./...`                                 | BUILD OK — proves D-01a (internal/agent/prompt importing tools+llm is cycle-free) | ✓ PASS  |
| `go vet ./...` clean                                     | `go vet ./...`                                   | VET OK                                                    | ✓ PASS  |
| TestBuildPrefixStable + TestCacheControlSeam + TestPrefixHash | `go test -count=1 ./internal/agent/prompt/ -run 'TestPrefixHash\|TestBuildPrefixStable\|TestCacheControlSeam'` | All subtests PASS | ✓ PASS  |
| Cache CLI tests (flag parsing, exit codes 0/1/2)         | `go test -count=1 ./cmd/aura/ -run TestCache`    | TestCacheStats_ParseSince, TestCacheAudit_AllEqual_Exit0, TestCacheAudit_Mutation_Exit1, TestCacheAudit_CorruptFixture_Exit2, TestCacheAudit_FixturesIncludeToolCalls — all PASS | ✓ PASS  |
| Race detector clean on key packages                      | `go test -count=1 -race ./internal/agent/prompt/ ./cmd/aura/` | Both packages PASS under race detector | ✓ PASS  |
| `aura cache-audit` prints 20 identical hash lines (binary) | `go run ./cmd/aura cache-audit`                 | 20 lines, all `d69144fde595...`, exit 0 | ✓ PASS  |
| `cache-audit` hidden from usage string                   | `go run ./cmd/aura 2>&1 \| grep cache-audit`     | No output (correctly hidden)                              | ✓ PASS  |

### Probe Execution

| Probe                                    | Command                                          | Result              | Status |
|------------------------------------------|--------------------------------------------------|---------------------|--------|
| `scripts/cache_invariant_audit.sh`       | `bash scripts/cache_invariant_audit.sh`          | exit 0, 20 identical hashes | PASS   |
| `scripts/cache_invariant_negative_test.sh` | `bash scripts/cache_invariant_negative_test.sh` | exit 0 (SC#5 proven) | PASS   |

### Requirements Coverage

| Requirement | Source Plan | Description                                                                 | Status      | Evidence                                                                                                                      |
|-------------|-------------|-----------------------------------------------------------------------------|-------------|-------------------------------------------------------------------------------------------------------------------------------|
| CAP-04      | 06-01 through 06-05 | KV cache builder stable-prefix + provider-aware. CI job scripts/cache_invariant_audit.sh asserts SHA-256(messages[0]) constant. 80% cache hit target on DeepSeek-V4. | SATISFIED (automated gates) + HUMAN NEEDED (live API criteria SC#2, SC#4 80% figure) | All automated SCs (SC#1, SC#3, SC#5) verified. SC#2 and the 80% target in SC#4 require live DeepSeek-V4 Flash session. |

### Anti-Patterns Found

| File                                         | Line | Pattern                                                        | Severity    | Impact                                                                                                    |
|----------------------------------------------|------|----------------------------------------------------------------|-------------|-----------------------------------------------------------------------------------------------------------|
| `cmd/aura/main.go` usage() string            | 68   | `cache-stats` missing from usage() advertised string           | ⚠️ Warning  | Operator-facing subcommand not discoverable via `aura` help. Caught by code review IN-03. No functional impact on phase goal. |
| `internal/cachemetrics/store_helpers.go:60`  | 60   | `numericFromFloat` no overflow guard for costs >= 1_000_000   | ⚠️ Warning  | Latent: costs are tiny today. Identified by code review WR-01. DB insert will error loudly if triggered. Not a silent failure. |
| `internal/cachemetrics/store_helpers.go:86`  | 86   | `anyInt64`/`anyNumericFloat` swallow parse errors, return 0 on unknown driver shape | ⚠️ Warning | Silent 0 for unmodeled pgx/PG aggregate return type. WR-02. Could misreport "0 cost, 0 tokens" — correctness regression. |
| `internal/runner/runner_persist.go:81`       | 81   | Non-atomic AppendTurn + persistCacheMetric (two independent writes) | ⚠️ Warning | Turn retry could duplicate assistant turn while metric has no row. WR-03. No transaction boundary. Consistency contract unspecified. |
| `internal/db/migrations/0007_cache_metrics.up.sql:23-24` | 23 | Index comment says "DESC serves --since reads" but ListCacheMetricsSince queries ORDER BY ASC | ℹ️ Info | Mismatch between comment and query direction. WR-04. Performance/documentation gap, not a correctness defect. |
| `cmd/aura/cache_audit.go:68-73`             | 70   | `repoRoot()` error silently discarded, falls back to relative path | ℹ️ Info   | Operator sees "fixture corrupt" instead of "could not locate repo root". WR-05. CI gate still fails loudly via exit 2. |

No `TBD`, `FIXME`, or `XXX` markers found in any phase-modified file. No debt-marker blockers.

### Human Verification Required

**Status:** SC#2 and the 80% hit-rate portion of SC#4 require a live DeepSeek-V4 Flash session. All automated checks passed. The two human items correspond to live-API-only criteria that the phase plan itself marks as OPERATOR/LIVE.

#### 1. Cache Warming — CachedTokens rises from turn 2 (SC#2)

**Test:** With a running Aura instance configured against DeepSeek-V4 Flash via OpenRouter, run `aura chat send "hello"` three times in the same conversation. Observe the per-turn usage output after each turn (or run `aura cache-stats --since=5m` after the session).

**Expected:** Turn 1 shows `cached_tokens = 0` (cache cold). Turn 2 shows `cached_tokens > 0` (cache warmed by turn 1's stable prefix). Turn 3 shows `cached_tokens >= turn 2's value` (monotonically rising as history grows but prefix stays stable).

**Why human:** Provider-side cache warming requires a real OpenRouter + DeepSeek-V4 Flash session with OPENROUTER_API_KEY set. The mechanism is: `PromptBuilder.Build` produces a byte-identical `messages[0]` every turn -> OpenRouter caches the prefix -> reports `prompt_tokens_details.cached_tokens` in the SSE usage chunk -> `openai_compat/usage.go:toUsage()` maps it to `CachedTokens` -> `runner_persist.go:persistAssistantAnswer` inserts it to `aura.cache_metrics`. The full pipeline is wired and tested with fakes; live confirmation requires the actual provider.

#### 2. End-to-End Cache Stats — Hit Rate >= 80% on a Typical Session (SC#4 80% figure)

**Test:** After running 5+ turns in a conversation with DeepSeek-V4 Flash, run `aura cache-stats --since=1h` and observe the TOTAL line's hit-rate.

**Expected:** Command exits 0 and prints a tabwriter table with per-turn rows plus a TOTAL summary line. Hit rate (cached/prompt tokens) should be >= 80% for a typical session where `messages[0]` is large relative to the new tokens per turn. The 80% is a target, not a hard gate (PRD OQ4 explicitly defers rate-gating to operator monitoring).

**Why human:** The SQL query pipeline is integration-tested (`TestCacheMetrics_WindowAndAggregate` PASS with Postgres up, 0.27s). The `aura cache-stats --since=bogus` path is unit-tested (exits 64). Confirming the 80% figure requires a live DeepSeek-V4 Flash session where cache warming actually occurs and the token ratio is visible.

---

## Gaps Summary

No gaps blocking phase goal achievement. All three automated success criteria (SC#1, SC#3, SC#5) are verified by actual execution. The two live-API criteria (SC#2, SC#4 80% figure) are correctly deferred to human verification per the phase plan's own OPERATOR/LIVE annotations and the PRD's OQ4 decision that hit-rate is measured, not CI-gated.

The code review (06-REVIEW.md) identified 5 warnings (WR-01 through WR-05) and 6 info items (IN-01 through IN-06) — none are blockers for the phase goal. WR-02 (silent zeros from `anyInt64`/`anyNumericFloat`) and WR-03 (non-atomic turn + metric write) are the most significant robustness gaps and should be addressed before the cost-accounting surface is relied upon in production. They are not VERIFICATION blockers.

---

_Verified: 2026-06-02T18:00:00Z_
_Verifier: Claude (gsd-verifier)_
