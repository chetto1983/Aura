---
phase: 06-kv-cache-builder
fixed_at: 2026-06-02T10:39:08.4906891Z
review_path: .planning/phases/06-kv-cache-builder/06-REVIEW.md
iteration: 1
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 06: Code Review Fix Report

**Fixed at:** 2026-06-02T10:39:08.4906891Z
**Source review:** .planning/phases/06-kv-cache-builder/06-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 6
- Fixed: 6
- Skipped: 0

## Fixed Issues

### CR-01: Assistant turns can be committed even when the new metric write fails

**Files modified:** `internal/runner/interfaces.go`, `internal/conversations/store.go`, `internal/runner/runner_persist.go`, `internal/runner/fakes_test.go`, `internal/runner/runner_cachemetric_test.go`, `cmd/aura/cachefakes.go`, `cmd/aura/cmdfakes_test.go`
**Commit:** df5abe20
**Applied fix:** Added `AppendAssistantTurnWithCacheMetric` on the conversation store so assistant turn insert, aggregate update, and cache metric insert run inside one transaction; routed runner assistant persistence through that seam and updated fakes/tests to prove rollback on metric failure.
**Verification:** `go test ./internal/runner -run 'TestPersistAssistantAnswer|TestPersistCacheMetric' -count=1`; `go test ./internal/conversations -run TestAppendTurn -count=1`; final `go test ./internal/runner -count=1`.

### CR-02: The cache audit checks only the last LLM request of each turn

**Files modified:** `cmd/aura/cache_audit.go`, `cmd/aura/cache_test.go`, `scripts/cache_invariant_audit.sh`, `scripts/cache_invariant_negative_test.sh`
**Commit:** f12be0d1
**Applied fix:** Captured every LLM request emitted during each replay turn, changed audit output to `request NN`, and updated the wrapper/negative proof to expect the current 22 request hashes.
**Verification:** `go test ./cmd/aura -run TestCacheAudit -count=1`; `bash scripts/cache_invariant_audit.sh`; `bash scripts/cache_invariant_negative_test.sh`.

### CR-03: The audit wrapper executes an environment-controlled command through eval

**Files modified:** `scripts/cache_invariant_audit.sh`, `scripts/cache_invariant_negative_test.sh`
**Commit:** bdefa53d
**Applied fix:** Replaced `AURA_CACHE_AUDIT_CMD` plus `eval` with `AURA_CACHE_AUDIT_BIN`, an executable path invoked directly; updated the negative proof to create temporary executable scripts for poisoned and empty output.
**Verification:** `bash scripts/cache_invariant_audit.sh`; `bash scripts/cache_invariant_negative_test.sh`; `rg -n "AURA_CACHE_AUDIT_CMD|AUDIT_CMD|eval" scripts/cache_invariant_audit.sh scripts/cache_invariant_negative_test.sh`.

### WR-01: Missing messages[0] can hash as a successful empty prefix

**Files modified:** `cmd/aura/cache_audit.go`, `cmd/aura/cache_test.go`
**Commit:** 34fe80ea
**Applied fix:** Added an explicit `len(req.Messages) == 0` check before hashing and a regression test asserting exit 2 with a missing-prefix diagnostic.
**Verification:** `go test ./cmd/aura -run 'TestCacheAudit_(MissingMessages0|Mutation|AllEqual)' -count=1`; final `go test ./cmd/aura -run TestCacheAudit -count=1`.

### WR-02: Fixture decoding does not enforce the documented response shape

**Files modified:** `cmd/aura/cache_audit.go`, `cmd/aura/cache_test.go`
**Commit:** 9ed3cbe4
**Applied fix:** Validated fixture responses during decode: exactly one of `text` or `tool_calls`, non-empty tool-call id/name/arguments, and valid JSON arguments; added corrupt fixture subtests.
**Verification:** `go test ./cmd/aura -run 'TestCacheAudit_(CorruptFixture|FixturesIncludeToolCalls|AllEqual)' -count=1`; final `go test ./cmd/aura -run TestCacheAudit -count=1`.

### WR-03: AggregateSince integration coverage can pass with a broken time filter

**Files modified:** `internal/db/cache_metrics_integration_test.go`
**Commit:** 6b2f253f
**Applied fix:** Moved integration rows into an isolated future window and changed aggregate assertions from `>=` to exact prompt/cache/turn/cost totals.
**Verification:** `go test -tags db_integration ./internal/db -run TestCacheMetrics_WindowAndAggregate -count=1`.

## Skipped Issues

None.

## Final Verification

- `go test ./internal/runner -count=1`
- `go test ./cmd/aura -run TestCacheAudit -count=1`
- `bash scripts/cache_invariant_audit.sh`
- `bash scripts/cache_invariant_negative_test.sh`
- `go test -tags db_integration ./internal/db -run TestCacheMetrics_WindowAndAggregate -count=1`

---

_Fixed: 2026-06-02T10:39:08.4906891Z_
_Fixer: the agent (gsd-code-fixer)_
_Iteration: 1_
