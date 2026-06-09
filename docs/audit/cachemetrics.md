# Audit: internal/cachemetrics

**Verdict:** needs-work — two defensive gaps in numeric encoding and one not-wired interface seam.
**Counts:** critical 0 / high 0 / medium 1 / low 2

---

## Findings

### [MEDIUM][BUG] NaN bypasses the range guard in `numericFromFloat`, producing a corrupt DB mantissa

**Location:** `internal/cachemetrics/store_helpers.go:73-82`
**Confidence:** high

**Detail:**  
The range guard is:
```go
if f > numericMaxCost || f < -numericMaxCost {
    return pgtype.Numeric{}, fmt.Errorf(...)
}
```
Both comparisons return `false` for `math.NaN()` (NaN is unordered), so NaN silently passes. The subsequent `scaled := f * 1e4` produces NaN; `int64(NaN*1e4)` on amd64 produces `math.MinInt64` (-9223372036854775808), and the function returns `pgtype.Numeric{Int: big.NewInt(math.MinInt64), Exp: -4, Valid: true}` — a corrupt value inserted into the DB without error.

The analogous `numericFromFloat` in `internal/conversations/store_helpers.go` has no range guard at all (accepts any float), so the gap is unique to this package.

In the current production path, `cost` in `runner_persist.go` comes from `llm.CostUSDValue`, which either dereferences a `*float64` from JSON-decoded provider data or computes a price-table product. Standard `encoding/json` never decodes a NaN (NaN is not valid JSON), so the bug is not reachable today. It becomes reachable if the cost value is ever sourced from a non-JSON path (e.g., a test mock, or a future provider adapter using a different wire format).

**Suggested fix:**  
Add a `math.IsNaN(f)` check before the range guard:
```go
if math.IsNaN(f) || f > numericMaxCost || f < -numericMaxCost {
    return pgtype.Numeric{}, fmt.Errorf("cost %v out of numeric(10,4) range ±%v", f, numericMaxCost)
}
```

---

### [LOW][BUG] `floatFromNumeric` silently propagates `+Inf`/`-Inf` from a Postgres Infinity numeric

**Location:** `internal/cachemetrics/store_helpers.go:87-96`
**Confidence:** medium

**Detail:**  
`floatFromNumeric` guards for `n.NaN` (returns 0) but not for `n.InfinityModifier`. `pgtype.Numeric.Float64Value()` returns `math.Inf(±1)` with `Valid: true` for numerics carrying `InfinityModifier == Infinity | NegativeInfinity`. The code path `f, err := n.Float64Value()` returns no error and `f.Valid = true`, so `floatFromNumeric` returns `+Inf` or `-Inf` instead of 0 or an error.

This propagates through `anyNumericFloat` (the `pgtype.Numeric` branch) into `Aggregate.TotalCostUSD` and ultimately the Telegram `/cost` display. The write path (`numericFromFloat`) correctly rejects `±Inf` via the `numericMaxCost` range guard, so a DB Infinity can only be introduced via direct DB manipulation or a future path that bypasses this package's write seam. The risk is low but the function's own comment (via `anyNumericFloat`'s WR-02 guarantee: "never a silent 0 for an unrecognizable aggregate") is violated in the opposite direction for Infinity inputs.

**Suggested fix:**  
Add an Infinity guard after the `n.NaN` check:
```go
func floatFromNumeric(n pgtype.Numeric) float64 {
    if !n.Valid || n.NaN || n.InfinityModifier != pgtype.Finite {
        return 0
    }
    ...
}
```
Or, to preserve the WR-02 "loud error" contract, promote the Infinity case to an error in `anyNumericFloat` before calling `floatFromNumeric`.

---

### [LOW][NOT-WIRED] `CacheMetricStore.Insert` is declared and injected but never dispatched by the Runner

**Location:** `internal/runner/interfaces.go:68-70`, `internal/runner/runner_persist.go:183-184`
**Confidence:** high

**Detail:**  
The `CacheMetricStore` interface declares `Insert(ctx, p sqlc.InsertCacheMetricParams) error`. The `Runner` accepts this interface as `d.CacheMetrics`, nil-checks it in `cacheMetricParams`, but never calls `r.cacheMetrics.Insert(...)`. The actual cache metric insert is performed inside `r.Conv.AppendAssistantTurnWithCacheMetric`, which calls `q.InsertCacheMetric` directly via the conversation store's transaction (see `internal/conversations/store.go:346`). The `cacheMetrics` field functions only as a presence guard (not-nil check), not as a call seam.

Consequences:
1. A test double or production implementation that returns an error from `Insert` will never surface that error through the runner.
2. The `CacheMetricStore` interface is misleading: it implies the runner dispatches to it, but the write goes through a different path entirely.
3. The nil-check in `cacheMetricParams` protects against a missing store but cannot prevent a broken store from going undetected.

**Verified with grep:** no production code calls `r.cacheMetrics.Insert(...)` or any method on the field — only the nil-guard at line 183.

**Suggested fix:**  
Either (a) remove `CacheMetricStore` and replace the nil-check with a check that `Conv` satisfies the metric-write path, clearly documenting the wiring; or (b) make the runner actually call `r.cacheMetrics.Insert` independently and remove the metric parameter from `AppendAssistantTurnWithCacheMetric` (separating the write seams). Option (b) also requires a compensating transaction pattern to keep the two writes atomic.

---

## What was checked

- All non-test Go files in `internal/cachemetrics/`: `store.go`, `store_helpers.go`.
- Test file `store_helpers_test.go` read for intended behavior baseline.
- Verified all exported symbols (`Store`, `Metric`, `Aggregate`, `MetricParams`, `NewInsertParams`, `New`) are used in production code via grep across `D:/Aura`.
- Verified no goroutines, channels, or shared mutable state (no race surface).
- Verified `go vet ./internal/cachemetrics/` is clean.
- Traced the full call path from `runner_persist.go` → `cacheMetricParams` → `AppendAssistantTurnWithCacheMetric` → `q.InsertCacheMetric` to confirm wiring.
- Inspected `pgtype.Numeric.Float64Value()` source at `~/go/pkg/mod/github.com/jackc/pgx/v5@v5.9.2/pgtype/numeric.go` for NaN/Infinity behavior.
- No dead unexported symbols: `timestamptzFrom`, `uuidFrom`, `numericFromFloat`, `floatFromNumeric`, `anyInt64`, `anyNumericFloat`, `numericMaxCost`, `numericScale` are all referenced within the package.
