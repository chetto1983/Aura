---
phase: 06-kv-cache-builder
reviewed: 2026-06-02T00:00:00Z
depth: standard
files_reviewed: 31
files_reviewed_list:
  - .github/workflows/ci.yml
  - cmd/aura/cache.go
  - cmd/aura/cache_audit.go
  - cmd/aura/cache_stats.go
  - cmd/aura/cache_test.go
  - cmd/aura/cachefakes.go
  - cmd/aura/chat.go
  - cmd/aura/chat_test.go
  - cmd/aura/cmdfakes_test.go
  - cmd/aura/main.go
  - internal/agent/llm_agent.go
  - internal/agent/prompt/builder.go
  - internal/agent/prompt/builder_test.go
  - internal/agent/prompt/cache_anthropic.go
  - internal/agent/prompt/hash.go
  - internal/agent/prompt/hash_test.go
  - internal/cachemetrics/store.go
  - internal/cachemetrics/store_helpers.go
  - internal/cachemetrics/store_helpers_test.go
  - internal/db/cache_metrics_integration_test.go
  - internal/db/db_test.go
  - internal/db/migrations/0007_cache_metrics.down.sql
  - internal/db/migrations/0007_cache_metrics.up.sql
  - internal/db/queries/cache_metrics.sql
  - internal/llm/client.go
  - internal/runner/fakes_test.go
  - internal/runner/interfaces.go
  - internal/runner/runner.go
  - internal/runner/runner_cachemetric_test.go
  - internal/runner/runner_persist.go
  - scripts/cache_invariant_audit.sh
  - scripts/cache_invariant_negative_test.sh
findings:
  critical: 0
  warning: 5
  info: 6
  total: 11
status: issues_found
---

# Phase 06: Code Review Report

**Reviewed:** 2026-06-02
**Depth:** standard
**Files Reviewed:** 31
**Status:** issues_found

## Summary

Phase 06 (KV-cache builder) is a tightly-scoped, well-disciplined slice. The two security-critical surfaces the brief flagged hold up under adversarial reading:

1. **`messages[0]` byte-identity invariant** — `PromptBuilder.Build` passes the supplied history through unmutated (`Messages: history`), never re-prepends or per-message-tags, and the Anthropic seam only touches the tools-side `ToolsCacheControl` scalar. The runtime-faithful `aura cache-audit` gate replays the *real* `Runner.Turn -> LlmAgent.Run -> Build` path (not a synthetic `Build()` shortcut) and is double-checked by an independent bash hash-diff. The invariant is genuinely enforced.
2. **SQL injection** — every `cache_metrics` query is fully parameterized (`$1..$5`, `$1::timestamptz` via `sqlc.arg`). No string concatenation anywhere. The `--since` window is `time.ParseDuration`-validated before any DB work and bound through `pgtype.Timestamptz`. Clean.
3. **No-skip-as-green** — the negative test (`cache_invariant_negative_test.sh`) actively proves the gate fails on both a poisoned hash stream AND an empty run; the integration tiers `t.Fatal` under `$CI` via `envOrSkip`. The discipline is real, not cosmetic.

No BLOCKER-tier defects were found. The findings below are robustness, observability, and consistency gaps — the strongest (WR-01, WR-02) concern silent data corruption / row duplication under failure or float-precision edge cases, and should be addressed before this surface is trusted for cost accounting.

## Warnings

### WR-01: `numericFromFloat` silently overflows / mis-rounds costs exceeding the `numeric(10,4)` integer range

**File:** `internal/cachemetrics/store_helpers.go:60-68`
**Issue:** `numericFromFloat` computes `scaled := f * 1e4` and casts to `int64(scaled)`. The column is `numeric(10, 4)` — at most 6 integer digits (max ~999999.9999). The Go side imposes no clamp, so a cost ≥ `1_000_000.0` produces a mantissa the DB rejects on INSERT (overflow → INSERT error surfaces, which is at least loud). More insidiously, the float→int path is lossy at the boundary: `f * 1e4` for a value like `0.0001 * very_large` accumulates IEEE-754 error before the `±0.5` round, so the "exact" claim in the comment is only true for small, well-behaved magnitudes. Costs are tiny today, so this is latent, but the function is presented as a general exact-encoder and is reused mentally from `conversations.numericFromFloat`.
**Fix:** Guard the domain and document the contract:
```go
func numericFromFloat(f float64) pgtype.Numeric {
    // cost_usd is numeric(10,4): clamp to the representable integer range so a
    // pathological cost is a loud error path, not silent truncation.
    if f > 999999.9999 || f < -999999.9999 {
        return pgtype.Numeric{Valid: false} // or return an error up the call chain
    }
    scaled := f * 1e4
    ...
}
```
At minimum add a test case at the boundary (e.g. `999999.9999`, `123.4567`) to `TestNumericFromFloat_RoundTrip`, which today only exercises values ≤ 12.3456.

### WR-02: `anyInt64` / `anyNumericFloat` swallow parse errors, silently reporting 0 for an unparseable aggregate

**File:** `internal/cachemetrics/store_helpers.go:86-128`
**Issue:** Both coercion helpers discard the error on the `string`/`[]byte` decode branches:
```go
case string:
    i, _ := strconv.ParseInt(n, 10, 64)   // err dropped
    return i
...
default:
    return 0                              // unknown driver shape -> silent 0
```
If pgx returns the `coalesce(sum(...))` aggregate in a shape none of the cases anticipate (a future pgx/PG version, a `pgtype.Int8` wrapper, etc.), `AggregateSince` reports `0` tokens / `$0.00` cost with no error. For a cost-accounting / cache-hit-rate surface that is a correctness regression presenting as "cache is working great, 0 cost" — exactly the kind of false-green this codebase elsewhere refuses. The `default: return 0` is the riskiest: it converts an unmodeled type into plausible-looking data.
**Fix:** Have the helpers return `(value, error)` (or at least log via `slog` at WARN on the `default` / parse-failure branch) and propagate up through `AggregateSince` so an unexpected decode shape fails loud rather than reporting fabricated zeros. The `default` arm in particular should never silently succeed.

### WR-03: `cache_metrics` row write is non-atomic with the assistant turn — a metric-insert failure leaves an orphaned assistant turn and re-runs duplicate it

**File:** `internal/runner/runner_persist.go:59-107`
**Issue:** `persistAssistantAnswer` does `AppendTurn` (assistant answer) and then `persistCacheMetric` as two independent, non-transactional writes:
```go
if err := r.Conv.AppendTurn(ctx, ...); err != nil { return err }
return r.persistCacheMetric(ctx, convID, seq, u, cost)   // separate write
```
If `persistCacheMetric` fails (the failure-surfaces path that `TestPersistAssistantAnswer_CacheMetricErrorSurfaces` deliberately exercises), the assistant turn is already committed but the error propagates out of `Turn`. A caller that retries the turn will see `CountTurns` already incremented, mint a new seq, and `AppendTurn` a *second* assistant answer — duplicating the visible conversation turn while the first one has no matching metric row. The metric and the turn it describes can permanently diverge.
**Fix:** Either (a) write the assistant turn and its metric in one DB transaction (the conversations + cachemetrics stores would need a shared tx seam), or (b) make the metric write best-effort-with-WARN rather than fatal (it is an observation, not load-bearing for conversation correctness), or (c) move the metric insert to a `seq`-keyed upsert (`ON CONFLICT (conversation_id, seq) DO NOTHING`) so a retry is idempotent. Document the chosen consistency contract — today it is "fail the turn but keep the half-write," which is the worst of the three.

### WR-04: `cache_metrics_ts_idx` is `DESC` but both window queries scan `ts >=` and `ORDER BY ts ASC` — the index serves the predicate, not the sort

**File:** `internal/db/migrations/0007_cache_metrics.up.sql:24` + `internal/db/queries/cache_metrics.sql:9`
**Issue:** The migration comment claims "a DESC index serves the `--since` reads," but `ListCacheMetricsSince` does `WHERE ts >= $1 ORDER BY ts ASC`. A `(ts DESC)` index can satisfy the range predicate but the planner must reverse-scan or sort for ASC output. The index direction and the query's sort direction are mismatched, and the comment asserts a benefit the query shape does not realize. (Performance is out of v1 scope, but this is flagged as a correctness-of-documentation / latent-confusion defect: the comment is wrong about what the index does.)
**Fix:** Either change the index to `(ts)` ASC to match the `ORDER BY ts ASC` reads, or correct the comment to state the index serves only the range predicate. Given both consumers (`ListSince` ASC, plus future range scans) the plain `(ts)` index is the honest choice.

### WR-05: `repoRoot()` failure in `cache-audit` silently falls back to a relative fixture dir, weakening the CI gate's failure mode

**File:** `cmd/aura/cache_audit.go:68-73`
**Issue:** `cacheAuditMain` does:
```go
dir := auditFixtureDir // "scripts/fixtures/cache_invariant" (relative)
if root, err := repoRoot(); err == nil {
    dir = filepath.Join(root, auditFixtureDir)
}
```
When `repoRoot()` errors, the error is discarded and `dir` stays the *relative* path. If the gate is ever invoked from an unexpected cwd where `go.mod` is not found, the audit reads from a relative path that may not exist → `loadFixtures` returns `exitFixture` (exit 2). That is acceptable *only* because `loadFixtures` happens to fail-loud on a missing file. But the silent swallow of the `repoRoot` error means the operator sees "fixture corrupt" instead of the true cause ("could not locate repo root"). For a CI gate whose entire value is a trustworthy failure signal, masking the real reason is a robustness gap.
**Fix:** Surface the `repoRoot` error explicitly when it occurs (it is a genuine environment problem), e.g. log it to `errOut` before falling back, or return `exitFixture` with a "could not locate go.mod from cwd" diagnostic rather than letting the relative path produce a misleading "fixture corrupt."

## Info

### IN-01: `numericScale` constant duplicated across `cachemetrics` and `conversations`

**File:** `internal/cachemetrics/store_helpers.go:24` (mirrors `internal/conversations/store_helpers.go:17`)
**Issue:** `numericScale = 4`, `numericFromFloat`, `floatFromNumeric` are near-verbatim copies of the `conversations` helpers (the comments even say "mirrors conversations.numericFromFloat"). `dupl` is configured at threshold 100 and excludes `_test.go`; these helpers are close enough to be a maintenance hazard — a fix to one (see WR-01/WR-02) must be mirrored by hand.
**Fix:** Extract the `numeric(10,4)` float↔pgtype conversion + the `anyInt64`/`anyNumericFloat` driver-shape coercion into a shared internal helper package (e.g. `internal/db/pgconv`) consumed by both domains. Per CLAUDE.md "REUSABLE CODE — never duplicate; extract a helper."

### IN-02: `anyInt`/`anyFloat` triplicated across `runner`, `cmd/aura`, and `internal/eval`

**File:** `internal/runner/runner_persist.go:237-259`, `cmd/aura/chat_render.go:171-189`, `internal/eval/capture_cot_eval.go:161-165`
**Issue:** `usageFromStateDelta` + `anyInt` + `anyFloat` are copied into three packages, each comment acknowledging the duplication ("duplicated here so the runner package does not import cmd/aura"). The StateDelta key contract (`prompt_tokens`/`completion_tokens`/`cache_hit_tokens`/`cost_usd`) is now defined in `llm_agent_events.go` and re-decoded in three places; a key rename would silently break two of them (they'd decode `0`).
**Fix:** Move `usageStateDelta` + `usageFromStateDelta` into the `agent` (or `llm`) package as a single encode/decode pair so the key names live in exactly one place and both directions are symmetric. This also closes the latent risk that the three copies drift.

### IN-03: `cmd/aura/main.go` `usage()` omits `cache-stats` (and intentionally `cache-audit`)

**File:** `cmd/aura/main.go:68-70`
**Issue:** `cache-stats` is an operator-facing, advertised command (per `cache.go`'s own doc) but is absent from the `usage()` string. `cache-audit` is correctly hidden, but `cache-stats` should be discoverable.
**Fix:** Add `cache-stats --since=<dur>` to the `usage()` line.

### IN-04: `anyInt64` `float64` branch truncates rather than rounds

**File:** `internal/cachemetrics/store_helpers.go:91-92`
**Issue:** `case float64: return int64(n)` truncates toward zero. For a token *sum* this is fine (sums of ints are exact in float64 up to 2^53), but it is an implicit assumption worth a one-line comment, since the same helper is the read path for aggregates that could in principle arrive as float from some driver.
**Fix:** Add a comment noting the truncation is safe because token sums are integral, or use a round if any non-integral float shape is ever possible.

### IN-05: `decodeFixture` `DisallowUnknownFields` is good, but `Finish` defaulting to `"stop"` hides a missing field

**File:** `cmd/aura/cache_audit.go:207-211`
**Issue:** `toFakeTurn` defaults an empty `Finish` to `"stop"`. Combined with `DisallowUnknownFields`, a fixture author who typos `finsh` would get a strict-parse rejection (good), but one who simply omits `finish` silently gets `"stop"`. For a deterministic replay-gate fixture this is mostly benign, but it means fixtures are not fully self-documenting about the asserted finish reason.
**Fix:** Optional — keep the default but document it in the `fixtureResponse` struct comment so fixture authors know omission means `stop`.

### IN-06: `numericFromFloat` comment claims "exact" — overstated

**File:** `internal/cachemetrics/store_helpers.go:57-59`
**Issue:** The comment says the stored cost "stays exact." Float→scaled-int encoding is not exact in general (see WR-01); it is exact only for values representable as `k / 10000` within float64 precision and within the column range. The comment as written invites future callers to trust it for arbitrary magnitudes.
**Fix:** Soften to "rounded to 4 decimals" rather than "exact," and pair with the WR-01 domain guard.

---

_Reviewed: 2026-06-02_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
