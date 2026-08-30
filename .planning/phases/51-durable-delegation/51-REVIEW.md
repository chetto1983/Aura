---
phase: 51-durable-delegation
reviewed: 2026-08-30T13:29:49Z
depth: standard
files_reviewed: 173
files_reviewed_list:
  - .env.example
  - cmd/arcadedb-mcp/tool_forget_test.go
  - cmd/arcadedb-mcp/tool_memory.go
  - cmd/arcadedb-mcp/tool_memory_actor_test.go
  - cmd/arcadedb-mcp/tool_memory_retrieval_test.go
  - cmd/aura/asset_processing_worker.go
  - cmd/aura/chat_boot.go
  - cmd/aura/delegation_pause_committer.go
  - cmd/aura/main.go
  - cmd/aura/serve.go
  - cmd/aura/serve_agui.go
  - cmd/aura/serve_channels.go
  - cmd/aura/serve_delegation.go
  - cmd/aura/serve_delegation_outbox_integration_test.go
  - cmd/aura/serve_drain.go
  - cmd/aura/serve_steer.go
  - cmd/aura/swarm_status_adapter.go
  - compose.yaml
  - internal/agent/display/payload.go
  - internal/agent/display/preview.go
  - internal/agent/display/preview_test.go
  - internal/agent/display/swarm.go
  - internal/agent/display/swarm_test.go
  - internal/agent/hooks.go
  - internal/agent/idempotency_operation.go
  - internal/agent/idempotency_operation_test.go
  - internal/agent/llm_agent_dispatch.go
  - internal/agent/swarm_context.go
  - internal/agent/tools/swarm_spawn.go
  - internal/agent/tools/swarm_spawn_schema_test.go
  - internal/agent/tools/swarm_spawn_test.go
  - internal/agent/tools/swarm_status.go
  - internal/agent/trust.go
  - internal/agui/approvals_api.go
  - internal/agui/server.go
  - internal/agui/server_swarm_events.go
  - internal/agui/server_swarm_events_status.go
  - internal/agui/server_swarm_events_test.go
  - internal/agui/server_swarm_transcript.go
  - internal/agui/server_swarm_transcript_test.go
  - internal/agui/translator.go
  - internal/arcadedb/admin.go
  - internal/arcadedb/client.go
  - internal/arcadedb/concurrent_fact_write_test.go
  - internal/arcadedb/fact_authority.go
  - internal/arcadedb/fact_lock.go
  - internal/arcadedb/memory.go
  - internal/arcadedb/memory_provenance.go
  - internal/arcadedb/transaction.go
  - internal/arcadedb/write_retry.go
  - internal/askuser/store.go
  - internal/askuser/store_fencing.go
  - internal/askuser/store_fencing_test.go
  - internal/askuser/store_unit_test.go
  - internal/channels/telegram/bot.go
  - internal/channels/telegram/bot_dispatch_operation_test.go
  - internal/channels/telegram/bot_dispatch_turn.go
  - internal/config/config.go
  - internal/config/config_knobs.go
  - internal/conversations/store_append.go
  - internal/conversations/store_append_tx_test.go
  - internal/conversations/store_fakedbtx_test.go
  - internal/conversations/store_test.go
  - internal/cron/store_notifications_fake_test.go
  - internal/cron/store_runs.go
  - internal/db/db_unit_test.go
  - internal/db/migrations/0103_steer_queue.down.sql
  - internal/db/migrations/0103_steer_queue.up.sql
  - internal/db/migrations/0105_pending_notifications_steer_queue.down.sql
  - internal/db/migrations/0105_pending_notifications_steer_queue.up.sql
  - internal/db/migrations/0106_paused_states_fencing.down.sql
  - internal/db/migrations/0106_paused_states_fencing.up.sql
  - internal/db/migrations/0107_ingestion_jobs_awaiting_input.down.sql
  - internal/db/migrations/0107_ingestion_jobs_awaiting_input.up.sql
  - internal/db/migrations/0111_delegation_delivery_keys.down.sql
  - internal/db/migrations/0111_delegation_delivery_keys.up.sql
  - internal/db/queries/conversation_turns.sql
  - internal/db/queries/delegation_delivery.sql
  - internal/db/queries/ingestion_jobs.sql
  - internal/db/queries/paused_states.sql
  - internal/db/queries/pending_notifications.sql
  - internal/db/queries/steer_queue.sql
  - internal/db/sqlc/conversation_turns.sql.go
  - internal/db/sqlc/delegation_delivery.sql.go
  - internal/db/sqlc/ingestion_jobs.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/paused_states.sql.go
  - internal/db/sqlc/pending_notifications.sql.go
  - internal/db/sqlc/querier.go
  - internal/db/sqlc/steer_queue.sql.go
  - internal/documents/jobs_store.go
  - internal/documents/jobs_store_awaiting_input.go
  - internal/documents/jobs_store_batch_integration_test.go
  - internal/documents/jobs_store_delegation_delivery.go
  - internal/documents/jobs_store_test.go
  - internal/documents/jobs_worker.go
  - internal/idempotency/types.go
  - internal/mcp/sdkclient.go
  - internal/runner/interfaces.go
  - internal/runner/resume_committer.go
  - internal/runner/runner.go
  - internal/runner/runner_deps.go
  - internal/runner/runner_steer.go
  - internal/runner/worker_pause_expirer.go
  - internal/runner/worker_pause_sweep.go
  - internal/runner/worker_pause_sweep_db_test.go
  - internal/runner/worker_pause_sweep_test.go
  - internal/runner/worker_pause_test.go
  - internal/steer/inbox.go
  - internal/steer/integration_pool_helper_test.go
  - internal/steer/pg_store.go
  - internal/steer/pg_store_test.go
  - internal/steer/pg_store_unit_test.go
  - internal/steer/queue_sweep.go
  - internal/steer/queue_sweep_test.go
  - internal/steer/queue_sweep_unit_test.go
  - internal/steer/steertest/fake.go
  - internal/swarm/brief.go
  - internal/swarm/brief_context_test.go
  - internal/swarm/brief_registry_test.go
  - internal/swarm/child_staleness.go
  - internal/swarm/delegation_artifact.go
  - internal/swarm/delegation_budget_test.go
  - internal/swarm/delegation_card.go
  - internal/swarm/delegation_delivery.go
  - internal/swarm/delegation_delivery_db_test.go
  - internal/swarm/delegation_delivery_test.go
  - internal/swarm/delegation_enqueue.go
  - internal/swarm/delegation_enqueue_test.go
  - internal/swarm/delegation_fanout.go
  - internal/swarm/delegation_fanout_test.go
  - internal/swarm/delegation_idempotency_test.go
  - internal/swarm/delegation_queue.go
  - internal/swarm/delegation_queue_lifecycle_test.go
  - internal/swarm/delegation_queue_test.go
  - internal/swarm/delegation_queue_unit_test.go
  - internal/swarm/delegation_resume.go
  - internal/swarm/delegation_resume_db_test.go
  - internal/swarm/delegation_resume_observer_test.go
  - internal/swarm/delegation_resume_security_test.go
  - internal/swarm/delegation_resume_test.go
  - internal/swarm/delegation_run.go
  - internal/swarm/delegation_terminal.go
  - internal/swarm/delegation_terminal_test.go
  - internal/swarm/report.go
  - internal/swarm/runner_adapter.go
  - internal/swarm/runner_adapter_test.go
  - internal/swarm/swarm.go
  - internal/swarm/swarm_depth.go
  - internal/swarm/swarm_depth_test.go
  - internal/swarm/swarm_runtime_test.go
  - internal/swarm/swarm_test.go
  - internal/swarm/transcript_api.go
  - internal/swarm/transcript_api_test.go
  - prd.md
  - progress.md
  - scripts/coverage_package_policy.json
  - web/src/AppShell.tsx
  - web/src/chat/displays/SwarmReportTable.tsx
  - web/src/chat/displays/swarmRow.ts
  - web/src/chat/displays/types.ts
  - web/src/chat/workers/useWorkerStatuses.ts
  - web/src/chat/workers/WorkerPane.test.tsx
  - web/src/chat/workers/WorkerPane.tsx
  - web/src/chat/workers/WorkerPicker.tsx
  - web/src/chat/workers/workerStream.ts
  - web/src/chat/workers/workerWatchControls.ts
  - web/src/chat/workers/WorkerWatchProvider.test.tsx
  - web/src/chat/workers/WorkerWatchProvider.tsx
  - web/src/i18n/resources.display.ts
  - web/src/shell/useWorkerPane.test.ts
  - web/src/shell/useWorkerPane.ts
  - web/src/shell/WorkerPaneShell.tsx
findings:
  critical: 2
  warning: 1
  info: 0
  total: 3
status: issues_found
---

# Phase 51: Code Review Report

**Reviewed:** 2026-08-30T13:29:49Z
**Depth:** standard
**Files Reviewed:** 173
**Status:** issues_found

## Summary

The remediation closes the previous fan-out outbox, terminal replay, trust-boundary, atomic enqueue, pause quarantine, runtime snapshot, and conversation-isolation findings. The two remaining previous findings about at-least-once mutating tools and the exclusion of `stalled` reports are now explicitly documented as accepted boundaries in Amendment #190, so they are not repeated as defects.

The re-review found two new correctness/data-loss blockers in lease recovery and terminal artifact delivery, plus one worker-pane state regression. Phase 51 is not ready to ship until the blockers are fixed and covered with deterministic failure-window tests.

## Remediation Verification

| Previous finding | Result | Evidence |
| --- | --- | --- |
| CR-01 fan-out claim preceded durable notification | Resolved | `cmd/aura/serve_delegation.go:120-181` creates the fan-out claim and pending notification in one transaction, while the SQL repeats eligibility predicates. |
| CR-02 success report replayed after transition failure | Resolved | `internal/swarm/delegation_terminal.go:53-129` stages keyed `pending_delivery`; `internal/swarm/delegation_queue.go:286-292` performs delivery-only recovery before worker execution. |
| CR-03 lease generation changed the operation root | Accepted boundary | `prd.md:10508-10515` explicitly retains at-least-once external tool behavior after a process death. |
| CR-04 dead-letter notification could be lost | Resolved | Terminal failure uses the same durable keyed delivery-only path and remains reclaimable beyond the worker attempt cap. |
| CR-05 resumed raw tool output lost its trust envelope | Resolved | The exact nonce-framed model-facing tool message is persisted and restored by `internal/agent/hooks.go` and `internal/agent/llm_agent_dispatch.go:142-146`. |
| CR-06 worker goal could forge policy sections | Resolved | `internal/swarm/brief.go:22-42` emits static policy as `RoleSystem` and nonce-framed goal/context JSON as untrusted `RoleUser` data. |
| WR-01 multi-goal enqueue could partially succeed | Resolved | `internal/swarm/delegation_enqueue.go` builds the batch and `internal/documents/jobs_store.go` inserts it in one identity transaction. |
| WR-02 one job could use two runtime snapshots | Resolved | `internal/swarm/delegation_run.go:150-177,250-264` snapshots once and removes the live runtime from child execution. |
| WR-03 one malformed pause starved later rows | Resolved | `internal/swarm/delegation_resume.go:166-211` continues after row failures and fenced rejection quarantines malformed rows. |
| WR-04 worker watch state crossed conversations | Resolved with regression | Ownership checks now prevent cross-conversation requests, but WR-01 below breaks valid same-conversation reload restoration. |
| WR-05 `stalled` lacked a report action | Accepted boundary | `prd.md:10512-10514` defines `stalled` as an orphan transcript without a terminal report artifact. |

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: Expired running delegations bypass the retry cap indefinitely

**Classification:** BLOCKER

**Files:** `internal/db/queries/ingestion_jobs.sql:34-58`; `internal/swarm/delegation_queue.go:286-307`; `internal/swarm/delegation_queue_test.go:265-325`

**Issue:** The claim predicate admits every expired `running` delegation regardless of `attempt_count`, then increments that count on every reclaim. `processJob` skips worker execution only for `pending_delivery`; an ordinary expired row immediately calls `runWithHeartbeat` even when the reclaim moved it past `max_attempts`. If the process repeatedly dies during model/tool execution before `recordFailure` or terminal staging, the row stays `running`, is reclaimed at counts `max+1`, `max+2`, and so on, and executes again without limit. This defeats the configured transient-attempt cap and expands the accepted at-least-once side-effect window into unbounded replay. The existing max-attempt test covers a returned failure followed by `recordFailure`, not death during the final execution.

**Fix:** Preserve delivery-only reclaim beyond the cap, but route an exhausted ordinary reclaim to terminal dead-letter staging before constructing or running a worker. For the current claim accounting, a guard after pending-delivery parsing can distinguish an expired final attempt because its post-claim count is greater than the cap:

```go
if pending != nil {
	return l.deliverPending(ctx, job, payload, pending)
}
if job.AttemptCount > job.MaxAttempts {
	return l.recordFailure(ctx, job, fmt.Errorf("delegation lease expired after final attempt"))
}
```

Prefer making that recovery state explicit in the store contract rather than relying on an implicit count convention. Add a database-backed fault test that seeds an expired `running` row at `attempt_count == max_attempts` with no `pending_delivery`, verifies that no model/tool runner is invoked, and verifies one retryable dead-letter delivery followed by a terminal row.

### CR-02: The archive checkpoint can permanently discard the required full-report artifact

**Classification:** BLOCKER

**Files:** `internal/swarm/delegation_terminal.go:82-99`; `internal/swarm/delegation_artifact.go:27-48`; `internal/swarm/delegation_delivery.go:213-238`; `internal/swarm/delegation_terminal_test.go:14-36`

**Issue:** `deliverPending` persists `ArchiveAttempted=true` before calling the archiver. A process death after that checkpoint causes every retry to skip archiving. An ordinary transient archive error is also swallowed by `archiveReport`, after which the attempted flag remains final and the bounded conversation card is delivered without an artifact. Conversely, if archive creation succeeds and the process dies before `ArtifactName` is persisted, the created object becomes unreferenced and retry still skips the archive. The conversation and steer projections contain only the bounded card/report; the artifact is the required user-accessible uncapped report, so this permanently violates SWARM-12 even though the terminal queue row retains internal payload data. The test only counts checkpoint writes and has no fault injection around either boundary.

**Fix:** Make archive creation idempotent with a stable key such as `pending.DeliveryKey`, propagate archive errors into the existing delivery retry path, and persist `ArtifactName` only after successful creation. A retry after success-but-before-checkpoint must resolve the same object:

```go
if pending.ArtifactName == "" {
	name, err := l.Delivery.Archiver.ArchiveReportIdempotent(
		ctx, identityID, payload.ConversationID, pending.DeliveryKey, filename, markdown,
	)
	if err != nil {
		return l.retryPendingDelivery(ctx, job, fmt.Errorf("archive delegation report: %w", err))
	}
	pending.ArtifactName = name
	if err := l.persistPendingDelivery(ctx, job, payload, pending); err != nil {
		return err
	}
}
```

Remove `ArchiveAttempted` as a terminal uncertainty marker. Add deterministic tests for death/error before archive, archive error, and successful archive followed by checkpoint failure; every path must eventually expose exactly one referenced full-report artifact.

## Warnings

### WR-01: Conversation isolation closes a valid persisted pane before registry hydration

**Classification:** WARNING

**Files:** `web/src/shell/useWorkerPane.ts:48-70`; `web/src/chat/workers/WorkerWatchProvider.tsx:24-36`; `web/src/chat/workers/WorkerPane.tsx:54-86`; `web/src/chat/displays/SwarmReportTable.tsx:47-56`

**Issue:** The shell restores only global `open` and `childId` values, with no originating conversation. On reload the provider registry starts empty, so `WorkerPane` sees `workerOwned=false` and immediately calls `onClose` before report-table effects can register the current conversation's workers. Multiple report tables also replace the single registry rather than contributing to it, so mount order can make an older persisted worker unowned even in the correct conversation. Cross-tenant requests are blocked, but the documented/tested persisted-open behavior is lost on reload and valid workers disappear based on effect order. The focused tests mount a pre-populated provider or test shell restoration in isolation; none exercises delayed report registration or multiple report cards.

**Fix:** Persist `{conversationId, childId, open}` together and restore only when the route conversation matches. Give the provider an explicit hydration/registration-ready state and do not auto-close until current-conversation reports have registered. Store registrations by report/fan-out key and union them, with cleanup on unmount, rather than replacing one global array. Add an integration test that restores a pane before delayed current-conversation registration and another with two report tables.

## Verification

- `git diff --check 3c60158f254b5588e11f79227e223017307b0448..HEAD` passed.
- `go test ./internal/swarm ./internal/agent ./internal/documents ./internal/conversations ./internal/steer ./internal/agui ./cmd/aura` passed six packages. `./internal/agent` failed two unchanged Windows temp-path classifier tests (`TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite` and `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts`); these failures are outside the Phase 51 file scope and do not validate or invalidate the reviewed paths.
- `npx vitest run src/chat/workers/WorkerPane.test.tsx src/chat/workers/WorkerWatchProvider.test.tsx src/shell/useWorkerPane.test.ts` passed 9 tests in 3 files.
- `npm run typecheck` passed.
- Full WSL/race, live database fault injection, Playwright E2E, and deployed-container recovery were not run in this review.

---

_Reviewed: 2026-08-30T13:29:49Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
