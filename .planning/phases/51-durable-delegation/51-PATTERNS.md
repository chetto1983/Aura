# Phase 51: Durable delegation - Pattern Map

**Mapped:** 2026-08-27
**Files analyzed:** 18 source files (10 modify, 5 create, 3 schema/migration) + 9 test targets
**Analogs found:** 17 / 18 source files have a strong same-repo analog; 2 test targets already
exist and need no new file (see "Already Covered — Do Not Re-plan").

**Read this before the File Classification table:** almost every "closest analog" in this phase
is `internal/documents/jobs_store.go` + `jobs_worker.go` (SWARM-09's queue), the swarm package's
own existing concern-split (`swarm.go`/`brief.go`/`swarm_depth.go`/`report.go`), or `internal/runner`'s
40-file concern-split (`resume_committer.go`, `runner_*.go`). This phase does not need a new
architectural shape anywhere — it needs the existing shapes extended, and PATTERNS.md is
organized to make that copy-paste-able.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/agent/tools/swarm_spawn.go` (modify) | tool/controller | request-response | itself — `Spec()`/`Execute()` shape already established | exact (self) |
| `internal/swarm/brief.go` (modify) | utility (string transform) | transform | itself — `structuredBrief` | exact (self) |
| `internal/swarm/swarm.go` (modify, background branch) | service/orchestrator | event-driven + CRUD (enqueue) | `internal/documents/jobs_worker.go` for the claim-loop shape; itself for the sync branch | role-match (new leg), exact (existing leg) |
| **NEW** `internal/swarm/delegation_queue.go` | service (claim-loop worker) | CRUD + event-driven | `internal/documents/jobs_worker.go` (`IngestionJobWorker.ProcessOnce`/`handleWithHeartbeat`) | exact |
| `internal/swarm/swarm_depth.go` (modify) | utility (guard) | transform | itself — `checkDepth`/`maxDepth` | exact (self) |
| `internal/swarm/report.go` (modify, minor) | utility (file I/O) | file-I/O | itself — `dumpTranscript` | exact (self) |
| `internal/steer/inbox.go` (modify, becomes interface) | model/types | CRUD | `internal/documents/jobs_store.go`'s split of model-types-vs-store (conceptually); `internal/askuser/store.go`'s `Store` wrapping sqlc queries | role-match |
| **NEW** `internal/steer/pg_store.go` | store | CRUD + TTL sweep | `internal/documents/jobs_store.go` (`PostgresIngestionJobStore`, `withIdentity` helper) for the CRUD half; `internal/runner/approval_expiry.go` for the sweep half | exact (composite of two analogs) |
| `internal/agent/llm_agent_steer.go` (modify, minor) | service (wiring) | transform | itself — `markSteer`/`drainSteer` already correct | exact (self, no new logic) |
| **NEW** `internal/runner/worker_pause_sweep.go` (or similar) | service (sweep) | batch | `internal/runner/approval_expiry.go` (`ExpirePendingApprovals`) | exact |
| `cmd/arcadedb-mcp/tool_memory.go` (modify, D-10) | controller (MCP tool handler) | request-response | itself — `memoryUpsertFactHandler`/`canonicalSubject` | exact (self) |
| `internal/arcadedb/memory.go` (modify, D-10/D-11) | service | CRUD | itself — `UpsertFact` | exact (self) |
| **NEW** `internal/arcadedb/fact_authority.go` | service (extracted concern) | transform (authorization check) | `internal/runner`'s pattern of extracting one concern per file (`resume_policy.go` beside `runner.go`) | role-match |
| Migration: `aura.paused_states` ADD COLUMN (D-12 fencing) | migration | schema | `internal/db/migrations/0088_document_machine_card.up.sql` (ALTER TABLE ADD COLUMN with COMMENT ON COLUMN) | exact |
| Migration: new steer/delegation-result table (D-06/D-07) | migration | schema | `aura.ingestion_jobs`'s own original migration shape (lease/TTL columns) — read via `internal/db/queries/ingestion_jobs.sql` for the column set to mirror | role-match |
| `internal/documents/jobs_store.go` (read-only reference; likely unmodified) | store | CRUD | n/a — this IS the analog for #2/#3 above | n/a |
| `internal/config/config.go` (modify, minor) | config | transform | itself — the three swarm caps already declared at the cited lines | exact (self) |

## Pattern Assignments

### `internal/swarm/delegation_queue.go` (NEW — service/claim-loop worker)

**Primary analog:** `internal/documents/jobs_worker.go` (272 LOC) — this is the INVENTORY-BEFORE-INVENTION
target named explicitly by the phase's constraints. Do not invent a new claim-loop shape; copy
this one and swap the handler.

**Imports pattern** (`internal/documents/jobs_worker.go:1-9`):
```go
package documents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)
```
The new file needs `internal/documents` itself (to reuse `IngestionJobQueue`/`ClaimIngestionJobsRequest`
etc. — this phase generalizes the SAME table via a new `job_type`, not a new package), plus
`internal/agent`, `internal/agent/tools`, `internal/gateway`, `internal/llm` for building the
worker `LlmAgent` exactly as `runChild` does.

**Core claim-loop pattern** (`internal/documents/jobs_worker.go:71-100`, `IngestionJobWorker.ProcessOnce`):
```go
func (w *IngestionJobWorker) ProcessOnce(ctx context.Context) (int, error) {
	if w == nil || w.Store == nil {
		return 0, fmt.Errorf("ingestion job worker has no store")
	}
	if w.IdentityID == "" {
		return 0, fmt.Errorf("ingestion job worker has no identity")
	}
	jobs, err := w.Store.Claim(ctx, ClaimIngestionJobsRequest{
		IdentityID:    w.IdentityID,
		WorkerID:      w.workerID(),
		LeaseDuration: w.leaseDuration(),
		BatchSize:     w.batchSize(),
	})
	if err != nil {
		return 0, err
	}
	w.recordQueueDepth(ctx)
	processed := 0
	for _, job := range jobs {
		if job.IdentityID != w.IdentityID {
			return processed, fmt.Errorf("claimed ingestion job %q has unexpected identity %q", job.ID, job.IdentityID)
		}
		if err := w.processJob(ctx, job); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
```
The delegation claim-loop's `processJob` equivalent replaces `handler.HandleIngestionJob(ctx, job)`
with "build an `agent.LlmAgent` from `job.Payload` exactly as `runChild` does, run it, and map the
outcome to `UpdateStatus`/`Retry`". The heartbeat-during-handle pattern below is REQUIRED reading
before writing this: it is the mechanism the new claim loop must reuse to avoid Pitfall 3
(a hard-coded 120s/240s assumption reclaiming a live worker) — the heartbeat, not the lease
duration, is what keeps a long-running delegation alive.

**Heartbeat-while-handling pattern** (`internal/documents/jobs_worker.go:184-229`):
```go
func (w *IngestionJobWorker) handleWithHeartbeat(ctx context.Context, job IngestionJob, handler IngestionJobHandler) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatResult := make(chan error, 1)
	go func() {
		heartbeatResult <- w.maintainLease(runCtx, cancel, job)
	}()
	handlerErr := handler.HandleIngestionJob(runCtx, job)
	cancel()
	heartbeatErr := <-heartbeatResult
	if errors.Is(handlerErr, ErrIngestionJobCompletionCommitted) {
		return handlerErr
	}
	if heartbeatErr != nil {
		return heartbeatErr
	}
	return handlerErr
}

func (w *IngestionJobWorker) maintainLease(ctx context.Context, cancel context.CancelFunc, job IngestionJob) error {
	interval := w.leaseDuration() / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_, err := w.Store.Heartbeat(ctx, HeartbeatIngestionJobRequest{
				IdentityID: job.IdentityID, JobID: job.ID, WorkerID: w.workerID(),
				LeaseGeneration: job.LeaseGeneration, LeaseDuration: w.leaseDuration(),
			})
			if err == nil {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			cancel()
			return err
		}
	}
}
```
**Where D-03's staleness finding attaches:** RESEARCH.md's Architecture Patterns section says
the liveness tick belongs in `runChild`'s existing `for ev, err := range worker.Run(ic)` loop
(`internal/swarm/swarm.go:195`), not in a fixed ticker like the one above. That loop sees every
worker event; the new claim-loop worker's per-attempt goroutine should call `Heartbeat` from
INSIDE that event loop (on every event, or every Nth event) rather than on the `jobs_worker.go`
fixed-interval ticker — copy the ticker's SHAPE (a goroutine racing the handler, `context.WithCancel`
to stop it, a buffered result channel) but change the tick SOURCE.

**Failure/retry/dead-letter pattern** (`internal/documents/jobs_worker.go:130-155`):
```go
func (w *IngestionJobWorker) recordHandlerFailure(ctx context.Context, job IngestionJob, cause error) error {
	if errors.Is(cause, ErrIngestionJobLeaseLost) {
		return cause
	}
	message := cause.Error()
	if job.MaxAttempts > 0 && job.AttemptCount >= job.MaxAttempts {
		if _, err := w.Store.UpdateStatus(ctx, w.transitionRequest(job, ingestionJobStatusDeadLetter,
			"handler_failed", message, "ingestion_job.dead_letter")); err != nil {
			return err
		}
		return nil
	}
	if _, err := w.Store.Retry(ctx, RetryIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: w.workerID(),
		LeaseGeneration: job.LeaseGeneration, Stage: job.Stage,
		ErrorCode: "handler_failed", ErrorMessage: message, EventMessage: message,
		NextAttemptAt: w.now().Add(w.retryDelay(job.AttemptCount)),
	}); err != nil {
		return err
	}
	return nil
}
```
This is the "8x delivery failure → dead_letter" behavior spike 100 measured as already correct
(D-01). The delegation claim-loop reuses this exact branch on `job_type='swarm_delegation'`
without change — do not write a second retry/backoff implementation.

**Claim predicate to reuse verbatim** (`internal/db/queries/ingestion_jobs.sql`, verified live
in spike 100):
```sql
SELECT * FROM aura.ingestion_jobs
WHERE identity_id = :identity_id
  AND (
    (status = 'queued' AND next_attempt_at <= now()
     AND (locked_until IS NULL OR locked_until < now()))
    OR (status = 'running' AND locked_until < now())
  )
  AND attempt_count < max_attempts
ORDER BY next_attempt_at, created_at
FOR UPDATE SKIP LOCKED
```

**The delegated-dispatch reservation marker** (`internal/swarm/swarm.go:183-193`, `runChild`) —
MANDATORY on every LlmAgent the new claim loop builds, or Pitfall 2 (orphaned reservation) recurs:
```go
ic := agent.InvocationContext{
	Ctx:       gateway.WithDelegatedDispatch(ctx),
	RequestID: uuid.Must(uuid.NewV7()),
	Budget:    budget,
}
```
Read `internal/gateway/delegated_reservation.go` in full before wiring this — `WithDelegatedDispatch`
only helps if `closeDelegatedReservation` is actually reached at both terminal hooks, and the
comment block there (lines 1-27) explains exactly why a claim-loop-driven worker needs it (no
Runner ever observes its event stream).

**Error handling idiom (domain rejection vs. wiring bug)** — from `internal/agent/tools/swarm_spawn.go:94-112`
(`Execute`), the established split this file's own enqueue path must follow:
```go
// Domain rejection (model-readable, NOT a Go error):
if e.MaxGoals > 0 && len(a.Goals) > e.MaxGoals {
	return NewResult(ctx, fmt.Sprintf(
		"error: too many goals — %d exceeds the AURA_SWARM_MAX_GOALS cap of %d; ...",
		len(a.Goals), e.MaxGoals))
}
// Wiring bug (real Go error, composition-root's fault, never the model's):
if e.Runner == nil {
	return ToolResult{}, fmt.Errorf("swarm_spawn: runner is not configured")
}
```

---

### `internal/agent/tools/swarm_spawn.go` (MODIFY — SWARM-01/02)

**Analog:** itself. The file already has every seam this phase needs; extend, do not rewrite.

**Current args/schema shape** (lines 47-64) — SWARM-01 adds a `context` field beside `goals`:
```go
type swarmSpawnArgs struct {
	Goals []string `json:"goals"`
}
```
and the hardcoded `json.RawMessage` params literal (lines 53-64) is what SWARM-02 must stop being
static — render it from `e.MaxGoals`/config caps at `Spec()` call time instead of as a literal.

**The tool-package interface seam that MUST NOT be broken** (lines 9-18):
```go
// swarmRunner is the engine seam swarm_spawn delegates to. It is defined HERE (in
// the tools package, NOT internal/swarm or internal/agent) so swarm_spawn.go
// imports neither — breaking the cycle tools→swarm→agent→tools.
type swarmRunner interface {
	Run(ctx context.Context, goals []string) (ToolResult, error)
}
```
Any new delegation seam (e.g. a "enqueue vs. run synchronously" branch) MUST extend THIS interface
signature (e.g. add a `context string` or a `background bool` parameter) rather than defining a
second interface or importing `internal/swarm` directly — the concrete adapter stays wired at
`cmd/aura`, unchanged in kind.

---

### `internal/swarm/swarm.go` (MODIFY, background branch — SWARM-03/04/05)

**Analog:** itself, but READ THE FILE-SIZE WARNING FIRST. At 245/600 LOC, RESEARCH.md's own
File Targets table flags this as "Watch" — the recommendation is to land the enqueue/background
branch in the NEW `delegation_queue.go` file above, not inline in `Run()`. The established
precedent for this exact move (extract a new concern file rather than grow the package's main
file) is the package's OWN existing shape:

**The existing swarm-package split-by-concern precedent** (already in this package, cite this as
the "<name>_<concern>.go" pattern the planner should extend, not invent):
- `swarm.go` (245 LOC) — orchestration: `Run`, `preflight`, `runWave`, `runChild`
- `brief.go` (49 LOC) — worker prompt construction only
- `swarm_depth.go` (38 LOC) — depth-guard only
- `report.go` (85 LOC) — transcript I/O + report marshaling only

A `delegation_queue.go` (enqueue + claim-loop) is the SAME kind of split this package already
uses four times. Do not merge the new concern into `swarm.go`.

**Sync-leg reuse (SWARM-04/05):** `runChild` (lines 162-232) already IS the synchronous worker
constructor a nested delegation reuses; SWARM-05 is not new orchestration code, it is removing
one line's restriction:
```go
// internal/swarm/swarm.go:169 (inside runChild)
Registry: tools.Without(rc.ParentRegistry, swarmSpawnTool),
```
RESEARCH.md's Common Pitfalls section is explicit that lifting this restriction reopens the
exact SWARM-08 defect class if the re-entry check anywhere still keys on `OperationScope` alone
— read `internal/agent/idempotency_operation.go` (below) before touching this line.

**Depth-guard degrade-vs-reject decision (SWARM-05):** current `checkDepth` (`swarm_depth.go:33-38`)
hard-rejects at the cap. hermes' alternative (named in CONTEXT.md/RESEARCH.md) is `role:
leaf|orchestrator` degrading to `leaf` past the cap rather than rejecting outright — if the
planner adopts that framing, it is a `swarm_depth.go` change (still ≤38→~60 LOC, no split needed),
not a `swarm.go` change.

---

### `internal/steer/pg_store.go` (NEW — D-06/D-07/D-08)

**Two analogs, composed:**

1. **CRUD/lease shape** — `internal/documents/jobs_store.go`'s `PostgresIngestionJobStore` (see
   full read above): the `withIdentity` wrapper pattern is the one to copy for a Postgres-backed
   steer/delegation-result table scoped by `identity_id`:
```go
// internal/documents/jobs_store.go:252-260
func (s *PostgresIngestionJobStore) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("ingestion job store is not configured")
	}
	if identityID == "" {
		return fmt.Errorf("ingestion job identity id is required")
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}
```

2. **The interface it must satisfy, unchanged** — `internal/agent/llm_agent_steer.go:19-21`:
```go
type SteerInbox interface {
	Drain(conv string) []steer.Message
}
```
This is the WHOLE reason the blast radius is contained (per RESEARCH.md's Risk Register on D-06):
`*steer.Inbox` (the in-memory type) and the new Postgres-backed type both just need to satisfy
this one-method interface (plus whatever `Push` signature the HTTP/Telegram callers use — check
`internal/steer/inbox.go`'s `Push` signature, lines 104-129, and preserve it exactly). Do NOT
widen this interface for the new implementation's convenience; adapt the new type to it.

3. **TTL sweep shape** — `internal/runner/approval_expiry.go` in full (41 LOC, read above) is
   the sweep-without-driving-a-turn precedent D-08 explicitly says to follow:
```go
func (r *Runner) ExpirePendingApprovals(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if r.approvalExpiry == nil {
		return 0, fmt.Errorf("expire pending approvals: expiry store is not configured")
	}
	due, err := r.approvalExpiry.ListExpiredPendingApprovals(ctx, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("expire pending approvals: %w", err)
	}
	expired := 0
	for _, pending := range due {
		resp := ResponseInput{Action: askuser.ActionExpired, Content: expiredApprovalContent}
		claim := r.resumeClaim(pending.Token, pending, resp)
		if err := r.resumeCommitter.CommitResume(ctx, claim); err != nil {
			if errors.Is(err, askuser.ErrPauseNotFound) {
				continue
			}
			return expired, fmt.Errorf("expire pending approval %s: %w", pending.Token, err)
		}
		expired++
		if err := r.applyResumeHook(ctx, pending, resp); err != nil {
			return expired, fmt.Errorf("expire pending approval %s hook: %w", pending.Token, err)
		}
	}
	return expired, nil
}
```
The row-kind-typed sweep D-07 asks for (one sweep, two TTL knobs read by `kind`) is this same
shape with the cutoff computed per-row from `kind` instead of a single fixed cutoff. D-08's "leave
a readable trace, in the same transaction" requirement is exactly what this function already does
for approvals (`CommitResume` + `applyResumeHook` write the expiry outcome and the conversation
turn together) — do not invent a second commit-then-notify shape.

**Model-only, not schema:** the `steer.SourceWorker` constant (`internal/steer/inbox.go:61`,
`= "swarm"`) and `markSteer`'s branch on it (`internal/agent/llm_agent_steer.go:97-102`) are
ALREADY CORRECT and need zero changes — the new Postgres-backed `Message` row must simply
preserve the `Source` column faithfully through `Push`→storage→`Drain`. Pitfall 5 in RESEARCH.md
names the regression this file could reintroduce if `Source` is dropped or defaulted anywhere in
the new store.

---

### `internal/runner/worker_pause_sweep.go` (NEW, sibling — D-08's per-worker-pause TTL leg, if separate from D-06's steer table)

Same analog as above (`internal/runner/approval_expiry.go`), same reasoning: RESEARCH.md's File
Targets table explicitly recommends a NEW sibling file rather than modifying `approval_expiry.go`
in place, because that file is approval-specific by name and by doc comment.

---

### D-12/D-13: fencing column + per-worker pause (SWARM-06)

**This is the single most important finding in this pattern map: the "level identity" D-13 asks
for ALREADY PARTIALLY EXISTS on `aura.paused_states`.** Read `internal/db/queries/paused_states.sql`
in full before designing a new column. Every paused-state query already selects:
```sql
resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id
```
`proxied_from_child_id`/`proxied_tool_call_id` (`internal/askuser/store.go:82-118`,
`internal/agent/tools/ask_user.go:77-99`) already carry "the flat worker id (`w1`..`wN`) that
this pause relays from" and "that child's originating tool_call id" — but for the SYNCHRONOUS
existing flow, where the PARENT re-offers the child's `ask_user` via its OWN pause
(`internal/swarm/swarm.go`'s `ChildReport{Status: StatusNeedsUserInput, ...}` →
`optionLabels`/D-05 proxy). This is attribution of a RELAYED pause, not a per-worker PERSISTED
pause the worker itself owns and can be resumed into directly (which is what D-12/D-13 for a
BACKGROUND worker need). **Do not conflate the two:** the planner should decide explicitly
whether the background case reuses `proxied_from_child_id` (extending its meaning) or adds a
genuinely new fencing column, and state which — RESEARCH.md's "Missing are the fencing id (D-12)
and the level identity (D-13)" was written without this column in view and may be stronger than
warranted; a plan that claims level identity as pure greenfield is not accurate.

**The conditional-update-as-idempotency-key idiom to reuse verbatim for D-12's fencing UPDATE**
(`internal/db/queries/paused_states.sql:59-64`, `MarkPausedStateResumed`):
```sql
-- name: MarkPausedStateResumed :execrows
UPDATE aura.paused_states
SET resumed_at = now(),
    resumed_answer = $2
WHERE token = $1
  AND resumed_at IS NULL;
```
and its Go caller (`internal/askuser/store.go:356-378`, `MarkResumed`):
```go
func (s *Store) MarkResumed(ctx context.Context, token string, ans ResumeAnswer) error {
	id, err := db.ParseUUID("token", token)
	if err != nil {
		return fmt.Errorf("mark resumed: %w", err)
	}
	answer, err := encodeAnswer(ans)
	if err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		n, mErr := q.MarkPausedStateResumed(ctx, sqlc.MarkPausedStateResumedParams{Token: id, ResumedAnswer: answer})
		if mErr != nil {
			return mErr
		}
		if n == 0 {
			return ErrPauseNotFound
		}
		return nil
	}); err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	return nil
}
```
D-12's `expectActionId` fencing is the SAME shape with one more `AND pending_action_id = $N`
clause added to the `WHERE`, per CONTEXT.md's own worked example:
```sql
UPDATE aura.paused_states
  SET resumed_at = now(), ...
  WHERE pause_id = $1 AND pending_action_id = $2 AND resumed_at IS NULL
```

**The batch/sorted-token deadlock-free claim to extend, not replace** (`internal/runner/resume_committer.go:63-81`,
`CommitResumeBatch` — read the full 204-line file before writing the new argument; this excerpt
is the shape, not the whole contract):
```go
// CommitResumeBatch claims ALL pauses (MarkResumedBatchTx claims in sorted-token order →
// two concurrent overlapping batches lock rows in the same order, no 40P01 deadlock)
func (p *PoolResumeCommitter) CommitResumeBatch(ctx context.Context, claims []ResumeClaim) (err error) {
	...
	if err := p.pause.MarkResumedBatchTx(ctx, q, answers); err != nil {
		...
	}
	...
}
```
Per CONTEXT.md's folded `approval-resume-defects` constraint: "new validation goes inside that
transaction's front door, never as a second path around it" — the D-12 fencing check belongs
INSIDE `MarkResumedBatchTx`'s per-token loop (`internal/askuser/store.go:389-410`), not as a
pre-check before `CommitResumeBatch` is called.

**Test analog for the fencing/claim-before-append discipline** — `internal/runner/resume_committer_test.go:39-67`
(`TestSplitResumeCommitter_CommitResume_ClaimsBeforeAppend`) is the shape a new
`TestPerWorkerPauseFencing`-style test should copy: seed a pending pause via the in-memory fake,
commit once, assert exactly one turn appended, commit AGAIN with the same claim, assert
`ErrPauseNotFound` and NO second turn. For D-12 the analogous test adds a MISMATCHED
`pending_action_id`/fencing id on the second attempt (not just a repeat), proving a stale decision
cannot resume a since-repurposed pause.

**Migration shape to copy** (`internal/db/migrations/0088_document_machine_card.up.sql`, ALTER
TABLE ADD COLUMN with a documenting comment):
```sql
ALTER TABLE aura.paused_states
    ADD COLUMN IF NOT EXISTS pending_action_id text;

COMMENT ON COLUMN aura.paused_states.pending_action_id IS
    'Fencing id (D-12): mirrors the value the worker/run considers its current pending '
    'action. A resume must supply the SAME value or the conditional UPDATE matches zero '
    'rows -- a stale decision cannot resume a pause that has since moved on.';
```
Per CLAUDE.md's imperative rule, `ls internal/db/migrations/ | tail -1` was `0102_paused_state_decision_policy`
at pattern-mapping time (2026-08-27) — the next slot (provisionally `0103`) MUST be re-derived at
the actual migration-creation point, never copied from this document.

---

### D-10/D-11: `cmd/arcadedb-mcp/tool_memory.go` + `internal/arcadedb/memory.go`

**D-10 analog — the exact host-side-rewrite placement to land beside** (`cmd/arcadedb-mcp/tool_memory.go:104-126`,
`memoryUpsertFactHandler`):
```go
targetFactKey := strings.TrimSpace(in.SupersedesFactKey)
// MEM-04 (D-19): rewritten here, before arcadedb.Fact is built, so the
// bridge, the CLI and host-driven writes are all covered -- the bridge
// alone (withMemoryUserIdentifier) would miss the latter two.
subject := canonicalSubject(in.Subject, identity, operatorDisplayName)
fact := arcadedb.Fact{
	Subject:     subject,
	...
	Source: arcadedb.FactSource{
		RunID: in.Source.RunID, MemoryIDs: in.Source.MemoryIDs,
	},
}
```
D-10 changes exactly this last block: `RunID` stops coming from `in.Source.RunID` (model-supplied,
`jsonschema:"required"` per `MemoryFactSource.RunID` at line 20) and instead comes from a
host-derived value read off `ctx`/the handler's closure — mirroring how `identity` itself already
arrives via `resolveCaller(ctx, tenants, req)` two lines above `canonicalSubject`, NOT from an
input field. The correct pattern is "closure over an ambient identity", i.e.:
```go
// D-10 target shape (not yet written): RunID stops being a field on
// MemoryUpsertFactInput.Source; it is derived the same way `identity` already is,
// from resolveCaller / ctx, and passed to arcadedb.FactSource{RunID: hostDerivedRunID, ...}.
```
**Input struct today** (`cmd/arcadedb-mcp/tool_memory.go:17-21`, `MemoryFactSource`):
```go
type MemoryFactSource struct {
	RunID     string   `json:"run_id" jsonschema:"the extraction or operator run supporting this fact"`
	MemoryIDs []string `json:"memory_ids,omitempty" jsonschema:"message or memory ids supporting this fact"`
}
```
D-10 removes `RunID` from this model-facing struct (or keeps the field accepted-but-ignored — the
plan MUST pick one per RESEARCH.md's Risk Register and document the choice, since CONTEXT.md does
not specify which).

**D-11 analog — where the supersede-authority check attaches** (`internal/arcadedb/memory.go:265-286`,
inside `UpsertFact`):
```go
factKey := factIdentity(fact)
written := FactWrite{Statement: fact.Statement}
if attached, err := c.attachFactSource(ctx, factKey, fact.Source, now); err != nil {
	return FactWrite{}, err
} else if attached {
	return written, nil
}
if fact.Supersedes {
	outcome, err := c.closeSuperseded(ctx, fact, factKey, validFrom, now)
	if err != nil {
		return FactWrite{}, err
	}
	if outcome.Refused {
		return FactWrite{
			Statement:  fact.Statement,
			Refused:    true,
			Reason:     outcome.Reason,
			Candidates: outcome.Candidates,
		}, nil
	}
	written.Superseded = outcome.Closed
}
```
D-9's dedup (`attachFactSource` short-circuit) is UNCHANGED and needs no work. D-11's new check
goes immediately before the existing `if fact.Supersedes {` branch: if the actor (host-derived,
same context source as D-10's RunID) is a worker, return the model-readable refusal INSTEAD of
calling `closeSuperseded` — following the exact same "domain rejection rides in the result, not
a Go error" idiom already shown for `swarm_spawn.go` above. Since `UpsertFact` returns
`(FactWrite, error)` rather than a `tools.ToolResult`, the model-readable shape here is
`FactWrite{Refused: true, Reason: "workers may not supersede a fact; ..."}` — reuse the EXISTING
`Refused`/`Reason`/`Candidates` fields `closeSuperseded` already populates for its own ambiguity
refusal (lines 108-124, `FactWrite` doc comment), not a new field.

**File-size-driven split (D-11):** `internal/arcadedb/memory.go` is 543/600 LOC. Extract the new
authority check into `internal/arcadedb/fact_authority.go` rather than adding it inline — this
mirrors `internal/runner`'s own precedent of pulling one policy/authorization concern into its
own file beside the orchestration file (e.g. `resume_policy.go` beside `runner.go`,
`resume_committer.go` as its own file rather than inlined into `runner.go`). `UpsertFact` in
`memory.go` then calls one function from `fact_authority.go`, keeping the growth off the
already-tight file.

---

### `internal/agent/idempotency_operation.go` — Pitfall 1 regression guard (READ, likely no new code)

**Why it is in this map even though RESEARCH.md marks SWARM-08 MOSTLY DONE:** any new nested- or
delegated-dispatch code path this phase adds (the claim-loop worker, a lifted depth restriction)
must be checked against this fix, not just the pre-existing swarm path. The load-bearing
guard (`internal/agent/idempotency_operation.go:41-60`):
```go
func deriveToolOperationContext(ctx context.Context, spec tools.Spec, args json.RawMessage) (context.Context, error) {
	parent, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return ctx, nil
	}
	toolFingerprint, err := tools.OperationFingerprint(spec, args)
	if err != nil {
		return nil, err
	}
	// Passthrough is a RE-ENTRY guard, so it keys on the whole identity of the call
	// (scope AND fingerprint), never on scope alone.
	if parent.Key.Scope == spec.OperationScope && parent.Fingerprint == toolFingerprint {
		return ctx, nil
	}
	...
}
```
**Existing regression test to extend, not duplicate** — `internal/agent/idempotency_operation_test.go:209`,
`TestDeriveToolOperationContextDerivesForNestedToolCall`, already pins this fix. A new test for
the claim-loop worker's own dispatch path should follow this test's shape (build two operations
sharing `OperationScopeAgent`, assert the derived child key differs), not re-derive the assertion
style from scratch.

---

## Shared Patterns

### Tool-package interface seam (breaking the tools→swarm→agent→tools cycle)
**Source:** `internal/agent/tools/swarm_spawn.go:9-18` (`swarmRunner`), concrete adapter wired at
`cmd/aura`.
**Apply to:** Any new model-facing surface this phase touches that needs to call into
`internal/swarm` or `internal/runner` — define the narrow interface in the CALLER's package
(mirrors `SteerInbox` in `internal/agent/llm_agent_steer.go:19-21` and `ChannelDeliverer` in
`internal/cron/deliver.go:22-27` — this idiom appears at least three times already in the tree,
it is not swarm-specific).
```go
type swarmRunner interface {
	Run(ctx context.Context, goals []string) (ToolResult, error)
}
```

### Conditional-update-as-idempotency-key
**Source:** `internal/db/queries/paused_states.sql:59-64` (`MarkPausedStateResumed :execrows`) +
`internal/askuser/store.go:356-378` (`MarkResumed`).
**Apply to:** D-12's fencing UPDATE, and any new claim/lease transition the delegation queue adds
beyond what `internal/documents/jobs_store.go` already provides.
```sql
UPDATE aura.paused_states
SET resumed_at = now(), resumed_answer = $2
WHERE token = $1
  AND resumed_at IS NULL;
```
```go
n, mErr := q.MarkPausedStateResumed(ctx, sqlc.MarkPausedStateResumedParams{Token: id, ResumedAnswer: answer})
if mErr != nil { return mErr }
if n == 0 { return ErrPauseNotFound }
```

### Model-readable domain rejection vs. real Go error
**Source:** `internal/agent/tools/swarm_spawn.go:94-112` (`Execute`).
**Apply to:** `internal/swarm/delegation_queue.go`'s enqueue rejection paths, D-11's worker-supersede
refusal in `internal/arcadedb/fact_authority.go`.
```go
// Domain rejection — model self-corrects, no Go error:
return NewResult(ctx, "error: too many goals — ...")
// Wiring bug — real error, composition root's fault:
return ToolResult{}, fmt.Errorf("swarm_spawn: runner is not configured")
```

### Host-side rewriting at the MCP boundary, before the domain type is built
**Source:** `cmd/arcadedb-mcp/tool_memory.go:104-107` (`canonicalSubject`, called before
`arcadedb.Fact{}` is constructed).
**Apply to:** D-10's `RunID` host-derivation — same file, same placement (immediately before the
`fact := arcadedb.Fact{...}` literal), so the bridge, the CLI and host-driven writes are all
covered by the one change, per the MEM-04/D-19 precedent this phase explicitly follows.

### Delegated-dispatch reservation marker (whoever opens a ledger row closes it)
**Source:** `internal/gateway/delegated_reservation.go` (full file, 119 LOC) + its call site
`internal/swarm/swarm.go:183-193` (`runChild`).
**Apply to:** Every `agent.InvocationContext` the new `delegation_queue.go` claim-loop worker
builds — this is not optional; omitting it reproduces the exact defect fixed in `791dcd7e0`.
```go
ic := agent.InvocationContext{
	Ctx:       gateway.WithDelegatedDispatch(ctx),
	RequestID: uuid.Must(uuid.NewV7()),
	Budget:    budget,
}
```

### Package-internal concern-split ("<name>_<concern>.go"), not a growing monolith
**Source (in-package precedent):** `internal/swarm/{swarm.go,brief.go,swarm_depth.go,report.go}`.
**Source (larger precedent, same idiom at scale):** `internal/runner/{runner.go,runner_compact.go,
runner_context.go,runner_conversation.go,runner_delete.go,runner_delete_reconcile.go,runner_deps.go,
runner_fastpath.go,runner_history.go,runner_interrupted.go,runner_mint_approval.go,runner_overhead.go,
runner_persist.go,resume_committer.go,resume_policy.go}` — 40+ files in one package, each one
concern, none of them re-deriving the others' state.
**Apply to:** `internal/swarm/swarm.go` (extract `delegation_queue.go` for the enqueue/claim-loop
concern) and `internal/arcadedb/memory.go` (extract `fact_authority.go` for D-11's authority check)
— both are named at-risk in RESEARCH.md's File Targets table and both have a same-package
precedent to copy rather than inventing a naming convention.

---

## Test Analogs (Wave 0)

| New Test File | Covers | Closest Analog | What To Copy |
|---|---|---|---|
| `internal/swarm/brief_context_test.go` (or extend `brief_test.go` if it exists) | SWARM-01 goal/context split | `internal/swarm/report_test.go` (`TestDumpTranscriptPath`) for file-local unit-test shape; assert both `briefObjective` and a new context section marker appear via `strings.Contains`, mirroring the load-bearing-literal assertion style already used for `briefObjective`/`briefOutput`/`briefTools`/`briefBoundaries` | Table-free direct-call unit test; assert exact section markers present, goal text isolated from context text |
| `internal/agent/tools/swarm_spawn_schema_test.go` (or extend existing `swarm_spawn_test.go`) | SWARM-02 live-cap schema rendering | none inside this file yet — nearest analog is any existing `Spec()`-json-decode test elsewhere in `internal/agent/tools` (grep `json.Unmarshal.*Spec().Parameters` in that package before writing a new decode helper) | Decode `Spec().Parameters` as JSON, assert the live `MaxGoals`/`SwarmChildTimeoutSec`/`MaxSwarmConcurrent` values appear in the schema/description text, not a hardcoded number |
| `internal/swarm/delegation_queue_test.go` (`db_integration`) | SWARM-03/09 enqueue + REAL claim/reclaim path | `internal/documents/jobs_worker_test.go` (513 LOC) for the worker-loop test shape; `internal/documents/integration_pool_helper_test.go` for the disposable-DB bootstrap | Copy `pipelineDisposablePool`'s `aura`/`aura_migrate`/`aura_app` three-role DSN composition VERBATIM (per the `db-integration-must-run-as-aura-app` project rule) rather than inventing a new bootstrap; this is what closes spike 100's own stated gap (the Go claim path was never exercised under a real crash) |
| `internal/runner/worker_pause_test.go` (`db_integration`) | SWARM-06 fencing + per-worker pause | `internal/runner/resume_committer_test.go` (156 LOC, `TestSplitResumeCommitter_CommitResume_ClaimsBeforeAppend` / `_CommitResumeBatch_ClaimsAllBeforeAppendAll`) | Copy the seed-pause / commit-once / commit-again-expect-`ErrPauseNotFound` shape; add a THIRD case with a mismatched fencing id to prove D-12's `expectActionId` semantics specifically (not just double-resume) |
| `internal/arcadedb/concurrent_fact_write_test.go` (`race` + `arcadedb_integration`) | SWARM-07 genuine goroutine fan-out | `internal/swarm/swarm_test.go:205-236` (`TestSwarmParallelTiming`) for the "N concurrent goroutines against one shared fake/real backend, assert no cross-contamination" shape, PLUS `internal/swarm/swarm_test.go`'s `defer goleak.VerifyNone(t)` convention (every test in this file uses it) | N real goroutines (not a sequential `for` loop — RESEARCH.md's Sampling Rate section is explicit that a sequential loop passing green would hide the actual hazard) calling `UpsertFact` concurrently against the SAME `factKey`; assert exactly one fact, N sources (D-9 dedup) |
| Live driver script (SC#1-#5) | Full E2E, model output verified past raw SSE | `.planning/spikes/098-steer-carries-worker-result/` and `.planning/spikes/099-worker-duration-and-progress/` `drive.sh`-style scripts (named in RESEARCH.md's Wave 0 Gaps and Sources) | Reassemble `TEXT_MESSAGE_CONTENT` deltas into the final message before asserting compliance/refusal (Pitfall 4) — a raw `grep` on the SSE stream is explicitly named as producing false verdicts in spike 098 |

### Already Covered — Do Not Re-plan

Two Wave 0 items RESEARCH.md marked "Check — likely added" are CONFIRMED to already exist; do not
create new files for these:
- **SWARM-08 regression guard:** `internal/agent/idempotency_operation_test.go:209`,
  `TestDeriveToolOperationContextDerivesForNestedToolCall`, already pins the `67d24aee4` fix.
- **SWARM-10 live-append transcript coverage:** `internal/swarm/report_test.go:53-103`,
  `TestDumpTranscriptPath`, already covers append-mode + best-effort-swallow-on-error for
  `dumpTranscript`. Any SWARM-10 work this phase does (an operator-facing exposure endpoint, per
  RESEARCH.md) is NEW work needing a NEW test for the exposure surface, not a rewrite of this one.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| Migration for D-06/D-07 (new steer/delegation-result table) | migration | schema | No existing table in this tree combines "one table, two row kinds, two TTL knobs" — `aura.ingestion_jobs` is the closest lease-queue shape but has no row-kind discriminator; the column SET should be designed fresh (kind, ttl-relevant timestamp(s), payload) informed by, not copied from, `ingestion_jobs`' lease columns (a delegation result does not need `locked_by`/`lease_generation` — it is not claimed/leased, it is delivered-once via `Drain`) |
| SWARM-02's live-render-into-schema mechanism itself | tool/controller | request-response | No existing `Spec()` in this codebase renders config values into its JSON Parameters at call time (every other tool's `Spec()` returns a `json.RawMessage` literal) — this is genuinely new plumbing; the Open Question in RESEARCH.md about NOT conflating this with the Tier A/B knob-catalog test is the relevant caution, not a missing analog for the mechanism |

## Metadata

**Analog search scope:** `internal/documents/` (jobs_store.go, jobs_worker.go, tests),
`internal/swarm/` (all files), `internal/steer/`, `internal/agent/` (llm_agent_steer.go,
idempotency_operation.go, swarm_context.go), `internal/runner/` (approval_expiry.go,
resume_committer.go, resume_committer_test.go, runner_multipause_test.go, interfaces.go),
`internal/askuser/store.go`, `internal/gateway/delegated_reservation.go`,
`internal/db/queries/paused_states.sql`, `internal/db/queries/ingestion_jobs.sql`,
`internal/db/migrations/` (0088, 0102 for shape reference), `cmd/arcadedb-mcp/tool_memory.go`,
`internal/arcadedb/memory.go`, `internal/agent/tools/swarm_spawn.go`, `internal/cron/deliver.go`
and `dispatch.go` (ConversationRecorder/ChannelDeliverer idiom, for the shared-pattern section).
**Files scanned (read in full or targeted range):** 24.
**Pattern extraction date:** 2026-08-27.
