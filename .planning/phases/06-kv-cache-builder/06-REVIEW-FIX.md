---
phase: 06-kv-cache-builder
fixed_at: 2026-06-02T00:00:00Z
review_path: .planning/phases/06-kv-cache-builder/06-REVIEW.md
iteration: 1
findings_in_scope: 5
fixed: 5
skipped: 0
status: all_fixed
---

# Phase 06: Code Review Fix Report

**Fixed at:** 2026-06-02
**Source review:** .planning/phases/06-kv-cache-builder/06-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 5 (WR-01..WR-05; Critical: 0)
- Fixed: 5
- Skipped: 0

Co-located Info items addressed opportunistically inside the warning commits:
IN-06 (soften "exact" comment → folded into WR-01), IN-04 (document token-sum
truncation safety → folded into WR-02). The other Info items (IN-01 helper
extraction to a shared `pgconv` package, IN-02 `usageFromStateDelta` triplication,
IN-03 `usage()` missing `cache-stats`, IN-05 fixture `finish` default) were left
out of scope per the fix brief (critical_warning only).

Verification after every Go edit: `go vet ./...` (clean), `go build ./...` (clean),
touched-package `go test` (green), `go test -race` on `internal/runner`
(`BASH_ENV=~/.aura-toolchain.sh`, green). Cache invariant gate
(`scripts/cache_invariant_audit.sh`) exits 0 with 20 identical `messages[0]`
hashes (`d69144fd…`); the negative gate (`cache_invariant_negative_test.sh`)
still fails loud on both a poisoned hash stream and empty output. No regression
to the prefix invariant.

## Fixed Issues

### WR-01: `numericFromFloat` silent overflow / mis-round beyond `numeric(10,4)` range

**Files modified:** `internal/cachemetrics/store_helpers.go`, `internal/cachemetrics/store_helpers_test.go`
**Commit:** e44e4919
**Applied fix:** Changed `numericFromFloat` to return `(pgtype.Numeric, error)`. Added a
`numericMaxCost = 999999.9999` domain guard: a magnitude outside ±numericMaxCost now
returns a loud error (propagated through `NewInsertParams`, which already returns an
error) instead of silently constructing an overflowing mantissa. Softened the comment
from "stays exact" to a precise statement of the representable domain (IN-06). Extended
`TestNumericFromFloat_RoundTrip` with the boundary fixtures the reviewer asked for
(`123.4567`, `999999.9999`, `-999999.9999`) and added `TestNumericFromFloat_OutOfRange`.
A `mustNumeric(t, f)` test helper keeps the in-range call sites one-liners.

### WR-02: `anyInt64` / `anyNumericFloat` swallow parse errors → silent `0`

**Files modified:** `internal/cachemetrics/store_helpers.go`, `internal/cachemetrics/store.go`, `internal/cachemetrics/store_helpers_test.go`
**Commit:** 7e6dc0c6
**Applied fix:** Both coercion helpers now return `(value, error)`. The string/`[]byte`
parse-failure branches and the `default` (unmodeled driver shape) arm return an error
naming the offending `%T`/value instead of fabricating `0`. `AggregateSince` propagates
each error with a field-tagged message (`prompt_tokens` / `cached_tokens` / `cost_usd`),
so an unexpected decode shape fails loud rather than reporting "0 tokens / $0.00 cost"
(the false-green the codebase refuses). Added a one-line note that the float64 branch's
truncation is safe for integral token sums (IN-04). Tests split into decode-shape
(success) and `*_UnparseableErrors` (error-on-bad-shape) cases.

### WR-03: `cache_metrics` row write non-atomic with the assistant turn

**Files modified:** `internal/db/queries/cache_metrics.sql`, `internal/db/sqlc/cache_metrics.sql.go`, `internal/db/sqlc/querier.go`, `internal/runner/runner_persist.go`
**Commit:** 9bea6c5a
**Applied fix:** Option (c) from the review — made the metric `INSERT` idempotent via
`ON CONFLICT (conversation_id, seq) DO NOTHING` (regenerated `sqlc`; the Go signature is
unchanged, only the embedded SQL + generated doc comment). A retry for an
already-recorded turn is now a no-op rather than a PK violation or duplicate metric.
Documented the chosen consistency contract on `persistAssistantAnswer`: the turn is the
load-bearing record, the metric is an append-only idempotent observation, and the metric
write still fails the turn loudly (no-skip discipline, preserved by the existing
`TestPersistAssistantAnswer_CacheMetricErrorSurfaces` / nil-store tests, both still green).
**Note — requires human verification:** the residual assistant-turn duplication on a
*fresh-seq* retry is a property of the shared `CountTurns`/`AppendTurn` seq model (it
affects the user turn and pause turn identically), not specific to the metric. Closing it
fully requires a shared transaction across the conversations + cachemetrics Stores
(option (a)), which is an architectural refactor deliberately deferred as out of phase
scope. A developer should confirm the documented "idempotent observation, deferred shared
tx" contract is acceptable for this surface before sign-off.

### WR-04: `cache_metrics_ts_idx` DESC vs `ORDER BY ts ASC` mismatch

**Files modified:** `internal/db/migrations/0007_cache_metrics.up.sql`
**Commit:** bdd329c4
**Applied fix:** Changed the index from `(ts DESC)` to a plain ascending `(ts)` so it
serves BOTH the `WHERE ts >= $1` range predicate and the `ORDER BY ts ASC` ordering of
`ListSince`/`AggregateSince` without a reverse-scan/sort step, and corrected the now-wrong
"a DESC index serves the --since reads" comment. Migration 0007 is new in this phase
(pre-merge, not yet shipped); the down migration drops the table (index drops with it), so
the direction change is safe and no test asserts the index DDL string.

### WR-05: `repoRoot()` failure silently falls back to a relative fixture dir

**Files modified:** `cmd/aura/cache_audit.go`
**Commit:** 3b96c54d
**Applied fix:** `cacheAuditMain` now captures the `repoRoot()` error and writes an
explicit diagnostic to `errOut` ("could not locate repo root (go.mod) from cwd … —
falling back to relative fixture dir") before the relative-path fallback. The operator
now sees the true environment cause instead of a misleading downstream "fixture corrupt".
The fallback behavior itself is unchanged, so the existing exit-code contract holds; the
negative-gate test confirms the audit still fails loud.

## Skipped Issues

None — all in-scope findings were fixed.

---

_Fixed: 2026-06-02_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
