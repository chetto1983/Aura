# Audit: internal/cachemetrics

**Verdict:** needs-work — one not-wired method, one misplaced constant; no bugs, no races
**Counts:** critical 0 / high 0 / medium 1 / low 1

## Scope

Files audited (production, excluding `*_test.go`):
- `internal/cachemetrics/store.go`
- `internal/cachemetrics/store_helpers.go`

Tests read for intent:
- `internal/cachemetrics/store_helpers_test.go`
- `internal/runner/runner_cachemetric_test.go`
- `internal/db/cache_metrics_integration_test.go`

Cross-repo grep performed across `D:/Aura` for all exported and unexported symbols.

---

## Findings

### [MEDIUM][NOT-WIRED] `cachemetrics.Store.Insert` is defined but never dispatched in any production path

**Location:** `internal/cachemetrics/store.go:40-44`
**Confidence:** high

**Detail:**

`(*Store).Insert` satisfies the `runner.CacheMetricStore` interface (declared at
`internal/runner/interfaces.go:68`). The runner stores the concrete `*cachemetrics.Store`
in its private `cacheMetrics` field (`runner.go:79,124`) and checks it for `nil`
(`runner_persist.go:183`). However, `r.cacheMetrics.Insert(...)` is **never called anywhere
in the runner or in any other production code path**.

The actual per-turn INSERT executes through a different route:

```
runner_persist.go:172  r.Conv.AppendAssistantTurnWithCacheMetric(ctx, turn, metric)
  → conversations/store.go:318,346  q.InsertCacheMetric(ctx, metric)   ← real DB insert
```

The `metric` value (`sqlc.InsertCacheMetricParams`) is built by `cacheMetricParams`
(which validates that `r.cacheMetrics != nil`), but it is then forwarded directly to the
conversation store's transactional helper — bypassing `r.cacheMetrics.Insert` entirely.

`cache_stats.go` uses only `AggregateSince` and `ListSince`, not `Insert`.

The nil-guard in `cacheMetricParams` gives the false impression that a non-nil
`cacheMetrics` store is required for the Insert path; in reality the guard exists only to
signal a composition-root wiring error — but the wired store's only method is never called.

In the unit-test fake (`fakes_test.go:156-184`), `fakeConvStore.AppendAssistantTurnWithCacheMetric`
appends directly to `f.cache.inserts` without calling `f.cache.Insert()`, which is
consistent with production behaviour but means the tests do not exercise the production
`cachemetrics.Store.Insert` code path at all.

**Suggested fix:**

Option A — Remove the wiring entirely and drop the `CacheMetricStore` interface from
the runner. The nil guard in `cacheMetricParams` is misleading; delete it together with
the `Deps.CacheMetrics` field and `r.cacheMetrics`. The actual insert already happens
transactionally through `ConversationStore`.

Option B — If a future use case (e.g. a non-transactional, best-effort metric path)
wants a standalone `CacheMetricStore.Insert`, add a call site at that time. Until then,
the field, interface method, and nil guard are dead weight that creates a false
requirement during dependency injection.

---

### [LOW][DEAD-CODE] `numericScale` constant defined in `store.go` but belongs with its sole user in `store_helpers.go`

**Location:** `internal/cachemetrics/store.go:23-24`
**Confidence:** high

**Detail:**

`numericScale` (value `4`) is declared in `store.go` but is only ever consumed in
`store_helpers.go:83`:

```go
// store_helpers.go:83
return pgtype.Numeric{Int: big.NewInt(int64(scaled)), Exp: -numericScale, Valid: true}, nil
```

`store.go` itself never references `numericScale`. Placing it in `store.go` breaks
colocation — a reader of `store_helpers.go` who wants to understand `numericFromFloat`
must chase the constant across files. The parallel in `internal/conversations/store_helpers.go:19`
correctly co-locates `numericScale` beside `numericFromFloat` in the same file.

This is not a bug (same package, compiles fine), but it is an avoidable maintenance
hazard: if `numericFromFloat` is ever moved, the constant is silently left behind.

**Suggested fix:** Move the `numericScale` constant (and its comment) from `store.go`
to `store_helpers.go`, immediately above `numericFromFloat`. Remove the vacated lines
from `store.go`.

---

## What was checked and found clean

- **Nil-pointer dereference:** `Store.q` is always non-nil (`New` assigns
  `sqlc.New(pool)` unconditionally; pool nil panics in sqlc, not here). No other
  pointer dereferences without guards.
- **Unchecked/swallowed errors:** All errors are `%w`-wrapped and returned. No silent
  discards.
- **Resource leaks:** `ListSince` calls `q.ListCacheMetricsSince`; the generated sqlc
  code (`cache_metrics.sql.go:78-103`) opens `rows`, calls `defer rows.Close()`, and
  checks `rows.Err()` — no leak.
- **Data races:** No goroutines, no shared mutable state. The package is stateless
  beyond the embedded `*sqlc.Queries` (which is safe by its own contract).
- **Integer overflow:** `int → int32` conversions for `Seq`, `PromptTokens`,
  `CachedTokens` in `NewInsertParams` are safe under realistic values (<2^31 tokens
  per turn). `numericFromFloat`'s `int64(scaled)` is safe for all values passing the
  `numericMaxCost` guard (max mantissa 9 999 999 999 << int64 max).
- **Rounding correctness:** `numericFromFloat` applies half-away-from-zero rounding
  (positive: `+0.5` then truncate; negative: `-0.5` then truncate via Go's toward-zero
  `int64(float64)` conversion — both branches are correct).
- **NaN/Inf guard order:** `math.IsNaN(f)` is evaluated first (short-circuit), so NaN
  never reaches the range comparison where NaN ordering is undefined.
- **`floatFromNumeric` error drop:** `Float64Value` errors and `!f.Valid` are coerced
  to `0.0`. This is intentional (documented) and appropriate at the read boundary for
  an aggregate display function — a database NULL reads as zero, not a fatal error.
- **`anyInt64`/`anyNumericFloat` WR-02 discipline:** Unmodeled driver shapes return
  errors (not silent zeros). Tests confirm all documented shapes and the error path.
- **Dead unexported symbols:** All unexported functions (`uuidFrom`, `numericFromFloat`,
  `floatFromNumeric`, `anyInt64`, `anyNumericFloat`, `timestamptzFrom`) are referenced
  from production code within the package. None are test-only.
