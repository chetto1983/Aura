# Audit: internal/cachemetrics

**Verdict:** needs-work — one medium logic bug (silent data loss), one low quality issue.

**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

---

### [MEDIUM][BUG] `NewInsertParams` silently discards `Seq` for callers that pass `seq=0` via `cacheMetricParams`

**Location:** `internal/cachemetrics/store_helpers.go:38-44` + `internal/runner/runner_persist.go:168`

**Confidence:** high

**Detail:**

`runner_persist.go:168` calls `r.cacheMetricParams(convID, 0, u, cost)` — hardcoding `seq=0`. This is
intentional: `AppendAssistantTurnWithCacheMetric` in `internal/conversations/store.go:333-354` detects
`metric.Seq <= 0` (actually `== 0` via int32 cast at line 339) and overwrites it with the freshly allocated
turn sequence number inside the DB transaction, so the *stored* metric row gets the correct seq. So far so
good.

However, `NewInsertParams` in `cachemetrics` receives `p.Seq = 0` and encodes it as `int32(0)`. The resulting
`sqlc.InsertCacheMetricParams.Seq` leaves the package with `Seq = 0`. The contract between the two layers
requires the *caller* (`conversations.Store`) to overwrite this field before the INSERT. There is nothing in
`MetricParams`, `NewInsertParams`, or their documentation that documents this expectation or enforces it.

**Risk:** Any future caller that constructs `MetricParams{Seq: 0, ...}` and calls
`cachemetrics.Store.Insert` directly (bypassing `AppendAssistantTurnWithCacheMetric`) will silently insert
with `seq=0`. Because the table has `ON CONFLICT (conversation_id, seq) DO NOTHING`, only the *first*
metric row per conversation would persist; all subsequent ones for that conversation would be silently
dropped — every assistant turn after the first would show zero tokens and zero cost in `cache-stats`.

The code is correct *today* only because there is exactly one call site and it goes through the conv-store
seam. The package API is a footgun for any direct caller.

**Suggested fix:** Two complementary options (pick one or both):

1. Add a note to `MetricParams.Seq` that `0` is a sentinel meaning "auto-assign" and document the
   responsibility contract explicitly.
2. Guard in `NewInsertParams`: if `p.Seq < 0` return an error; treat `0` as the sentinel and document it
   in the function comment. This does not prevent the footgun but at least makes the convention visible.
3. Alternatively, remove `Seq` from `MetricParams` entirely and let the conversations store stamp it after
   allocation, accepting a `sqlc.InsertCacheMetricParams` with `Seq=0` as the public API contract.

---

### [LOW][BUG] `NewInsertParams`: UUID parse error returned without turn context; inconsistent with cost error

**Location:** `internal/cachemetrics/store_helpers.go:30-36`

**Confidence:** high

**Detail:**

When `uuidFrom` fails (line 30-33), the raw error is returned bare:

```go
convID, err := uuidFrom("conversation_id", p.ConversationID)
if err != nil {
    return sqlc.InsertCacheMetricParams{}, err   // no seq context
}
cost, err := numericFromFloat(p.CostUSD)
if err != nil {
    return sqlc.InsertCacheMetricParams{}, fmt.Errorf("conversation_id %q seq %d: %w", ...) // has context
}
```

The UUID error already contains `"invalid conversation_id \"<value>\": ..."` (from `uuidFrom`'s own
`fmt.Errorf`), so the call site in `runner_persist.go:194` adds `"persist cache metric: %w"` — producing a
message that is readable but lacks the `seq` number. The cost error path adds `"conversation_id %q seq %d:"`
before re-wrapping. The two error paths are inconsistent: one includes the seq, the other does not. In a
multi-turn conversation this makes triage harder.

**Suggested fix:**

```go
if err != nil {
    return sqlc.InsertCacheMetricParams{}, fmt.Errorf("conversation_id %q seq %d: %w", p.ConversationID, p.Seq, err)
}
```

---

## What was checked and found clean

**Races:** No goroutines, no shared mutable state. `Store` holds a `*sqlc.Queries` (which wraps a
`*pgxpool.Pool`, itself goroutine-safe). Every method takes a `context.Context` and returns synchronously.
No mutexes needed and none are missing.

**Resource leaks:** No `rows`, file handles, `time.Ticker`, or channels opened by this package. The sqlc
`ListCacheMetricsSince` call in `internal/db/sqlc/cache_metrics.sql.go` closes the rows with `defer
rows.Close()` in generated code. No leaks possible from the cachemetrics layer.

**Dead code:** Every exported and unexported symbol is reachable:

- `New` — called in `cmd/aura/chat.go:143`, `cmd/aura/cache_stats.go:44`, `cmd/aura/serve_channels.go:124`, `internal/eval/skills_snippet_reuse_cot_eval_test.go:246`, `internal/runner/live_e2e_test.go:150`.
- `Insert` — via `runner.CacheMetricStore` interface at `internal/runner/interfaces.go:69`.
- `ListSince` / `AggregateSince` — called in `cmd/aura/cache_stats.go:50,45` and `cmd/aura/serve_channels.go:130`.
- `Metric` / `Aggregate` — used in `cmd/aura/cache_stats.go:82`.
- `MetricParams` / `NewInsertParams` — used in `internal/runner/runner_persist.go:186-192`.
- All unexported helpers (`timestamptzFrom`, `uuidFrom`, `numericFromFloat`, `floatFromNumeric`, `anyInt64`, `anyNumericFloat`, `numericMaxCost`, `numericScale`) are used within the package and exercised by `store_helpers_test.go`.

**Not-wired code:** None. All three store methods are reachable through production call paths.

**Logic correctness:**

- `numericFromFloat`: rounding is half-away-from-zero for both positive and negative values.
  Verified: `int64(x - 0.5)` for negative `x` truncates toward zero (Go spec), which yields
  half-away-from-zero semantics (e.g., `-5.5 -> int64(-6.0) = -6`). The comment is accurate.
- Boundary guard `f > numericMaxCost || f < -numericMaxCost` correctly allows `±999999.9999` through
  (same float64 literal) and rejects anything larger.
- `anyInt64` / `anyNumericFloat` type switches cover all shapes pgx v5 plausibly returns for
  `coalesce(sum(integer), 0)` and `coalesce(sum(numeric), 0)` respectively. The `nil` / `struct{}` default
  cases error loudly (WR-02 compliant).
- `int -> int32` narrowing at lines 40-42: unchecked, but the DB column is `INTEGER` (int32 range) and
  LLM token counts are nowhere near 2^31. No practical risk.
