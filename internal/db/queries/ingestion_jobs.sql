-- The GENERIC asset queue. Document, image and audio uploads all ride these statements.
--
-- This file was document_control_plane.sql until 2026-08-17, when the four statements that
-- gave it that name — CreateDocument, GetDocumentBySearchID, DeleteDocumentTags,
-- UpsertDocumentTag — went with the document catalog they wrote (migration 0098). The queue
-- was always the other half of the file and is the only half with callers.
--
-- The job rows lost document_id and version_id in the same migration. They were nullable and
-- measured NULL on every row in the deployment: the queue dispatches by MODALITY through
-- ProcessorSet.For and reads its subject out of `payload`, so it never had a use for a link
-- into a catalog.

-- name: CreateIngestionJob :one
INSERT INTO aura.ingestion_jobs (
    identity_id, job_type, asset_id, status,
    idempotency_key, stage, max_attempts, next_attempt_at, payload,
    pipeline_generation
) VALUES (
    sqlc.arg(identity_id), sqlc.arg(job_type), sqlc.narg(asset_id), sqlc.arg(status),
    sqlc.arg(idempotency_key), sqlc.arg(stage), sqlc.arg(max_attempts),
    sqlc.arg(next_attempt_at), sqlc.arg(payload), sqlc.arg(pipeline_generation)
)
ON CONFLICT (identity_id, job_type, idempotency_key) DO UPDATE
SET updated_at = now()
RETURNING *;
-- name: ClaimIngestionJobs :many
-- job_type is an OPTIONAL filter (sqlc.narg): NULL (the document ingestion
-- worker's own call) claims across every job_type exactly as before this
-- filter was added -- zero behavior change for that caller. A non-NULL value
-- (the swarm delegation claim loop, job_type='swarm_delegation') scopes the
-- claim so a background-delegation claim loop and the document ingestion
-- worker can run concurrently against the SAME table without ever stealing
-- each other's lease (Phase 51, SWARM-09).
WITH candidates AS (
    SELECT queued_job.id, queued_job.status AS prior_status
    FROM aura.ingestion_jobs queued_job
    WHERE queued_job.identity_id = sqlc.arg(identity_id)
      AND queued_job.attempt_count < queued_job.max_attempts
      AND (sqlc.narg(job_type)::text IS NULL OR queued_job.job_type = sqlc.narg(job_type)::text)
      AND (
        (queued_job.status = 'queued' AND queued_job.next_attempt_at <= now()
         AND (queued_job.locked_until IS NULL OR queued_job.locked_until < now()))
        OR (queued_job.status = 'running' AND queued_job.locked_until < now())
      )
    ORDER BY queued_job.next_attempt_at, queued_job.created_at
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
), claimed AS (
    UPDATE aura.ingestion_jobs job
    SET status = 'running',
        locked_by = sqlc.arg(locked_by),
        locked_until = now() + sqlc.arg(lease_duration)::interval,
        attempt_count = attempt_count + 1,
        lease_generation = lease_generation + 1,
        completed_at = NULL,
        updated_at = now()
    FROM candidates candidate
    WHERE job.id = candidate.id
    RETURNING job.*, candidate.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, detail, pipeline_generation, attempt_generation, lease_generation
    )
    SELECT claimed.identity_id, 'ingestion_job', claimed.id, claimed.id,
           claimed.prior_status, 'running',
           CASE WHEN claimed.prior_status = 'running' THEN 'job_lease_reclaimed'
                ELSE 'job_claimed' END,
           '{}'::jsonb, claimed.pipeline_generation, claimed.attempt_generation,
           claimed.lease_generation
    FROM claimed
    RETURNING job_id
)
SELECT claimed.id, claimed.job_type, claimed.status, claimed.idempotency_key, claimed.stage, claimed.attempt_count, claimed.max_attempts, claimed.locked_by, claimed.locked_until, claimed.next_attempt_at, claimed.payload, claimed.error_code, claimed.error_message, claimed.created_at, claimed.updated_at, claimed.completed_at, claimed.identity_id, claimed.asset_id, claimed.pipeline_generation, claimed.attempt_generation, claimed.lease_generation FROM claimed
JOIN events ON events.job_id = claimed.id
ORDER BY claimed.next_attempt_at, claimed.created_at;
-- name: HeartbeatIngestionJob :one
UPDATE aura.ingestion_jobs
SET locked_until = now() + sqlc.arg(lease_duration)::interval,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation)
  AND locked_until > now()
RETURNING *;
-- name: UpdateIngestionJobStatus :one
WITH target AS (
    SELECT target_job.id, target_job.status AS prior_status
    FROM aura.ingestion_jobs target_job
    WHERE target_job.id = sqlc.arg(id)
      AND target_job.identity_id = sqlc.arg(identity_id)
      AND target_job.status = 'running'
      AND target_job.locked_by = sqlc.arg(locked_by)
      AND target_job.lease_generation = sqlc.arg(lease_generation)
    FOR UPDATE
), transitioned AS (
    UPDATE aura.ingestion_jobs job
    SET status = sqlc.arg(status), stage = sqlc.arg(stage),
        error_code = sqlc.arg(error_code), error_message = sqlc.arg(error_message),
        locked_by = NULL, locked_until = NULL,
        completed_at = CASE WHEN sqlc.arg(status)::text IN (
            'succeeded', 'failed', 'dead_letter', 'canceled'
        ) THEN now() ELSE NULL END,
        updated_at = now()
    FROM target WHERE job.id = target.id
    RETURNING job.*, target.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, message, detail, trace_id,
        pipeline_generation, attempt_generation, lease_generation
    )
    SELECT transitioned.identity_id, 'ingestion_job', transitioned.id, transitioned.id,
           transitioned.prior_status, transitioned.status, sqlc.arg(event_type),
           sqlc.arg(event_message), sqlc.arg(event_detail), sqlc.arg(trace_id),
           transitioned.pipeline_generation, transitioned.attempt_generation, transitioned.lease_generation
    FROM transitioned
    RETURNING job_id
)
SELECT transitioned.id, transitioned.job_type, transitioned.status, transitioned.idempotency_key, transitioned.stage, transitioned.attempt_count, transitioned.max_attempts, transitioned.locked_by, transitioned.locked_until, transitioned.next_attempt_at, transitioned.payload, transitioned.error_code, transitioned.error_message, transitioned.created_at, transitioned.updated_at, transitioned.completed_at, transitioned.identity_id, transitioned.asset_id, transitioned.pipeline_generation, transitioned.attempt_generation, transitioned.lease_generation FROM transitioned JOIN events ON events.job_id = transitioned.id;
-- name: RetryIngestionJob :one
WITH target AS (
    SELECT target_job.id, target_job.status AS prior_status FROM aura.ingestion_jobs target_job
    WHERE target_job.id = sqlc.arg(id) AND target_job.identity_id = sqlc.arg(identity_id)
      AND target_job.status = 'running' AND target_job.locked_by = sqlc.arg(locked_by)
      AND target_job.lease_generation = sqlc.arg(lease_generation)
    FOR UPDATE
), retried AS (
    UPDATE aura.ingestion_jobs job
    SET status = 'queued', stage = sqlc.arg(stage),
        error_code = sqlc.arg(error_code), error_message = sqlc.arg(error_message),
        locked_by = NULL, locked_until = NULL,
        next_attempt_at = sqlc.arg(next_attempt_at), updated_at = now()
    FROM target WHERE job.id = target.id
    RETURNING job.*, target.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, message, pipeline_generation, attempt_generation, lease_generation
    )
    SELECT retried.identity_id, 'ingestion_job', retried.id, retried.id,
           retried.prior_status, 'queued', 'job_retry_scheduled',
           sqlc.arg(event_message), retried.pipeline_generation,
           retried.attempt_generation, retried.lease_generation
    FROM retried
    RETURNING job_id
)
SELECT retried.id, retried.job_type, retried.status, retried.idempotency_key, retried.stage, retried.attempt_count, retried.max_attempts, retried.locked_by, retried.locked_until, retried.next_attempt_at, retried.payload, retried.error_code, retried.error_message, retried.created_at, retried.updated_at, retried.completed_at, retried.identity_id, retried.asset_id, retried.pipeline_generation, retried.attempt_generation, retried.lease_generation FROM retried JOIN events ON events.job_id = retried.id;
-- name: CountIngestionJobsByStatus :one
SELECT count(*) FROM aura.ingestion_jobs
WHERE identity_id = sqlc.arg(identity_id) AND status = sqlc.arg(status);
-- name: ParkIngestionJobAwaitingInput :execrows
-- 51-06b (SWARM-06 SC#4, Task 1): a claim-loop worker's AwaitingInput report parks its
-- row instead of succeeding, failing or dead-lettering. The conditional UPDATE (status
-- must still be 'running', owned by the SAME lease_generation the claim minted) IS the
-- atomicity gate the caller relies on: RowsAffected==0 means the lease was already lost
-- (reclaimed/expired) between the claim and this call, and the caller MUST NOT also
-- write a pause -- a pause with no parked row would be answered into nothing (Task 1's
-- own atomicity requirement). payload is REPLACED wholesale (not merged): the caller
-- computes the full updated map (the original DelegationPayload plus its new `resume`
-- sub-object) in Go and passes it whole, mirroring CreateIngestionJob's own plain
-- sqlc.arg(payload) rather than a jsonb `||` merge. attempt_count is untouched
-- deliberately -- a human being asked a question is not a failed attempt.
UPDATE aura.ingestion_jobs
SET status = 'awaiting_input',
    locked_by = NULL,
    locked_until = NULL,
    payload = sqlc.arg(payload),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation);
-- name: UnparkIngestionJob :execrows
-- 51-06b (Task 2): the resume observer's un-park, once the worker's pause has been
-- answered. The conditional UPDATE (status must still be 'awaiting_input') IS the
-- idempotency key -- RowsAffected==1 for exactly one caller; a second observer pass, or
-- an observer racing a concurrent one, un-parks zero rows. payload is REPLACED wholesale
-- with the caller's already-merged DelegationResumeState (History + the operator's
-- AnswerContent), so the SHIPPED ClaimIngestionJobs loop that claims this row next reads
-- everything the resume rebuild needs with no second read of aura.paused_states.
-- attempt_count stays untouched -- an answered question is not a retry.
UPDATE aura.ingestion_jobs
SET status = 'queued',
    next_attempt_at = now(),
    payload = sqlc.arg(payload),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'awaiting_input';
-- name: ResolveIngestionJobAwaitingInput :execrows
-- 51-06b (Task 3, D-08 extended to the queue row): an unanswered worker pause expires
-- and its parked row must not be left waiting for a human who never came. `failed` (not
-- dead_letter) is the terminal state: dead_letter means "retried to exhaustion", and a
-- human declining to answer is a different outcome from the worker's own retry budget
-- running out. The conditional UPDATE (status must still be 'awaiting_input') is the
-- idempotency key -- a second sweep pass, or a sweep racing an operator's late answer
-- that already un-parked the row, resolves zero rows.
UPDATE aura.ingestion_jobs
SET status = 'failed',
    error_code = 'awaiting_input_expired',
    error_message = sqlc.arg(error_message),
    completed_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'awaiting_input';
-- name: ListAnsweredAwaitingInputJobs :many
-- 51-06b (Task 2): the resume observer's read -- which parked jobs may now resume. The
-- join crosses into aura.paused_states (RLS-scoped to the SAME identity_id predicate
-- below via db.WithIdentityTx's app.current_identity carrier) rather than living in
-- paused_states.sql: this is the OBSERVER's own concern (which parked job, read from the
-- ingestion_jobs side), and internal/askuser stays untouched by this plan. The join key is
-- the pause TOKEN this specific park cycle minted (job.payload->'resume'->>'pause_token'),
-- not owning_worker_id alone -- a job can pause more than once across its lifetime
-- (resume, do more work, pause again), and owning_worker_id (= the job's own id) would be
-- identical across every one of those pauses, so only the fresh token disambiguates THIS
-- park cycle's pause from an older, already-resumed one.
SELECT job.id, job.identity_id, job.payload,
       pause.token AS pause_token, pause.pending_action_id, pause.resumed_answer
FROM aura.ingestion_jobs job
JOIN aura.paused_states pause
  ON pause.token = (job.payload->'resume'->>'pause_token')::uuid
WHERE job.identity_id = sqlc.arg(identity_id)
  AND job.status = 'awaiting_input'
  AND pause.resumed_at IS NOT NULL
ORDER BY job.created_at
LIMIT sqlc.arg(row_limit);
-- aura.ingestion_events has no standalone statement of its own for the shipped
-- fenced/terminal transitions above CreateIngestionJob/ClaimIngestionJobs/
-- UpdateIngestionJobStatus/RetryIngestionJob: every one of those writes an `events` CTE
-- row so the timeline can never disagree with the transition it records. The three
-- 51-06b :execrows statements above (Park/Unpark/Resolve) deliberately do NOT, matching
-- the plan's own :execrows shape (a plain conditional UPDATE, mirroring
-- MarkPausedStateResumedFenced) rather than a CTE -- a park/un-park/expire cycle is
-- already fully reconstructable from aura.paused_states' own created_at/resumed_at and
-- the job's updated_at, so this is a documented scope boundary, not an oversight.
