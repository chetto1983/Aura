---
phase: 39-idempotency-observability-pack
reviewed: 2026-07-22T02:08:17Z
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
  warning: 0
  info: 0
  total: 1
status: issues_found
---

# Phase 39: Code Review Report

**Reviewed:** 2026-07-22T02:08:17Z

**Depth:** standard

**Files Reviewed:** 150

**Status:** issues_found

## Summary

Iteration 5 re-reviewed the exact cumulative 150-file inventory, including all 12 in-scope files changed by the two iteration-4 fix commits from 3b7cff248 through d355169c9. The export-delete foreground/reconciler race is resolved: recovery eligibility now respects the foreground grace and active leases, exact-reservation completion is observable, restart recovery still succeeds, and the losing foreground path returns a 201 receipt whose authenticated download is a usable ZIP.

The maintenance fix correctly makes a killed reset receipt terminal without reacquiring or re-executing the reset, but it introduces a new blocker. It uses the one-second client retry hint as its crash/orphan deadline, so a duplicate can mark an operation indeterminate while the original reset owner is still alive and executing. A controlled disposable-PostgreSQL probe reproduced this on the real registry and CLI.

No source file was modified during this review.

## Scope and Validation

- Scope reconstruction: prior REVIEW inventory 150; iteration-4 source/test diff 12; exact union 150; missing files 0.
- Untagged targeted Go suite across cmd/aura and all relevant Phase-39 packages - PASS.
- Relevant production packages: go vet - PASS.
- Focused foreground/reconciler receipt, recovery, lease, completion-observation, cancellation, failure-resume, and same-operation race tests repeated 20 times - PASS.
- Deterministic HTTP proof: foreground returns 201 with a UUID export_id, non-empty download_url, authenticated HTTP 200 application/zip download, and non-empty archive - PASS.
- Disposable PostgreSQL db-migrate bootstrap plus db-reset completed replay, killed-parent indeterminate receipt, and post-reset sentinel preservation - PASS.
- Disposable PostgreSQL export-delete recovery eligibility/completion observation, snapshot/version/freeze/lease tests, and process-restart recovery - PASS.
- Historical PostgreSQL idempotency contract, replay-expiry boundaries, retention disjoint claims, and persisted two-phase lifecycle tests - PASS.
- Controlled active-reset duplicate probe: original helper remained alive; after 1.5 seconds the duplicate Aura CLI exited 2 with the indeterminate outcome and SQL state was indeterminate - product failure reproduced.
- Deployable observability static verifier - PASS. The negative-fixture script could not be faithfully rerun because pwsh is unavailable in this Windows shell and Windows PowerShell line-wraps the expected error text.
- scripts/check-file-size.sh - PASS, all 2051 tracked source files within the 600-LOC cap.
- git diff --check 3b7cff248..d355169c9 - PASS.
- Current Windows race execution is unavailable because this shell has CGO disabled; the iteration-4 fix run recorded WSL race PASS for the modified runner path.

## Iteration-4 Finding Resolution Matrix

| Iteration-4 finding | Result | Evidence |
|---|---|---|
| CR-01: recovery can delete the conversation while foreground returns 409 without the export handle | Resolved | Fresh reservations wait through a three-minute recovery grace that exceeds the two-minute detached finalizer. Active teardown leases are excluded, expired leases remain recoverable, and a losing claimant observes exact-reservation deletion as success. Focused tests repeated 20 times, the live PostgreSQL eligibility/restart gates, and the HTTP 201 plus usable ZIP proof all pass. |
| WR-01: a hard process exit leaves db reset permanently in progress | Crash path resolved, active path unsafe | A killed owner now transitions to terminal indeterminate without executing reset, and the sentinel survives. However, the same CAS fires after the shared one-second retry hint even when the original owner is still alive; see CR-01. |

## Historical-Finding Regression Matrix

| Historical finding | Iteration-5 result |
|---|---|
| Original CR-01 HTTP enforcement and operation propagation | Resolved without regression. |
| Original CR-02 CLI mutation registry | Completed replay, conflict, killed-reset terminalization, and no-reexecution are proven. Active reset duplicate classification regressed; see CR-01. |
| Original CR-03 scheduler parent/tool child scopes | Resolved without regression. |
| Original CR-04 durable export publication and conflict safety | Resolved, including snapshot/version coverage, publication ordering, writer freeze, recovery grace, lease ownership, exact completion observation, restart recovery, and 201 usable-download receipt. |
| Original CR-05 retention crash resume and fresh authorization | Resolved without regression, including unchanged-plan refresh and non-planned immutability. |
| Original CR-06 local second version check | Resolved without regression. |
| Original CR-07 replay expiry | Resolved without regression, including equality at the wall-clock boundary. |
| Original CR-08 centralized redaction | Resolved without regression. |
| Original WR-01 readiness timeout/goroutine behavior | Resolved without regression. |
| Original WR-02 compactor pagination | Resolved without regression. |
| Iteration-2 db-migrate bootstrap | Resolved without regression. |
| Iteration-2 export reservation ordering/recovery and owner-export physical expiry | Resolved without regression. |
| Iteration-2 retention stale-plan behavior | Resolved without regression. |
| Iteration-3 db-reset durable receipt and sentinel replay | Completed and killed-owner cases are resolved; active-owner duplicate semantics regressed; see CR-01. |
| Iteration-3 export cancellation/failure/restart recovery | Resolved without regression. |
| Iteration-3 retention refresh/immutability and independent owner-export sweeps | Resolved without regression. |

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: a duplicate reset marks a live operation indeterminate after one second

**Classification:** BLOCKER

**File:** internal/idempotency/maintenance.go:86-112; internal/idempotency/store.go:17-20; cmd/aura/idempotency.go:270-277

**Issue:** MaintenanceRegistry.Begin stores retry_after as now plus defaultRetryAfter, and the shared default is one second. On any matching duplicate after that timestamp, Begin atomically changes the row from in_progress to indeterminate. retry_after is client retry guidance, not proof that the effect owner crashed; the Phase-39 contract explicitly requires a duplicate whose original is still executing to remain typed in_progress. The reset parent keeps executing the destructive child after acquisition, so a reset can legitimately exceed one second. A duplicate then terminalizes the live receipt. When the original later calls Complete, its conditional transition affects zero rows and the command reports operation completion unavailable after the schema reset already happened.

The failure was reproduced against a disposable PostgreSQL database using the existing subprocess helper. The helper acquired the real maintenance registry receipt and remained alive. After 1.5 seconds, the same-key real Aura CLI retry exited 2 with operation outcome is indeterminate; SQL showed state=indeterminate while process inspection simultaneously showed the original helper still alive.

**Fix:** Separate retry guidance from crash ownership. Give maintenance operations a real execution lease or liveness record, renew it while the parent owns the child, and reconcile to indeterminate only after that lease expires with no live owner. A session/advisory-lock ownership signal may also provide a strong process-loss discriminator because the registry connection remains open during execution. Keep retry_after only for the immediate in_progress response. Add a deterministic live test that keeps the original helper alive beyond retry_after and proves duplicates remain in_progress, then kills it, advances beyond the independent recovery lease, and proves the next duplicate becomes terminal indeterminate with the sentinel preserved. Retain concurrent Complete-versus-reconcile CAS coverage.

## Warnings

None.

## Final Assessment

Critical: 1

Warning: 0

Info: 0

Total: 1

Status: issues_found
