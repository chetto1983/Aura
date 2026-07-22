---
phase: 39
fixed_at: 2026-07-22T01:06:00Z
review_path: .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
iteration: 3
findings_in_scope: 4
fixed: 4
skipped: 0
status: all_fixed
---

# Phase 39: Code Review Fix Report

**Fixed at:** 2026-07-22T01:06:00Z
**Source review:** .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
**Iteration:** 3

**Summary:**
- Findings in scope: 4
- Fixed: 4
- Skipped: 0

## Fixed Issues

### CR-01: db reset destroys its own idempotency receipt and can repeat a successful destructive reset

**Files modified:** `cmd/aura/db_reset_idempotency_integration_test.go`, `cmd/aura/idempotency.go`, `internal/idempotency/maintenance.go`
**Commit:** 38266f19e
**Status:** fixed: requires human verification
**Applied fix:** Moved db-reset ownership and completed replay receipts into a migration-role-owned `public.aura_maintenance_operations` registry outside the reset `aura` schema. The registry is created lazily under a transaction-scoped advisory lock, preserves same-key completed output across the destructive reset, and rejects concurrent ownership. A disposable PostgreSQL test performs reset, creates a post-reset sentinel, retries the same key, and proves the completed result is replayed without destroying the sentinel.

### CR-02: an interrupted export-delete permanently reserves and write-freezes the conversation

**Files modified:** `cmd/aura/chat_boot.go`, `cmd/aura/serve.go`, `cmd/aura/serve_drain.go`, `internal/conversations/store_export_version_integration_test.go`, `internal/conversations/store_fakedbtx_test.go`, `internal/conversations/store_identity.go`, `internal/db/migrate_0048_integration_test.go`, `internal/db/migrations/0048_export_delete_lifecycle.down.sql`, `internal/db/migrations/0048_export_delete_lifecycle.up.sql`, `internal/db/queries/conversations.sql`, `internal/db/sqlc/conversations.sql.go`, `internal/db/sqlc/models.go`, `internal/db/sqlc/querier.go`, `internal/runner/conversation_delete_lifecycle_test.go`, `internal/runner/runner_delete.go`, `internal/runner/runner_delete_reconcile.go`, `internal/runner/runner_delete_recovery_integration_test.go`, `internal/runner/runner_delete_recovery_test.go`
**Commit:** 6957cb133
**Status:** fixed: requires human verification
**Applied fix:** Added migration 0048's durable reserved/teardown-started lifecycle, stable operation-derived ownership, short worker leases, same-operation resume, and a bounded boot/interval reconciler. Request cancellation is detached only after the reservation commit; process restart and final-delete failure resume the stored operation, competing operations cannot adopt it, and only a pre-teardown reservation can be released. Unit race tests cover cancellation, final failure, and same-operation contention; live PostgreSQL tests cover process restart, lease exclusion, and migration down/up recovery.

### WR-01: an unchanged retention snapshot cannot be re-planned after its authorization window expires

**Files modified:** `internal/db/queries/retention_operations.sql`, `internal/db/sqlc/querier.go`, `internal/db/sqlc/retention_operations.sql.go`, `internal/retention/store.go`, `internal/retention/store_integration_test.go`
**Commit:** 842bada31
**Status:** fixed: requires human verification
**Applied fix:** Changed deterministic-token conflicts to insert-or-refresh behavior that advances `created_at` and `updated_at` only while the durable operation remains `planned`; deleting, retryable, and completed operations are reloaded without timestamp or status mutation. Live PostgreSQL race coverage plans at T0, replans the identical expired snapshot, applies it immediately, and separately proves all non-planned timestamps remain unchanged.

### WR-02: owner-export byte collection is skipped whenever primary retention fails

**Files modified:** `internal/cron/handlers/retention.go`, `internal/cron/handlers/retention_test.go`
**Commit:** 2a50c0393
**Status:** fixed: requires human verification
**Applied fix:** Removed early returns from primary retention Plan/Apply and owner-export sweep failures, allowed every independent cleanup branch to run, accumulated successful deletion counts, and returned `errors.Join` after all branches completed. Handler tests prove owner-export GC runs after Plan failure and Apply failure, continues past a failing export backend, and reports both primary and export errors.

## Validation

- `go test -race ./... -count=1` — PASS across the full Go repository.
- Disposable/live PostgreSQL db-reset replay sentinel, migration 0048 round-trip, export-delete restart/lease recovery, and retention refresh/immutability tests — PASS.
- Export-delete restart recovery live PostgreSQL race test repeated 10 times — PASS.
- Retention refresh/immutability live PostgreSQL race tests repeated 10 times — PASS.
- Retention handler focused race tests repeated 50 times — PASS.
- Per-commit gofmt, go vet, golangci-lint/staticcheck, and file-size hooks — PASS.
- `go build ./...`, `git diff --check`, and full repository 600-LOC guard — PASS.

---

_Fixed: 2026-07-22T01:06:00Z_
_Fixer: the agent (gsd-code-fixer)_
_Iteration: 3_
