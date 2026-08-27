---
phase: 51-durable-delegation
plan: 06a
type: execute
wave: 3
depends_on: ["51-01", "51-02"]
files_modified:
  - internal/db/migrations/NNNN_paused_states_fencing.up.sql
  - internal/db/migrations/NNNN_paused_states_fencing.down.sql
  - internal/db/queries/paused_states.sql
  - internal/askuser/store.go
  - internal/runner/resume_committer.go
  - internal/runner/worker_pause_test.go
autonomous: false
requirements: [SWARM-06]
must_haves:
  truths:
    - "The fencing id is a real column on aura.paused_states, not a jsonb path — a Postgres conditional UPDATE can compare it (D-12)"
    - "A resume carrying a stale fencing id matches zero rows and returns ErrPauseNotFound; the pause is not resumed and nothing is double-driven"
    - "The fencing comparison happens INSIDE CommitResumeBatch's existing transaction, at its front door — never as a second path around it"
    - "A pause read past its expiry reads as absent (lazy expiry), so a stale prompt is never surfaced or fed to a resume"
    - "The pause carries the identity of the level it came from, so a later resume can name exactly which worker's line of work it belongs to (D-13). This plan makes that identity STORABLE and QUERYABLE; plan 51-06b is what actually continues the worker"
    - statement: "SWARM-06 edge (unclassified/idempotency): resolving the same pause twice with the same fencing id succeeds exactly once — the second call reports not-found rather than re-executing the tool call"
      verification: explicit
    - statement: "SWARM-06 edge (unclassified/authority): exactly ONE column set answers 'which worker does this pause belong to', and a test names it — the checkpoint's decision is enforced, not merely recorded"
      verification: explicit
  prohibitions:
    - "MUST NOT put the fencing id in resume_context's jsonb — LibreChat keeps the mirror flat precisely because a nested JSON field cannot be compared in a CAS, and the same reasoning applies to a Postgres conditional UPDATE"
    - "MUST NOT add the fencing check as a pre-check before CommitResumeBatch — it belongs inside MarkResumedBatchTx's per-token loop"
    - "MUST NOT treat aura.paused_states as greenfield — proxied_from_child_id and proxied_tool_call_id already ship and already carry a worker id"
    - "MUST NOT hardcode a migration number: read the slot with `ls internal/db/migrations/ | tail -1` at landing time"
    - "MUST NOT claim SWARM-06 satisfied from this plan — the resume leg that continues a paused worker is plan 51-06b"
  artifacts:
    - path: "internal/runner/worker_pause_test.go"
      provides: "db_integration fencing proof, as aura_app"
    - path: "internal/db/queries/paused_states.sql"
      provides: "MarkPausedStateResumedFenced — the shipped conditional update plus one predicate"
  key_links:
    - from: "internal/db/queries/paused_states.sql"
      to: "internal/askuser/store.go (MarkResumedBatchTx)"
      via: "conditional UPDATE guarded on the fencing column"
      pattern: "pending_action_id"
    - from: "internal/runner/resume_committer.go"
      to: "internal/askuser/store.go"
      via: "ExpectActionID threaded through ResumeClaim into the batch loop"
      pattern: "ExpectActionID"
---

<objective>
Make a pause fenceable and attributable, so that when plan 51-06b resumes a worker it resumes
**that** worker's line of work and nothing else (**SWARM-06**, **SC#4**).

**D-12:** each worker that needs the operator opens its own pause, fenced by an action id.
Following LibreChat's `ApprovalLifecycle`: the unit that pauses holds at most one pending
action, and `pendingActionId` is a **flat top-level field mirroring** `pendingAction.actionId`
*specifically* so an atomic status transition can guard on it. `resolve`/`expire` take an
`expectActionId` so — in LibreChat's own words — *"a stale decision can't resume a job that has
since paused for a different action"*, and `resolve` returns true to exactly one caller because
*"a double-drive re-executes tools and double-bills"*. `peek` additionally applies lazy expiry.
Aura-specific consequence: `resume_context` is `jsonb`, and the fencing id must therefore be a
**COLUMN**, not a jsonb path.

**D-13:** a nested worker asks like anyone else; the pause carries the identity of the level it
came from. No auto-deny, no bespoke chain-suspension.

**Folded constraint (`approval-resume-defects`, PRD #133):** *"New validation goes inside that
transaction's front door, never as a second path around it."* `MarkResumed`'s `RowsAffected==0`
gate and the `WHERE resumed_at IS NULL` conditional update ARE the idempotency key, and
`CommitResumeBatch` claims every pause under sorted-token deadlock-free ordering in ONE
cross-store transaction. The D-12 fencing id and the D-13 level identity are validated INSIDE
it. The TTL precedent this inherits: `AURA_ASKUSER_PAUSE_TTL_SEC` = 172800 (48h), with `<=0`
explicitly disabling expiry.

**Scope boundary, stated so nobody reads more into this plan than it delivers.** This plan makes
a pause *fenceable, attributable and lazily expiring*. It does **not** continue a paused worker:
persisting the worker's continuation state, observing an answered pause, returning the queue row
to a claimable state and injecting the answer as the pending `ask_user` tool result are plan
**51-06b** (wave 4). Purpose: the guard rails, landed inside the shipped transaction.
Output: one migration, one fenced query, one threaded argument, one `db_integration` proof.
</objective>

## Artifacts this phase produces

- Files: two migration files (slot read at landing time)
- Column: `aura.paused_states.pending_action_id`, plus whichever level-identity column the
  checkpoint below selects
- Query: `MarkPausedStateResumedFenced :execrows`
- Functions: `askuser.Store.MarkResumedFenced`
- Struct fields: `runner.ResumeClaim.ExpectActionID`, `askuser.Pending.PendingActionID`,
  `askuser.InsertParams.PendingActionID`

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/51-durable-delegation/51-RESEARCH.md
@.planning/phases/51-durable-delegation/51-PATTERNS.md
@.planning/phases/51-durable-delegation/51-CONTEXT.md
@CLAUDE.md
</context>

<tasks>

<task type="checkpoint:decision" gate="blocking">
  <name>Checkpoint: does the per-worker pause EXTEND the shipped proxied_from_child_id / proxied_tool_call_id columns or add new ones (D-12/D-13)</name>
  <decision>D-12/D-13 are ONE-WAY: a guarded column on a shipped table plus a new argument on the resume path. AND the pattern mapper found infrastructure the research did not have in view — decide whether the per-worker pause EXTENDS `proxied_from_child_id`/`proxied_tool_call_id` or adds genuinely new columns.</decision>
  <context>
`aura.paused_states` ALREADY carries `proxied_from_child_id` and `proxied_tool_call_id`
(`internal/askuser/store.go:82-118`, `internal/agent/tools/ask_user.go:77-111`,
`internal/runner/runner_persist.go:385-399`, and `internal/db/queries/paused_states.sql` selects
them in every query). They hold "the flat worker id (`w1`..`wN`) this pause relays from" and
"that child's originating tool_call id" — but for the SYNCHRONOUS existing flow, where the
PARENT re-offers the child's `ask_user` through its OWN pause, and where the MODEL fills them at
its own discretion (`ask_user`'s schema marks both "Optional, model-discretionary"). That is
attribution of a RELAYED pause, model-asserted, not a per-worker PERSISTED pause the worker owns
and can be resumed into directly.

`51-RESEARCH.md` says "missing are the fencing id (D-12) and the level identity (D-13)"; the
pattern mapper states that sentence was written without these columns in view and may be
stronger than warranted. **Treating this as greenfield is a defect.** Decide explicitly.

Reversal cost (RESEARCH.md §Risk Register): LOW for the column itself (a nullable ADD COLUMN,
trivially droppable), MEDIUM for the call-site contract — once callers depend on
`expectActionId` for correctness it cannot be silently dropped without re-auditing every resume
call site for the double-drive hazard it was added to prevent.
  </context>
  <options>
    <option id="extend-proxied">
      <name>Extend the existing `proxied_from_child_id` / `proxied_tool_call_id` for the level identity, add only `pending_action_id` as new</name>
      <pros>One meaning per column set; no near-duplicate columns; the synchronous relay and the background pause read the same attribution</pros>
      <cons>Widens the meaning of two shipped columns — every existing reader must be re-checked for the assumption "this pause was relayed by a parent"</cons>
      <obligation>
        Enumerate and re-check EVERY reader of `proxied_from_child_id` before the migration
        lands: `grep -rn 'proxied_from_child_id\|ProxiedFromChildID' internal/ cmd/`. For each
        hit, state whether it assumed "a parent relayed this" and what changes now that a worker
        may have written it itself. Additionally: the column is currently MODEL-supplied
        (`ask_user`'s schema, "Optional, model-discretionary"), and a host-written per-worker
        pause makes it host-supplied on that path — say which writer wins and add a test that
        a model-supplied value cannot overwrite a host-supplied one.
      </obligation>
    </option>
    <option id="new-columns">
      <name>Add new columns for the background per-worker pause, leaving the proxied pair to mean exactly what it means today</name>
      <pros>No shipped reader changes meaning; the two flows stay legible</pros>
      <cons>Two column sets describing "which worker" invites divergence and a future "which one is authoritative" bug</cons>
      <obligation>
        Name the authority rule in the migration's `COMMENT ON COLUMN` and enforce it in code:
        **the new column is authoritative for a background per-worker pause; the proxied pair is
        authoritative for a synchronous model-relayed pause; they are never both non-NULL on one
        row.** Add a CHECK constraint expressing exactly that, and a `db_integration` test that
        inserting a row with both populated fails. Then name which single accessor every reader
        must call to ask "which worker owns this pause" (one function, not a field read at N
        sites), and assert with `grep` that no other site reads either column directly.
      </obligation>
    </option>
  </options>
  <resume-signal>Select: extend-proxied or new-columns. The chosen option's `<obligation>` is binding on the executor as written — the answer does not need to restate it.</resume-signal>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Fencing column + fenced conditional resume inside the existing transaction (D-12)</name>
  <files>internal/db/migrations/NNNN_paused_states_fencing.up.sql, internal/db/migrations/NNNN_paused_states_fencing.down.sql, internal/db/queries/paused_states.sql, internal/askuser/store.go, internal/runner/resume_committer.go, internal/runner/worker_pause_test.go</files>
  <read_first>
    - `internal/db/queries/paused_states.sql` in full (every query's selected column set, and `MarkPausedStateResumed :execrows` at lines 59-64)
    - `internal/askuser/store.go` (`MarkResumed` at 356-378 — the `RowsAffected==0` → `ErrPauseNotFound` idiom to copy; `MarkResumedBatchTx` at 389-410 — where the fencing check goes; the `Pending`/`InsertParams` structs at 82-118 with `proxied_from_child_id`)
    - `internal/runner/resume_committer.go` in full (204 LOC — `CommitResumeBatch`'s sorted-token deadlock-free ordering; the new argument threads through here)
    - `internal/runner/runner_persist.go:385-399` (how `ProxiedFromChildID` becomes a `*string` today — the mint site the fencing id joins)
    - `internal/runner/resume_committer_test.go:39-67` (`TestSplitResumeCommitter_CommitResume_ClaimsBeforeAppend` — the test shape to copy)
    - `internal/db/migrations/0088_document_machine_card.up.sql` (ALTER TABLE ADD COLUMN + `COMMENT ON COLUMN` style)
    - `D:/tmp/LibreChat/packages/api/src/stream/ApprovalLifecycle.ts` (129 LOC — `resolve`/`expire`/`peek` with `expectActionId` and lazy expiry) and `packages/api/src/agents/hitl/policy.ts` (`PendingActionContext`: `actionId`, `runId`, `ttlMs`, `interruptId`, `threadId`, `requestFingerprint`, `resumeContext`) — D-00 requires reading these first
    - `51-PATTERNS.md` §`D-12/D-13` in full
  </read_first>
  <behavior>
    - A resume supplying the CURRENT `pending_action_id` succeeds and marks the pause resumed exactly once.
    - A resume supplying a STALE `pending_action_id` matches zero rows, returns `ErrPauseNotFound`, appends NO conversation turn, and re-executes NO tool call.
    - A second resume with the same correct id also returns `ErrPauseNotFound` (the `resumed_at IS NULL` half still holds).
    - The fencing predicate is evaluated inside `MarkResumedBatchTx`'s per-token loop, within `CommitResumeBatch`'s single cross-store transaction — a test asserts no resume path exists that skips it.
    - A pause whose `pending_action_id` is NULL (every row that predates this migration, and every ordinary operator pause) resumes exactly as it does today — the fence is additive, never a new precondition on the shipped path.
    - Reading a pause past its `expires_at` yields absent (lazy expiry) even before the sweeper runs.
    - N pauses for N workers on one conversation resolve independently; resolving worker 2's pause leaves worker 1's pending.
    - Exactly one column set answers "which worker owns this pause", per the checkpoint's decision, and a test enforces it.
  </behavior>
  <action>
Read the next migration slot with `ls internal/db/migrations/ | tail -1` and use the next
integer — never a number from a document. Add `pending_action_id text` (nullable, so existing
rows are valid) plus whatever the checkpoint decided for the level identity, each with a
`COMMENT ON COLUMN` naming D-12/D-13 and stating why it is a column and not a jsonb path: a
nested JSON field cannot be compared inside a conditional UPDATE. If the checkpoint selected
`new-columns`, the CHECK constraint from its `<obligation>` lands in this same migration. Write
the `.down.sql`.

Add `MarkPausedStateResumedFenced :execrows` to `internal/db/queries/paused_states.sql` as the
shipped `MarkPausedStateResumed` with ONE more predicate: `AND (pending_action_id IS NULL OR
pending_action_id = $N)` when the caller supplies no fence, and a strict `AND pending_action_id
= $N` when it does — express this as sqlc's nullable-parameter idiom rather than two queries, so
there is one statement and one place the predicate can drift. Keep the `AND resumed_at IS NULL`
clause: the fencing id is an ADDITIONAL guard, never a replacement for the shipped idempotency
key. Regenerate sqlc.

Thread `ExpectActionID` through `ResumeClaim` → `CommitResumeBatch` → `MarkResumedBatchTx`, and
apply the check INSIDE the per-token loop. The folded `approval-resume-defects` constraint is
explicit: new validation goes inside that transaction's front door, never as a second path
around it. Preserve the sorted-token ordering exactly — it is what makes two concurrent
overlapping batches deadlock-free (40P01).

Apply lazy expiry on read in the pause getter so a past-`expires_at` record reads as absent,
mirroring LibreChat's `peek`. Expiry is then enforced on read AND by the sweeper (plan 51-06b),
not only by the sweeper.

Write `internal/runner/worker_pause_test.go` (`db_integration`) test-first, copying
`resume_committer_test.go`'s seed/commit-once/commit-again shape and adding the THIRD case that
is the whole point: a MISMATCHED fencing id on the second attempt (not merely a repeat),
proving a stale decision cannot resume a pause that has since moved on. Add the FOURTH case the
shipped path depends on: a NULL-fence pause still resumes. Use the three-role DSN bootstrap from
`internal/documents/integration_pool_helper_test.go` so it runs as `aura_app`. Add daemon-free
unit tests for the fencing predicate's pure comparison and for lazy expiry so the package's 85%
floor holds without Postgres.

Apply DEEP REFACTOR ON TOUCH to every file edited; `resume_committer.go` and
`internal/askuser/store.go` (547 LOC today — split a concern into
`internal/askuser/store_fencing.go` if the fenced path pushes it up) each stay ≤600 LOC.
  </action>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./internal/runner/... ./internal/askuser/... &amp;&amp; go test -race ./internal/runner/... ./internal/askuser/...</automated>
    <automated>go test -tags=db_integration ./internal/runner/ -run 'TestPerWorkerPauseFencing|TestWorkerPauseLazyExpiry|TestUnfencedPauseStillResumes' -v</automated>
  </verify>
  <acceptance_criteria>
    - The migration's numeric prefix equals `ls internal/db/migrations/ | tail -1`'s number + 1 at landing time, shown in the commit body
    - `internal/db/queries/paused_states.sql` contains `pending_action_id` in the fenced update AND still contains `AND resumed_at IS NULL` in the same query
    - `grep -c 'pending_action_id' internal/db/migrations/*_paused_states_fencing.up.sql` returns ≥2 (column + COMMENT)
    - `grep -rn "resume_context->>'pending_action_id'\|resume_context ->>" internal/db/queries/paused_states.sql` returns no matches (the fencing id is never read from jsonb)
    - The fencing check appears inside `MarkResumedBatchTx`, not in a caller: `grep -n 'ExpectActionID' internal/askuser/store.go` shows it used within the batch loop
    - `go test -tags=db_integration ./internal/runner/ -run TestPerWorkerPauseFencing` exits 0 **run as `aura_app`**, never the superuser `aura` (superuser+BYPASSRLS yields a FALSE GREEN on identity scoping), and the test asserts a foreign-identity pause is invisible
    - The `db_integration` tests take >1s of real runtime (a sub-second run is a skip tell); the skip helper calls `t.Fatal` when the DSN env is unset and `$CI` is set (CLAUDE.md NO SKIP-AS-GREEN IN CI)
    - The test includes a mismatched-fencing-id case asserting `ErrPauseNotFound` AND zero appended conversation turns, and a NULL-fence case asserting the shipped path still resumes
    - The checkpoint's chosen authority rule is enforced by a test, not only documented (per its `<obligation>`)
    - `make db-migrate` applies cleanly; re-run is a no-op
    - `wc -l internal/askuser/store.go internal/runner/resume_committer.go` each report ≤ 600
  </acceptance_criteria>
  <done>A stale decision cannot resume a pause that has since paused for a different action, the guard lives inside the shipped transaction, and exactly one column set says which worker owns a pause.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| operator answer → paused worker execution | an answer resumes a tool call the operator may no longer intend |
| model-supplied pause attribution → host trust | `proxied_from_child_id` is model-discretionary today; a host-written per-worker id must not be forgeable by the model |

## STRIDE Threat Register (ASVS L1, block on: high)

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-51-22 | Tampering | stale decision resuming a repurposed pause | high | mitigate | D-12 fencing column + conditional UPDATE with `RowsAffected==1`, inside `CommitResumeBatch`'s transaction |
| T-51-23 | Tampering / Repudiation | double-drive re-executing tools and double-billing | high | mitigate | The shipped `resumed_at IS NULL` predicate is retained beside the fencing id; exactly one caller wins |
| T-51-24 *(shared with 51-06b — same threat id on purpose, not a collision: 51-06a mints the fence, 51-06b consumes it)* | Spoofing | one worker's answer resuming another worker's pause | high | mitigate | Level identity on the pause + per-pause fencing id; sibling-independence proven in 51-06b |
| T-51-25 | Information Disclosure | cross-identity pause resume | high | mitigate | Every query runs under the identity-scoped transaction; `db_integration` test as `aura_app` asserts a foreign pause is invisible |
| T-51-47 | Spoofing | the model forging a worker id on a pause it did not raise | high | mitigate | The checkpoint's authority rule: a host-written per-worker attribution is the authoritative one, enforced by CHECK constraint and/or a writer-precedence test — never a model-discretionary field read as ground truth |
</threat_model>

<verification>
- `make db-migrate` clean and idempotent
- `go build ./... && go vet ./... && go test ./... && go test -race ./internal/runner/... ./internal/askuser/...`
- `go test -tags=db_integration ./internal/runner/...` as `aura_app`, >1s runtime
- Live SC#4 scenario is driven in plan 51-08
</verification>

<success_criteria>
- The fencing id is a column, the check is inside the shipped transaction, and an unfenced pause still resumes
- Exactly one column set answers "which worker", enforced by a test
- Nothing here claims SWARM-06 complete — 51-06b continues the worker
</success_criteria>

<output>
Create `.planning/phases/51-durable-delegation/51-06a-SUMMARY.md` when done
</output>
