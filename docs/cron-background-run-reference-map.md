# Cron And Background Run Reference Map

This map supports ADR-032 and the Phase 8 refactor. It exists so future Aura
work can find the code evidence, external scheduler references, and example
patterns without relying on chat history.

## Decision

Cron is a scheduled entrypoint, not an execution runtime.

Every due schedule must become a durable fire record with an idempotency key,
then enter the same `RunRequest` or workflow-backed execution path as chat,
silent jobs, tools, and swarm.

## Aura Evidence

- `D:/Aura/internal/cron/scheduler.go`
  - Current scheduler scans `DueTasks`, calls a `Dispatcher`, and then writes
    `MarkFired`.
  - Useful detector shape, but the direct dispatcher is the part to remove.

- `D:/Aura/internal/cron/store.go`
  - Current durable state is schedule-centric: `scheduled_tasks` stores
    `next_run_at`, `last_run_at`, `last_error`, `last_output`,
    `last_metrics_json`, and `wake_signature`.
  - Missing: one row per scheduled occurrence, idempotency key, attempt state,
    run/workflow id, overlap state, missed/coalesced state.

- `D:/Aura/internal/telegram/scheduler_handlers.go`
  - Current dispatcher sends reminders, runs wiki maintenance, and runs
    scheduled agent jobs directly from Telegram bot methods.
  - Good behavior to preserve: agent jobs already have constrained tool access,
    `propose_only` write policy, `wake_if_changed`, `notify`, metrics, and
    `RunTaskNow`.
  - Refactor target: these become normal run/workflow handlers, not cron
    callbacks.

- `D:/Aura/internal/agent/tools/registry/scheduler.go`
  - The model-facing task tool already narrows operations to `schedule`,
    `list`, `cancel`, and `run_now`.
  - Good guard: `every_minutes` minimum is five minutes to avoid cost loops.

- `D:/Aura/internal/db/migrations`
  - `scheduled_tasks` is already durable and indexed by `(status, next_run_at)`.
  - Migration target: add `scheduled_task_fires` or map fires into workflow
    steps with equivalent fields.

## Local Examples

- `D:/tmp/picobot/cmd/picobot/main.go`
  - Cron fires are routed back through `chat.Inbound`, which is the correct
    architectural shape for Aura.
  - Rejected part: `D:/tmp/picobot/internal/cron/scheduler.go` is in-memory and
    cannot satisfy durability, missed-run, retry, or audit requirements.

- `D:/tmp/nanobot/nanobot/cron/service.py`
  - Useful ideas: workspace-scoped cron store, JSON job state, file lock,
    action log, run history, corrupt-store preservation, and manual run support.
  - Rejected part: callback execution still makes cron own too much work for
    Aura's run/event model.

- `D:/tmp/elysia/elysia/api/app.py`
  - Uses `AsyncIOScheduler` for maintenance loops with deliberately staggered
    intervals.
  - Rejected part: this is fine for API housekeeping, but not enough for
    scheduled agent runs with ownership, delivery, retries, and user-visible
    observability.

- `D:/tmp/llm_wiki/src/lib/scheduled-import.ts`
  - Useful ideas for source-watch jobs: active run IDs, `scanning` guard, md5
    change detection, explicit stop semantics, and skipped-file logging.
  - Refactor target: adapt these guard ideas to Aura source-ingest background
    jobs, but route execution through workflow and run events.

## External Sources

- Kubernetes CronJob docs:
  - <https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/>
  - Adopt: explicit missed-run deadline, concurrency policy, scheduled timestamp,
    idempotent jobs.
  - Avoid: approximate scheduling must not be hidden from Aura observability.

- Temporal schedule protocol docs:
  - <https://api-docs.temporal.io/>
  - Adopt: overlap policy, catch-up window, backfill, pause-on-failure,
    action results, missed-catchup and overlap metrics.
  - Avoid: depending on Temporal as a core service before Aura has its local
    workflow semantics clean.

- Celery periodic task docs:
  - <https://docs.celeryq.dev/en/4.4.1/userguide/periodic-tasks.html>
  - Adopt: only one scheduler should own a schedule at a time, or duplicates
    happen.

- Quartz CronTrigger and Trigger docs:
  - <https://www.quartz-scheduler.org/documentation/quartz-2.5.x/tutorials/tutorial-lesson-06.html>
  - <https://www.quartz-scheduler.org/api/2.5.x/org/quartz/Trigger.html>
  - Adopt: misfire policy is a first-class design decision.
  - Avoid: unlimited catch-up bursts.

## Target Contract

Minimum schedule fire fields:

- `schedule_id`
- `schedule_version`
- `scheduled_at`
- `detected_at`
- `fire_id`
- `idempotency_key`
- `owner_principal_id`
- `delegated_actor_id`
- `delivery_mode`
- `capability_grant_snapshot`
- `run_id` or `workflow_id`
- `status`
- `attempt_count`
- `last_error`

Default policies:

- Missed recurring runs coalesce to the latest due fire inside the catch-up
  window.
- One-shot runs fire once if still inside grace; otherwise they become missed.
- Overlap defaults to forbid per schedule.
- Parallel fires require explicit idempotent read-only policy and max
  concurrency.
- Retries reuse the same fire id and idempotency key.
- Cancellation preserves history and prevents future fires.
- Notifications go through `run_outbox`, never direct cron sends.

## Verification Fixtures

Add tests for:

- stale one-shot inside grace fires once;
- stale one-shot outside grace becomes missed;
- recurring downtime coalesces older occurrences;
- overlapping due fire is coalesced or skipped by policy;
- retry uses the same fire id and idempotency key;
- schedule cancellation prevents future fires but preserves prior fire rows;
- in-flight cancellation appends run/workflow cancellation events;
- reminder delivery is produced through outbox, not direct channel send;
- agent job is submitted as a delegated actor with expected capability grants;
- wiki maintenance runs as workflow-backed maintenance, not a cron callback.
