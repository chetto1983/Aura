---
phase: 51-durable-delegation
reviewed: 2026-08-30T11:51:46Z
depth: standard
files_reviewed: 143
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
  - cmd/aura/serve_drain.go
  - cmd/aura/serve_steer.go
  - cmd/aura/swarm_status_adapter.go
  - compose.yaml
  - internal/agent/display/payload.go
  - internal/agent/display/preview.go
  - internal/agent/display/preview_test.go
  - internal/agent/display/swarm.go
  - internal/agent/display/swarm_test.go
  - internal/agent/idempotency_operation.go
  - internal/agent/idempotency_operation_test.go
  - internal/agent/swarm_context.go
  - internal/agent/tools/swarm_spawn.go
  - internal/agent/tools/swarm_spawn_schema_test.go
  - internal/agent/tools/swarm_spawn_test.go
  - internal/agent/tools/swarm_status.go
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
  - internal/db/queries/ingestion_jobs.sql
  - internal/db/queries/paused_states.sql
  - internal/db/queries/pending_notifications.sql
  - internal/db/queries/steer_queue.sql
  - internal/db/sqlc/ingestion_jobs.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/paused_states.sql.go
  - internal/db/sqlc/pending_notifications.sql.go
  - internal/db/sqlc/querier.go
  - internal/db/sqlc/steer_queue.sql.go
  - internal/documents/jobs_store.go
  - internal/documents/jobs_store_awaiting_input.go
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
  - internal/swarm/delegation_card.go
  - internal/swarm/delegation_delivery.go
  - internal/swarm/delegation_delivery_db_test.go
  - internal/swarm/delegation_delivery_test.go
  - internal/swarm/delegation_enqueue.go
  - internal/swarm/delegation_fanout.go
  - internal/swarm/delegation_idempotency_test.go
  - internal/swarm/delegation_queue.go
  - internal/swarm/delegation_queue_lifecycle_test.go
  - internal/swarm/delegation_queue_test.go
  - internal/swarm/delegation_queue_unit_test.go
  - internal/swarm/delegation_resume.go
  - internal/swarm/delegation_resume_db_test.go
  - internal/swarm/delegation_resume_observer_test.go
  - internal/swarm/delegation_resume_test.go
  - internal/swarm/delegation_run.go
  - internal/swarm/report.go
  - internal/swarm/runner_adapter.go
  - internal/swarm/runner_adapter_test.go
  - internal/swarm/swarm.go
  - internal/swarm/swarm_depth.go
  - internal/swarm/swarm_depth_test.go
  - internal/swarm/swarm_test.go
  - internal/swarm/transcript_api.go
  - internal/swarm/transcript_api_test.go
  - prd.md
  - web/src/AppShell.tsx
  - web/src/chat/displays/SwarmReportTable.tsx
  - web/src/chat/displays/swarmRow.ts
  - web/src/chat/displays/types.ts
  - web/src/chat/workers/useWorkerStatuses.ts
  - web/src/chat/workers/WorkerPane.tsx
  - web/src/chat/workers/WorkerPicker.tsx
  - web/src/chat/workers/workerStream.ts
  - web/src/chat/workers/WorkerWatchProvider.tsx
  - web/src/i18n/resources.display.ts
  - web/src/shell/useWorkerPane.ts
  - web/src/shell/WorkerPaneShell.tsx
findings:
  critical: 6
  warning: 5
  info: 0
  total: 11
status: issues_found
---

# Phase 51: Code Review Report

**Reviewed:** 2026-08-30T11:51:46Z
**Depth:** standard
**Files Reviewed:** 143
**Status:** issues_found

## Summary

The durable-delegation paths contain six ship-blocking correctness or security defects. The principal failures are non-durable fan-out claiming, non-atomic terminal delivery, retry identities that permit mutating tools to execute again, and persisted prompt history that loses trust-boundary framing. Five additional warnings affect fan-out creation, hot model-profile consistency, pause resumption fairness, and worker UI state.

This was a static standard-depth review. Tests were not run because the requested conclusion was limited to the highest-confidence findings already established.

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: Fan-out notification claims are permanent before delivery is durable

**Classification:** BLOCKER

**Files:** `D:/Repo/Aura/internal/swarm/delegation_fanout.go:90-119`, `D:/Repo/Aura/internal/db/queries/steer_queue.sql:115-130`

**Issue:** `nudgeFanout` permanently sets `nudged_at` before calling the external channel. A process crash after the update and before the channel call loses the notification. If the channel returns an error, persistence of the retry notification is itself best-effort; an insertion failure is only logged and the function still returns success. Future sweeps cannot recover either case because `nudged_at` is already set. The claim update also omits the candidate query's `drained_at IS NULL` and `expired_at IS NULL` predicates, so a row drained or expired between selection and claim can still be sent.

**Fix:** Create a durable outbox row and mark the fan-out claimed in one database transaction. Deliver from the outbox and mark that outbox item complete only after a successful channel result. Keep failed items retryable. If the current conditional update remains, repeat all eligibility predicates in it and use a leased/retryable claim rather than a permanent `nudged_at` write.

### CR-02: Success delivery is replayed when the fenced terminal transition fails

**Classification:** BLOCKER

**Files:** `D:/Repo/Aura/internal/swarm/delegation_queue.go:350-388`, `D:/Repo/Aura/internal/swarm/delegation_delivery.go:219-248`

**Issue:** `DeliverReport` archives, appends an assistant turn, and pushes a steer row before `UpdateStatus(..., "succeeded")`. A steer failure after the append, or a database/fence failure after all delivery side effects, leaves the job running. Lease expiry then reclaims and reruns the whole job, appending another card and pushing another result. None of these writes carries a durable job-level delivery key, so this produces duplicate conversation history and notifications.

**Fix:** Persist the terminal report, fenced job transition, and a job-keyed delivery/outbox record atomically. Make archive, conversation append, and steer publication idempotent on the delegation job ID (or a persisted terminal-delivery ID), and retry only the delivery state instead of rerunning the worker after execution has completed.

### CR-03: Lease generation changes the idempotency root and permits duplicate mutations

**Classification:** BLOCKER

**Files:** `D:/Repo/Aura/internal/swarm/delegation_run.go:221-247`, `D:/Repo/Aura/internal/swarm/delegation_idempotency_test.go:86-95`

**Issue:** The root operation key is `job.ID + ":" + LeaseGeneration`. A worker can complete a mutating tool call and then lose its heartbeat or crash before the job reaches a terminal state. Reclaim increments `LeaseGeneration`, creates a new operation identity, and prevents the gateway ledger from replaying the committed result. The retried worker can therefore repeat external mutations. The test explicitly requires this unsafe key change.

**Fix:** Use a stable business-operation key derived from the delegation job ID and fingerprint. Keep lease generation only in the queue ownership fence/correlation metadata. Derive deterministic child tool-call keys from the stable job operation and persisted logical call identity, then change the test to require the same operation key across lease reclaims.

### CR-04: Dead-letter delivery can be lost permanently after the row becomes terminal

**Classification:** BLOCKER

**File:** `D:/Repo/Aura/internal/swarm/delegation_queue.go:396-460`

**Issue:** `recordFailure` transitions the job to `dead_letter` before attempting to decode and deliver the terminal report. `deliverDeadLetter` then treats every decode, conversation, and steer failure as best-effort logging. Because the queue row is already terminal, there is no retry path. Operators can permanently miss exhausted-worker failures, and the transition-before-steer ordering also lets the fan-out completeness check observe zero unfinished jobs before the final result row exists.

**Fix:** Persist the dead-letter report and delivery/outbox work in the same transaction as the fenced terminal transition. Retry delivery independently until complete. Do not make terminal notification dependent on re-decoding mutable payload after the terminal state has committed.

### CR-05: Resumed workers replay untrusted tool output without its security envelope

**Classification:** BLOCKER

**Files:** `D:/Repo/Aura/internal/swarm/swarm.go:469-514`, `D:/Repo/Aura/internal/swarm/delegation_resume.go:68-92`

**Issue:** The durable history recorder stores raw `res.Preview`, while the live agent stores non-trusted tool output through a nonce-bearing untrusted-output envelope. The comments acknowledge that every non-trusted tool loses that framing. On resume, `buildResumeTurns` feeds this raw content back as ordinary `RoleTool` history. Malicious document, web, or tool content captured before `ask_user` is consequently replayed without the prompt-injection boundary enforced during the original run.

**Fix:** Persist the exact model-facing tool-result message after trust wrapping, exposed through a safe agent history/event API. Alternatively persist provenance and re-wrap every untrusted result on resume with a newly generated trusted envelope. Add a resume test with adversarial tool output and assert that raw output is never present outside the envelope.

### CR-06: Worker context can forge control sections in the execution brief

**Classification:** BLOCKER

**Files:** `D:/Repo/Aura/internal/swarm/brief.go:40-63`, `D:/Repo/Aura/internal/swarm/brief_context_test.go:73-109`

**Issue:** `structuredBrief` concatenates goal and context verbatim into the same user message as the worker's objective, tool guidance, and boundaries. Context can inject identical `## Objective`, `## Tool guidance`, or `## Boundaries` headers and supply competing instructions. The purported mitigation test proves the forged header remains as a second line-start `## Objective`; ordering does not give the first header any enforceable authority. Workers retain tools, so this is a prompt-injection path with side effects.

**Fix:** Put trusted worker policy in a higher-priority system/developer message and pass goal/context as explicitly untrusted structured data in a separate user/tool message. Preserve provenance and apply the same untrusted-content envelope used elsewhere. Replace the test with assertions that untrusted text cannot create control messages or unframed policy sections.

## Warnings

### WR-01: Multi-goal enqueue can expose a partial fan-out

**Classification:** WARNING

**File:** `D:/Repo/Aura/internal/swarm/delegation_enqueue.go:76-105`

**Issue:** Each goal is inserted independently. If goal N fails, earlier rows remain queued and claimable even though the tool returns only an error and no expected fan-out cardinality is persisted. Those workers can run and notify an incomplete fan-out.

**Fix:** Insert the complete fan-out in one transaction, or persist a fan-out parent with expected count and keep children non-claimable until every child row has been committed.

### WR-02: One delegation can combine two hot runtime snapshots

**Classification:** WARNING

**Files:** `D:/Repo/Aura/internal/swarm/delegation_run.go:250-260`, `D:/Repo/Aura/internal/swarm/swarm.go:273-278`

**Issue:** `delegationBudget` snapshots the hot runtime for loop limits, then `runChild` snapshots it again for client/model configuration. A settings publish between those calls gives one job limits from one profile and model/client/reasoning settings from another, violating the immutable per-run snapshot contract.

**Fix:** Take one runtime snapshot at the start of the claimed job and pass its client, config, reasoning settings, and derived budget together through `RunConfig`; do not call `Snapshot()` again inside `runChild` for that run.

### WR-03: One malformed answered pause starves all later resumptions

**Classification:** WARNING

**Files:** `D:/Repo/Aura/internal/swarm/delegation_resume.go:145-185`, `D:/Repo/Aura/internal/db/queries/ingestion_jobs.sql:241-261`

**Issue:** Answered parked jobs are ordered oldest-first. `ProcessOnce` returns on the first payload or fence error, leaving that same row in `awaiting_input`; it is selected first on every later pass and prevents all later answered jobs from being unparked.

**Fix:** Continue processing the remaining rows, aggregate/report errors after the batch, and quarantine or terminally resolve structurally invalid parked rows so they cannot remain a permanent poison item.

### WR-04: Worker pane state leaks across conversation changes

**Classification:** WARNING

**Files:** `D:/Repo/Aura/web/src/chat/workers/WorkerWatchProvider.tsx:17-33`, `D:/Repo/Aura/web/src/shell/useWorkerPane.ts:48-66`, `D:/Repo/Aura/web/src/AppShell.tsx:410-415`

**Issue:** The provider remains mounted when `activeThreadId` changes, but its worker list is not reset. The pane's watched child and open state are global local-storage values with no conversation key. Switching threads can therefore show workers from the prior conversation and request the old child ID from the new conversation's AG-UI endpoint.

**Fix:** Scope worker lists and watched-child state by conversation ID. Reset/close the pane when the conversation changes, or key the provider by conversation and persist a composite conversation/child selection.

### WR-05: The UI excludes a terminal `stalled` worker from report actions

**Classification:** WARNING

**Files:** `D:/Repo/Aura/web/src/chat/displays/swarmRow.ts:60-81`, `D:/Repo/Aura/internal/swarm/report.go:19-24`

**Issue:** The backend status vocabulary treats `stalled` as a terminal outcome, but `isTerminalSwarmStatus` recognizes only `ok`, `failed`, and `dead_letter`. Report affordances guarded by this helper remain unavailable for stalled workers even when their terminal artifact/card exists.

**Fix:** Include `stalled` in the terminal predicate and add a UI test covering report access for every backend terminal status.

---

_Reviewed: 2026-08-30T11:51:46Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
