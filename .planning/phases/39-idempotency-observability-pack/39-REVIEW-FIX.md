---
phase: 39
fixed_at: 2026-07-22T00:00:02Z
review_path: .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
iteration: 2
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 39: Code Review Fix Report

**Fixed at:** 2026-07-22T00:00:02Z
**Source review:** .planning/phases/39-idempotency-observability-pack/39-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 6
- Fixed: 6
- Skipped: 0

## Fixed Issues

### CR-01: CLI idempotency makes fresh installs and schema upgrades unable to run `db migrate`

**Files modified:** `cmd/aura/db.go`, `cmd/aura/db_migrate_idempotency_integration_test.go`, `cmd/aura/idempotency.go`, `cmd/aura/idempotency_test.go`, `internal/db/migration_lock.go`
**Commit:** 9675a362c
**Status:** fixed: requires human verification
**Applied fix:** Bypassed the runtime idempotency registry only for migration dispatch, added a bootstrap-role advisory migration lock, and covered fresh-empty, version-42, and version-45 databases with two real CLI migrations and no child-bypass environment variable.

### CR-02: Export-delete can destroy live state before its version check, and the version omits snapshot-affecting writes

**Files modified:** `internal/agui/owner_export.go`, `internal/agui/retention_api.go`, `internal/agui/retention_api_test.go`, `internal/conversations/store.go`, `internal/conversations/store_export_version_integration_test.go`, `internal/conversations/store_fakedbtx_test.go`, `internal/conversations/store_helpers.go`, `internal/conversations/store_identity.go`, `internal/db/migrations/0047_conversation_snapshot_version.down.sql`, `internal/db/migrations/0047_conversation_snapshot_version.up.sql`, `internal/db/queries/conversations.sql`, `internal/db/sqlc/conversations.sql.go`, `internal/db/sqlc/models.go`, `internal/db/sqlc/querier.go`, `internal/runner/conversation_delete_lifecycle_test.go`, `internal/runner/runner_delete.go`
**Commit:** 224c3dc0e
**Status:** fixed: requires human verification
**Applied fix:** Added a monotonic exported-snapshot version advanced by conversation, turn, and asset mutations; reserved export deletion durably before teardown; finalized only with the reservation token; and proved version conflicts cause zero cancel, pause-expiry, eviction, job termination, share revocation, or delete effects.

### CR-03: Retention apply accepts stale plans after policy or candidate-set drift

**Files modified:** `internal/db/queries/retention_operations.sql`, `internal/db/retention_operations_contract_test.go`, `internal/db/sqlc/querier.go`, `internal/db/sqlc/retention_operations.sql.go`, `internal/retention/engine.go`, `internal/retention/engine_test.go`, `internal/retention/plan.go`, `internal/retention/plan_test.go`, `internal/retention/store.go`, `internal/retention/store_integration_test.go`
**Commit:** b4ba00a40
**Status:** fixed: requires human verification
**Applied fix:** Rebuilt and compared the current candidate/policy plan before first authorization, enforced exact token and bounded plan age, atomically transitioned `planned` operations to `deleting`, and preserved durable retry/resume semantics after authorization.

### CR-04: A replay past `replay_expires_at` is still returned until a cleanup sweep happens

**Files modified:** `internal/idempotency/store.go`, `internal/idempotency/store_integration_test.go`, `internal/idempotency/store_test.go`
**Commit:** 2b8f82b67
**Status:** fixed: requires human verification
**Applied fix:** Enforced `replay_expires_at <= now` as a read-time authorization boundary before constructing replay data, with unit and live PostgreSQL coverage at before, equal, and after boundaries while physical payload bytes remain unswept.

### WR-01: Repeated readiness polls leak one goroutine per permanently wedged probe

**Files modified:** `internal/agui/readiness.go`, `internal/agui/readiness_test.go`, `internal/agui/server.go`, `internal/knowledge/probe.go`, `internal/knowledge/probe_unit_test.go`
**Commit:** 2303633cf
**Status:** fixed: requires human verification
**Applied fix:** Shared one in-flight concrete check per configured readiness probe across repeated polls, retained request-local deadlines, bounded Neo4j connection acquisition/socket connect/close, and added a 12-poll goleak/race regression proving only one wedged execution exists.

### WR-02: Owner-export expiry hides archives but never reclaims their object-store bytes

**Files modified:** `cmd/aura/serve.go`, `cmd/aura/serve_dispatch.go`, `internal/agui/owner_export_objectstore.go`, `internal/agui/owner_export_objectstore_test.go`, `internal/cron/handlers/retention.go`, `internal/cron/handlers/retention_test.go`, `internal/objectstore/fake.go`, `internal/objectstore/filesystem.go`, `internal/objectstore/objectstore_test.go`, `internal/objectstore/s3.go`, `internal/objectstore/types.go`
**Commit:** dcfe4ae57
**Status:** fixed: requires human verification
**Applied fix:** Persisted a durable owner/export/expiry record before archive publication, wired the existing scheduled retention owner to a 128-record bounded sweep, deleted archive bytes before finalizing metadata, and proved failed deletes retry to physical `Head`/`Get` absence in both fake-store regression tests and a live temporary Garage bucket.

---

_Fixed: 2026-07-22T00:00:02Z_
_Fixer: the agent (gsd-code-fixer)_
_Iteration: 2_
