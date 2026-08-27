---
phase: 51-durable-delegation
plan: 06b
type: execute
wave: 4
depends_on: ["51-06a", "51-10"]
files_modified:
  - internal/db/migrations/NNNN_ingestion_jobs_awaiting_input.up.sql
  - internal/db/migrations/NNNN_ingestion_jobs_awaiting_input.down.sql
  - internal/db/queries/ingestion_jobs.sql
  - internal/documents/jobs_store.go
  - internal/swarm/swarm.go
  - internal/swarm/delegation_queue.go
  - internal/swarm/delegation_resume.go
  - internal/swarm/delegation_resume_test.go
  - internal/runner/worker_pause_sweep.go
  - internal/runner/worker_pause_sweep_test.go
  - cmd/aura/serve_delegation.go
autonomous: true
requirements: [SWARM-06]
must_haves:
  truths:
    - "A background worker that needs the operator opens its OWN persisted pause, attributed to that worker, and its queue row parks in a non-terminal, non-claimable state instead of succeeding, failing or dead-lettering"
    - "The question reaches the operator naming WHICH worker raised it, recorded to the origin conversation AND pushed where the identity is reachable (the 268580e23 precedent, reusing the delivery built in plan 51-10)"
    - "Answering that pause CONTINUES that worker: the worker is rebuilt from persisted continuation state, the operator's answer enters as the tool result of the pending ask_user call, and the worker produces work it had not produced before the question"
    - "The rebuilt worker is the SAME worker — same goal, same depth, same identity, same promoted tool set — and the promoted set comes back for FREE: NewLlmAgent sets activated: deriveActivated(hist, cfg.Registry) and everLoaded: deriveEverLoaded(hist, cfg.Registry) where hist = [system] + cfg.UserTurns (internal/agent/llm_agent_construct.go:38-39), so persisting the tool_search call/result pair verbatim IS the whole mechanism. LibreChat needed discoveredTools because its rebuilt graph has a schema-only toolMap; Aura solved the same problem structurally, by a different mechanism. LibreChat's agent_id rule still applies and is carried as agent_identity"
    - "The operator's answer is never replayed twice: the queue row returns to claimable exactly once, under a conditional UPDATE whose RowsAffected==1 is the idempotency key"
    - "Answering worker 2's pause leaves worker 1's pause pending and worker 1's queue row parked — siblings neither cross-resume nor cross-cancel"
    - "A resumed worker does NOT re-execute the tool calls it already ran before pausing — resume continues the run, it does not restart it"
    - "ExpireWorkerPauses expires a pause past AURA_ASKUSER_PAUSE_TTL_SEC and writes its readable trace in the SAME transaction (D-08); a TTL <= 0 disables expiry, and an expired pause resolves its parked queue row deterministically rather than leaving it parked forever"
    - statement: "SWARM-06 edge (unclassified/expiry trace): a pause expired by the sweeper leaves a readable trace in the conversation written in the same transaction (D-08 applied to pauses)"
      verification: explicit
    - statement: "SWARM-06 edge (unclassified/deferred tools): a worker that promoted a deferred tool via tool_search, then paused, can still dispatch that tool after resume — because the persisted history retains the tool_search assistant ToolCalls entry paired with its RoleTool result by ToolCallID, and deriveActivated/deriveEverLoaded re-read that pair at construction. Nothing is re-applied from a list, nothing is rediscovered, and internal/agent is not touched"
      verification: explicit
    - statement: "SWARM-06 edge (concurrency): the resume observer and the claim loop running concurrently return the row to claimable exactly once, and the claim loop cannot claim a parked row"
      verification: backstop
  prohibitions:
    - "MUST NOT auto-deny a nested worker's question — no bespoke chain-suspension (D-13); hermes' _subagent_auto_deny is the fallback only where no durable queue exists, and this phase has one"
    - "MUST NOT reuse the agent_job path wholesale — it auto-rejects ask_user with maxAutoRejects=8, the exact inverse of SWARM-06"
    - "MUST NOT restart the worker from its brief on resume — that re-executes already-billed tool calls, which is exactly the double-drive LibreChat's resolve-returns-true-once exists to prevent"
    - "MUST NOT persist a derived promoted-tool list — no promoted_tools state field, no RunConfig.PromotedTools, no write to the agent's activated set. D-00 / INVENTORY BEFORE INVENTION: deriveActivated already re-grants from the seeded history, and a flat []string applied directly is precisely the unanchored path deriveActivated's tool_call anchoring exists to forbid ('any tool able to emit text could mint its own permissions')"
    - "MUST NOT truncate, summarize or reorder the persisted history across the tool_search assistant ToolCalls entry and its paired RoleTool result — dropping either half silently un-grants every deferred tool the worker had loaded, and THAT is the real 'unknown tool' regression LibreChat measured"
    - "MUST NOT change a single line under internal/agent to make the resume work — internal/agent is not in this plan's files_modified and must not become so; if the resume seems to need a new LlmAgentConfig field or an exported setter, the design is wrong"
    - "MUST NOT invent a second claim path for resumed rows — the resume observer returns the row to claimable and the SHIPPED ClaimIngestionJobs loop picks it up"
    - "MUST NOT introduce a new delegation table (D-01) — the continuation state lives in the queue row's existing payload jsonb"
    - "MUST NOT hardcode a migration number: read the slot with `ls internal/db/migrations/ | tail -1` at landing time"
  artifacts:
    - path: "internal/swarm/delegation_resume.go"
      provides: "continuation state, the answered-pause observer, and the rebuild-with-answer path"
      min_lines: 120
    - path: "internal/runner/worker_pause_sweep.go"
      provides: "per-worker pause TTL sweep with a readable trace, and deterministic resolution of the parked queue row"
  key_links:
    - from: "internal/swarm/delegation_queue.go"
      to: "aura.paused_states"
      via: "a claim-loop worker's AwaitingInput report opens its own pause and parks its row"
      pattern: "StatusNeedsUserInput|awaiting_input"
    - from: "internal/swarm/delegation_resume.go"
      to: "internal/swarm/swarm.go (runChild via RunConfig.ResumeTurns)"
      via: "the rebuilt worker is seeded with the persisted history plus the answer as the pending tool result"
      pattern: "ResumeTurns"
    - from: "internal/swarm/delegation_resume.go"
      to: "aura.ingestion_jobs"
      via: "conditional UPDATE returning a parked row to claimable exactly once"
      pattern: "awaiting_input"
---

<objective>
Close **SWARM-06 / SC#4**: a background worker that needs the operator asks, and **answering
continues that worker's line of work**.

Plan 51-06a made a pause fenceable and attributable. This plan is the half that was missing:
the pause has to be *opened by the worker*, the worker's queue row has to *park* instead of
finishing, and — the part with no shipped analog anywhere in the tree — the answer has to
**resume** the worker.

Why it needs to be built rather than wired: today `swarm.go:205-209` handles
`ev.Actions.AwaitingInput` by recording it into `ChildReport` and calling `continue` — **the
worker does not pause**; the PARENT re-offers the question through its own `ask_user`. And
`runner`'s `CommitResumeBatch`/`MarkResumed` resume a *runner turn*, whereas a claim-loop worker
is a bare `LlmAgent` stream in a goroutine with no checkpoint. There is no existing path from
"the operator answered" back into "that worker keeps working".

Decisions carried: **D-00** (LibreChat read before designing — `SerializableJobData`'s
`agent_id` field is why `agent_identity` is persisted; its `discoveredTools` field is why this
plan persists **no** tool list at all: D-00 cuts BOTH ways, and the in-tree inventory
(`llm_agent_construct.go:38-39`) shows Aura already re-derives the promoted set from the
history, so copying `discoveredTools` would have been invention over inventory),
**D-12** (the pause is fenced by the action id 51-06a added), **D-13** (a nested worker asks like
anyone else; the pause carries the level it came from; no auto-deny), **D-08** (an expired pause
leaves a readable trace, written in the same transaction), **D-01** (the continuation state rides
the existing queue row's `payload` jsonb — no new table), **D-05** (delivery is server-side, no
channel excluded), **D-14** (one single Phase 51).
</objective>

## Artifacts this phase produces

- Files: `internal/swarm/delegation_resume.go`, `internal/swarm/delegation_resume_test.go`,
  `internal/runner/worker_pause_sweep.go`, `internal/runner/worker_pause_sweep_test.go`,
  two migration files (slot read at landing time)
- Status value: `aura.ingestion_jobs.status = 'awaiting_input'` (added to the shipped CHECK)
- Types: `swarm.DelegationResumeState`, `swarm.DelegationResumeObserver`
- Functions: `swarm.NewDelegationResumeObserver`, `(*DelegationResumeObserver).ProcessOnce`,
  `swarm.parkAwaitingInput`, `runner.ExpireWorkerPauses`
- Struct field: `swarm.RunConfig.ResumeTurns` — exactly ONE new field. `RunConfig.PromotedTools`
  was REMOVED from this list at plan revision: the promoted set is re-derived from the seeded
  history by the shipped `NewLlmAgent`, so there is nothing to persist and nothing to re-apply
- Query: `ParkIngestionJobAwaitingInput :execrows`, `UnparkIngestionJob :execrows`

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/51-durable-delegation/51-RESEARCH.md
@.planning/phases/51-durable-delegation/51-PATTERNS.md
@.planning/phases/51-durable-delegation/51-CONTEXT.md
@.planning/phases/51-durable-delegation/51-06a-SUMMARY.md
@.planning/phases/51-durable-delegation/51-10-SUMMARY.md
@CLAUDE.md
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: A background worker opens its own attributed pause and parks its queue row (D-12/D-13)</name>
  <files>internal/db/migrations/NNNN_ingestion_jobs_awaiting_input.up.sql, internal/db/migrations/NNNN_ingestion_jobs_awaiting_input.down.sql, internal/db/queries/ingestion_jobs.sql, internal/documents/jobs_store.go, internal/swarm/delegation_queue.go, internal/swarm/delegation_resume.go, internal/swarm/delegation_resume_test.go</files>
  <read_first>
    - `internal/swarm/swarm.go:186-212` — `runChild`'s `for ev, err := range worker.Run(ic)` loop and its `ev.Actions.AwaitingInput` → `ChildReport{Status: StatusNeedsUserInput, Question, Options, ToolCallID}` capture. This is the SHIPPED capture point; do not add a second one
    - `internal/swarm/delegation_queue.go` (plan 51-01 — the handler that calls `runChild` and branches on the returned report's `Status`; `StatusNeedsUserInput` was explicitly left to this plan)
    - `internal/db/migrations/0025_document_control_plane.up.sql:92-114` — the `aura.ingestion_jobs` table, and in particular the shipped `status` CHECK: `queued, running, succeeded, failed, dead_letter, canceled`. There is no `completed` and no `awaiting_input`
    - `internal/db/migrations/0093_document_pipeline_convergence.up.sql:301-310` — the shipped `DROP CONSTRAINT … ADD CONSTRAINT CHECK (status IN (…))` idiom to copy for widening a status vocabulary
    - `internal/db/queries/ingestion_jobs.sql` — the claim predicate, to confirm a parked row is invisible to BOTH the delegation claim loop and the documents ingestion worker
    - `internal/agent/tools/ask_user.go:70-111` — `ProxiedFromChildID`/`ProxiedToolCallID`, and the fact that both are model-discretionary today
    - `internal/runner/runner_persist.go:385-399` — how a pause row is actually minted (`askuser.InsertParams`), the shape a host-side mint must produce
    - `51-06a-SUMMARY.md` — which column the checkpoint chose for the level identity, and the authority rule it obliges
  </read_first>
  <behavior>
    - A claim-loop worker whose `runChild` report comes back `StatusNeedsUserInput` writes a `paused_states` row carrying its own worker/level identity (host-supplied, per 51-06a's authority rule), the worker's originating `ToolCallID`, and a freshly minted `pending_action_id`.
    - The same operation parks the queue row at `status='awaiting_input'`: non-terminal, and invisible to the claim predicate, so no claim pass and no lease expiry can pick it up while a human is thinking.
    - The pause insert and the park happen in ONE transaction: a pause with no parked row would be answered into nothing, a parked row with no pause would never be answered.
    - Parking does NOT consume an attempt — a human being asked a question is not a failed attempt.
    - The documents ingestion worker never sees an `awaiting_input` row, and the delegation claim loop never claims one.
    - The question reaches the operator naming which worker raised it, through plan 51-10's delivery (recorded to the origin conversation AND pushed where reachable) — not through a new delivery policy invented here.
    - No auto-deny exists on this path at any depth.
  </behavior>
  <action>
Read the next migration slot with `ls internal/db/migrations/ | tail -1` and use the next
integer — never a number from a document. Widen `aura.ingestion_jobs`'s status CHECK to include
`'awaiting_input'`, copying `0093`'s `DROP CONSTRAINT … ADD CONSTRAINT` idiom in SHAPE — but NOT
its constraint NAME. `0093` drops a constraint that was declared with an explicit name, whereas
`0025`'s status CHECK is an **inline unnamed** column constraint that Postgres auto-names
(`ingestion_jobs_status_check` by the usual `<table>_<column>_check` rule). **Look the real name
up rather than assuming it** — `SELECT conname FROM pg_constraint WHERE conrelid =
'aura.ingestion_jobs'::regclass AND contype = 'c';` — and use `DROP CONSTRAINT IF EXISTS` so a
divergent auto-name is a loud no-op rather than a failed migration. Write it
with a `COMMENT` naming D-12/D-13 and stating the invariant: **an `awaiting_input` row is
non-terminal and non-claimable — it is waiting on a human, not on a worker.** Write the
`.down.sql` (it must fail loudly, not silently drop rows, if any `awaiting_input` row exists).
This is a generalization of the generic queue, which is exactly what D-01 measured it to be —
NOT a swarm-specific column.

In `internal/db/queries/ingestion_jobs.sql` add `ParkIngestionJobAwaitingInput :execrows`
(`UPDATE … SET status='awaiting_input', locked_by=NULL, locked_until=NULL, updated_at=now()
WHERE id=$1 AND status='running' AND locked_by=$2`) — the conditional update IS the idempotency
key, per the shipped `WHERE resumed_at IS NULL` idiom; `RowsAffected==0` means the lease was
already lost and the caller must NOT also write a pause. Expose it on
`internal/documents/jobs_store.go` beside the other status transitions rather than reaching into
sqlc from `internal/swarm`. Regenerate sqlc.

In `delegation_queue.go`'s handler, take the `StatusNeedsUserInput` branch plan 51-01 deferred
here. Do not add a second capture of `AwaitingInput` — `runChild` already produced `Question`,
`Options` and `ToolCallID` in the report. Mint the `pending_action_id`, build
`askuser.InsertParams` host-side with the worker attribution 51-06a's checkpoint made
authoritative, and write the pause AND the park inside ONE transaction. Explicitly do NOT
auto-deny (D-13): hermes' `_subagent_auto_deny` is the fallback only where no durable queue
exists, and this phase has one. Explicitly do NOT reuse the `agent_job` path wholesale — it
auto-rejects `ask_user` with `<auto-rejected: scheduled job has no human responder>` and
`maxAutoRejects = 8`, the exact inverse of SWARM-06.

Surface the question through plan 51-10's `DelegationDelivery` — the same seam the consolidated
report uses (record to the origin conversation AND push where reachable, `268580e23`). Do not
choose a new delivery policy here. Note in the SUMMARY that `Registry.DeliverToIdentity` picks
the first started channel in `sort.Strings` order with no origin concept, and that this
selection has never actually been exercised between two candidates (Telegram is the only
`Deliverer` in the tree) — do not describe today's behaviour as "the delivery policy".

Create `internal/swarm/delegation_resume.go` for this plan's new concern (never inline in
`delegation_queue.go`, which is already at its ceiling with enqueue + claim + retry) and put the
park/pause pairing plus `DelegationResumeState` (Task 2) in it. Tests go in
`delegation_resume_test.go` with the `db_integration` tag, using
`internal/documents/integration_pool_helper_test.go`'s three-role DSN bootstrap so they run as
`aura_app`. Daemon-free unit tests cover the pure minting and the state marshalling.
  </action>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./internal/swarm/... ./internal/documents/... &amp;&amp; go test -race ./internal/swarm/... ./internal/documents/...</automated>
    <automated>go test -tags=db_integration ./internal/swarm/ -run 'TestWorkerOpensOwnPause|TestParkedRowNotClaimable' -v</automated>
  </verify>
  <acceptance_criteria>
    - `make db-migrate` applies cleanly; re-run is a no-op; the migration's numeric prefix equals `ls internal/db/migrations/ | tail -1`'s number + 1 at landing time, shown in the commit body
    - `grep -c "awaiting_input" internal/db/migrations/*_ingestion_jobs_awaiting_input.up.sql` returns ≥2 (constraint + COMMENT)
    - `grep -v '^\s*//' internal/swarm/delegation_queue.go | grep -ci 'auto-reject\|autoDeny'` returns 0
    - A `db_integration` test proves a parked row is claimed by NEITHER the delegation claim loop NOR `ClaimIngestionJobs` for the documents worker, and that its `attempt_count` did not increase
    - A `db_integration` test proves the pause insert and the park are atomic: with the park forced to fail, no `paused_states` row exists
    - `go test -tags=db_integration ./internal/swarm/` exits 0 **run as `aura_app`**, never the superuser `aura` (superuser+BYPASSRLS gives a FALSE GREEN on identity scoping)
    - The `db_integration` tests take >1s of real runtime (a sub-second run is a skip tell); the skip helper calls `t.Fatal` when the DSN env is unset and `$CI` is set (CLAUDE.md NO SKIP-AS-GREEN IN CI)
    - `wc -l internal/swarm/delegation_queue.go internal/swarm/delegation_resume.go` each report ≤ 600
  </acceptance_criteria>
  <done>A background worker's question is a real, attributed, fenced pause with its queue row parked non-terminal and non-claimable, and the operator sees which worker asked.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: The resume leg — answering the pause continues that worker's line of work</name>
  <files>internal/swarm/delegation_resume.go, internal/swarm/delegation_resume_test.go, internal/swarm/swarm.go, internal/swarm/delegation_queue.go, internal/db/queries/ingestion_jobs.sql, internal/documents/jobs_store.go, cmd/aura/serve_delegation.go</files>
  <read_first>
    - `internal/agent/llm_agent.go:150-180` — `LlmAgentConfig`, and specifically `UserTurns []llm.Message`: it is a FULL message slice, not a single prompt, which is the seam that makes a seeded rebuild possible at all
    - `internal/agent/llm_agent_construct.go:12-16,:30-50` — `hist := [system] + cfg.UserTurns`, then `activated: deriveActivated(hist, cfg.Registry)` and `everLoaded: deriveEverLoaded(hist, cfg.Registry)`. **Read this BEFORE designing the state shape: the promoted set is RE-DERIVED FROM THE HISTORY AT CONSTRUCTION**, so seeding `UserTurns` with the persisted turns re-grants it for free
    - `internal/agent/llm_agent_promote.go` — the header comment (*"The grant is CONVERSATION-scoped, not turn-scoped… deriveActivated re-reads those results at construction"*); `deriveActivated`'s anchoring rationale at ~43-50 (*"only a RoleTool message whose ToolCallID belongs to a tool_search call is read. Otherwise any tool able to emit text could mint its own permissions"*); the registry intersection at ~105-112 (`if tool, ok := reg.Get(name); ok && tool.Spec().Deferred`) — **already shipped, do not re-invent it as a mitigation**; and `isDeferredUnloaded` at ~220-238, which consults `everLoaded` (never forgets), so the `maxPromotedDeferredTools = 10` LRU cap on `activated` cannot break dispatch after a resume
    - `internal/swarm/swarm.go:158-186` — `runChild`'s worker construction, especially `UserTurns: []llm.Message{{Role: llm.RoleUser, Content: structuredBrief(goal)}}`, the flat `SessionID`, and `LedgerConversationID`
    - `internal/askuser/store.go` — how an answered pause is read back (`resumed_answer`, `ResumeAnswer{Action, Content}`) and `MarkResumedFenced` from 51-06a
    - `internal/documents/jobs_worker.go` — `ProcessOnce`'s claim/handle/transition shape, which the observer must NOT duplicate
    - `D:/tmp/LibreChat/packages/api/src/stream/interfaces/IJobStore.ts` (530 lines) — `SerializableJobData`: read WHY it is deliberately reference-free, and read the `discoveredTools` and `agent_id` fields with their comments. D-00 requires this before designing the state shape
    - `51-CONTEXT.md` `<code_context>` §"Measured findings that contradict prior assumptions" — the two LibreChat resume traps, verbatim
  </read_first>
  <behavior>
    - `DelegationResumeState` round-trips through the queue row's `payload` jsonb losslessly and is reference-free (plain values only — no live objects, no channels, no func fields), so it survives a daemon restart.
    - It carries at minimum: worker id, goal, context, `depth`, conversation id, parent run id, the pending `ToolCallID`, the `pending_action_id`, the pause token, the agent identity the worker was built as, and the worker's accumulated message history up to and including the assistant message that issued the `ask_user` call. It carries NO derived tool list.
    - The persisted `history` retains the `tool_search` assistant message's `ToolCalls` entry paired with its `RoleTool` result by the same `ToolCallID`. That pair, and nothing else, is what re-grants the deferred tools at rebuild.
    - The observer picks up ONLY pauses that are answered (`resumed_at IS NOT NULL`) whose queue row is `awaiting_input`, and returns that row to `queued` with `next_attempt_at = now()` **without incrementing `attempt_count`**.
    - The un-park is a conditional UPDATE: `RowsAffected==1` for exactly one caller; a second observer pass, or an observer racing the claim loop, un-parks zero rows.
    - The shipped `ClaimIngestionJobs` loop — not a second claim path — then claims the un-parked row.
    - On that claim, the handler detects a `ResumeState` in the payload and rebuilds the worker through `runChild` with `RunConfig.ResumeTurns` = persisted history + one `llm.Message{Role: RoleTool, ToolCallID: state.PendingToolCallID, Content: <the operator's answer>}`. `runChild` seeds those turns into `LlmAgentConfig.UserTurns`; `NewLlmAgent` does the rest.
    - The rebuilt worker dispatches a deferred tool that was promoted before the pause **without** calling `tool_search` again — via `deriveActivated`/`deriveEverLoaded` reading the seeded history, with zero lines changed under `internal/agent`.
    - The rebuilt worker does NOT re-execute any tool call present in the persisted history: a test counts dispatches across pause and resume and asserts the pre-pause calls happen exactly once in total.
    - The rebuilt worker is the same identity and the same depth as the one that paused; a state whose agent identity does not match the environment it is being rebuilt in is refused loudly rather than run.
    - Answering worker 2 leaves worker 1's pause pending and worker 1's row parked.
  </behavior>
  <action>
This is the leg with no in-tree analog. Design it from LibreChat (D-00), name every persisted
field, and do not let "resume" degrade into "restart with an answer" — a restart re-executes
already-billed tool calls, which is precisely the double-drive LibreChat's
`resolve`-returns-true-to-exactly-one-caller exists to prevent.

**(a) What is persisted.** Define `DelegationResumeState` in `delegation_resume.go`, marshalled
into the queue row's EXISTING `payload` jsonb (D-01: the generic queue stays generic; swarm
shape lives in the payload — no new table, no new column). Mirror
`SerializableJobData`'s discipline explicitly: **reference-free, plain values only**, because
that is what makes it survive a process boundary. Fields, each with the reason it exists in a
comment:
- `worker_id`, `goal`, `context`, `depth`, `conversation_id`, `parent_run_id` — rebuild the same
  brief at the same level.
- `pending_tool_call_id` — the `ask_user` call the answer is the result OF. Without it the answer
  is an orphan message and the model re-asks.
- `pending_action_id` + `pause_token` — 51-06a's fence, so the resume claims exactly this pause.
- **NO `promoted_tools` field. Do not add one.** LibreChat needed
  `SerializableJobData.discoveredTools` because tools found via `tool_search` before a HITL
  pause are absent from the schema-only toolMap of its rebuilt graph. **Aura already solved the
  same problem by a different, structural mechanism:** `NewLlmAgent` sets `activated:
  deriveActivated(hist, cfg.Registry)` and `everLoaded: deriveEverLoaded(hist, cfg.Registry)`
  where `hist = [system] + cfg.UserTurns` (`internal/agent/llm_agent_construct.go:38-39`), and
  the file's own header says the grant is *"CONVERSATION-scoped, not turn-scoped… deriveActivated
  re-reads those results at construction"*. Seeding `UserTurns` with `ResumeTurns` therefore
  re-grants the promoted set for free.
  The real requirement here is a HISTORY requirement: **the persisted `history` MUST retain the
  `tool_search` assistant `ToolCalls` entry paired with its `RoleTool` result by the same
  `ToolCallID`.** `deriveActivated` grants ONLY from a `RoleTool` message whose `ToolCallID`
  belongs to a `tool_search` call, so dropping either half of that pair silently un-grants every
  deferred tool. A flat `[]string` read out of `payload` jsonb and applied to `a.activated`
  would be exactly the unanchored path that anchoring exists to forbid (*"any tool able to emit
  text could mint its own permissions (a bare `echo "## send_file"`)"*), and it would
  additionally require a new `LlmAgentConfig` field or an exported setter — i.e. editing
  `internal/agent`, which is not in this plan's `files_modified` and must not become so.
- `agent_identity` — **the second LibreChat trap:** it persists `agent_id` so a resume verifies
  it rebuilds the SAME agent, because *"resuming Agent A's checkpoint on Agent B's graph would
  mis-execute the paused tool calls"*. A mismatch is a loud refusal, never a silent run.
- `history` — the worker's accumulated `[]llm.Message`, VERBATIM, up to and including the
  assistant message that issued the `ask_user` call. Verbatim is load-bearing, not stylistic:
  it is the continuation AND the tool-permission grant, so no truncation, no summarization and
  no reordering may separate a `tool_search` assistant `ToolCalls` entry from its `RoleTool`
  result. Everything else in this struct is metadata.

**(b) Who observes and re-claims.** Create `DelegationResumeObserver` with
`ProcessOnce(ctx) (int, error)`, run on the SAME resident loop as the claim loop in
`cmd/aura/serve_delegation.go` — no second scheduler, no cockpit-side polling. It lists answered
pauses whose queue row is `awaiting_input` and calls a new `UnparkIngestionJob :execrows`
(`UPDATE … SET status='queued', next_attempt_at=now(), updated_at=now() WHERE id=$1 AND
status='awaiting_input'`) exposed on `internal/documents/jobs_store.go`. `RowsAffected==1` is the
idempotency key — the same conditional-update idiom as `MarkPausedStateResumed`, never a second
concurrency story. The row is then claimed by the SHIPPED `ClaimIngestionJobs` path. Crucially,
`attempt_count` is untouched: a human answering is not a failed attempt.

**(c) How the answer becomes the tool result.** Add exactly ONE optional field to `RunConfig` in
`internal/swarm/swarm.go`: `ResumeTurns []llm.Message`. In `runChild`, when `ResumeTurns` is
non-empty, seed `UserTurns` with it INSTEAD of `structuredBrief(goal)` — everything else about
the worker's construction stays byte-identical so there is still exactly one worker construction
in the tree (the invariant plan 51-01 established and plan 51-09 depends on). The resumed
handler builds `ResumeTurns` as `state.History` plus one final `llm.Message{Role: llm.RoleTool,
ToolCallID: state.PendingToolCallID, Content: answer.Content}`, reading the answer from the
pause's `resumed_answer` (`askuser.ResumeAnswer`).

That is the ENTIRE tool-permission story: `NewLlmAgent` re-derives `activated` and `everLoaded`
from those seeded turns. Do NOT write to the agent's activated set, do NOT add a `PromotedTools`
field, and do NOT touch `internal/agent` — `git diff --stat internal/agent/` must come back empty
for this plan. Keep `swarm.go` ≤600 LOC and apply DEEP REFACTOR ON TOUCH.

Write the tests FIRST in `delegation_resume_test.go`. The `db_integration` set: park → answer →
observe → un-park exactly once → claim → the worker continues. The daemon-free set (this is
where the coverage floor is actually earned): `DelegationResumeState` round-trip, the
reference-free assertion, the answer-to-tool-result message assembly for a given
`PendingToolCallID`, the agent-identity mismatch refusal, and a history round-trip assertion
that the `tool_search` assistant `ToolCalls` entry and its paired `RoleTool` result both survive
the `payload` jsonb marshal/unmarshal with the same `ToolCallID` — that pair IS the promotion
mechanism, so losing it is the regression worth a test.
The load-bearing behavioural test uses a scripted fake LLM client: a worker that calls tool X,
then `ask_user`, then pauses; after resume it must call tool Y and finish — and the test asserts
tool X was dispatched exactly ONCE across both halves.
  </action>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./internal/swarm/... &amp;&amp; go test -race ./internal/swarm/... ./internal/agent/...</automated>
    <automated>go test -tags=db_integration ./internal/swarm/ -run 'TestDelegationResumeContinuesWorker|TestUnparkExactlyOnce|TestResumeKeepsPromotedTools|TestSiblingPauseUnaffected' -v</automated>
  </verify>
  <acceptance_criteria>
    - A test asserts the resumed worker **continued past its question**: it produced at least one tool dispatch or final answer that did not exist before the pause. Sibling independence alone does NOT satisfy this criterion
    - A test asserts a tool dispatched BEFORE the pause is dispatched exactly once in total across pause + resume (no re-execution, no double-billing)
    - A test asserts a deferred tool promoted via `tool_search` before the pause is dispatchable after resume without a second `tool_search` call
    - A test asserts the round-tripped `DelegationResumeState.History` still contains the `tool_search` assistant message's `ToolCalls` entry AND its `RoleTool` result, paired by the same `ToolCallID` (that pair is the promotion mechanism; losing it is the real regression)
    - `git diff --stat internal/agent/` shows **zero** lines changed by this plan — the promoted set is re-derived by the shipped `NewLlmAgent`, so this plan has no business editing `internal/agent`
    - `grep -rn 'PromotedTools\|promoted_tools' internal/swarm/ cmd/aura/ | grep -v _test` returns 0
    - A test asserts an `agent_identity` mismatch refuses loudly and runs nothing
    - `grep -v '^\s*//' internal/swarm/delegation_resume.go | grep -c 'ResumeTurns'` returns ≥1 and `grep -v '^\s*//' internal/swarm/delegation_resume.go | grep -c 'agent.NewLlmAgent('` returns 0 (the rebuild goes through `runChild`, not a second construction)
    - `grep -c 'attempt_count' internal/db/queries/ingestion_jobs.sql` shows the un-park statement does not touch it; a `db_integration` test asserts `attempt_count` is unchanged across park → resume
    - Two concurrent observer passes un-park exactly one row (`RowsAffected==1` for one caller only)
    - `go test -tags=db_integration ./internal/swarm/` exits 0 **run as `aura_app`**, never the superuser `aura`
    - The `db_integration` tests take >1s of real runtime (a sub-second run is a skip tell); the skip helper calls `t.Fatal` when the DSN env is unset and `$CI` is set
    - `wc -l internal/swarm/swarm.go internal/swarm/delegation_resume.go internal/swarm/delegation_queue.go` each report ≤ 600
  </acceptance_criteria>
  <done>Answering a background worker's question makes that worker keep working — same worker, same tools, no replay — and the proof is a test that shows work done after the answer, not a sibling that stayed still.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: A per-worker pause sweep that expires with a trace and unparks its queue row (D-08)</name>
  <files>internal/runner/worker_pause_sweep.go, internal/runner/worker_pause_sweep_test.go, cmd/aura/serve_delegation.go</files>
  <read_first>
    - `internal/runner/approval_expiry.go` (41 LOC in full — `ExpirePendingApprovals(ctx, cutoff, limit)`, the sweep + `AURA_ASKUSER_PAUSE_TTL_SEC` precedent, `<=0` disables expiry)
    - `internal/runner/resume_committer.go` (the `CommitResume` + `applyResumeHook` pairing that writes the expiry outcome and the conversation turn together)
    - `internal/steer/queue_sweep.go` (plan 51-02 — the sibling sweep this one must not duplicate the shape of by accident; reuse the idiom, not the code path)
    - `51-PATTERNS.md` §`internal/runner/worker_pause_sweep.go`
  </read_first>
  <behavior>
    - `ExpireWorkerPauses(ctx, now, limit)` expires a per-worker pause past `AURA_ASKUSER_PAUSE_TTL_SEC` and writes its readable trace in the SAME transaction; if the trace write fails, the pause is NOT marked expired and the sweep reports the error.
    - A TTL `<= 0` disables expiry entirely (the shipped precedent).
    - Expiring a worker pause resolves its parked queue row deterministically — it does not leave an `awaiting_input` row parked forever with nobody coming.
    - The trace names which worker asked and what went unanswered, so the agent can truthfully report what did NOT happen.
    - The sweep is idempotent: a second pass expires zero additional rows.
    - A vanished row is skipped, not fatal to the whole sweep.
  </behavior>
  <action>
Create `internal/runner/worker_pause_sweep.go` as a NEW sibling of `approval_expiry.go` — that
file is approval-specific by name and doc comment, and RESEARCH.md's File Targets table
explicitly recommends a sibling, not a modification. Copy its shape exactly: list due, loop,
commit each outcome atomically with its trace, skip a vanished row, return
`(expired int, err error)`.

D-08 is the load-bearing part and it extends one step further here than it does for a steer row:
the transaction that marks the pause expired must ALSO resolve the parked queue row, because a
pause and its parked row were created together (Task 1) and must die together. Decide and state
the terminal state explicitly — `failed` with an `expired: the operator never answered` error
message is the honest one, and it keeps `dead_letter` meaning "retried to exhaustion". Whatever
is chosen, write it in the `COMMENT`/doc comment and assert it in a test.

Wire the sweep on the resident loop in `cmd/aura/serve_delegation.go`, beside the claim loop and
the resume observer — one lifecycle, one shutdown-cancellable context, log-and-continue on
error. Do not create a second scheduler.

Tests: expiry-writes-a-trace, rollback-on-trace-failure (inject a failing trace writer through
the interface seam — daemon-free, and this is where the pure logic's coverage comes from),
TTL `<=0` disables expiry, the parked queue row reaches its stated terminal state, sweep
idempotency, and the vanished-row skip.
  </action>
  <verify>
    <automated>go build ./... &amp;&amp; go test -race ./internal/runner/... ./internal/swarm/... &amp;&amp; go vet ./internal/runner/... ./cmd/aura/...</automated>
    <automated>go test -tags=db_integration ./internal/runner/ -run 'TestExpireWorkerPauses|TestExpiredWorkerPauseResolvesQueueRow' -v</automated>
  </verify>
  <acceptance_criteria>
    - `internal/runner/worker_pause_sweep.go` exists and contains `func (r *Runner) ExpireWorkerPauses(`
    - `git diff --stat internal/runner/approval_expiry.go` shows zero lines changed
    - A test proves an expired worker pause wrote its trace in the same transaction (trace-writer failure ⇒ pause not marked expired)
    - A test proves the parked queue row reaches the stated terminal state and is no longer `awaiting_input`
    - `grep -c 'NewDelegationResumeObserver\|ExpireWorkerPauses' cmd/aura/serve_delegation.go` returns ≥2 and `grep -c 'NewDelegationClaimLoop' cmd/aura/*.go` still returns 1 (one lifecycle, not three schedulers)
    - `go test -tags=db_integration ./internal/runner/` exits 0 **run as `aura_app`**; >1s of real runtime; the skip helper calls `t.Fatal` when the DSN env is unset and `$CI` is set
    - `wc -l internal/runner/worker_pause_sweep.go` reports ≤ 600
  </acceptance_criteria>
  <done>An unanswered worker question expires with a readable trace and takes its parked queue row with it — nothing is left waiting for a human who never came.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| persisted worker continuation state → rebuilt worker execution | `payload` jsonb is replayed into an executing agent: its history, its tool ids and its promoted tool set all become execution inputs |
| operator answer → paused worker execution | an answer resumes a tool call the operator may no longer intend |
| worker question text → operator channel | untrusted worker-authored text is shown to a human and may carry instructions |

## STRIDE Threat Register (ASVS L1, block on: high)

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-51-24 *(shared with 51-06a — same threat id on purpose, not a collision: 51-06a mints the fence, 51-06b consumes it)* | Spoofing | one worker's answer resuming another worker's pause | high | mitigate | 51-06a's fencing id + level identity; the un-park is keyed on the pause that was actually answered; sibling-independence test |
| T-51-26 | Repudiation | a pause expiring with no trace, or a parked row abandoned forever | high | mitigate | D-08: expiry, trace and queue-row resolution share one transaction; rollback on trace failure (Task 3) |
| T-51-27 | Elevation of Privilege | worker question text read as an operator instruction | medium | mitigate | The question is rendered to the human as worker-authored; any model-facing echo travels under the untrusted envelope, never `<user_steer>` (plan 51-10's delivery) |
| T-51-48 | Tampering / Elevation of Privilege | a FORGED `tool_search` result inside the persisted `history` minting a deferred-tool grant at rebuild | high | mitigate | This is a reason to persist the history VERBATIM rather than a derived list — not a reason to add a new mitigation. `deriveActivated` anchors every grant to a `RoleTool` message whose `ToolCallID` belongs to an actual `tool_search` call, AND intersects the loaded names with the live registry (`if tool, ok := reg.Get(name); ok && tool.Spec().Deferred`); **both are already shipped** in `internal/agent/llm_agent_promote.go`. The residual write surface is whoever can write `payload` jsonb, i.e. the identity-scoped `aura.ingestion_jobs` row covered by T-51-37. Test: a history naming a deferred tool absent from the live registry rebuilds without it, and a bare `RoleTool` message with no matching `tool_search` `ToolCallID` grants nothing |
| T-51-49 | Tampering | replayed `history` re-executing already-billed tool calls (double-drive) | high | mitigate | Resume seeds history as CONTEXT, never as a dispatch queue; the test counts pre-pause dispatches and asserts exactly one in total |
| T-51-36 | Spoofing | a resume state rebuilt as a different agent/identity than the one that paused | high | mitigate | `agent_identity` is persisted and compared; a mismatch is a loud refusal (LibreChat's `agent_id` rule) |
| T-51-37 | Information Disclosure | continuation state carrying the worker's full history in `payload` jsonb | medium | mitigate | The row is identity-scoped exactly like every other `aura.ingestion_jobs` row; the `db_integration` test as `aura_app` asserts a foreign `identity_id` row is invisible. Recorded as a real disclosure surface: a delegation payload now contains conversation content, which it did not before |
</threat_model>

<verification>
- `make db-migrate` clean and idempotent
- `go build ./... && go vet ./... && go test ./... && go test -race ./internal/runner/... ./internal/askuser/... ./internal/swarm/... ./internal/agent/...`
- `go test -tags=db_integration ./internal/swarm/... ./internal/runner/...` as `aura_app`, >1s runtime
- Live SC#4 scenario is driven in plan 51-08 — and its verdict is "the worker continued", not "the sibling did not move"
</verification>

<success_criteria>
- SC#4 is real: a worker asks, the operator answers on their own channel, and THAT worker keeps working
- No re-execution, no cross-resume, no lost promoted tools, no abandoned parked row
- Zero new tables; the continuation state rides the queue row D-01 measured as sufficient
</success_criteria>

<output>
Create `.planning/phases/51-durable-delegation/51-06b-SUMMARY.md` when done
</output>
