---
phase: 39-idempotency-observability-pack
reviewed: 2026-07-21T21:04:44Z
depth: standard
files_reviewed: 91
files_reviewed_list:
  - .env.example
  - .github/workflows/ci.yml
  - cmd/aura/chat_boot.go
  - cmd/aura/idempotency.go
  - cmd/aura/main.go
  - cmd/aura/retention.go
  - cmd/aura/serve.go
  - cmd/aura/serve_dispatch.go
  - cmd/aura/serve_lifecycle.go
  - cmd/aura/serve_readiness_test.go
  - compose.yaml
  - internal/activelearn/bounded_seen.go
  - internal/activelearn/learner.go
  - internal/agent/llm_agent.go
  - internal/agent/llm_agent_retry.go
  - internal/agent/mcptools/bridge.go
  - internal/agent/mcptools/bridge_reconnect.go
  - internal/agent/mcptools/bridge_reconnect_mutation_test.go
  - internal/agent/metrics.go
  - internal/agent/tools/spec.go
  - internal/agui/conversations_api.go
  - internal/agui/idempotency_http.go
  - internal/agui/owner_export.go
  - internal/agui/readiness.go
  - internal/agui/readiness_test.go
  - internal/agui/retention_api.go
  - internal/agui/server.go
  - internal/config/config_learning.go
  - internal/config/config_retention.go
  - internal/cron/dispatch.go
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
  - internal/db/observability.go
  - internal/db/queries/idempotency_operations.sql
  - internal/db/queries/retention_operations.sql
  - internal/db/sqlc/idempotency_operations.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/querier.go
  - internal/gateway/idempotency_test.go
  - internal/gateway/reserve.go
  - internal/idempotency/context.go
  - internal/idempotency/fingerprint.go
  - internal/idempotency/store.go
  - internal/idempotency/store_integration_test.go
  - internal/idempotency/types.go
  - internal/learningretention/compactor.go
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
  - internal/obs/meter.go
  - internal/readiness/state.go
  - internal/readiness/state_test.go
  - internal/reasoninglearn/learner.go
  - internal/reasoningstore/store.go
  - internal/redact/string.go
  - internal/retention/engine.go
  - internal/retention/local.go
  - internal/runner/runner.go
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
findings:
  critical: 8
  warning: 2
  info: 0
  total: 10
status: issues_found
---

# Phase 39: Code Review Report

**Reviewed:** 2026-07-21T21:04:44Z  
**Depth:** standard  
**Files Reviewed:** 91  
**Status:** issues_found

## Summary

The phase adds substantial registry, retention, compaction, readiness, and observability machinery, but several production boundaries do not actually preserve the promised behavior. The most serious defects are missing HTTP/CLI idempotency enforcement, strict-profile operation-scope failures, an export-delete path that can delete data not present in any durable export, and retention/replay paths that turn crash or expiry states into stranded or falsely successful operations.

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: HTTP mutation metadata is never installed, so direct mutations remain non-idempotent and strict agent mutations are denied

**Classification:** BLOCKER  
**File:** `internal/agui/idempotency_http.go:32-35`; `cmd/aura/chat_boot.go:261`; `internal/gateway/reserve.go:39-44`  
**Issue:** `httpMutationRoutes`, `parseIdempotencyKey`, and `writeIdempotencyDecision` are only referenced by tests and their own validation helper. No production AG-UI handler wraps a request with `idempotency.WithOperation`, and the only production `WithOperation` call sites are CLI preparation and scheduler dispatch. At the same time, `assembleChatEnv` enables the operation registry globally and `beginOperation` fails closed when a mutating tool has no operation context. Consequently, direct HTTP mutation handlers ignore `Idempotency-Key` and can repeat effects, while strict-profile `/agent/run` mutations reach the gateway without an operation and are denied as `operation context missing`.

**Fix:** Add one production mutation middleware at route registration that authenticates first, buffers/normalizes the typed mutation intent, constructs the trusted operation, calls `Begin`, projects every non-acquired decision, and only invokes the handler on `acquired`. Ensure `/agent/run` carries a derived tool operation into the gateway, then add an integration test against the real mux (not helper-only tests) proving one effect for a duplicate request.

### CR-02: The CLI prints operation keys but never acquires or completes the durable registry

**Classification:** BLOCKER  
**File:** `cmd/aura/idempotency.go:77-117`; `cmd/aura/main.go:52-58`  
**Issue:** `prepareCLIIdempotency` only places an `Operation` value in a process-global context. It does not open the registry or call `Begin`, `Complete`, or `MarkIndeterminate`. Most inventoried mutating commands are dispatched through argument-only functions and never consume `cliInvocationContext` at all. A user is therefore told an `operation-key` is safe to retry even though commands such as `config set`, identity operations, chat mutations, and database mutations can execute again.

**Fix:** Route every inventoried CLI mutation through a common execution wrapper backed by `idempotency.Store`: acquire before dispatch, replay/deny non-acquired decisions, complete only after a successful effect, and mark ambiguous failures indeterminate. Make the command inventory register executable adapters, not metadata only, and add subprocess/integration tests that reuse a key across two real invocations.

### CR-03: Scheduler operations cannot authorize mutating agent tools because their scopes can never match

**Classification:** BLOCKER  
**File:** `internal/cron/dispatch.go:174-207`; `internal/gateway/reserve.go:43-49`  
**Issue:** every scheduled handler receives one operation with scope `scheduler.run`. `agent_job` forwards that same context to `LlmAgent`, but mutating built-in tools declare `agent.tool` (and MCP mutations declare `mcp.tool`). `beginOperation` requires exact equality between the context scope and `spec.OperationScope`, so a scheduled agent's mutating call is deterministically denied with `operation metadata mismatch`. This breaks the supported headless agent-job execution path in strict profiles.

**Fix:** Treat the scheduler operation as the parent logical run and derive a child operation per mutating tool call using the immutable task/run identity plus canonical tool intent and the tool's finite scope. Preserve the parent correlation separately. Add a strict-profile `agent_job` integration test that executes a harmless mutating fake exactly once and replays it on retry.

### CR-04: Export-delete releases the snapshot lock and deletes into an ephemeral response-only archive

**Classification:** BLOCKER  
**File:** `internal/agui/owner_export.go:116-122`; `internal/agui/owner_export.go:219-235`; `internal/agui/retention_api.go:112-145`  
**Issue:** `Export` releases `ExportSnapshot.Release` when it returns. `ExportDelete` then calls the destructive lifecycle after that release, so a concurrent turn can append data between the archived snapshot and deletion; those new bytes are deleted without being exported. Production also publishes the archive only to a request-local `MemoryExportDestination`. Deletion occurs before the handler copies that memory to the client, so a disconnect or write failure leaves the conversation deleted and no durable archive from which the owner can recover.

**Fix:** Hold the conversation's exclusion lock across snapshot, durable publish, verification, and the conditional delete, with a version checked by the delete transaction. Publish to durable owner-scoped object storage with a retention period and return a resumable export identifier/URL; do not delete based on a request-local memory buffer. Test a concurrent append and a client disconnect after delete authorization.

### CR-05: Retention apply recomputes the live plan before loading the persisted operation, preventing crash recovery

**Classification:** BLOCKER  
**File:** `internal/retention/engine.go:126-139`  
**Issue:** `Apply` rebuilds candidates from current external state and requires the supplied token to match that new snapshot before it calls `GetByToken`. If a process crashes after deleting an artifact and recording `ArtifactRemoved`, but before metadata/item finalization, the artifact is absent from the new candidate scan and the plan token changes. The durable item that was explicitly designed to resume from `artifact_result=removed` can no longer be claimed, permanently stranding the operation. Ordinary candidate churn between `plan` and `apply` has the same effect.

**Fix:** Resolve the immutable persisted operation by token first and claim its stored items. Use per-item revalidation/version checks to reject changed resources; do not require the current global candidate set to reproduce the old token. If freshness authorization is required, persist an expiry/version on the plan and validate that explicit policy instead. Add a crash test that stops after `RecordArtifact` and successfully resumes the same token with the external bytes already absent.

### CR-06: Local retention deletes a replacement artifact without rechecking its version

**Classification:** BLOCKER  
**File:** `internal/retention/local.go:99-121`  
**Issue:** the engine compares `Revalidation.Version` to the planned candidate, then calls `Remove`. `Remove` performs a new `Lstat`, but checks only for a symlink and ignores `localVersion(info)`. A file or directory replaced/updated between revalidation and removal is deleted even though it no longer matches the authorized plan. This is a direct data-loss race on active trace/crash artifacts.

**Fix:** Make validation and removal one adapter-owned conditional operation. At minimum compare the second `Lstat` version with `candidate.Version` and return a typed retry conflict; preferably use a trusted directory handle plus an atomic rename-to-quarantine/conditional metadata protocol so replacement cannot cross the deletion boundary. Add a test that swaps the candidate after `Revalidate` and proves the replacement survives.

### CR-07: Expired or non-200 replays are projected as a fabricated HTTP 200 success

**Classification:** BLOCKER  
**File:** `internal/idempotency/store.go:130-142`; `internal/db/queries/idempotency_operations.sql:99-112`; `internal/agui/idempotency_http.go:119-125`  
**Issue:** replay expiry clears body, preview, and sidecar while leaving the operation `completed`. `readExistingDecision` still returns `DecisionReplay` with an empty result, and the HTTP projector always writes status 200. Even before expiry, the replay model stores no original HTTP status or safe headers, so a 201/202/204 response is changed on retry. After expiry, clients receive an empty apparent success for a result the service no longer possesses; the gateway similarly decodes an empty replay as a zero-value successful `ToolResult`.

**Fix:** Persist a bounded replay envelope containing the original status and allowlisted headers. Distinguish `completed_result_expired` from a valid replay and project it as an explicit terminal result-unavailable response (without re-executing), never as success. Reject `DecisionReplay` when all replay representations are absent, and cover post-GC HTTP/tool retries.

### CR-08: Central log redaction skips error-valued attributes and can leak credentials

**Classification:** BLOCKER  
**File:** `internal/obs/init.go:146-151`; `internal/redact/string.go:10-30`  
**Issue:** `redactAttr` sanitizes only `slog.KindString`. The codebase normally logs failures as `"err", err`, which is a `KindAny` value, so connection errors containing database URLs, HTTP userinfo, bearer tokens, or API keys bypass the central redactor entirely. This contradicts the process-wide redaction boundary and exposes credentials through JSON logs/collectors.

**Fix:** Normalize `error`, `fmt.Stringer`, and resolved `slog.LogValuer` values through `redact.String` before emission, recursively handling groups. Extend token patterns to common credential keys (including password/secret forms) and add tests using `slog.Any("err", errors.New("postgres://user:pass@host/db"))` and nested groups.

## Warnings

### WR-01: The readiness budget does not bound a probe that ignores cancellation

**Classification:** WARNING  
**File:** `internal/agui/readiness.go:51-64`  
**Issue:** all probes receive a two-second context, but the handler unconditionally calls `wg.Wait()`. Any injected or future probe that fails to honor `ctx.Done()` hangs `/readyz` indefinitely, despite the documented shared hard budget. A wedged readiness handler can in turn wedge Compose/load-balancer health decisions.

**Fix:** collect results with a `select` on the results channel and `ctx.Done()` and return `dependency_unavailable` at the deadline without waiting for non-cooperative probes. Keep probe goroutines unable to block on result delivery and add a test with a check that never returns.

### WR-02: Per-bucket compaction permanently starves buckets after the first page

**Classification:** WARNING  
**File:** `internal/learningretention/neo4j_store.go:50-56`; `internal/learningretention/compactor.go:86-90`  
**Issue:** `Buckets` always returns the first alphabetically ordered `BucketLimit` values, and `CompactBatch` has no cursor or rotation state. When more than 128 learned buckets exist, every scheduled pass revisits the same first page and later buckets never receive the promised per-bucket cap enforcement. The global cap does not guarantee an individual late bucket is bounded by `BucketCap`.

**Fix:** page by a stable cursor (`bucket > $after`) persisted or advanced across passes, or rotate a deterministic start point, and continue until every bucket is eventually visited. Add a fixture with more than `BucketLimit` buckets and an over-cap bucket on the second page.

---

_Reviewed: 2026-07-21T21:04:44Z_  
_Reviewer: the agent (gsd-code-reviewer; generic-agent workaround)_  
_Depth: standard_
