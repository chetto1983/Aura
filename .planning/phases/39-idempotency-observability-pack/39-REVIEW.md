---
phase: 39-idempotency-observability-pack
reviewed: 2026-07-22T02:48:15Z
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
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 39: Code Review Report

**Reviewed:** 2026-07-22T02:48:15Z

**Depth:** standard

**Files Reviewed:** 150

**Status:** clean

## Summary

Iteration 6 re-reviewed the exact cumulative 150-file inventory and the two non-generated files changed by the iteration-5 fix commit `8bac675ab`: `internal/idempotency/maintenance.go` and `cmd/aura/db_reset_idempotency_integration_test.go`. The inventory is unchanged: the prior REVIEW list and the cumulative source/test diff union are both exactly 150 unique files, with no missing or extra path.

The maintenance registry now separates client retry guidance from crash ownership correctly. A dedicated migration-role connection retains an operation-keyed PostgreSQL session advisory lock for the destructive child's lifetime, while a durable five-minute `lease_expires_at` independently gates crash reconciliation. A duplicate cannot terminalize a live owner, an owner death releases the session signal without bypassing the remaining lease, and only a no-owner duplicate after lease expiry may compare-and-swap `in_progress` to `indeterminate`. Lost reconciliation races re-read the durable outcome, so concurrent `Complete` produces only the valid completed/replay or stale-transition/indeterminate result.

The export-delete recovery receipt and every historical Phase-39 finding remain resolved. Foreground/reconciler completion observation, lease/recovery eligibility, restart recovery, durable publication, retention lifecycle, idempotency expiry, redaction, readiness, pagination, and observability gates show no regression.

No source file was modified during this review.

## Scope and Validation

- Scope reconstruction: prior REVIEW inventory 150; `git diff 4ca90c771..8bac675ab` non-generated delta 2; exact cumulative union 150; unique files 150; missing files 0; extra files 0.
- Full static trace of `MaintenanceRegistry.Begin`, advisory-lock keying, dedicated connection lifetime, DDL upgrade/backfill, `Complete`, `MarkIndeterminate`, reset caller ownership, and reset schema boundaries - PASS.
- Exact disposable PostgreSQL state-machine probe - PASS: live helper held one advisory lock; beyond `retry_after` the duplicate remained `in_progress`; killing the owner released the lock to zero; with 296.619 seconds remaining on the independent lease the duplicate still remained `in_progress`; after lease expiry the result became terminal `indeterminate`; the sentinel survived; a second retry preserved the terminal state and timestamp.
- Committed disposable PostgreSQL reset test - PASS without skip, including completed replay, live-owner duplicate behavior, killed-owner reconciliation, sentinel preservation, stable terminal retry, and 20 `Complete`-versus-reconcile races.
- Disposable PostgreSQL historical gates - PASS: db-migrate bootstrap, conversations export-delete recovery eligibility/completion observation, idempotency store contract, retention disjoint claims/two-phase lifecycle/unchanged-plan refresh, runner process-restart recovery receipt, and migration 0048 round trip.
- Focused foreground/reconciler receipt, recovery, lease, completion-observation, cancellation, failure-resume, and same-operation race tests repeated 20 times - PASS.
- Targeted Phase-39 Go tests and production-package `go vet` - PASS.
- Full `go test ./...` and `go vet ./...` - PASS.
- `golangci-lint run --timeout=5m ./...` - PASS with 0 issues. Its runner emitted path-relativity diagnostics for an older external temporary review tree, not repository findings.
- Deployable observability verifier - PASS: 4 dashboards, 20 alerts, 83 checked queries, bounded synthetic runtime series, OTLP trace, and 4 provisioned dashboards.
- `scripts/check-file-size.sh` - PASS, all 2051 tracked source files within the 600-LOC cap.
- `git diff --check 4ca90c771..8bac675ab` - PASS.
- Iteration-5 fix validation also records WSL race PASS for both the changed reset integration and the relevant untagged packages.

## Iteration-5 Finding Resolution Matrix

| Iteration-5 finding | Result | Evidence |
|---|---|---|
| CR-01: a duplicate reset marks a live operation indeterminate after one second | Resolved | `retry_after` remains client guidance only. A session advisory lock proves live ownership, and an independent five-minute lease gates orphan reconciliation. The exact real-CLI probe proves live-owner and killed-owner/pre-lease duplicates remain `in_progress`; only no-owner/post-lease reconciliation becomes stable `indeterminate`, with no reset re-execution and the sentinel preserved. Twenty disposable-database `Complete`-versus-reconcile races accept only the two correct CAS outcomes. |

## Historical-Finding Regression Matrix

| Historical finding | Iteration-6 result |
|---|---|
| Original CR-01 HTTP enforcement and operation propagation | Resolved without regression. |
| Original CR-02 CLI mutation registry | Resolved without regression: completed replay, conflict safety, live-owner fail-closed behavior, killed-owner lease gating, terminal crash classification, and no re-execution are proven. |
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
| Iteration-3 db-reset durable receipt and sentinel replay | Resolved without regression across completed, active-owner, killed-owner/pre-lease, killed-owner/post-lease, and terminal replay cases. |
| Iteration-3 export cancellation/failure/restart recovery | Resolved without regression. |
| Iteration-3 retention refresh/immutability and independent owner-export sweeps | Resolved without regression. |
| Iteration-4 export foreground/reconciler receipt race | Resolved without regression; the foreground loser observes exact-reservation completion and returns the usable durable receipt. |
| Iteration-4 hard-exit reset orphan | Resolved without regression; owner death releases the session lock and post-lease reconciliation terminates safely without rerunning reset. |
| Iteration-5 live-reset misclassification | Resolved; advisory ownership and the independent lease prevent the one-second retry hint from terminalizing a live operation. |

## Narrative Findings (AI reviewer)

No critical, warning, or info findings.

## Final Assessment

Critical: 0

Warning: 0

Info: 0

Total: 0

Status: clean
