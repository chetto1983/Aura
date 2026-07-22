---
phase: 39-idempotency-observability-pack
reviewed: 2026-07-22T01:21:08Z
depth: standard
files_reviewed: 150
files_reviewed_list:
  - .env.example
  - .github/workflows/ci.yml
  - cmd/aura/chat_boot.go
  - cmd/aura/db.go
  - cmd/aura/db_migrate_idempotency_integration_test.go
  - cmd/aura/db_reset_idempotency_integration_test.go
  - cmd/aura/idempotency.go
  - cmd/aura/idempotency_test.go
  - cmd/aura/main.go
  - cmd/aura/main_env_test.go
  - cmd/aura/mutation_coverage_test.go
  - cmd/aura/retention.go
  - cmd/aura/serve.go
  - cmd/aura/serve_dispatch.go
  - cmd/aura/serve_drain.go
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
  - internal/db/migrate_0048_integration_test.go
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
  - internal/db/migrations/0048_export_delete_lifecycle.down.sql
  - internal/db/migrations/0048_export_delete_lifecycle.up.sql
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
  - internal/idempotency/maintenance.go
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
  - internal/runner/runner_delete_reconcile.go
  - internal/runner/runner_delete_recovery_integration_test.go
  - internal/runner/runner_delete_recovery_test.go
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
  critical: 1
  warning: 1
  info: 0
  total: 2
status: issues_found
---

# Phase 39: Code Review Report

**Reviewed:** 2026-07-22T01:21:08Z

**Depth:** standard

**Files Reviewed:** 150

**Status:** issues_found

## Summary

Iteration 4 reviewed the cumulative 150-file inventory and every in-scope file changed from 8d573792e through 2a50c0393. All four iteration-3 findings are fixed on their stated paths: completed reset receipts survive schema reset and replay safely; export-delete cancellation, final-delete failure, and restart recovery are durable; unchanged expired retention plans can be refreshed without mutating non-planned operations; and owner-export garbage collection runs despite primary-retention failures.

The phase is still not clean. The export-delete recovery worker can race a live foreground request, complete the deletion, and cause the request to return a false conflict without the export identifier or download URL. The reset-safe maintenance registry also has no crash reconciliation for stale in-progress rows.

No source file was modified during this review.

## Validation

- Untagged targeted Go suite across cmd/aura and all relevant Phase-39 packages — PASS.
- Relevant production packages: go vet — PASS.
- Fresh, migration-42, and migration-45 db-migrate bootstrap integration test — PASS.
- Disposable PostgreSQL db-reset sentinel replay: first reset succeeded, same-key retry replayed identical output without executing, and the post-reset sentinel survived — PASS.
- Migration 0048 down/up round trip — PASS.
- Export snapshot version, writer freeze, lease non-adoption, cancellation-after-reserve, final-delete retry, and process-restart recovery tests — PASS.
- Retention unchanged-plan refresh, immediate apply, and deleting/retryable/completed immutability tests — PASS.
- Historical idempotency concurrency/conflict/indeterminate and replay-expiry before/equal/after integration tests — PASS.
- Historical bounded/disjoint retention claims and persisted two-phase lifecycle integration tests — PASS.
- Recovery unit paths repeated 20 times — PASS.
- scripts/check-file-size.sh — PASS, 2051 tracked source files within the limit.
- git diff --check 8d573792e..2a50c0393 — PASS.
- Windows race execution was unavailable because this shell has CGO disabled; the untagged and live database gates above executed normally.
- Controlled stale-reset receipt probe: a two-hour-old in-progress maintenance row whose retry_after had expired still returned “operation is still in progress”, Aura exit 2, and remained in_progress — product failure reproduced.

## Iteration-3 Finding Resolution Matrix

| Iteration-3 finding | Result | Evidence |
|---|---|---|
| CR-01: db reset destroys its own receipt and can execute twice | Resolved for normal completion and replay | db reset now uses a migration-role registry in public, outside the reset aura schema. The live sentinel test proves the first result completes and a same-key retry does not execute. Hard process loss leaves this separate registry stale; see WR-01. |
| CR-02: interrupted export-delete permanently freezes the conversation | Requested recovery paths resolved | Migration 0048 persists reservation phase, worker, and lease. Finalization detaches from request cancellation, failed attempts release only the worker lease, and the boot reconciler resumes the stored reservation. Live and unit cancellation/final-failure/restart cases pass. Concurrent recovery can still lose the foreground success response; see CR-01. |
| WR-01: unchanged expired retention plan cannot be refreshed | Resolved | Only a still-planned conflicting token refreshes created_at/updated_at. Real-store tests prove immediate apply after refresh and timestamp/status immutability for deleting, retryable, and completed operations. |
| WR-02: primary retention failure starves owner-export collection | Resolved | Plan/apply and every owner-export sweeper run as independent branches; their errors are joined afterward. Tests prove export sweeps still execute after both Plan and Apply failures and later sweepers continue after an earlier sweep error. |

## Historical-Finding Regression Matrix

| Historical finding | Iteration-4 result |
|---|---|
| Original CR-01 HTTP enforcement and operation propagation | Resolved without regression. |
| Original CR-02 CLI mutation registry | Normal mutation and db-migrate ownership remain resolved; completed db-reset replay is fixed, but reset crash reconciliation remains incomplete; see WR-01. |
| Original CR-03 scheduler parent/tool child scopes | Resolved without regression. |
| Original CR-04 durable export publication and conflict safety | Publication, version coverage, reservation ordering, cancellation, transient failure, and restart recovery are present; foreground/reconciler completion reporting remains unsafe; see CR-01. |
| Original CR-05 retention crash resume and fresh authorization | Resolved without regression, including unchanged-plan refresh and non-planned immutability. |
| Original CR-06 local second version check | Resolved without regression. |
| Original CR-07 replay expiry | Resolved without regression, including equality at the wall-clock boundary. |
| Original CR-08 centralized redaction | Resolved without regression. |
| Original WR-01 readiness timeout/goroutine behavior | Resolved without regression. |
| Original WR-02 compactor pagination | Resolved without regression. |

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: recovery can delete the conversation while the foreground returns 409 without the export handle

**Classification:** BLOCKER

**File:** internal/db/queries/conversations.sql:124-164; internal/runner/runner_delete.go:118-135; internal/runner/runner_delete_reconcile.go:91-110; internal/agui/owner_export.go:261-275; internal/agui/retention_api.go:143-167

**Issue:** ListReservedConversationDeletes returns every reserved or teardown-started row, including a fresh reservation whose foreground request is between its committed reserve and ClaimDeleteTeardown. The minute-tick reconciler can claim that row first and complete the deletion. The foreground claim then affects zero rows because another worker owns the lease, and resumeReservedConversationDelete returns zero with no error without distinguishing “busy” or “completed”. OwnerExporter converts every affected value other than one into ErrOwnerExportConflict. The HTTP handler therefore sends 409 “conversation changed during export” and omits export_id/download_url even though the same durable reservation successfully deleted the conversation and the archive is already published. The client loses the only returned handle to its required export after the source conversation is gone. The existing same-operation race test asserts only that one delete occurs; it deliberately ignores every caller’s affected/error result, so it does not cover this response-loss path.

**Fix:** prevent recovery from competing with a live foreground operation and make completion observable. Filter recovery candidates so a reserved row is eligible only after a recovery grace greater than the detached foreground-finalization window, and a teardown-started row only when its lease is absent or expired. Additionally, when a same-reservation claim returns busy or the row disappears, observe durable lifecycle completion rather than returning a version conflict. A separate durable delete-operation record that survives conversation deletion is the strongest completion receipt. Add a deterministic HTTP test that pauses the foreground after reserve, lets the reconciler claim and finish, then proves the request returns 201 with a usable export_id/download_url rather than 409.

## Warnings

### WR-01: a hard process exit leaves db reset permanently in progress

**Classification:** WARNING

**File:** internal/idempotency/maintenance.go:86-140; cmd/aura/idempotency.go:198-206,218-244

**Issue:** the reset-safe public registry correctly survives a successful reset, but it has no equivalent of the application registry’s crash reconciler. Begin stores retry_after for a new in-progress row. On every later Begin, StateInProgress returns DecisionInProgress even when retry_after is long expired; the code merely substitutes another retry duration in the response and never transitions the row. No scheduler, startup pass, or maintenance command reconciles this table. A kill or host loss after acquisition but before Complete/MarkIndeterminate therefore leaves the operation key permanently unusable and reports “still in progress” forever instead of the required terminal indeterminate result. A controlled disposable-database probe with a two-hour-old row reproduced the state unchanged.

**Fix:** atomically transition expired maintenance in-progress rows to indeterminate, either during Begin under a conditional update or through an explicit maintenance reconciler. Never reacquire or execute the reset automatically. Add a subprocess test that terminates the parent after durable acquisition, advances beyond retry_after, retries the same key, and proves the response is indeterminate, the reset is not executed, and the row is terminal.

## Final Assessment

Critical: 1

Warning: 1

Info: 0

Total: 2

Status: issues_found
