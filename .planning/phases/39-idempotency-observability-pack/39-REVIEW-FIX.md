---
phase: 39
fixed_at: 2026-07-22T01:53:52Z
review_path: .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
iteration: 4
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 39: Code Review Fix Report

**Fixed at:** 2026-07-22T01:53:52Z
**Source review:** .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
**Iteration:** 4

**Summary:**
- Findings in scope: 2
- Fixed: 2
- Skipped: 0

## Fixed Issues

### CR-01: recovery can delete the conversation while the foreground returns 409 without the export handle

**Files modified:** `internal/agui/retention_api_test.go`, `internal/conversations/store_export_version_integration_test.go`, `internal/conversations/store_identity.go`, `internal/db/queries/conversations.sql`, `internal/db/sqlc/conversations.sql.go`, `internal/db/sqlc/querier.go`, `internal/runner/conversation_delete_lifecycle_test.go`, `internal/runner/runner_delete.go`, `internal/runner/runner_delete_recovery_integration_test.go`, `internal/runner/runner_delete_recovery_test.go`
**Commit:** 85eb2b7f2
**Status:** fixed: requires human verification
**Applied fix:** Restricted recovery eligibility so reserved rows are invisible until a three-minute grace exceeds the two-minute detached foreground finalization window, while teardown-started rows are eligible only with no worker or an expired lease. A losing foreground claimant now polls the exact durable reservation and treats terminal row absence as successful completion instead of a version conflict, retrying only while another worker still owns the lifecycle. Deterministic runner coverage pauses the foreground after reservation and lets the reconciler finish first; an HTTP-level proof confirms the foreground still returns 201 with a valid export ID and an authenticated, usable ZIP download URL. Live PostgreSQL tests prove fresh reservations and active leases are excluded, abandoned/expired work is eligible, completion is durably observable, and restart recovery still succeeds.

### WR-01: a hard process exit leaves db reset permanently in progress

**Files modified:** `cmd/aura/db_reset_idempotency_integration_test.go`, `internal/idempotency/maintenance.go`
**Commit:** d355169c9
**Status:** fixed: requires human verification
**Applied fix:** Added a fingerprint-bound atomic compare-and-swap in maintenance `Begin` that changes only expired `in_progress` receipts to terminal `indeterminate`; the operation is never reacquired or automatically re-executed, and concurrent completion/indeterminate transitions remain terminal. A disposable-database subprocess helper is force-killed after durable acquisition, the deadline is advanced past `retry_after`, and the real CLI retry proves an indeterminate response without executing reset. A post-reset sentinel survives both the first and a second retry, while the receipt remains indeterminate with an unchanged terminal timestamp.

## Validation

- `go test ./...` — PASS across the full Go repository.
- `sqlc generate` followed by `git diff --exit-code` — PASS; generated queries are in sync.
- `go test -race ./internal/runner` under WSL — PASS, including the deterministic foreground/reconciler race.
- Live PostgreSQL `TestExportDeleteRecoveryEligibilityAndCompletionObservation` — PASS without skip.
- Live PostgreSQL `TestExportDeleteProcessRestartRecoversDurableReservation` — PASS without skip.
- Disposable PostgreSQL `TestDBResetSameKeyReplaysWithoutDestroyingPostResetSentinel` — PASS without skip, including the killed-parent indeterminate branch.
- Per-commit gofmt, go vet, golangci-lint/staticcheck, and file-size hooks — PASS.
- `git diff --check` — PASS.

---

_Fixed: 2026-07-22T01:53:52Z_
_Fixer: the agent (gsd-code-fixer)_
_Iteration: 4_
