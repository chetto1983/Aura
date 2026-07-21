---
phase: 39-idempotency-observability-pack
reviewed: 2026-07-21T22:28:03Z
depth: standard
files_reviewed: 118
files_reviewed_list:
  - .env.example
  - .github/workflows/ci.yml
  - cmd/aura/chat_boot.go
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
  - internal/conversations/store_identity.go
  - internal/cron/dispatch.go
  - internal/cron/handlers/agentjob_test.go
  - internal/cron/handlers/learning_compaction.go
  - internal/cron/handlers/retention.go
  - internal/cron/observability.go
  - internal/cron/scheduler.go
  - internal/cron/scheduler_test.go
  - internal/cron/store.go
  - internal/db/db.go
  - internal/db/idempotency_operations_contract_test.go
  - internal/db/migration_head.go
  - internal/db/migrations/0043_idempotency_operations.down.sql
  - internal/db/migrations/0043_idempotency_operations.up.sql
  - internal/db/migrations/0044_retention_operations.up.sql
  - internal/db/migrations/0045_scheduler_learning_compaction_kind.up.sql
  - internal/db/migrations/0046_idempotency_replay_envelope.down.sql
  - internal/db/migrations/0046_idempotency_replay_envelope.up.sql
  - internal/db/observability.go
  - internal/db/queries/conversations.sql
  - internal/db/queries/idempotency_operations.sql
  - internal/db/queries/retention_operations.sql
  - internal/db/sqlc/conversations.sql.go
  - internal/db/sqlc/idempotency_operations.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/querier.go
  - internal/gateway/idempotency_test.go
  - internal/gateway/reserve.go
  - internal/idempotency/context.go
  - internal/idempotency/fingerprint.go
  - internal/idempotency/store.go
  - internal/idempotency/store_integration_test.go
  - internal/idempotency/store_test.go
  - internal/idempotency/types.go
  - internal/idempotency/types_test.go
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
  - internal/retention/store.go
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
  critical: 4
  warning: 2
  info: 0
  total: 6
status: issues_found
---

# Phase 39: Code Review Report

**Reviewed:** 2026-07-21T22:28:03Z
**Depth:** standard  
**Files Reviewed:** 118
**Status:** issues_found

## Summary

Iteration 2 closes substantial parts of the first review: production HTTP enforcement, scheduler-to-tool child operations, the second local version check, central error/Stringer redaction, readiness response latency, and bucket pagination are now present and exercised. The phase is still not releasable. Four correctness blockers remain in the CLI bootstrap path, export-delete concurrency, retention authorization, and replay expiry, plus two resource/data-lifecycle warnings.

Targeted verification observed during this review:

- `go test ./cmd/aura ./internal/agui ./internal/agent ./internal/cron/handlers ./internal/gateway ./internal/idempotency ./internal/learningretention ./internal/obs ./internal/redact ./internal/retention ./internal/runner` — PASS.
- `npx vitest run src/api/idempotency.test.ts --coverage.enabled=false` — PASS (3/3).
- `bash scripts/check-file-size.sh` — PASS (2041 tracked source files).

Passing tests do not cover the adversarial states below.

## Original-Finding Resolution Matrix

| Original | Iteration-2 result | Evidence |
|---|---|---|
| CR-01 | Resolved | Production `Server.Mux` wraps the complete unsafe-route inventory; the real-mux duplicate-create test proves one effect, and agent tools derive a scoped child operation. |
| CR-02 | Not resolved cleanly | Ordinary CLI mutations now acquire/complete the registry, but the wrapper makes `db migrate` depend on roles/schema that the command itself must create; see CR-01 below. |
| CR-03 | Resolved | Scheduler operations remain parents and strict agent mutations derive stable `agent.tool` children; the agent-job replay test proves one effect. |
| CR-04 | Not resolved | Publish is durable and the in-process thread lock is held, but snapshot versioning is incomplete and irreversible teardown precedes the conditional delete; see CR-02 below. |
| CR-05 | Partially resolved with regression | Persisted operations are now loaded before claims, so crash resume is reachable; first-use fresh-token/drift authorization was removed entirely; see CR-03 below. |
| CR-06 | Resolved at the requested minimum | `LocalArtifacts.Remove` performs a second version comparison and maps replacement to a retryable version conflict. |
| CR-07 | Not resolved | Status/allowlisted headers and a result-expired decision were added, but wall-clock expiry is ignored until GC clears the row; see CR-04 below. |
| CR-08 | Resolved | Resolved `LogValuer`, `error`, `fmt.Stringer`, string, and nested group values pass through the central redactor with regression tests. |
| WR-01 | Response-latency defect resolved; resource warning remains | `/readyz` returns on the shared deadline, but repeated polls leak non-cooperative probe goroutines; see WR-01 below. |
| WR-02 | Resolved | Ordered `$after` pagination plus per-store rotation reaches later bucket pages in a long-lived compactor. |

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: CLI idempotency makes fresh installs and schema upgrades unable to run `db migrate`

**Classification:** BLOCKER  
**File:** `cmd/aura/idempotency.go:156-168`; `cmd/aura/main.go:60`; `cmd/aura/db.go:44-56`; `internal/db/queries/idempotency_operations.sql:1-74`
**Issue:** every inventoried mutation is intercepted before command dispatch. The parent unconditionally opens `cfg.DB.URL` as the runtime app role and calls the latest generated idempotency queries. `db migrate` is itself inventoried. On a fresh Compose database, `aura_app`/`aura_migrate` are created only by `dbMigrate`'s later `EnsureRoles`, so the parent cannot connect. On an existing database below migration 0043 the registry table is absent, and on a database at 0043-0045 the generated query references migration-0046 columns that do not exist. The command needed to create/upgrade the registry therefore cannot reach its own migration code. This also breaks CI jobs and the `aura-migrate` one-shot service that run migration against a fresh database.

**Fix:** give schema/bootstrap commands an idempotency owner that does not depend on the schema being upgraded. A narrow migration-role advisory lock plus migration tracker/version contract is appropriate for `db migrate`; alternatively bootstrap the registry only after roles and a compatible registry schema exist. Add fresh-empty, 0042-to-head, and 0045-to-0046 subprocess tests against disposable databases; none may set the child-bypass environment variable.

### CR-02: Export-delete can destroy live state before its version check, and the version omits snapshot-affecting writes

**Classification:** BLOCKER  
**File:** `internal/runner/runner_delete.go:74-123`; `internal/conversations/store_identity.go:145-170`; `internal/db/queries/conversations.sql:99-109,120-139`; `internal/agui/retention_api.go:40-76`
**Issue:** the conditional lifecycle performs cancel, pause expiry, session eviction, background-job termination, and share revocation before `DeleteForIdentityIfVersion` is attempted. If another process advances the conversation between export and delete, the final DELETE returns zero only after those irreversible effects have already run; the conversation survives with shares and runtime state destroyed. The version is only `last_active_at`. Rename and reasoning-metadata writes change data included in `conversation.json` without advancing that value, and those HTTP handlers do not take the runner thread lock. A concurrent rename can therefore be absent from the archive while the conditional DELETE still succeeds and destroys the newer row. The in-memory thread lock only serializes turns in one process and does not close either path.

**Fix:** reserve deletion with a database-backed state/version transition before any teardown and keep that ownership through teardown/final delete. Use an explicit monotonic snapshot version advanced by every exported conversation/turn/asset mutation, or a transaction/advisory-lock protocol observed by all writers. A version conflict must perform zero cancel/expire/evict/terminate/revoke effects. Add tests for rename/metadata races and for a cross-process version conflict proving the conversation and all shares/runtime state survive unchanged.

### CR-03: Retention apply accepts stale plans after policy or candidate-set drift

**Classification:** BLOCKER  
**File:** `internal/retention/engine.go:120-169`; `internal/retention/plan.go:44-82`; `.planning/phases/39-idempotency-observability-pack/39-06-PLAN.md:130-137,157-165`
**Issue:** the CR-05 fix correctly loads the persisted operation before resuming, but `Apply` now never calls `Source.Candidates`, rebuilds the plan, compares the current policy version, or checks an explicit plan expiry. Any old token found in PostgreSQL is executable indefinitely. A candidate removed from current eligibility, a TTL changed to retain it, a newly protected member, or a changed member set no longer invalidates first apply. This contradicts the locked Phase-39 acceptance criteria that any version/action/member change invalidates apply and that the CLI requires an exact fresh token. Per-item version/activity checks do not revalidate class TTL, action membership, or policy authorization.

**Fix:** distinguish first authorization from crash resume durably. While an operation is still `planned`, rebuild the current plan and require the same token/current policy (and preferably a bounded plan expiry), then atomically transition the operation to `deleting`. Once any item has crossed that transition, resume the persisted items without global recomputation. Add tests for policy-version, TTL, member-add/remove, and action drift before first claim, plus the existing crash-after-removal resume case.

### CR-04: A replay past `replay_expires_at` is still returned until a cleanup sweep happens

**Classification:** BLOCKER  
**File:** `internal/idempotency/store.go:131-153`; `internal/idempotency/store_test.go:105-158`
**Issue:** `readExistingDecision` returns `DecisionResultExpired` only when `replay_cleared_at` is set or all representations are already absent. It verifies that `replay_expires_at` exists but never compares it with the store clock. Consequently an expired HTTP/tool/CLI result remains replayable for minutes, days, or indefinitely when the GC scheduler is delayed or disabled. The new test named `completed replay expired` only constructs a cleared row with a future expiry and does not exercise the actual deadline. The 30-day replay limit is therefore cleanup-liveness-dependent rather than an authorization boundary.

**Fix:** when reading a completed operation, return `DecisionResultExpired` whenever `replay_expires_at <= now`, regardless of whether bytes have been swept. Keep the bounded sweeper as physical reclamation only. Add before/equal/after boundary tests with retained bytes and integration coverage proving expired HTTP/tool responses are never emitted.

## Warnings

### WR-01: Repeated readiness polls leak one goroutine per permanently wedged probe

**Classification:** WARNING  
**File:** `internal/agui/readiness.go:55-75`; `internal/agui/readiness_test.go:117-149`
**Issue:** the handler now returns at two seconds, but every request starts a fresh goroutine that directly calls `probe.Check`. A check that never returns remains live after the handler clears `pending`; the next load-balancer/Compose poll starts another. The test releases its probe during cleanup and therefore proves response latency, not bounded lifetime. A permanently wedged dependency adapter can grow goroutines without bound under routine health polling.

**Fix:** track one in-flight execution per named probe and have later polls observe/timeout that same execution instead of spawning another. Ensure concrete network probes also set transport/dial deadlines. Add a repeated-poll goleak test showing a non-cooperative check leaves at most one bounded in-flight probe, not one per request.

### WR-02: Owner-export expiry hides archives but never reclaims their object-store bytes

**Classification:** WARNING  
**File:** `internal/agui/owner_export_objectstore.go:29-56`; `internal/agui/retention_api.go:155-211`; `internal/objectstore/types.go:33-60`
**Issue:** `expiresAt` is embedded in the object key and `Open` refuses reads after it, but `PutOptions` has no TTL and no owner-export row or sweep ever calls `Store.Delete`. Every export-delete archive therefore remains in Garage indefinitely after its advertised expiry, inaccessible yet still containing the deleted conversation and assets. The implementation calls this a retention window without implementing physical retention.

**Fix:** persist an owner-scoped export record with object ref and expiry and add a bounded retryable sweep that deletes the object then finalizes metadata, or configure and verify an object-store lifecycle rule for the prefix. Add an after-expiry test that asserts both access denial and `Head`/`Get` absence, including retry after delete failure.

---

_Reviewed: 2026-07-21T22:28:03Z_
_Reviewer: the agent (gsd-code-reviewer; generic-agent workaround)_  
_Depth: standard_
