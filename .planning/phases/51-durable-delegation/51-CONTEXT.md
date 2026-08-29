# Phase 51: Durable delegation - Context

**Gathered:** 2026-08-26
**Status:** Blocked on the design-gate spike (D-01/D-02/D-03) before `/gsd-plan-phase`

<domain>
## Phase Boundary

A delegated worker gets a brief worth acting on and limits it can see, a worker can
orchestrate workers of its own, and a top-level delegation stops holding the operator's turn
hostage - results re-enter the conversation when the work is actually done.

11 requirements: SWARM-01..SWARM-11 (`.planning/REQUIREMENTS.md:140-150`).

**This discussion did NOT close the design gate.** ROADMAP section 51 forbids
`/gsd-plan-phase` until the unresolved inventory question is answered by measurement. It
sharpened the question and named what the spike must measure. See D-01/D-02/D-03.

</domain>

<decisions>
## Implementation Decisions

### Standing constraint (applies to every decision below)

- **D-00:** **LibreChat (`D:/tmp/LibreChat`) is the reference implementation for this phase.
  Read how it solves a problem before designing a solution; inventing requires a written
  justification.** Operator instruction, given verbatim mid-discussion: *"guarda sempre
  librechat non inventare niente"*. This is CLAUDE.md's INVENTORY BEFORE INVENTION and STOP
  BEFORE BESPOKE rules with the corpus named. It already changed three answers in this
  discussion (D-10, D-12, D-13) away from what was about to be designed.
  `D:/tmp/hermes-agent` remains the second reference, named by ROADMAP section 51.

### The design gate - what the spike must measure

The substrate is entirely on paper and the approved design doc is stale: it targets
migration slot `0026` while `internal/db/migrations/` is at `0102`, and its "Current
Context" section predates the ingestion job queue that now implements most of what it
proposes. Three questions were deliberately NOT answered from the armchair.

- **D-01:** **The durable substrate's shape is decided by measurement, not by the approved
  doc.** The spike compares a single hermes-shaped table against generalizing the existing
  `aura.ingestion_jobs` queue engine, on three real failure cases: a worker that dies
  mid-flight, a daemon restart, and a delivery that fails 8 times. The winner is recorded
  with its evidence. Candidates and what already exists are in `<code_context>`.
- **D-02:** **Whether `agent_message_send` (and the 4-table messaging schema behind it) is
  in Phase 51 at all is decided by the same spike**, which must measure whether background
  delegation needs a separate message channel or whether the delegation ledger suffices.
  None of SWARM-01..11 names that tool; the doc's slice-1 built it and explicitly declined
  to touch `swarm_spawn`, which is the opposite of what SWARM-01..05 require.
- **D-03:** **The worker termination model is decided by measuring real worker durations on
  a live fan-out.** Today every worker dies at `AURA_SWARM_CHILD_TIMEOUT_SEC=120` wall-clock.
  hermes rejects that model outright in favour of progress-based staleness (see
  `<code_context>`); with background delegation a hard 120s ceiling makes the background
  nearly pointless. The spike produces the number; the model follows it.

### How a result re-enters the conversation (SC#1, SWARM-03)

- **D-04:** **The shipped steer rail is the mid-turn delivery mechanism. No second delivery
  mechanism is built.** `internal/steer` + `LlmAgent.drainSteer` already deliver at a round
  boundary behind a non-forgeable nonce attribution envelope, with forged-marker scrubbing,
  without consuming budget, and without touching `history[0..2]` (the KV-cache prefix). The
  `Message.Source` field already carries the producing channel, so worker attribution rides
  for free. Operator decision: *"la steer e gia fatta"*.
  - **AMENDED 2026-08-26 by spike 098 (3 live runs, `deepseek/deepseek-v4-flash-0731:nitro`):
    the rail is validated, the ENVELOPE is not.** `drainSteer` demonstrably places the marker
    outside the `trust="untrusted"` tool-output envelope at a real round boundary, the model
    parses that structure correctly and quotes `SteerChannelNote` verbatim, `history[0..2]` is
    untouched and every run reached `RUN_FINISHED`. But `<user_steer>` *declares the operator
    as author*, and a worker report declares a worker: the model trusts the payload's
    self-declared authorship over the envelope and discounts the entire report as an injection
    attempt. It refused in all three shapes tested — an implausible worker, a contradicted
    report, and a plausible uncontradicted report carrying a credible instruction. Since a
    backgrounded worker's report is the ONLY copy of that result, SC#1 would pass mechanically
    and fail semantically. **Keep the rail; mint a second envelope that declares worker
    authorship and grants tool-result trust, not operator trust** — the two-envelope shape
    (authority-conferring for the operator, authority-negating for background completions)
    observed shipped in the Claude Code harness driving this session. D-04's "no second
    delivery mechanism" survives; its implicit "and no second envelope" does not.
    Evidence: `.planning/spikes/098-steer-carries-worker-result/README.md`.
- **D-05:** **No channel is excluded from background delegation.** hermes gates background
  work behind `async_delivery_supported()` and refuses on channels that cannot deliver after
  the turn ends. Aura does not need that gate: steer already arrives from both the cockpit
  (`internal/agui/server_run_steer.go`) and Telegram
  (`internal/channels/telegram/bot_dispatch_steer.go`), so delivery is server-side, not
  session-bound.
- **D-06:** **The steer inbox moves from in-memory to Postgres with a TTL.** Operator
  decision: *"io metterei tutto su postgres con una ttl piu sicuro e meno fragile"*. This
  closes two measured gaps in reusing the steer rail for worker results: `internal/steer`
  is documented as *"in-memory, single-replica-by-construction"* and *"a steer is consumed
  by Drain, never replayed"*, so a completion pushed while no turn is running, or across a
  daemon restart, is silently lost - which SC#1 forbids. It also retires amendment #133's
  single-replica boundary note.
  **Scope consequence that MUST be named in the PRD amendment, not discovered by the
  planner: this reopens `internal/steer`, Phase 52 code merged 2026-08-25.** Legitimate
  under CLAUDE.md's fix-on-touch rule, but it is not in ROADMAP section 51's accounting.
  - **Reversibility:** one-way - needs a migration creating the queue table, and changes the
    contract of `steer.Inbox` (`Push`/`Drain`) that Phase 52's AG-UI route, Telegram
    dispatch, and `llm_agent_steer.go` all call.
- **D-07:** **One table, rows typed by kind, two TTLs.** A steer and a worker result have
  very different lifetimes: an operator steer is already harmful if delivered hours late
  ("stop, change direction"), a worker report stays valid for hours. One queue table with
  typed rows (`steer` | `delegation_result`), one sweep, two knobs in the env catalog. A
  stale steer delivered late becomes impossible by construction.
  - **Reversibility:** costly - the row-kind discriminator and the two TTL columns are
    schema; undoing means a second migration and re-deriving the TTL from the row kind at
    every read site.
- **D-08:** **An expired row is never silently dropped.** It leaves a readable trace in the
  conversation, written in the same transaction that marks it expired - following the
  shipped precedent where approval expiry persists an internal `expired` refusal and its
  matching `RoleTool` answer atomically, so the agent can truthfully report what did not
  happen. LibreChat does the same thing (`error: 'Approval expired before a decision was
  made'` written with `completedAt` on the expire transition). Consistent with the project's
  core value and with "Errors should never pass silently".

### Concurrent memory writes (SWARM-07, SC#5)

- **D-09:** **Duplicate suppression already works; no work needed.** `factIdentity(fact)`
  derives a content key and `attachFactSource` attaches an additional source to the existing
  fact instead of creating a second one. Two workers learning the same thing produce one
  fact with two sources - already the behaviour SC#5 asks for. Inventory finding, recorded
  so the planner does not build it again.
- **D-10:** **Provenance splits: `RunID` and worker identity become host-derived and leave
  the model-facing schema; `MemoryIDs` stays model-supplied.** Today provenance is
  `FactSource{RunID, MemoryIDs}` and `RunID` arrives from the model
  (`cmd/arcadedb-mcp/tool_memory.go:123-125`, and the input field is marked
  `jsonschema:"required"`). Attribution is therefore model-asserted, which SC#5's "no fact
  attributed to the parent" cannot rest on. Follow LibreChat: `createMemoryTool({ userId,
  setMemory, ... })` closes the actor at construction and exposes only `key`+`value` to the
  model - **the model gets no field to lie in**, rather than a field the host corrects.
  Underneath, `tenantContext.ts` carries `{tenantId, userId, requestId}` in an
  `AsyncLocalStorage`, with `runAsSystem()` as the only escape hatch and even that preserves
  `userId`/`requestId`. Aura's equivalent of layer 2 already exists as `context.Context`
  (`agent.WithSwarmContext`, `tools.WithToolCallContext`). `MemoryIDs` stays with the model
  because which retrieved memories support a fact is genuinely the model's knowledge.
  The fix lands in `cmd/arcadedb-mcp` - our own fork - never wrapped in Aura.
  - **Reversibility:** one-way - changes the published schema of a shipped MCP tool
    (`memory_upsert_fact`), and touches the same caller set MEM-04/D-19 already covers at
    that point: the bridge, the CLI, and host-driven writes.
- **D-11:** **Workers do not supersede.** A worker ADDS facts; closing a still-valid fact
  stays with the parent and the operator. A worker attempting a correction gets a
  model-readable error. This removes the concurrency hazard by scope rather than by locking:
  `UpsertFact` is a sequence of separate `Command` calls, each its own transaction, and only
  the create leg has a race compensation (on create failure it retries `attachFactSource`).
  `closeSuperseded` has none, and Phase 45's fix to it was semantic (close exactly 1, not 8),
  not concurrency-safe. Accepted cost, stated: a worker cannot rectify what it discovers to
  be wrong.

### The worker-to-operator relay (SWARM-06, SC#4)

- **D-12:** **Each worker that needs the operator opens its own pause, attributed to it and
  fenced by an action id.** Follow LibreChat's `ApprovalLifecycle`: the unit that pauses is
  the run, a run holds at most one pending action, and `pendingActionId` is kept as a **flat
  top-level field mirroring** `pendingAction.actionId` *specifically* so an atomic status
  transition can guard on it. `resolve`/`expire` take an `expectActionId` so - in
  LibreChat's words - *"a stale decision can't resume a job that has since paused for a
  different action"*, and `resolve` returns true to exactly one caller because *"a
  double-drive re-executes tools and double-bills"*. `peek` additionally applies **lazy
  expiry**: a past-`expiresAt` record reads as `null`, so a stale prompt is never surfaced
  or fed to a resume - expiry is enforced on read AND by the sweeper, not only by the
  sweeper. N workers asking at once therefore produce N independent, independently-answerable
  pauses that cannot cross-resume. **Aura-specific consequence:** `resume_context` is
  `jsonb`, and LibreChat keeps the mirror flat because *"a nested JSON field can't be
  compared inside a Redis Lua CAS"* - the same reasoning applies to a Postgres conditional
  `UPDATE`, so the fencing id must be a **column**, not a jsonb path.
  - **Reversibility:** one-way - adds a guarded column to the pause table and a new argument
    to the resume path that `CommitResumeBatch` claims under.
- **D-13:** **A nested synchronous worker asks like anyone else; the pause carries the
  identity of the level it came from.** No auto-deny, no bespoke chain-suspension. LibreChat
  identifies a pause by `runId` = the LangGraph `checkpoint_ns` - a nested subgraph has its
  own namespace, so the pause names its level and the resume replays into exactly that
  level. `PendingAction` also carries `interruptId` + `threadId` for cross-process resume,
  plus `requestFingerprint` and `resumeContext`, because *"the resume POST can't reconstruct
  it"* and the fingerprint catches a config swap between pause and answer. Aura already has
  the landing place: `resume_context`, normalized by migration 0102, where
  `allowed_decisions` lives. Missing are the fencing id (D-12) and the level identity.

### Delivery envelope UX (SWARM-12, added 2026-08-29 after the D-03 live checkpoint)

- **D-15:** **The chat gets a chip, the canvas gets the content, Telegram gets one short static
  message, and a worker is a thread the operator can switch to.** Measured 2026-08-29 (PRD
  Amendment #172): the absent-operator nudge pushed the raw JSON report to Telegram in chunks,
  the SC#1 record writes `[Delegated worker report -- goal: ...]
<JSON>` as an assistant
  bubble, and the 51-07 transcript has no cockpit viewer. Operator decisions, verbatim: *"il
  transcript deve arrivare in un canvas a parte ed in telegram deve arrivare solo un
  messaggio"*, *"aggiungerei anche sul cockpit la possibilità di vedere l'agente lavorare su
  una chat parallela"*, *"telegram deve essere semplice"*. Reference reading and the Aura
  inventory (everything reused, nothing invented: `ArtifactsPanel` + the `aura.artifact` seam
  `send_file` uses, `agui.Translate` over the transcript's own `agent.Event` type, the
  `sseAdapter` frame->part mapping, `toolSummary.ts`, assistant-ui's second read-only `Thread`)
  are in `51-UX-ENVELOPE-RESEARCH.md`; the planner reads it before writing the gap plan.
  - **Telegram is decided, not open:** exactly one message — status, goal in one line, a
    bounded summary, "dettagli nel cockpit". No edited-in-place status message (hermes gateway
    pattern rejected for Telegram), no narration, no body, no progress relay.
  - **Open for the planner** (ask, do not assume): third resizable panel vs a tab of the
    Artifacts panel; reasoning deltas in the pane or tool calls + text only.
  - **Sequencing:** the gap plan (51-11) runs BEFORE the live DoD gate 51-08, which then scores
    the envelope the operator asked for, not the measured one. 51-08's `depends_on` gains 51-11.
  - **Reversibility:** two-way for the cockpit and the card; the artifact persisted for the full
    report reuses the existing artifact store, no new schema (D-02 stands).

### Sequencing

- **D-14:** **Spike -> PRD amendment -> one single Phase 51.** The spike runs first and IS
  the ROADMAP-mandated design gate; its measured outcome goes into the PRD amendment that
  SWARM-11 requires before any code; then `/gsd-plan-phase` plans one phase carrying all 11
  requirements. No phase split, no renumbering. **The amendment must also carry the three
  work items ROADMAP section 51 does not account for: the steer queue moving to Postgres
  (D-06), the `memory_upsert_fact` schema change (D-10), and the pause fencing column
  (D-12).**

### Claude's Discretion

D-01, D-02 and D-03 are explicitly delegated to a measurement rather than to a preference.
Claude designs the spike and reports the numbers; the numbers decide, and the PRD records
them with the date and the evidence. What the spike does NOT prove must be stated alongside
what it does - a number without its perimeter is another assumption in disguise.

### Folded Todos

- **`approval-resume-defects`** (`.planning/todos/pending/approval-resume-defects.md`,
  source: PRD amendment #133) - **folded as a constraint, not as work.** All three defects
  closed 2026-08-25. What it binds Phase 51 to: *"New validation goes inside that
  transaction's front door, never as a second path around it"* - `MarkResumed`'s
  `RowsAffected==0` gate and the `WHERE resumed_at IS NULL` conditional update ARE the
  idempotency key (D-06 of that record), and `CommitResumeBatch` claims every pause under
  sorted-token deadlock-free ordering in ONE cross-store transaction. The D-12 fencing id
  and the D-13 level identity must be validated **inside** that transaction. It also fixes
  the TTL precedent Phase 51 inherits: `AURA_ASKUSER_PAUSE_TTL_SEC` = 172800 (48h), with
  `<=0` explicitly disabling expiry.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The approved-but-stale design doc
- `docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md` (551 lines) - the
  approved Postgres-first substrate: claimable tasks, short leases extended by heartbeat,
  fencing on `attempt_count`+`locked_by` with a typed `ErrLeaseLost`, at-least-once delivery
  with idempotency keys, transient-vs-permanent retry with exponential backoff and full
  jitter, A2A lifecycle with `waiting_input` as a non-terminal pause woken transactionally.
  **Read it for its mechanics and its sources, NOT as a settled scope decision:** it targets
  migration slot `0026` (the tree is at `0102`), its "Current Context" predates
  `aura.ingestion_jobs`, and D-01/D-02 explicitly reopen its schema and its
  `agent_message_send` perimeter.

### Reference implementations (D-00 - read before designing)
- `D:/tmp/LibreChat/packages/api/src/stream/GenerationJobManager.ts` (1,985 lines) - durable,
  multi-replica run management: `createJob`/`subscribe`/`subscribeWithResume`/`emitChunk`/
  `getResumeState`/`abortJob`/`expireStaleApprovals`, composed from two injected services.
- `D:/tmp/LibreChat/packages/api/src/stream/interfaces/IJobStore.ts` (530 lines) - the store
  seam the D-01 spike should mirror. `SerializableJobData` is deliberately reference-free
  ("no object references, suitable for Redis/external storage"), which is what makes the
  store swappable InMemory -> Redis. Method surface: `transitionStatus` (atomic, guarded),
  `getRunningJobs`, `cleanup`, `recordActivity`, `getActiveJobIdsByUser`.
- `D:/tmp/LibreChat/packages/api/src/stream/ApprovalLifecycle.ts` (129 lines) - the guarded
  pause/resolve/expire state machine behind D-12.
- `D:/tmp/LibreChat/packages/api/src/agents/hitl/policy.ts` - `PendingActionContext`:
  `actionId`, `runId` (checkpoint namespace), `ttlMs`, `interruptId`, `threadId`,
  `requestFingerprint`, `resumeContext`. Behind D-13.
- `D:/tmp/LibreChat/packages/data-schemas/src/config/tenantContext.ts` - the
  `AsyncLocalStorage<{tenantId,userId,requestId}>` ambient actor context behind D-10.
- `D:/tmp/LibreChat/packages/api/src/agents/memory.ts` - `createMemoryTool({userId, ...})`,
  the per-actor closure that gives the model no field to lie in. Behind D-10.
- `D:/tmp/hermes-agent/tools/delegate_tool.py` (4,133 lines) - `goal`/`context` as separate
  params (SWARM-01); `_build_dynamic_schema_overrides` rebuilding the schema on every
  `get_definitions()` pass so the model sees the operator's real limits (SWARM-02);
  `background` deprecated and ignored so the model cannot opt out (SWARM-03);
  `role: leaf|orchestrator` with `max_spawn_depth`, degrading `orchestrator` to `leaf` past
  the cap (SWARM-05); `_subagent_auto_deny` as the fallback only where no durable queue
  exists (contrast with D-12).
- `D:/tmp/hermes-agent/tools/async_delegation.py` (1,515 lines) - the durable completion
  ledger: PID+start-time owner fencing, `recover_abandoned_delegations()`,
  `restore_undelivered_completions()`, delivery claim with a 300s expiry, attempts counted
  at claim time, terminal `dropped` after 8; and the progress-based staleness monitor behind
  D-03.
- hermes commits `a94ebf5f5` (harden steer lifecycle ownership) and `9d4ef04ed` (bind
  steering to session generation) - named by ROADMAP section 51; they harden the ownership
  transfer *"the batch's lifecycle is owned by the async registry now, not the parent turn"*,
  which touches Phase 52's shipped steering.

### Aura code this phase reopens or depends on
- `internal/steer/inbox.go` - the queue D-06 moves to Postgres. Read its header comment for
  the in-memory/single-replica/consume-once contract being replaced.
- `internal/agent/llm_agent_steer.go` - `drainSteer`, `wrapUserSteer`,
  `scrubSteerLookalikes`; the delivery rule and the `history[0..2]` invariant D-04 relies on.
- `internal/runner/approval_expiry.go` - `ExpirePendingApprovals(ctx, cutoff, limit)`, the
  sweep pattern D-07/D-08 follow, and the precedent for resolving without driving a model
  turn.
- `cmd/arcadedb-mcp/tool_memory.go` - `MemoryUpsertFactInput.Source`, the required
  model-supplied provenance D-10 removes; and `canonicalSubject`, the MEM-04/D-19 precedent
  for host-side rewriting at that exact point.
- `internal/arcadedb/memory.go` - `UpsertFact` (the non-atomic Command sequence behind D-11),
  `factIdentity`/`attachFactSource` (D-09), `FactSource` (D-10).
- `internal/arcadedb/client.go:268` - `Script` runs several statements in one BEGIN/COMMIT
  transaction and `UpsertFact` does not use it. Relevant to D-11's rejected alternative.
- `.planning/todos/pending/approval-resume-defects.md` plus PRD amendment #133 - the folded
  transaction constraint.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`aura.ingestion_jobs` + `internal/documents/jobs_store.go` / `jobs_worker.go`** - a
  working Postgres lease queue: `FOR UPDATE SKIP LOCKED`, `locked_by`/`locked_until`,
  `attempt_count`/`max_attempts`, `next_attempt_at`, `lease_generation`, reclaim of expired
  `running` leases in the same claim predicate, and an `aura.ingestion_events` audit trail.
  **This is approximately the schema the design doc proposes for `aura.swarm_tasks`.** One of
  the two D-01 candidates.
- **`internal/cron`** - held-connection advisory-lock claim (`claim.go`), heartbeat on the
  same connection (`heartbeat.go`), boot orphan recovery (`recover.go`), and
  `deliverToOrigin` (`dispatch.go`), which already returns a run outcome to the origin
  conversation/channel.
- **`internal/steer` + `drainSteer`** - the delivery rail D-04 adopts.
- **`internal/runner` pause machinery** - `CommitResumeBatch`, `MarkResumed`,
  `ExpirePendingApprovals`, per-pause `allowed_decisions` in `resume_context` (migration
  0102). The relay of D-12/D-13 rides this, inside its transaction.
- **`agent.WithSwarmContext` / `tools.WithToolCallContext`** - the typed private-key context
  idiom that carries per-invocation deps; the Go equivalent of LibreChat's ALS actor context
  (D-10).

### Established Patterns
- **Tool-package interface seam** - `swarm_spawn.go` defines `swarmRunner` in the tools
  package and takes it injected, breaking the tools->swarm->agent->tools cycle; the concrete
  adapter is wired at `cmd/aura`. Any new delegation seam follows this, not a new import edge.
- **Model-readable domain rejection** - a domain refusal rides in the `NewResult` string so
  the model self-corrects; a real Go error is reserved for a wiring bug. D-11's
  worker-supersede refusal follows this.
- **Conditional-update-as-idempotency-key** - `WHERE resumed_at IS NULL` with a
  `RowsAffected==0` gate. The same idiom appears in hermes (`mark_completion_delivered`
  returns `rowcount == 1`) and LibreChat (`transitionStatus` returns true to exactly one
  caller). Reuse it; do not invent a second concurrency story.
- **Host-side rewriting at the MCP boundary** - `canonicalSubject` rewrites the subject
  before `arcadedb.Fact` is built, deliberately placed so the bridge, the CLI and
  host-driven writes are all covered by one change (MEM-04/D-19). D-10 lands beside it.

### Integration Points
- **`internal/agent/tools/swarm_spawn.go`** (112 LOC) - the whole model-facing surface today:
  `goals[]` only, a hardcoded `json.RawMessage` schema that cannot show live caps, and
  `Deferred: true`. SWARM-01 splits goal/context here; SWARM-02 makes the schema render
  per manifest.
- **`internal/agent/swarm_context.go:20`** - workers receive `Without(reg, "swarm_spawn")`,
  which is what forecloses SWARM-05's nesting. hermes' `role: leaf|orchestrator` parameter is
  the alternative the phase should weigh against registry surgery.
- **`internal/swarm/swarm.go`** - `RunConfig`, budget reservation (`budgetReserve = 3`,
  `TryReserve`), wave-based concurrency at `MaxSwarmConcurrent`, and per-child failure
  isolation. The background path must not lose the budget reservation semantics.
- **`internal/config/config.go:95-97`** - the three live caps (`AURA_SWARM_MAX_GOALS=8`,
  `AURA_SWARM_CHILD_TIMEOUT_SEC=120`, `AURA_SWARM_MAX_CONCURRENT=4`) that SWARM-02 must
  surface into the rendered schema. Note `AURA_SWARM_MAX_DEPTH` is deliberately absent from
  the knob catalog (`config_knobs_test.go:105` bans it).

### Measured findings that contradict prior assumptions
- **AG-UI run-detach does NOT satisfy SC#1.** `internal/agui/runregistry.go` is
  *"in-memory by design: a daemon restart loses resumability"*, and `handleRunDetached`
  holds `TryLockThread` for the run's whole duration (the lock is released by the session's
  terminal cleanup, not by the HTTP response). It detaches the **stream** from the request,
  not the **work** from the turn - the operator still cannot keep talking on that thread.
  The ROADMAP's open inventory question can be answered "no" for this half already.
- **`agent_job` is the closest existing "work later, report back"** - a fresh `LlmAgent`
  built directly (mirroring `swarm.runChild`, never `runner.Turn`), step budget inherited
  from the row, delivered through cron - **but it auto-rejects `ask_user`**
  (`<auto-rejected: scheduled job has no human responder>`, `maxAutoRejects = 8`), which is
  the exact inverse of SWARM-06. Reusing that path wholesale would ship SC#1 and break SC#4.
- **LibreChat has already hit the deferred-tool resume bug this phase would hit.**
  `SerializableJobData.discoveredTools` exists because tools found via `tool_search` before a
  HITL pause are absent from the schema-only toolMap of the rebuilt graph, and *"resume would
  fail with 'unknown tool'"*. Aura has the same deferred pattern and the same `tool_search`.
  A backgrounded worker that discovers a deferred tool, pauses to ask the operator (D-12),
  and resumes traverses precisely this path. Relevant to SWARM-08.
- **LibreChat persists `agent_id` on the job** so a resume verifies it rebuilds the SAME
  agent that paused - *"resuming Agent A's checkpoint on Agent B's graph would mis-execute
  the paused tool calls"*. A resumed worker must be rebuilt as the same worker.
- **LibreChat attributes token usage per subagent** (`UsageMetadata.agent_id`: *"graph agent
  id / subagent agent id"*). Relevant to how the shared `agent.Budget` accounts for
  background workers whose spend lands outside the parent's turn.

</code_context>

<specifics>
## Specific Ideas

- Operator, on the delivery rail: *"la steer e gia fatta"* - reuse it, do not build a second
  one.
- Operator, on durability: *"io metterei tutto su postgres con una ttl piu sicuro e meno
  fragile"*.
- Operator, standing, given twice: *"guarda come fa librechat"*, then *"guarda sempre
  librechat non inventare niente"*.
- Three architecture questions were explicitly refused as armchair decisions and pushed to a
  measurement (D-01/D-02/D-03). This matches CLAUDE.md's PRD-first principle: measure, then
  amend, then implement - never the inverse.

</specifics>

<deferred>
## Deferred Ideas

- **Making `UpsertFact` atomic via the existing `Script` (BEGIN/COMMIT) method** - rejected
  for this phase in favour of D-11's narrower scope fix. It remains the right answer if
  concurrent supersede ever needs to be supported, and it would touch the memory write path
  for every caller, not just the swarm. Belongs in a memory-correctness phase.
- **Serializing worker memory writes behind a per-identity serializer** - rejected: it
  reintroduces a bottleneck in exactly the fan-out this phase exists to enable.
- **Extracting SWARM-07 into its own memory-correctness phase** - considered during slicing
  and rejected by D-14; recorded because the argument (the write path belongs to every
  caller, not only the swarm) stays valid if Phase 51 grows too large during planning.

</deferred>

---

*Phase: 51-durable-delegation*
*Context gathered: 2026-08-26*
