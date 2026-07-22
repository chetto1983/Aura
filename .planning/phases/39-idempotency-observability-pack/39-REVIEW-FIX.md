---
phase: 39
fixed_at: 2026-07-22T02:31:25Z
review_path: .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
iteration: 5
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 39: Code Review Fix Report

**Fixed at:** 2026-07-22T02:31:25Z
**Source review:** .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
**Iteration:** 5

**Summary:**
- Findings in scope: 1
- Fixed: 1
- Skipped: 0

## Fixed Issues

### CR-01: a duplicate reset marks a live operation indeterminate after one second

**Files modified:** `cmd/aura/db_reset_idempotency_integration_test.go`, `internal/idempotency/maintenance.go`
**Commit:** 8bac675ab
**Status:** fixed: requires human verification
**Applied fix:** Separated client retry guidance from crash ownership. Each reset operation now acquires an operation-keyed PostgreSQL session advisory lock that the migration-role registry connection retains for the full child execution. `retry_after` remains only the typed `in_progress` retry hint, while a new independently durable five-minute `lease_expires_at` recovery deadline gates crash reconciliation. A duplicate can compare-and-swap `in_progress` to terminal `indeterminate` only when the advisory lock proves no live database owner and the independent recovery lease has expired; it never reacquires or re-executes the reset. A lost CAS re-reads the durable row so a concurrent `Complete` returns replay, while a winning reconciliation makes `Complete` return the existing stale-transition error.

The disposable-PostgreSQL subprocess test now keeps the helper alive beyond `retry_after`, invokes the real same-key Aura CLI, and proves the result remains `in_progress` without changing the receipt or destroying the sentinel. It then kills the helper, waits for the session advisory lock to disappear, advances only the independent lease, and proves the next duplicate becomes terminal `indeterminate`; the sentinel survives and a second retry preserves the terminal state and timestamp. Twenty disposable-database Complete-versus-reconcile races prove the only valid outcomes are completed/replay or stale-transition/indeterminate.

## Validation

- `go test ./internal/idempotency ./cmd/aura` - PASS.
- Disposable PostgreSQL `go test -tags=db_integration ./cmd/aura -run '^TestDBResetSameKeyReplaysWithoutDestroyingPostResetSentinel$' -count=1 -v` - PASS without skip, including live-owner, killed-owner, stable-terminal, sentinel, and 20 Complete-versus-reconcile race assertions.
- The same disposable PostgreSQL test under WSL `go test -race -tags=db_integration ...` - PASS without skip.
- WSL `go test -race ./internal/idempotency ./cmd/aura` - PASS.
- `go test ./...` - PASS across the full Go repository.
- `go vet ./...` and `go vet -tags db_integration ./cmd/aura` - PASS.
- `golangci-lint run --timeout=5m ./...` - PASS with 0 issues.
- Pre-commit gofmt, file-size, go vet, and golangci-lint hooks - PASS.
- `git diff --check` - PASS.

---

_Fixed: 2026-07-22T02:31:25Z_
_Fixer: the agent (gsd-code-fixer)_
_Iteration: 5_
