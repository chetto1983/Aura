---
phase: 39-idempotency-observability-pack
reviewed: 2026-07-22T00:15:53Z
depth: standard
files_reviewed: 141
files_reviewed_list:
  - .env.example
  - .github/workflows/ci.yml
  - cmd/aura/chat_boot.go
  - cmd/aura/db.go
  - cmd/aura/db_migrate_idempotency_integration_test.go
  - cmd/aura/idempotency.go
  - cmd/aura/idempotency_test.go
  - cmd/aura/main.go
  - cmd/aura/main_env_test.go
  - cmd/aura/mutation_coverage_test.go
  - cmd/aura/retention.go
  - cmd/aura/serve.go
  - cmd/aura/serve_dispatch.go
  - cmd/aura/serve_lifecycle.go
  - cmd/aura/serve_readiness_test.go
  - compose.yaml
  - internal/activelearn/bounded_seen.go
  - internal/activelearn/learner.go
  - internal/agent/idempotency_operation.go
  - internal/agent/llm_agent.go
  - internal/agent/llm_agent_retry.go
  - internal/agent/llm_agent_retry_gateway_test.go
  - internal/agent/mcptools/bridge.go
  - internal/agent/mcptools/bridge_reconnect.go
  - internal/agent/mcptools/bridge_reconnect_mutation_test.go
  - internal/agent/metrics.go
  - internal/agent/tools/spec.go
  - internal/agui/conversations_api.go
  - internal/agui/idempotency_http.go
  - internal/agui/idempotency_http_test.go
  - internal/agui/owner_export.go
  - internal/agui/owner_export_objectstore.go
  - internal/agui/owner_export_objectstore_test.go
  - internal/agui/readiness.go
  - internal/agui/readiness_test.go
  - internal/agui/retention_api.go
  - internal/agui/retention_api_test.go
  - internal/agui/server.go
  - internal/config/config_learning.go
  - internal/config/config_retention.go
  - internal/conversations/store.go
  - internal/conversations/store_export_version_integration_test.go
  - internal/conversations/store_fakedbtx_test.go
  - internal/conversations/store_helpers.go
  - internal/conversations/store_identity.go
  - internal/cron/dispatch.go
  - internal/cron/handlers/agentjob_test.go
  - internal/cron/handlers/learning_compaction.go
  - internal/cron/handlers/retention.go
  - internal/cron/handlers/retention_test.go
  - internal/cron/observability.go
  - internal/cron/scheduler.go
  - internal/cron/scheduler_test.go
  - internal/cron/store.go
  - internal/db/db.go
  - internal/db/idempotency_operations_contract_test.go
  - internal/db/migration_head.go
  - internal/db/migration_lock.go
  - internal/db/migrations/0043_idempotency_operations.down.sql
  - internal/db/migrations/0043_idempotency_operations.up.sql
  - internal/db/migrations/0044_retention_operations.up.sql
  - internal/db/migrations/0045_scheduler_learning_compaction_kind.up.sql
  - internal/db/migrations/0046_idempotency_replay_envelope.down.sql
  - internal/db/migrations/0046_idempotency_replay_envelope.up.sql
  - internal/db/migrations/0047_conversation_snapshot_version.down.sql
  - internal/db/migrations/0047_conversation_snapshot_version.up.sql
  - internal/db/observability.go
  - internal/db/queries/conversations.sql
  - internal/db/queries/idempotency_operations.sql
  - internal/db/queries/retention_operations.sql
  - internal/db/retention_operations_contract_test.go
  - internal/db/sqlc/conversations.sql.go
  - internal/db/sqlc/idempotency_operations.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/querier.go
  - internal/db/sqlc/retention_operations.sql.go
  - internal/gateway/idempotency_test.go
  - internal/gateway/reserve.go
  - internal/idempotency/context.go
  - internal/idempotency/fingerprint.go
  - internal/idempotency/store.go
  - internal/idempotency/store_integration_test.go
  - internal/idempotency/store_test.go
  - internal/idempotency/types.go
  - internal/idempotency/types_test.go
  - internal/knowledge/probe.go
  - internal/knowledge/probe_unit_test.go
  - internal/learningretention/compactor.go
  - internal/learningretention/compactor_integration_test.go
  - internal/learningretention/compactor_test.go
  - internal/learningretention/neo4j_store.go
  - internal/learningretention/reservoir.go
  - internal/learningretention/telemetry.go
  - internal/mcp/client.go
  - internal/mcp/http_client.go
  - internal/mcp/observability.go
  - internal/mcp/tool_methods.go
  - internal/neostore/learning.go
  - internal/objectstore/fake.go
  - internal/objectstore/filesystem.go
  - internal/objectstore/objectstore_test.go
  - internal/objectstore/s3.go
  - internal/objectstore/types.go
  - internal/obs/boundary.go
  - internal/obs/catalog.go
  - internal/obs/init.go
  - internal/obs/init_test.go
  - internal/obs/meter.go
  - internal/readiness/state.go
  - internal/readiness/state_test.go
  - internal/reasoninglearn/learner.go
  - internal/reasoningstore/store.go
  - internal/redact/string.go
  - internal/retention/engine.go
  - internal/retention/engine_test.go
  - internal/retention/local.go
  - internal/retention/local_test.go
  - internal/retention/plan.go
  - internal/retention/plan_test.go
  - internal/retention/store.go
  - internal/retention/store_integration_test.go
  - internal/runner/conversation_delete_lifecycle_test.go
  - internal/runner/runner.go
  - internal/runner/runner_delete.go
  - internal/runner/runner_resume.go
  - internal/runner/runner_resume_idempotency_test.go
  - internal/toolselectlearn/learner.go
  - internal/toolselectstore/store.go
  - observability/grafana/dashboards/aura-agents.json
  - observability/grafana/dashboards/aura-data-retention.json
  - observability/grafana/dashboards/aura-overview.json
  - observability/grafana/dashboards/aura-tools-mcp.json
  - observability/prometheus/rules/aura-alerts.yml
  - observability/prometheus/rules/aura-recording.yml
  - observability/prometheus/tests/aura-rules.test.yml
  - observability/tempo/tempo.yml
  - scripts/verify-observability.ps1
  - scripts/verify-observability.Tests.ps1
  - web/src/api/idempotency.test.ts
  - web/src/api/idempotency.ts
  - web/src/main.tsx
findings:
  critical: 2
  warning: 2
  info: 0
  total: 4
status: issues_found
---

# Phase 39: Code Review Report

**Reviewed:** 2026-07-22T00:15:53Z

**Depth:** standard

**Files Reviewed:** 141

**Status:** issues_found

## Summary

Iteration 3 verified all six iteration-2 findings and rechecked the earlier original findings against every non-generated source, test, migration, and configuration file in the review scope plus all 49 in-scope files changed from 4ac8f5de3 through dcfe4ae57. Four iteration-2 fixes are complete. The export-delete and retention fixes close their reported safety gaps but introduce recovery or liveness defects. Two additional release-blocking correctness failures remain, so the phase is not clean.

No source file was modified during this review.

## Validation

- Untagged targeted Go suite: go test across cmd/aura and the relevant agui, agent, conversations, cron, gateway, idempotency, knowledge, learning-retention, object-store, observability, redaction, retention, and runner packages — PASS.
- Relevant production packages: go vet — PASS.
- Fresh/42/45 schema migration bootstrap integration test with db_integration — PASS.
- Disposable PostgreSQL integration suites for conversation export versions, idempotency replay/concurrency/expiry, and retention persistence/lifecycle — PASS.
- scripts/check-file-size.sh — PASS, 2045 tracked source files within the limit.
- git diff --check 4ac8f5de3..dcfe4ae57 — PASS.
- Controlled disposable-database reset reproduction — FAIL as a product contract: the command printed “ok: schema reset”, then “operation completion unavailable”, and returned non-success after the destructive effect had completed.

Passing suites do not exercise the four adversarial states below.

## Iteration-2 Finding Resolution Matrix

| Iteration-2 finding | Result | Evidence |
|---|---|---|
| CR-01: db migrate bootstrap deadlock | Resolved | The command now uses the migration owner and the migration-role advisory lock/tracker path. Fresh, migration-42, and migration-45 disposable schemas all reached head successfully. |
| CR-02: export-delete stale snapshot and teardown ordering | Safety fix resolved; recovery blocker remains | All exported snapshot mutations advance the version and the database reservation is committed before teardown. A cancellation or failure after that commit has no durable recovery path; see CR-02. |
| CR-03: retention accepted stale plans | Safety fix resolved; liveness warning remains | First apply now checks policy, age, and a rebuilt live candidate token before authorizing the durable snapshot. Re-planning the same expired snapshot cannot refresh its creation time; see WR-01. |
| CR-04: replay bytes served at the expiry boundary | Resolved | The store rejects replay whenever replay_expires_at is not after now, including equality, before returning stored bytes. Before/equal/after integration coverage passes. |
| WR-01: readiness leaked one goroutine per poll | Resolved | The per-server coordinator shares one in-flight probe by index. Repeated concurrent polls join it, and timeout tests prove one invocation for a non-cooperative probe. Neo4j network and acquisition deadlines are bounded. |
| WR-02: owner-export bytes never physically expired | Mechanism resolved; scheduling warning remains | Durable expiry markers, bounded ordered sweep, archive-before-marker deletion, retry preservation, expired-open denial, and production wiring are present. An unrelated primary-retention error prevents this sweep from running; see WR-02. |

## Earlier Original-Finding Regression Matrix

| Original finding | Iteration-3 result |
|---|---|
| CR-01 HTTP enforcement and operation propagation | Resolved without regression. The complete unsafe-route inventory remains behind the idempotency wrapper and strict agent mutations derive scoped child operations. |
| CR-02 CLI mutation registry | Regressed for db reset only; see CR-01. Ordinary mutations remain acquired/completed, and db migrate uses its independent schema owner. |
| CR-03 scheduler parent/tool child scopes | Resolved without regression. |
| CR-04 durable export publication and conflict safety | Publication, version coverage, and pre-teardown reservation are resolved; post-reservation recovery remains blocked; see CR-02. |
| CR-05 retention crash resume and fresh authorization | Crash resume and authorization safety are resolved; same-snapshot re-plan freshness remains broken; see WR-01. |
| CR-06 local second version check | Resolved without regression. |
| CR-07 replay expiry | Resolved without regression. |
| CR-08 centralized redaction | Resolved without regression. |
| WR-01 readiness timeout behavior | Resolved without regression. |
| WR-02 compactor pagination | Resolved without regression. |

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: db reset destroys its own idempotency receipt and can repeat a successful destructive reset

**Classification:** BLOCKER
**File:** cmd/aura/idempotency.go:72-73,170-185,197-258; cmd/aura/db.go:124-133; internal/db/migrations/0043_idempotency_operations.down.sql:1

**Issue:** db reset is still registered as a normal runtime-registry mutation. The parent acquires an idempotency row, then the child performs the full schema reset. Migration 0043 down drops the table containing that acquired row. After the child reports success, Complete cannot find the receipt, so the wrapper returns “operation completion unavailable”. A retry with the same operation key creates a new row in the rebuilt table and performs the destructive reset again. The review reproduced this exact sequence on a disposable database: the schema reset succeeded, the command returned failure afterward, and the successful effect had no replayable receipt. Data created after an ambiguous first result can therefore be destroyed by the instructed retry.

**Fix:** place reset ownership and its durable completion receipt outside the schema being reset. A separate maintenance registry/database is one valid design; a schema-local idempotency row is not. Preserve the same-key completed result across the reset boundary. Add a disposable-database test that performs reset, inserts a sentinel after the successful result, retries the same key, and proves the reset is replayed without executing and the sentinel survives.

### CR-02: an interrupted export-delete permanently reserves and write-freezes the conversation

**Classification:** BLOCKER
**File:** internal/runner/runner_delete.go:76-94,108-141; internal/db/queries/conversations.sql:66-68,93-99,107-121; internal/conversations/store_identity.go:144-170

**Issue:** export-delete now commits delete_reservation before any runtime or external teardown, which correctly closes the earlier stale-version race. Every later step and the final conditional delete still use the request context. Cancellation, process death, or a transient final-delete failure after the reservation commit leaves the row reserved. Normal deletion explicitly excludes reserved rows, and no release operation, durable delete-operation record, lease, recovery worker, or startup reconciliation exists. A different operation key derives a different reservation and cannot adopt the row; the original HTTP operation is in-progress or indeterminate and cannot safely rerun. The conversation remains readable but all guarded snapshot writes and future deletes are permanently denied until direct database intervention.

**Fix:** make the reserved delete a durable, resumable lifecycle. Persist operation state with the reservation, use detached bounded finalization after the commit where appropriate, and reconcile reserved rows after cancellation/restart. Permit only the same durable operation to resume destructive teardown; release a reservation only while no irreversible teardown has occurred. Add integration tests for cancellation immediately after reserve, failure of the final reserved delete, and process restart recovery.

## Warnings

### WR-01: an unchanged retention snapshot cannot be re-planned after its authorization window expires

**Classification:** WARNING
**File:** internal/retention/engine.go:98-115,191-201; internal/db/queries/retention_operations.sql:1-9

**Issue:** Apply correctly rejects a planned operation whose CreatedAt is outside PlanValidity. BuildPlan is deterministic, so an unchanged policy/candidate snapshot produces the same token. CreateRetentionOperation handles that token conflict by updating only token to itself and returns the original row, retaining its old CreatedAt. Calling Plan again after expiry therefore returns a token that Apply immediately rejects, and repeating Plan can never refresh it unless the candidate set changes or an operator edits the database. The fix preserves safety but can indefinitely deny legitimate retention work for a stable old snapshot.

**Fix:** for a still-planned conflicting operation, atomically refresh its authorization generation/timestamps, or include an issued-at generation in the plan token. Never rewrite deleting, retryable, or completed operations. Test Plan at T0, Plan again with the same candidates after PlanValidity, then immediate successful Apply, including the real PostgreSQL store.

### WR-02: owner-export byte collection is skipped whenever primary retention fails

**Classification:** WARNING
**File:** internal/cron/handlers/retention.go:41-65

**Issue:** the handler returns immediately when the primary retention engine cannot plan or apply. The independent owner-export sweep is sequenced only afterward, so a persistent failure in any unrelated retention source or adapter prevents expired owner-export archives from being physically deleted indefinitely. The new object-store expiry machinery is therefore effective only while the primary engine remains healthy.

**Fix:** run the primary retention engine and owner-export sweep as independent bounded cleanup branches, then join/report their errors after both have had a chance to run. Add handler tests proving the owner-export sweeper is invoked when Plan fails and when Apply fails.

## Final Assessment

Critical: 2

Warning: 2

Info: 0

Total: 4

Status: issues_found
