# Phase 51: Durable delegation - Research

**Researched:** 2026-08-27
**Domain:** Durable background task execution (Postgres lease queue generalization), mid-turn
result delivery (steer rail + envelope trust), nested agent orchestration, concurrent
knowledge-graph writes, HITL pause/resume fencing.
**Confidence:** HIGH — every substantive claim below traces to a live spike on the running
stack, a committed fix already on `master`, or a direct code read. LOW-confidence items are
called out individually (mostly in the Open Questions and Assumptions Log).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (verbatim, `51-CONTEXT.md`)

**Standing constraint:**
- **D-00:** LibreChat (`D:/tmp/LibreChat`) is the reference implementation for this phase. Read
  how it solves a problem before designing a solution; inventing requires a written
  justification. `D:/tmp/hermes-agent` remains the second reference.

**The design gate (superseded by measurement — see Gate Status Correction below):**
- **D-01:** The durable substrate's shape is decided by measurement, not the approved doc.
- **D-02:** Whether `agent_message_send` (and the 4-table messaging schema) is in Phase 51 at
  all is decided by the same spike.
- **D-03:** The worker termination model is decided by measuring real worker durations on a
  live fan-out.

**How a result re-enters the conversation (SC#1, SWARM-03):**
- **D-04:** The shipped steer rail is the mid-turn delivery mechanism. No second delivery
  mechanism is built. **AMENDED by spike 098:** the rail is validated, the envelope was not —
  a second envelope declaring worker authorship (not operator authority) is required. D-04's
  "no second delivery mechanism" survives; its implicit "and no second envelope" does not.
- **D-05:** No channel is excluded from background delegation — delivery is server-side
  (steer + Telegram bot dispatch), not session-bound.
- **D-06:** The steer inbox moves from in-memory to Postgres with a TTL. **Reopens
  `internal/steer`, Phase 52 code merged 2026-08-25.** Reversibility: one-way.
- **D-07:** One table, rows typed by kind (`steer` | `delegation_result`), one sweep, two TTL
  knobs. Reversibility: costly.
- **D-08:** An expired row is never silently dropped — it leaves a readable trace in the
  conversation, written in the same transaction that marks it expired.

**Concurrent memory writes (SWARM-07, SC#5):**
- **D-09:** Duplicate suppression already works; no work needed (`factIdentity` +
  `attachFactSource`).
- **D-10:** Provenance splits: `RunID` and worker identity become host-derived and leave the
  model-facing schema; `MemoryIDs` stays model-supplied. Fix lands in `cmd/arcadedb-mcp`.
  Reversibility: one-way — changes the published schema of a shipped MCP tool.
- **D-11:** Workers do not supersede — a worker ADDS facts; closing a still-valid fact stays
  with the parent/operator. Accepted cost: a worker cannot rectify what it discovers to be
  wrong.

**The worker-to-operator relay (SWARM-06, SC#4):**
- **D-12:** Each worker that needs the operator opens its own pause, attributed to it and
  fenced by an action id (LibreChat `ApprovalLifecycle`). The fencing id must be a **column**,
  not a jsonb path (a Postgres conditional `UPDATE` cannot CAS on a nested field).
  Reversibility: one-way — adds a guarded column to the pause table (`aura.paused_states`) and
  a new argument to the resume path `CommitResumeBatch` claims under.
- **D-13:** A nested synchronous worker asks like anyone else; the pause carries the identity
  of the level it came from (LibreChat: `runId` = checkpoint namespace). Missing today: the
  fencing id (D-12) and the level identity.

**Sequencing:**
- **D-14:** Spike -> PRD amendment -> one single Phase 51. No phase split, no renumbering. The
  amendment must carry the three work items ROADMAP section 51 does not account for: D-06,
  D-10, D-12.

### Claude's Discretion

D-01, D-02 and D-03 were explicitly delegated to measurement rather than preference (now
resolved — see Gate Status Correction). Claude designs validation and reports numbers; what
the measurement does NOT prove must be stated alongside what it does.

### Deferred Ideas (OUT OF SCOPE)

- Making `UpsertFact` atomic via the existing `Script` (BEGIN/COMMIT) method — rejected for
  this phase in favor of D-11's narrower scope fix. Belongs in a memory-correctness phase.
- Serializing worker memory writes behind a per-identity serializer — rejected: reintroduces a
  bottleneck in exactly the fan-out this phase exists to enable.
- Extracting SWARM-07 into its own memory-correctness phase — rejected by D-14.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SWARM-01 | Worker brief separates goal from context | UNTOUCHED. `swarm_spawn` schema is `{goals: []string}` only (`internal/agent/tools/swarm_spawn.go:44-47`); `structuredBrief(goal string)` (`internal/swarm/brief.go:30`) takes one undifferentiated string. See Already In The Tree. |
| SWARM-02 | Model sees operator's real concurrency/depth caps in the tool schema | UNTOUCHED. `Spec()` params are a hardcoded `json.RawMessage` literal; the three live caps exist in config but are never interpolated into the schema. See Already In The Tree + Common Pitfalls (do not confuse with the unrelated Tier A/B "knob catalog" test). |
| SWARM-03 | Top-level delegation returns the turn immediately; model cannot opt out | UNTOUCHED for the backgrounding half (swarm.Run is fully synchronous — `internal/swarm/swarm.go` `Run`/`runWave` block until every wave completes). The "cannot opt out" half is trivially already true (no `background` flag exists to opt out of). D-01/D-02/D-04 spikes supply the substrate and delivery mechanics this requirement is built on. |
| SWARM-04 | A worker-issued delegation runs synchronously | Not yet reachable — blocked behind SWARM-05 (nesting is entirely foreclosed today). |
| SWARM-05 | Worker can itself orchestrate, bounded by depth | PARTIAL. Depth-limit machinery already exists and is tested (`internal/swarm/swarm_depth.go`, `checkDepth`, `AURA_SWARM_MAX_DEPTH`), but `runChild` hands every worker `tools.Without(rc.ParentRegistry, "swarm_spawn")` (`swarm.go` `swarmSpawnTool` const + comment "flat v1 forbids nested swarms"), which forecloses it categorically before depth ever matters beyond 1. |
| SWARM-06 | Worker-raised question reaches the operator, attributed, and resuming it resumes that worker | PARTIAL. `runChild` already captures a worker's `ask_user` pause into `ChildReport{Status: StatusNeedsUserInput, Question, Options, ToolCallID}` and the comment states "the parent re-offers them via its own ask_user" — but there is no persisted per-worker pause, no fencing column (D-12), no level identity (D-13); a paused worker's own execution state is not checkpointed or resumable today. |
| SWARM-07 | Concurrent workers write durable facts without corruption/duplication/misattribution | PARTIAL. Dedup (D-09) already works with zero new code. Attribution (D-10) is UNTOUCHED — `RunID` is still model-supplied and `jsonschema:"required"` (`cmd/arcadedb-mcp/tool_memory.go:20,120-125`). Supersede-restriction (D-11) is UNTOUCHED — nothing stops a worker from calling `supersedes`. |
| SWARM-08 | Workers reason over the live flattened tool surface, verified not assumed | MOSTLY DONE as of commit `67d24aee4` (2026-08-26): the operation-fingerprint denial that blocked 100% of worker tool dispatches is fixed and re-measured at 0 denials (spike 099). Registry inheritance itself (`Without(reg,"swarm_spawn")`) was already correct; dispatch was the defect. |
| SWARM-09 | Delegated work is durable — survives restart, claimable from Postgres, never silently lost/retried | UNTOUCHED as code. Design decided by D-01/spike 100: generalize `aura.ingestion_jobs`, do not build a new table. Zero lines connect `internal/swarm` to that queue today. This is the phase's core remaining work. |
| SWARM-10 | Operator/parent can tail a live per-child transcript | PARTIAL. `dumpTranscript` (`internal/swarm/report.go:49`) already writes every worker event, append-mode, to `$AURA_RUN_DIR/<conv>/swarm/<childID>.jsonl` — OS-level tailable today. No API/cockpit surface exposes the path to an operator; this is a filesystem artifact, not a product feature yet. |
| SWARM-11 | PRD amendment lands before any of this phase's code | DONE. PRD Amendment #154 (`prd.md:8504-8585`) is committed. Three bug fixes surfaced BY the spikes and folded INTO the amendment's own findings (`67d24aee4`, `791dcd7e0`, `268580e23`) landed before the amendment's docs commit (`a798f6005`) — these are measurement byproducts the amendment itself narrates, not scope-creep pre-empting it. |
</phase_requirements>

## Gate Status Correction

**Both documents naming this phase BLOCKED are stale. Do not re-run the design gate.**

- `.planning/ROADMAP.md` §"Phase 51: Durable delegation" still carries its
  `> REVISED 2026-08-24 — NOT plan-ready` banner.
- `.planning/phases/51-durable-delegation/51-CONTEXT.md` line 4 still reads `Status: Blocked
  on the design-gate spike (D-01/D-02/D-03) before /gsd-plan-phase`.

**PRD Amendment #154 (`prd.md` lines 8504–8585, committed 2026-08-26/27) closed the gate.**
`/gsd-plan-phase` is legitimately running now. CONTEXT.md was written 2026-08-26, before the
four spikes (098, 099, 100, 101) returned. Their measured answers supersede D-01/D-02/D-03 as
written in CONTEXT.md — the planner should treat the three as answered, not open.

### D-01 — durable substrate shape

**Measured answer: generalize `aura.ingestion_jobs`. Do not build a new delegation table.**
Evidence: `.planning/spikes/100-durable-substrate-shape/README.md`. The three named failure
modes were built as real rows under a synthetic `job_type` and the existing claim predicate
applied verbatim: a worker dying mid-flight (`running`, lease expired) **is** reclaimed; a
delivery failing 8 times (`attempt_count = max_attempts`) **is not** re-claimed (stops rather
than loops); a live worker's lease **is not** robbed. A daemon restart needs no new recovery
code — the lease's own expiry-and-reclaim IS the recovery path.

**What this does NOT prove:**
- The claim **predicate** was measured as a hand-built SQL SELECT over synthetic rows, not the
  Go **claim path** (`ClaimIngestionJobs` + its store code) under an actual process restart. No
  job flowed through the real code during the crash test.
- Nothing here says HOW a swarm worker becomes a queue row: payload shape, who enqueues, and
  whether the parent turn blocks on the enqueue are design questions the spike deliberately did
  not answer — they are this phase's job.
- The crash test SIGKILLed the daemon **2 seconds in, before any worker had dispatched a
  tool**. A crash after partial side effects (a `shell_exec` that ran but never reported) is a
  materially different and unmeasured case.
- One deployment, one profile (`single_user_hardened`), one routed model.

### D-02 — the message channel

**Measured answer: `agent_message_send` and the 4-table messaging schema do NOT belong in
Phase 51.** Evidence: `.planning/spikes/101-message-channel-necessity/README.md`. A reminder
scheduled from the cockpit was delivered to Telegram with **zero rows in any messaging table**.
Aura already has two delivery paths — the steer rail for a present operator, and
`aura.pending_notifications` + `internal/cron/deliver.go` → `channels.Registry.DeliverToIdentity`
for an absent one, with backoff, an attempts budget, and a tri-state contract that refuses to
double-deliver. The happy path never touches the outbox — it delivers inline; the outbox is the
**retry** substrate, not the delivery one. LibreChat corroborates by absence: forty schemas, not
one is a delivery channel.

**What this does NOT prove:**
- **One delivery, one channel, one identity.** Telegram is the only `Deliverer` in the tree, so
  nothing exercised the fan-out's choice between candidates, or its owns-but-failed leg.
  `Registry.DeliverToIdentity` walks started channels in `sort.Strings` order, first-wins, with
  **no origin concept** — that choice has never actually been made between two real candidates.
- **The outbox itself was never exercised.** Its retry/backoff/dead-letter behavior is read from
  the schema and queries, not measured — the happy path bypassed it entirely.
- Nothing here says how a worker's report should be phrased or attributed — that is spike 098's
  question, answered separately (below).
- Whether a delegated result should follow the operator to their phone or wait in the cockpit
  they asked from was **observed, not judged** by this spike — Phase 51 inherits the fix that
  was made (`268580e23`: also record to the origin conversation), but the underlying design
  choice (push vs. record vs. both) is worth the planner re-confirming as intentional, not
  merely inherited.

### D-03 — worker termination model

**Measured answer: a wall-clock cap is the wrong instrument.** Evidence:
`.planning/spikes/099-worker-duration-and-progress/README.md`. Four workers on executable
goals finished in **5.15 / 5.51 / 7.58 / 7.80s** against a 120s cap (23× margin). Across three
runs the cap fired exactly **once**, and it caught an OpenRouter `context deadline exceeded`
(an upstream stall), while the worker most worth interrupting (70.31s calling tools that could
never succeed) sailed under the cap and returned `ok`. Both reference implementations
(LibreChat, hermes) reap on **inactivity**, not age. The refresh point already exists —
`runChild`'s `for ev, err := range worker.Run(ic)` loop (`swarm.go:185`) sees every worker
event, exactly where LibreChat calls `recordActivity`.

**Correction to a number this phase would otherwise trust:** `AURA_SWARM_CHILD_TIMEOUT_SEC` is
**120s nominal but 240s effective**, because a budget trip severs the expired deadline
(`llm_agent.go:307-314`, `context.WithoutCancel`) and the recovery call is then bounded by
`AURA_LLM_TOTAL_TIMEOUT_SEC` (120s in this deployment). **Any lease/reclaim interval sized
against the nominal 120s would reclaim a worker that is still alive.**

**What this does NOT prove:**
- Four small goals is not a load characterization — nothing here says what a genuinely heavy
  worker's real duration distribution looks like.
- The design answer ("the tick belongs in `runChild`'s event loop") is stated, not built — no
  liveness/heartbeat code exists yet that consumes that loop.
- The 240s effective bound is arithmetic from two configured values plus one observation at
  139.77s; the full 240s was never itself observed end-to-end.
- One deployment, one profile, one routed model (`deepseek/deepseek-v4-flash-0731:nitro`).

### Bonus: D-04's "second envelope" — was open in the amendment text, closed same day

Amendment #154's own text says "the one thing still open is spike 098's second envelope." That
was true when the amendment was drafted but **is no longer true**: commit `f60d109f4` (same day,
07:50) shipped it, and `6793cf6b3` records the spike as validated. `markSteer`
(`internal/agent/llm_agent_steer.go`) now picks the delivery envelope by author, keyed on
`steer.SourceWorker = "swarm"` — reusing the existing untrusted-tool-output envelope rather than
minting a third one. **What remains genuinely open:** nothing pushes `steer.SourceWorker` into
the inbox yet. The swarm returns its reports synchronously today; wiring a background
completion to arrive under that source is Phase 51's work, not spike 098's.

## Already In The Tree

**Legend:** DONE = requirement is satisfied by code already on `master`, no further work.
PARTIAL = some real portion is shipped; the rest is named. UNTOUCHED = nothing built yet.

| Req | Status | Evidence |
|---|---|---|
| SWARM-01 | UNTOUCHED | `internal/agent/tools/swarm_spawn.go:44-47` — schema is `{"goals":{"type":"array","items":{"type":"string"}}}`, no `context` field. `internal/swarm/brief.go:30` — `structuredBrief(goal string)` builds all four brief sections (`## Objective`/`## Output format`/`## Tool guidance`/`## Task boundaries`) from ONE string. |
| SWARM-02 | UNTOUCHED | `swarm_spawn.go` `Spec()` — `params` is a literal `json.RawMessage` constant; nothing reads `config.Config` at render time. The three live caps (`MaxSwarmGoals`, `SwarmChildTimeoutSec`, `MaxSwarmConcurrent`) sit in `internal/config/config.go:95-97` unconnected to the schema. |
| SWARM-03 | UNTOUCHED (background half) | `internal/swarm/swarm.go` `Run()` calls `runWave` in a plain `for` loop and returns `marshalReports(reports)` only after every wave finishes — fully synchronous, no queue write, no early return. |
| SWARM-04 | Not reachable | Depends on SWARM-05 (nesting) shipping first; today there is no worker-issued delegation to be sync or async. |
| SWARM-05 | PARTIAL | Depth-cap code exists and is tested (`internal/swarm/swarm_depth.go`: `defaultMaxDepth`, `maxDepth()` reads `AURA_SWARM_MAX_DEPTH`, `checkDepth`; `swarm_test.go:464` `TestSwarmDepthGuard`) but is unreachable because `runChild` builds every worker with `Registry: tools.Without(rc.ParentRegistry, swarmSpawnTool)` (`swarm.go`, comment: "flat v1 forbids nested swarms"). |
| SWARM-06 | PARTIAL | `runChild` (`swarm.go`) already turns a worker's `ev.Actions.AwaitingInput` into `ChildReport{Status: StatusNeedsUserInput, Question, Options, ToolCallID}`. No persisted per-worker pause exists; `internal/runner`'s pause machinery (`aura.paused_states`, `CommitResumeBatch`, `MarkResumed`) is built for one pending action per RUN, not per-worker, and carries no fencing column (D-12) or level identity (D-13) yet. |
| SWARM-07 | PARTIAL | Dedup: `internal/arcadedb/memory.go` `UpsertFact` (line 245) calls `factIdentity(fact)` then `c.attachFactSource(ctx, factKey, fact.Source, now)` BEFORE ever creating a new fact — two workers learning the same thing already produce one fact, two sources (D-09, zero new work). Attribution: `MemoryUpsertFactInput.Source.RunID` is still model-supplied and `jsonschema:"required"` (`cmd/arcadedb-mcp/tool_memory.go:20`, constructed at line ~124) — UNTOUCHED (D-10). Supersede: `fact.Supersedes` is a plain bool the model sets with no actor check — UNTOUCHED (D-11), nothing stops a worker from superseding a parent's fact today. |
| SWARM-08 | MOSTLY DONE | Fixed in `67d24aee4` (2026-08-26): `deriveToolOperationContext` (`internal/agent/idempotency_operation.go`) used to key its re-entry passthrough guard on scope alone, which matched `swarm_spawn` against every one of the ten `OperationScopeAgent` tools a worker may call, denying 100% of worker dispatches (41/41 in spike 099). Fixed to key on scope AND fingerprint; re-measured at 0 denials, 3 real `shell_exec` executions with captured output. |
| SWARM-09 | UNTOUCHED | No code path writes a swarm delegation into `aura.ingestion_jobs` or any other durable store. `internal/documents/jobs_store.go` (408 LOC) and `jobs_worker.go` (272 LOC) are the reusable engine (`ClaimIngestionJobs`, `TransitionIngestionJobRequest`, lease/attempt/fencing columns) — proven correct against the three D-01 scenarios but with zero swarm callers. |
| SWARM-10 | PARTIAL | `internal/swarm/report.go:49-69` `dumpTranscript` already appends every `agent.Event` as one JSON line per event to `$AURA_RUN_DIR/<conv>/swarm/<childID>.jsonl`, best-effort, live during the run — genuinely `tail -f`-able today at the filesystem level. No HTTP/cockpit/CLI surface exposes the path or streams it to an operator. |
| SWARM-11 | DONE | PRD Amendment #154 committed (`a798f6005`, `prd.md:8504-8585`) narrating and ratifying all four spike outcomes before any of Phase 51's substantive implementation code. The three defect-fix commits (`67d24aee4`, `791dcd7e0`, `268580e23`) are measurement byproducts folded into the amendment's own text, not implementation racing ahead of it. |

### The three live defects found and fixed during the design gate (context for the planner)

These are already fixed on `master` — **do not re-plan them**, but the pattern each reveals is
a landmine for new code this phase adds (see Common Pitfalls):

1. **`operation fingerprint mismatch` denied every worker tool call** (`67d24aee4`,
   `internal/agent/idempotency_operation.go`). Root cause: a re-entry guard that only compared
   `OperationScope`, not the full call identity. Fixed and re-measured at 0 denials.
2. **Every worker tool call orphaned its reservation** (`791dcd7e0`,
   `internal/gateway/delegated_reservation.go`, new 119-LOC file). Root cause: the gateway
   writes `start`, the Runner writes `end` from `agent.Event` frames — but `runChild` consumes
   `worker.Run(ic)` itself and never routes frames through a Runner, so `end` never got written.
   30 minutes later `gateway.Reconciler` stamped 5/5 successful worker calls as
   `crash-orphaned … indeterminate` in an append-only ledger. Fixed via
   `gateway.WithDelegatedDispatch(ctx)` — a context marker `runChild` now sets so the gateway
   closes its own reservation at `CompleteOperation`/`MarkOperationIndeterminate` instead of
   waiting on a Runner that never sees the frames. Re-measured: 5/5 closed, reconciler anti-join
   returns 0.
3. **A scheduled run's outcome never reached the conversation it was scheduled from**
   (`268580e23`, `internal/cron/dispatch.go` +52 LOC, `cmd/aura/serve_dispatch.go` +28 LOC).
   `scheduler_tasks.origin_conversation_id` was recorded all along but only the approval-pause
   path ever read it. Fixed via a cron-local `ConversationRecorder` interface (mirroring the
   existing `ChannelDeliverer` idiom) that appends the outcome to the origin conversation in the
   same dispatch, best-effort. Validated live: conversation advanced from seq 6 to seq 7.

### The `docs/aura-quality-snapshot.md` "Swarm E2E" row — what it does NOT cover

Re-measured 2026-08-27 (`0e7cc821a`). The parallelization half that was previously **broken
outright** (0 denials now, was 41) is fixed and re-verified on 3 live fan-outs, with every
worker answer checked against ground truth taken from the sandbox before the run. **But the row
is only PARTLY covered**, and this is the honest starting point for Phase 51's own Definition
of Done: the live E2E harness `internal/eval/harness_swarm_e2e_test.go` **remains deleted**
(removed with the rest of `internal/eval`), so:
- mail+WhatsApp MCP read-back — NOT run, no number claimed
- the `<1.5×` timing ratio vs. a sequential control — NOT run
- the `≥90%` LLM-judge score — NOT run
- the no-over-spawn check — NOT run

The 5.15–7.80s durations are four small, sandbox-local goals on one deployment/profile/model —
not a load characterization. Phase 51's SC#1-#5 will need their own live-driven evidence; this
row's gaps are not retroactively closed by the design-gate spikes and should not be assumed
closed when Phase 51's VALIDATION.md is built.

## Summary

The phase was blocked on three inventory questions and all three now have measured answers that
point the same direction: **the substrate exists; what's missing is a path into it.** Postgres
already has a generalizable lease queue (`aura.ingestion_jobs`) that survives every D-01 failure
mode without new schema. Aura already has two working delivery mechanisms (steer rail for a
present operator, outbox+channel-registry for an absent one) and does NOT need the
`agent_message_send`/four-table design the 2026-06-29 doc specified. And the 120s wall-clock
child timeout is the wrong instrument for staleness — both reference implementations reap on
inactivity, and Aura already owns the event loop (`runChild`) where that tick belongs.

Three live defects were found by *driving the real agent* rather than reading code, and all
three are already fixed on `master`: a worker could not dispatch a single tool (operation
fingerprint collapse), every worker tool call silently mis-recorded itself as a crash 30 minutes
later (reservation ownership split), and a scheduled run's result never reached the conversation
that asked for it (an unread column). None of these need re-planning, but the pattern behind
each — shared operation scopes, split ledger ownership, unread provenance columns — is exactly
the shape of bug this phase's NEW code (a delegation queue processor, a persisted per-worker
pause, a host-derived provenance field) can reintroduce if built without re-checking the same
assumptions.

**Primary recommendation:** Plan Phase 51 as three build tracks over the SAME already-decided
substrate, in this order: (1) generalize `aura.ingestion_jobs` into a delegation queue and wire
`swarm_spawn`'s background path through it (SWARM-03/09), reusing the exact lease/fence/backoff
SQL that already exists; (2) split the worker brief and render the tool schema live, then lift
the registry restriction under the existing depth guard (SWARM-01/02/05/04); (3) build the
per-worker persisted pause with a fencing column on `aura.paused_states` and wire
`steer.SourceWorker` as the delivery source for a background completion (SWARM-06, SC#1/#4),
plus the host-derived memory provenance fix (SWARM-07/SC#5). Do not design a new table, a new
message channel, or a new envelope for any of this — three architectural decisions this phase
might otherwise re-litigate are already closed by measurement.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|---|---|---|---|
| Worker brief construction (goal + context split) | Backend (Go, in-process) | — | Pure string-building in `internal/swarm`; no I/O |
| Tool schema live-rendering (SWARM-02) | Backend (Go, in-process) | — | `Spec()` render happens per-manifest build inside the daemon process, before any network hop |
| Background delegation queue (SWARM-09) | Database/Storage (Postgres) | Backend (claim/worker loop) | `aura.ingestion_jobs` generalization owns durability; a Go claim loop in the daemon owns execution |
| Mid-turn result delivery, present operator (SC#1) | Backend (Go, in-process) | — | `internal/steer` + `drainSteer` inject into the SAME daemon process's in-flight turn; no client/browser involvement |
| Absent-operator delivery (already shipped) | Backend (Go, in-process) | Client channel (Telegram) | `pending_notifications` → `cron/deliver.go` → `channels.Registry.DeliverToIdentity`; the channel implementation is the only client-facing leg |
| Nested worker orchestration (SWARM-04/05) | Backend (Go, in-process) | — | Recursive `swarm.Run` call inside a worker's own `LlmAgent`; no new tier |
| Worker-to-operator HITL relay (SWARM-06) | Backend (Go, in-process) + Database (Postgres pause row) | Client channel (Telegram/cockpit) | The pause and its fencing live in Postgres; the QUESTION reaches the operator through whatever channel the run's UserTurns came from |
| Concurrent memory writes (SWARM-07) | Database/Storage (ArcadeDB) | Backend (`cmd/arcadedb-mcp`) | Fact identity/dedup and provenance correctness are graph-side invariants; the host-derived actor context is the Go MCP server boundary |
| Live per-child transcript (SWARM-10) | Filesystem (Backend-adjacent) | API/Backend (if exposed) | Currently a raw JSONL file under `$AURA_RUN_DIR`; any operator-facing exposure is a new Backend read endpoint, not a new tier |

**Misassignment risk this map heads off:** none of Phase 51's capabilities belong in a browser
or CDN tier — everything here is daemon-process or Postgres/ArcadeDB. A plan that proposes a
cockpit-side polling loop for delegation status, for example, should be checked against this:
the steer rail already delivers mid-turn server-side, and the cockpit is a passive SSE consumer
of that, not a driver of delivery.

## Standard Stack

**No new external dependency is introduced by this phase.** Every piece — Postgres lease queue,
steer inbox, ArcadeDB memory client, pause machinery — is Aura's own existing code, generalized
or extended. `go.mod` needs no new entry. This section is intentionally thin; the "stack" this
phase reuses is enumerated in Don't Hand-Roll below, not installed.

## Package Legitimacy Audit

**Not applicable — this phase installs no external packages.** No `npm view` / `pip index` /
`cargo search` verification is required. If a plan for this phase proposes any new dependency
(e.g., a job-queue library), that is a deviation from this research and should trigger the full
Package Legitimacy Gate protocol at plan time, not be assumed pre-cleared by this document.

## Architecture Patterns

### Delegation flow, target shape (background path, SWARM-03/09)

```
Operator turn (daemon process)
   │
   ├─ model calls swarm_spawn{goals:[...], ...}
   │
   ▼
swarm.Run() — preflight (depth/goals-cap/budget), same as today
   │
   ├─ [SYNC leg, unchanged] nested/depth>0 orchestrator worker
   │     → runs waves in-process exactly as today (SWARM-04)
   │
   └─ [NEW background leg, top-level call, SWARM-03]
         │
         ▼
     enqueue one row per goal into generalized aura.ingestion_jobs
     (job_type='swarm_delegation', payload={goal, context, conv_id,
      parent_run_id, depth}, idempotency_key=deterministic)
         │
         ▼
     swarm_spawn returns a "queued, N workers dispatched" ToolResult
     IMMEDIATELY — the parent turn's LLM sees this and can keep going
         │                                            (operator keeps talking)
         ▼
     [daemon-resident claim loop, FOR UPDATE SKIP LOCKED,
      reusing ClaimIngestionJobs verbatim]
         │
         ▼
     claims row → builds worker LlmAgent exactly as runChild does today
     → gateway.WithDelegatedDispatch(ctx) (reuse the 791dcd7e0 fix)
     → runs to completion or persisted pause (SWARM-06)
         │
         ├─ success → TransitionIngestionJob(completed) + push
         │            steer.Message{Source: steer.SourceWorker, ...}
         │            into the Postgres-backed inbox (D-06/D-07)
         │
         └─ needs operator → open a row in aura.paused_states,
              fenced by the new action-id column (D-12), attributed
              to this worker (D-13's level identity)
         │
         ▼
     next round boundary of the ORIGINAL turn (if still running) OR
     next turn the operator opens on that conversation →
     drainSteer() delivers the worker_report envelope (already built,
     f60d109f4) — model reads it as tool-result-trust evidence, not
     as an operator instruction
```

**What is genuinely new work in this diagram:** the enqueue call inside `swarm_spawn` for the
top-level/background case, the daemon-resident claim loop (a new small worker analogous to
`internal/documents/jobs_worker.go` but dispatching swarm goals instead of ingestion), the
Postgres-backed steer/delegation-result table (D-06/D-07/D-08), and the per-worker persisted
pause with its fencing column (D-12/D-13). Everything else in the diagram is either already
shipped or reused verbatim.

### Pattern 1: Generalize, don't duplicate, a lease queue

**What:** `aura.ingestion_jobs` + `ClaimIngestionJobs`'s `FOR UPDATE SKIP LOCKED` predicate,
`locked_by`/`locked_until`/`lease_generation` fencing, `attempt_count`/`max_attempts` retry
budget, `next_attempt_at` backoff, and the `(identity_id, job_type, idempotency_key)` UNIQUE
dedupe key are the correct substrate for a delegation queue too — proven against all three D-01
failure modes.
**When to use:** Any phase-51 durable task needs (SWARM-09), keyed by a new `job_type` value
(e.g. `swarm_delegation`), never a parallel table.
**Example:**
```sql
-- Source: internal/db/queries/ingestion_jobs.sql (verified live in spike 100)
(status = 'queued' AND next_attempt_at <= now()
 AND (locked_until IS NULL OR locked_until < now()))
OR (status = 'running' AND locked_until < now())     -- an owner that died
... AND attempt_count < max_attempts
ORDER BY next_attempt_at, created_at
FOR UPDATE SKIP LOCKED
```

### Pattern 2: Whoever opens a ledger row closes it

**What:** `internal/gateway/delegated_reservation.go` (new, 119 LOC, commit `791dcd7e0`) closes
a dispatch's reservation at the gateway's OWN terminal hooks when a `gateway.WithDelegatedDispatch`
marker is on the context, instead of relying on a Runner that a worker's event stream never
reaches.
**When to use:** Any new component that runs an `LlmAgent` OUTSIDE the normal
`Runner`-drives-a-turn path (the new background claim-loop worker qualifies) MUST set this
marker or repeat the exact orphaned-reservation defect this phase already fixed once.
**Example:**
```go
// Source: internal/swarm/swarm.go, runChild (fixed 791dcd7e0)
ic := agent.InvocationContext{
    Ctx:       gateway.WithDelegatedDispatch(ctx),
    RequestID: uuid.Must(uuid.NewV7()),
    Budget:    budget,
}
```

### Pattern 3: Choose the delivery envelope by author, not by channel

**What:** `markSteer` (`internal/agent/llm_agent_steer.go`) branches on `steer.Message.Source ==
steer.SourceWorker` to pick the untrusted-tool-output envelope instead of the operator-authority
`<user_steer>` envelope. Already built and tested (`f60d109f4`).
**When to use:** Whatever Phase 51 code eventually calls `Inbox.Push` for a delegated worker's
completion MUST pass `steer.SourceWorker` as the source — any other string falls through to the
operator envelope and reintroduces the exact privilege-confusion spike 098 found (the model
discounting/misreading a worker's report because the envelope claims operator authorship).
**Example:**
```go
// Source: internal/agent/llm_agent_steer.go
func markSteer(m steer.Message) (marked, envelope string) {
    if m.Source == steer.SourceWorker {
        return "\n" + wrapUntrustedToolOutput(m.Source, m.Text), "worker_report"
    }
    return wrapUserSteer(m.Text), "user_steer"
}
```

### Anti-Patterns to Avoid

- **A second delivery channel or a fourth-table messaging schema.** Explicitly closed by D-02 /
  spike 101. If a plan proposes `agent_message_send` or anything shaped like it, that is a
  regression against measured evidence, not a design choice.
- **A new delegation table.** Explicitly closed by D-01 / spike 100. `job_type` is the
  discriminator; a new table duplicates tested lease/fence/backoff code.
- **Sizing any new timeout/lease constant against the nominal `AURA_SWARM_CHILD_TIMEOUT_SEC`
  value (120s).** The effective worst case is 240s (D-03 finding). A lease shorter than that
  will reclaim a live worker.
- **A re-entry/passthrough guard that keys on scope alone.** This is the exact shape of the
  SWARM-08 defect (`67d24aee4`) — any NEW nested-dispatch or delegated-dispatch code path that
  reuses `OperationScope` comparisons must compare the full call identity (scope + fingerprint).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---|---|---|---|
| Durable claimable work queue | A new `aura.swarm_tasks` table with its own lease/fence/backoff | Generalize `aura.ingestion_jobs` (`job_type='swarm_delegation'`) | Measured correct against all 3 D-01 failure modes already; a new table duplicates tested code — the exact mistake CLAUDE.md's own history names (8,640 LOC of ingestion bookkeeping duplicating CocoIndex) |
| Background result delivery to the operator | `agent_message_send` tool + 4-table messaging schema | Steer rail (present operator) + `pending_notifications`/`cron/deliver.go` (absent operator) | Measured live end-to-end across a channel boundary with zero messaging-table rows; LibreChat corroborates by having no message channel at all |
| Worker-authorship-safe mid-turn injection | A new envelope/marker scheme | `wrapUntrustedToolOutput` keyed on `steer.SourceWorker` | Already built (`f60d109f4`); Aura already had a marker meaning exactly "evidence from a named non-operator source" |
| Idempotent conditional state transition | A custom CAS/locking scheme for pause resolution | `WHERE resumed_at IS NULL` + `RowsAffected==0` conditional UPDATE | Already the idiom in `MarkResumed`; matches hermes (`rowcount==1`) and LibreChat (`transitionStatus` returns true once) — one concurrency story, reused everywhere |
| Per-actor provenance closure | A model-trusted `run_id` field the tool schema exposes | Host-derived actor context (`context.Context`/`agent.WithSwarmContext`) closing over the write, mirroring LibreChat's `createMemoryTool({userId,...})` | The model gets no field to lie in, rather than a field the host has to correct after the fact — the difference between "trust by construction" and "trust by validation" |
| Worker liveness / staleness detection | A poll over `aura.tool_invocations` | A tick inside `runChild`'s existing `for ev, err := range worker.Run(ic)` loop | The ledger's `start`/`end` rows have split ownership (the very defect just fixed) and cannot reliably distinguish a live worker from an orphaned reservation; the event loop sees every worker event directly |

**Key insight:** every "don't hand-roll" in this phase is not a generic industry library
recommendation — it is a specific, already-built piece of Aura's own tree that a spike proved
correct against this phase's exact failure modes. The hand-rolling risk here is internal
duplication (rebuilding what `internal/documents/jobs_store.go` already does), not
reach-for-an-external-package temptation.

## Common Pitfalls

### Pitfall 1: A re-entry guard keyed on scope alone silently misauthorizes
**What goes wrong:** Two tools sharing an `OperationScope` (e.g., `swarm_spawn` and any
agent-scoped tool) collapse into the same "already inside an operation of this scope, don't
derive a new one" passthrough — a worker's OWN tool call inherits its parent's operation and
fingerprint, and the gateway denies it as a mismatch.
**Why it happens:** The guard was written when no agent-scoped tool could dispatch inside
another; nesting broke that assumption silently.
**How to avoid:** Any new nested-or-delegated dispatch path must key re-entry checks on the
FULL call identity (scope AND fingerprint), per the `67d24aee4` fix in
`internal/agent/idempotency_operation.go`.
**Warning signs:** 100%, deterministic denial rate on a specific tool inside a specific caller
— not a race, not intermittent. If a new delegated-worker code path shows this shape, check
`deriveToolOperationContext` first.

### Pitfall 2: Ledger rows with split start/end ownership orphan silently
**What goes wrong:** A component that runs an agent turn OUTSIDE the normal Runner path (any
new claim-loop worker qualifies) opens a gateway reservation (`start`) but nothing ever writes
its `end`, because the Runner — the normal `end` writer — never sees that component's event
frames. Half an hour later the reconciler stamps a SUCCEEDED call as a permanent
`crash-orphaned … indeterminate` in an append-only ledger.
**Why it happens:** The reservation's two halves have two different writers by design (gateway
opens, Runner closes), and that design assumes every dispatch flows through a Runner.
**How to avoid:** Set `gateway.WithDelegatedDispatch(ctx)` on any dispatch path that does not
go through the normal Runner loop (already the fix pattern in `runChild`).
**Warning signs:** `start` rows with no matching `end` in `aura.tool_invocations`, visible via
`gateway.Reconciler`'s own anti-join query.

### Pitfall 3: A hard-coded 120s assumption undercounts the real worst case
**What goes wrong:** Sizing a new lease, timeout, or reclaim interval against
`AURA_SWARM_CHILD_TIMEOUT_SEC=120` reclaims a worker that is still legitimately running.
**Why it happens:** A budget trip severs the expired deadline (recovery call uses
`context.WithoutCancel`) and the recovery call is then bounded by a SECOND timeout
(`AURA_LLM_TOTAL_TIMEOUT_SEC`), so the effective worst case is the SUM of the two (measured
240s in this deployment), not the nominal value alone.
**How to avoid:** Any new durable lease duration for a swarm delegation row should be derived
from (or configured no shorter than) `SwarmChildTimeoutSec + LLMTotalTimeoutSec`, not
`SwarmChildTimeoutSec` alone.
**Warning signs:** A worker reclaimed and re-run while its original attempt was still
producing a valid (if late) result.

### Pitfall 4: Raw SSE/log grepping produces false verdicts on model behavior
**What goes wrong:** `grep -qi BANANA` on a raw SSE stream reported "OBEYED" twice in spike 098
when the model had actually REFUSED and was quoting the injected word while explaining the
refusal.
**Why it happens:** Text-delta events are token-split; the true final message only exists after
reassembly, and a keyword match on the raw wire format catches quotation as easily as
compliance.
**How to avoid:** Any Phase 51 validation driver that inspects model output for a
compliance/refusal verdict must reassemble `TEXT_MESSAGE_CONTENT` deltas into the final message
(and read the reasoning trace where available) before asserting anything.
**Warning signs:** A pass/fail verdict derived from a single grep line rather than the
reconstructed message.

### Pitfall 5: Delivering a worker's report under the wrong envelope source silently loses it
**What goes wrong:** If new code pushes a worker's completion into the steer inbox with any
`Source` other than `steer.SourceWorker`, `markSteer` routes it through `wrapUserSteer` — the
operator-authority envelope — and the model, per spike 098's three live runs, discounts the
payload as a spoofing attempt because it declares worker authorship inside an envelope that
claims operator authorship. For a backgrounded worker whose report is the ONLY copy of the
result, this is silent, complete data loss disguised as a mechanically successful delivery
(`RUN_FINISHED` still fires).
**How to avoid:** The producer side of D-06 (whatever pushes a completed delegation into the
Postgres-backed inbox) must set `Source: steer.SourceWorker` unconditionally for delegation
results, never the channel name.
**Warning signs:** A worker report arrives, the turn completes normally, but the model's final
answer does not incorporate or even mention the worker's finding.

### Pitfall 6: `Registry.DeliverToIdentity`'s channel selection has never actually chosen
**What goes wrong:** The registry walks started channels in `sort.Strings` order and
first-delivers-wins, with no origin concept. Telegram is currently the ONLY `Deliverer`, so this
selection logic has literally never been exercised between two real candidates.
**Why it matters for Phase 51:** If this phase (or a future one) adds a second pushable channel,
delivery destination becomes alphabetical, not preferential, silently.
**How to avoid:** Not a Phase 51 blocker (no second `Deliverer` exists yet), but plans should
not describe today's Telegram-only behavior as "the delivery policy" — it is an accident of
there being one implementation.

### Pitfall 7: A hardcoded migration number will be wrong by landing time
**What goes wrong:** The next free migration slot measured during research (`0103`, since
`0102_paused_state_decision_policy` is the latest at research time) may not be free by the time
this phase's plans actually execute, if another phase's migration lands first.
**How to avoid:** Per CLAUDE.md's imperative rule, re-run `ls internal/db/migrations/ | tail -1`
at EACH migration-creation point during execution, never reuse the number recorded here.

## Code Examples

### The existing lease-queue claim predicate (reuse verbatim for delegation rows)
```sql
-- Source: internal/db/queries/ingestion_jobs.sql, verified live in spike 100
-- (.planning/spikes/100-durable-substrate-shape/README.md)
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

### The delegated-dispatch reservation-closing marker (reuse for the new claim-loop worker)
```go
// Source: internal/swarm/swarm.go (runChild), fixed by 791dcd7e0
ic := agent.InvocationContext{
    Ctx:       gateway.WithDelegatedDispatch(ctx),
    RequestID: uuid.Must(uuid.NewV7()),
    Budget:    budget,
}
```

### The worker-authorship envelope selection (reuse the source constant, not the string)
```go
// Source: internal/steer/inbox.go
const SourceWorker = "swarm"

// Source: internal/agent/llm_agent_steer.go
func markSteer(m steer.Message) (marked, envelope string) {
    if m.Source == steer.SourceWorker {
        return "\n" + wrapUntrustedToolOutput(m.Source, m.Text), "worker_report"
    }
    return wrapUserSteer(m.Text), "user_steer"
}
```

### The conditional-update-as-idempotency-key idiom (reuse for D-12's fencing UPDATE)
```go
// Pattern already established in internal/runner's MarkResumed (referenced in
// 51-CONTEXT.md's Established Patterns): a bare UPDATE ... WHERE <predicate>
// IS NULL, gated on RowsAffected==0/1, is the ONE concurrency idiom this
// codebase uses for "exactly one caller wins" — LibreChat's transitionStatus
// and hermes' mark_completion_delivered do the identical thing. D-12's
// resolve/expire with expectActionId should follow this shape exactly:
// UPDATE aura.paused_states
//   SET resumed_at = now(), ...
//   WHERE pause_id = $1 AND pending_action_id = $2 AND resumed_at IS NULL
// then check RowsAffected == 1.
```

## State of the Art

| Old Approach (approved-but-stale 2026-06-29 design doc) | Current Approach (measured 2026-08-26/27) | When Changed | Impact |
|---|---|---|---|
| New Postgres-first `aura.swarm_tasks`-shaped table for delegation | Generalize `aura.ingestion_jobs` with a new `job_type` | Spike 100, 2026-08-26 | No new migration for the queue itself; reuse tested lease/fence/backoff |
| `agent_message_send` tool + 4-table messaging schema for background delivery | Steer rail (present) + existing outbox/channel-registry (absent) | Spike 101, 2026-08-26 | No new tool, no new schema; wire a path INTO existing delivery, not a new delivery system |
| Hard 120s wall-clock child timeout as the termination model | Inactivity-based staleness, ticked from `runChild`'s existing event loop | Spike 099, 2026-08-26 | Design target changes from "pick a better constant" to "build a liveness signal"; also corrects the effective bound from 120s to 240s |
| `<user_steer>` as the sole mid-turn delivery envelope | Two envelopes chosen by author: operator (`<user_steer>`) vs. worker (untrusted tool-output, `steer.SourceWorker`) | Spike 098 + fix `f60d109f4`, 2026-08-26/27 | A worker's report is no longer discounted as a spoofing attempt by the model |
| Gateway `start`/Runner `end` reservation split assumed universal | Delegated dispatches close their own reservation via `gateway.WithDelegatedDispatch` | Fix `791dcd7e0`, 2026-08-26 | Any future non-Runner dispatch path must adopt this marker or repeat the defect |
| Scheduler delivery assumed "channel that owns the identity" is sufficient | Origin conversation ALSO gets the outcome recorded, via `ConversationRecorder` | Fix `268580e23`, 2026-08-26/27 | Establishes the precedent Phase 51's own result-delivery should follow: record to origin AND push where reachable |

**Deprecated/outdated:**
- `docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md` — approved but its
  schema decision (a new table) and its `agent_message_send` scope are both superseded by
  measurement. Its MECHANICS section (leases, fencing, at-least-once, backoff-with-jitter,
  `waiting_input` as a non-terminal pause) remain valid reading; its "what to build" is not.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|---|---|---|
| A1 | The next free migration slot is `0103` | File Targets and Sizes | LOW — explicitly re-derived at landing time per CLAUDE.md's rule; stated here only as a research-time snapshot |
| A2 | A background delegation row's `payload` should carry `{goal, context, conv_id, parent_run_id, depth}` | Architecture Patterns | MEDIUM — this shape is not measured, only inferred from what `swarm_spawn`'s reservation `args_raw` already durably carries (spike 100) plus SWARM-01's goal/context split; the planner should treat this as a strawman schema, not a locked one |
| A3 | The push-vs-record-vs-both delivery choice observed in `268580e23` (push to owning channel AND record to origin conversation) is the intended precedent for Phase 51's own worker-completion delivery, not merely an artifact of the reminder use case | Gate Status Correction (D-02) | MEDIUM — if wrong, Phase 51 might build a delivery path that diverges from the just-established precedent unnecessarily |
| A4 | `steer.SourceWorker` (the string `"swarm"`) is the correct and ONLY source value Phase 51's background-completion producer should ever set, with no room for a per-worker-id source variant | Common Pitfalls #5, Architecture Patterns | LOW — directly read from shipped code and its doc comment, but the comment does not address a future need to distinguish WHICH worker within an envelope's Source field (today that lives in the message text, not `Source`) |

**If this table is empty:** N/A — see rows above. Everything else in this document traces to a
spike README, a committed diff, or a direct code read, and is tagged accordingly in-line.

## Open Questions

1. **Does SWARM-02's "tool schema shows real caps" conflict with the Tier A/B "knob catalog"
   test banning `AURA_SWARM_MAX_DEPTH`?**
   - What we know: `internal/config/config_knobs_test.go:105` bans `AURA_SWARM_MAX_DEPTH` (and
     `AURA_LOOP_*`/`AURA_LLM_*`/`AURA_FS_*`) from an operator-facing "Tier A+B" knob registry
     (verified by reading the test — it validates `AURA_PROFILE`, `AURA_OBJECTSTORE_*`,
     `AURA_AGUI_*` entries in a `byName` registry, an unrelated concept to a tool's JSON
     schema).
   - What's unclear: whether the planner might conflate "the operator-facing settings catalog"
     with "the swarm_spawn tool's rendered JSON schema" and avoid surfacing depth in the
     schema out of an abundance of caution.
   - Recommendation: these are different catalogs serving different consumers (a human admin
     surface vs. a model-facing tool schema). SWARM-02 should render `AURA_SWARM_MAX_DEPTH`
     (and the other two caps) into the tool's OWN schema/description without touching or
     needing an entry in the Tier A/B knob test's registry.

2. **What payload/enqueue contract does the background delegation row actually need?**
   - What we know: the goal text is already durable inside `swarm_spawn`'s reservation
     `args_raw` (spike 100), so the DATA exists; what's missing is a row that says the work is
     owed.
   - What's unclear: exact payload schema, who writes the enqueue (the tool's `Execute` inline,
     or a deferred append after the tool returns), and whether the parent turn's LLM call
     blocks on the enqueue transaction or fires-and-forgets.
   - Recommendation: this is squarely a planning/design decision for Phase 51 itself — the
     spikes deliberately left it open (stated explicitly in spike 100's "What This Spike Does
     NOT Prove"). Do not treat any payload shape in this document as settled.

3. **Should the crash-after-partial-side-effects case (unmeasured by spike 100) get its own
   validation task, or is it acceptable residual risk for this phase?**
   - What we know: the only crash test SIGKILLed the daemon 2 seconds in, before any worker
     tool dispatch. A worker that ran `shell_exec`, produced a side effect, and then the daemon
     died before recording completion is a different and harsher case.
   - What's unclear: whether Phase 51's SC#1-#5 require this case to be exercised live, or
     whether it is acceptable to note it as a known gap (the way the Swarm E2E row already
     notes its own gaps honestly).
   - Recommendation: at minimum, name this as an explicit VALIDATION.md line item with a
     stated verdict ("not exercised this phase, documented risk") rather than letting it be
     silently assumed covered by spike 100.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|---|---|---|---|---|
| Postgres (`aura-postgres` container) | SWARM-09 durable queue, D-06 steer table, D-12 pause fencing | ✓ | Live-verified in spikes 099-101 (2026-08-26/27) | — |
| ArcadeDB (`arcadedb-mcp` sidecar) | SWARM-07 concurrent memory writes | ✓ | Live-verified in prior memory spikes (031-035, 044) | — |
| Docker / `docker compose` (`aura`, sidecars) | All four Phase-51 spikes ran against the live containerized stack | ✓ | Verified live 2026-08-26/27 | — |
| OpenRouter (`deepseek/deepseek-v4-flash-0731:nitro`, DB-driven routing) | Every live spike's model calls | ✓ | Confirmed live and DB-driven (`aura.settings`) — never assume statically | — |
| `internal/eval` live-judge E2E harness | Full SC criteria (mail/WhatsApp read-back, timing ratio, judge score) | ✗ | Deleted (see `docs/aura-quality-snapshot.md` Swarm E2E row) | None — this phase's own live-driven validation must substitute; do not assume the deleted harness can be resurrected without rebuilding it |

**Missing dependencies with no fallback:**
- The deleted `internal/eval` live-judge harness — Phase 51's Definition of Done (live E2E
  ≥9.8) must be satisfied by a fresh, phase-specific live scenario, not by restoring the old
  harness.

**Missing dependencies with fallback:**
- None beyond the item above.

## Validation Architecture

### Test Framework

| Property | Value |
|---|---|
| Framework | Go `testing` + `gotestsum`; `db_integration` build tag for real-Postgres tests (precedent: `internal/documents/jobs_store_test.go`, `jobs_worker_test.go`, `integration_pool_helper_test.go`) |
| Config file | `internal/documents/integration_pool_helper_test.go` (DSN/pool bootstrap pattern to copy) |
| Quick run command | `go test ./internal/swarm/... ./internal/steer/... ./internal/agent/... -run <Test> -v` |
| Full suite command | `go test ./... && go test -race ./internal/agent/... ./internal/gateway/... ./internal/swarm/... ./internal/runner/... ./internal/steer/... ./internal/documents/...` |

### Phase Requirements -> Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|---|---|---|---|---|
| SWARM-01 | Brief renders goal and context as separate sections, deterministically | unit | `go test ./internal/swarm/ -run TestStructuredBrief` | ❌ Wave 0 (extend `brief.go`'s existing test file, or new `brief_context_test.go`) |
| SWARM-02 | Rendered `Spec()` schema/description embeds the LIVE `MaxSwarmGoals`/`MaxSwarmConcurrent`/`AURA_SWARM_MAX_DEPTH` values, not hardcoded text | unit | `go test ./internal/agent/tools/ -run TestSwarmSpawnSpecReflectsConfig` | ❌ Wave 0 |
| SWARM-03 | A background delegation returns a ToolResult immediately; the enqueued row appears in the generalized queue | db_integration | `go test -tags=db_integration ./internal/swarm/ -run TestBackgroundDelegationEnqueues` | ❌ Wave 0 (new file, model on `jobs_store_test.go`) |
| SWARM-03 (live) | Operator keeps talking mid-delegation; consolidated result lands in `aura.conversation_turns` when work finishes | live E2E | manual/driven-agent scenario against the running stack (see Sampling Rate) | ❌ Wave 0 — no automated harness; must be a phase-specific live driver script, following the `.planning/spikes/09{8,9}/drive.sh` pattern |
| SWARM-04 | A depth>0 worker's own delegation blocks until its children report, inside its own turn | unit + race | `go test -race ./internal/swarm/ -run TestNestedDelegationSynchronous` | ❌ Wave 0 |
| SWARM-05 | Nesting bounded by `AURA_SWARM_MAX_DEPTH`; depth cap degrades or rejects, never silently ignores | unit | `go test ./internal/swarm/ -run TestSwarmDepthGuard` (extend existing `swarm_test.go:464`) | ✅ partial — extend, don't rewrite |
| SWARM-06 | A worker's `ask_user` opens a fenced, attributed pause; answering resumes exactly that worker | db_integration | `go test -tags=db_integration ./internal/runner/ -run TestPerWorkerPauseFencing` | ❌ Wave 0 |
| SWARM-06 (live) | Question surfaces in the operator's channel naming the worker; answering resumes that worker's line of work | live E2E | driven-agent scenario, nested delegation + injected `ask_user` | ❌ Wave 0 |
| SWARM-07 | N concurrent workers writing facts produce no duplicates, no lost writes, no cross-attribution | db_integration (ArcadeDB) + race | `go test -race -tags=arcadedb_integration ./internal/arcadedb/ -run TestConcurrentWorkerFactWrite` | ❌ Wave 0 — this is the phase's one genuine concurrency/race surface; see Sampling Rate below |
| SWARM-08 | A worker dispatches every tool in its flattened surface without a fingerprint-mismatch denial | unit (regression guard for `67d24aee4`) | `go test ./internal/agent/ -run TestDeriveToolOperationContext_NestedScope` | Check — likely added alongside the `67d24aee4` fix; verify before assuming Wave 0 |
| SWARM-09 | A worker that dies mid-flight, a daemon restart, and 8x delivery failure each resolve correctly for a REAL enqueued swarm delegation (not the synthetic rows spike 100 used) | db_integration | `go test -tags=db_integration ./internal/swarm/ -run TestDelegationClaimReclaim` | ❌ Wave 0 — this closes spike 100's own stated gap ("the Go claim path was never exercised") |
| SWARM-10 | Per-child transcript is written live, append-only, and is readable by an external tail during the run | integration | `go test ./internal/swarm/ -run TestDumpTranscriptLiveAppend` (extend existing coverage if present) | Check — `report.go` likely has existing coverage; verify |
| SWARM-11 | N/A — process requirement, not a code behavior | manual-only | git log inspection (amendment commit precedes implementation commits) | N/A |

### CLAUDE.md-mandated db_integration identity discipline

**Every `db_integration` test above MUST run as `aura_app`, never as the superuser `aura`
role.** The `aura` role is superuser+BYPASSRLS; a hand-run test as `aura` produces a FALSE GREEN
on any identity-scoping bug in the new delegation queue rows (which carry `identity_id` exactly
like `aura.ingestion_jobs` does today). Copy the coverage gate's `aura_app`/`aura_migrate` DSN
composition pattern rather than inventing a new one; verify locally with
`scripts/coverage_docker.sh` before trusting a green run.

### Sampling Rate

- **Per task commit:** targeted unit tests for the file just touched (`go test ./internal/<pkg>/`)
- **Per wave merge:** `go build ./... && go vet ./... && go test ./... && go test -race
  ./internal/agent/... ./internal/gateway/... ./internal/swarm/... ./internal/runner/...
  ./internal/steer/... ./internal/documents/...`
- **Phase gate:** full `db_integration` matrix as `aura_app` + a fresh live-driven scenario per
  SC#1-#5, scored per CLAUDE.md's >9.8 bar — NOT a green test suite (ACC-01/ACC-02 policy,
  established Phase 45, reaffirmed by ROADMAP's "On evidence" note)

**What a too-coarse sampling rate would miss, per requirement:**
- **SWARM-07 (concurrency):** A single-threaded or sequential test of the fact-write path would
  never exercise the actual race — the requirement is specifically about N WORKERS writing
  CONCURRENTLY. This needs `-race` AND genuine goroutine fan-out (mirroring
  `internal/swarm/swarm_test.go`'s existing wave-concurrency test shape), not a loop of
  sequential calls. A test that calls `UpsertFact` N times in a `for` loop on one goroutine
  would pass green while leaving the actual concurrency hazard (two `Command` calls racing on
  the same content key) completely unexercised.
- **SWARM-09 (crash recovery):** A predicate-only test (spike 100's own method) is a WEAKER
  signal than exercising the real `ClaimIngestionJobs` Go code path under an actual process
  kill — the spike explicitly flagged this gap. A db_integration test that only asserts the SQL
  predicate's logic (as spike 100 did) would miss any bug in the Go transaction/error-handling
  code wrapping that predicate.
- **SWARM-03/06 (live E2E):** A mocked-agent or scripted-SSE test would miss exactly the
  spoofing/discounting behavior spike 098 found — that defect was invisible to mechanics-only
  testing (the frame delivered correctly, `RUN_FINISHED` fired, and the test would have shown
  green) and only surfaced by reading the model's actual reasoning trace. Any Phase 51
  validation for SC#1/#4 must include a live model run with output verified past the raw
  SSE/grep level (Pitfall 4).
- **SWARM-08 (tool dispatch):** A test that only checks registry membership (`Without(reg,
  "swarm_spawn")` returns the right tool NAMES) would have passed throughout the entire period
  the fingerprint-mismatch defect existed — the defect was in DISPATCH, not registry
  construction. Any regression guard must actually dispatch a tool through the gateway, not
  just inspect the registry.

### Wave 0 Gaps

- [ ] `internal/agent/tools/swarm_spawn_schema_test.go` (or extend `swarm_spawn_test.go`) —
      covers SWARM-02's live-cap rendering
- [ ] `internal/swarm/brief_context_test.go` — covers SWARM-01's goal/context split
- [ ] `internal/swarm/delegation_queue_test.go` (db_integration) — covers SWARM-03/09's enqueue
      and claim/reclaim, running the REAL `ClaimIngestionJobs` Go path (closing spike 100's
      stated gap), as `aura_app`
- [ ] `internal/runner/worker_pause_test.go` (db_integration) — covers SWARM-06's fencing column
      and per-worker pause, as `aura_app`
- [ ] `internal/arcadedb/concurrent_fact_write_test.go` (race + arcadedb_integration) — covers
      SWARM-07, genuine goroutine fan-out, not sequential calls
- [ ] A phase-specific live driver script (model: `.planning/spikes/098/drive.sh` and
      `.planning/spikes/099/drive.sh`) for SC#1-#5, since `internal/eval`'s harness stays deleted
- [ ] Migration file(s) for D-06 (Postgres steer/delegation-result table), D-10 (no migration —
      Go-only schema change), D-12 (ALTER `aura.paused_states` ADD COLUMN for the fencing id)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---|---|---|
| V2 Authentication | No | This phase does not touch authentication |
| V3 Session Management | Partial | The per-worker pause (D-12/D-13) is a session-adjacent state machine; fencing by action id IS the session-integrity control (prevents a stale decision resuming the wrong pause) |
| V4 Access Control | Yes | The generalized delegation queue rows carry `identity_id` exactly like `aura.ingestion_jobs` today — RLS/tenant scoping must be preserved for the new `job_type`, and any db_integration test MUST run as `aura_app` (not superuser `aura`) to catch a scoping regression (CLAUDE.md mandate) |
| V5 Input Validation | Yes | Worker report content delivered via the steer rail is untrusted model-generated text; it MUST go through the existing `wrapUntrustedToolOutput`/HTML-escaping path (already the D-04 fix), never the un-escaped operator envelope |
| V6 Cryptography | No | No new cryptographic primitive; the existing nonce minter (`toolOutputNonce()`) is reused, not reimplemented |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---|---|---|
| A worker's report is delivered under an envelope that grants it operator authority (mid-turn prompt injection via self-declared authorship) | Spoofing / Elevation of Privilege | Envelope selection by `steer.SourceWorker`, never by channel name (already built, `f60d109f4`); Pitfall 5 above names the exact regression shape |
| A background delegation row's `identity_id` scoping is bypassed by a claim query run under a superuser-equivalent role | Tampering / Information Disclosure | `db_integration` tests for the new `job_type` MUST run as `aura_app`, never `aura` (CLAUDE.md mandate, `db-integration-must-run-as-aura_app` precedent) |
| A stale/expired pause is resumed by a late-arriving decision, executing a tool call the operator no longer intends | Tampering | D-12's fencing column + `expectActionId`-style conditional UPDATE (`WHERE ... AND pending_action_id = $N AND resumed_at IS NULL`), following the same `RowsAffected==0` idiom already proven in `MarkResumed` |
| A worker asserts its own `run_id`/provenance in `memory_upsert_fact`, forging attribution or masking whose write corrupted the graph | Spoofing / Repudiation | D-10: `RunID`/worker identity become host-derived (closed over `context.Context`), removed from the model-facing schema entirely — "the model gets no field to lie in" |
| A worker calls `supersedes` and closes a parent-asserted or operator-asserted fact it should not have authority over | Tampering / Elevation of Privilege | D-11: workers do not supersede — a worker attempting a correction gets a model-readable refusal, enforced at the same point D-10's actor context is available |

## File Targets and Sizes

**Migration numbering:** the latest migration on disk at research time is
`0102_paused_state_decision_policy` (`ls internal/db/migrations/ | tail -1`). The next free
slot is provisionally **0103**, but per CLAUDE.md's imperative rule this number MUST be
re-derived by re-running that exact command at each migration-creation point during execution
— it is not to be hardcoded from this document, since other in-flight phases could take the
slot first.

| File | Current LOC | Action | >600 LOC risk |
|---|---|---|---|
| `internal/agent/tools/swarm_spawn.go` | 112 | Modify — add `context` field to args/schema, live-render caps into `Spec()` (SWARM-01/02) | Low — schema/description growth is bytes, not LOC; watch the `swarmSpawnDescription` token-cost note already in the file |
| `internal/swarm/brief.go` | 49 | Modify — `structuredBrief(goal, context string)`, new section marker | Low |
| `internal/swarm/swarm.go` | 245 | Modify — add background-vs-sync branch in `Run()`, enqueue call, depth-gated registry restriction removal (SWARM-03/04/05) | **Watch.** This file already carries preflight, wave concurrency, and child lifecycle; adding a background/enqueue branch risks crossing 600 LOC. Plan to split a new `internal/swarm/delegation_queue.go` for the enqueue/claim-loop logic rather than growing `swarm.go` further — mirrors the existing `swarm_depth.go`/`report.go`/`brief.go` split-by-concern precedent in this same package |
| `internal/swarm/swarm_depth.go` | small (not yet measured in detail; contains `maxDepth`, `checkDepth`) | Modify — degrade-to-leaf-at-cap logic if the phase adopts hermes' `role: leaf|orchestrator` framing instead of hard rejection | Low |
| `internal/swarm/report.go` | 85 | Modify (minor) — no change expected unless SWARM-10 gets an exposure API; if so, split into a new `internal/swarm/transcript_api.go` | Low |
| **NEW** `internal/swarm/delegation_queue.go` (or similarly named) | 0 | Create — the claim-loop worker analogous to `internal/documents/jobs_worker.go`, dispatching swarm goals instead of ingestion jobs | New file — keep ≤600 LOC from the start; if the claim loop plus the completion-delivery wiring grows large, split delivery into its own file (`delegation_delivery.go`) |
| `internal/documents/jobs_store.go` | 408 | Modify — extend for the new `job_type` if any new columns/queries are needed (likely none; `job_type`/`payload` are already generic) | **Watch** — already at 408/600; avoid adding swarm-specific columns here, keep the generic queue generic and push swarm-specific shape into `payload` jsonb |
| `internal/documents/jobs_worker.go` | 272 | Read as a pattern reference for the new claim-loop worker; likely NOT modified directly | Low |
| `internal/steer/inbox.go` | 151 | Modify (D-06/D-07) — this is the file that changes shape most: in-memory `map[string][]Message` becomes a Postgres-backed store behind the SAME `Push`/`Drain` interface contract (`SteerInbox` in `llm_agent_steer.go` already isolates callers from the concrete type) | **Watch** — adding Postgres query logic to a 151-LOC in-memory file will likely need a split: keep `inbox.go` as the interface/types file, add a new `internal/steer/pg_store.go` for the Postgres implementation |
| `internal/agent/llm_agent_steer.go` | 154 | Modify (minor) — likely no change; `markSteer`/`drainSteer` already handle both sources correctly, this file mainly needs the PRODUCER wired elsewhere | Low |
| `internal/runner/approval_expiry.go` | 41 | Read as the sweep pattern for D-08 (expired row leaves a readable trace); likely a new sibling file for the delegation-result TTL sweep, not a modification of this one (it is approval-specific) | Low |
| `aura.paused_states` (via `internal/runner`) | N/A (DB table) | Modify — ALTER TABLE ADD COLUMN for D-12's fencing action id; new migration `01NN_paused_states_fencing.up.sql` | N/A (schema, not LOC) |
| `cmd/arcadedb-mcp/tool_memory.go` | 416 | Modify (D-10) — remove `run_id` from `MemoryUpsertFactInput`'s model-facing schema, derive it from host context instead | Low — removing a field shrinks, not grows, this file |
| `internal/arcadedb/memory.go` | 543 | Modify (D-10/D-11) — `UpsertFact`'s `FactSource` construction takes a host-derived actor param; add the D-11 supersede-authority check | **Watch** — already at 543/600, close to the ceiling; a supersede-authority check plus an actor-context parameter could push this over. Plan to extract the D-11 authority check into a new `internal/arcadedb/fact_authority.go` rather than growing this file further |
| `internal/config/config.go` | large multi-hundred-line file (caps at lines 95-97 confirmed) | Modify (minor) — no new caps expected; SWARM-02 reads the three that already exist | Low |
| **NEW** migration `0103_swarm_delegation_job_type.up.sql` (or no-op if `job_type` needs zero schema change) | 0 | Create only if a CHECK constraint or index needs updating for the new `job_type` value; `aura.ingestion_jobs` is likely schema-stable and needs no migration at all for D-01's generalization | New file, small |
| **NEW** migration for D-06 (Postgres steer/delegation-result table) | 0 | Create — one table, `kind` discriminator column, two TTL columns per D-07 | New file, small |
| **NEW** migration for D-12 (fencing column on `aura.paused_states`) | 0 | Create — single `ADD COLUMN` | New file, small |

**Files at real risk of exceeding 600 LOC if the planner does not pre-split:**
`internal/swarm/swarm.go` (245 -> risk if background/enqueue logic is added inline instead of
into a new file) and `internal/arcadedb/memory.go` (543 -> risk if D-11's authority check is
added inline instead of extracted). Both should be named in the plan as "modify + extract new
file," not "modify in place."

## Risk Register

### D-06 — steer inbox: in-memory -> Postgres with TTL (one-way)

**What it breaks if wrong:** `internal/steer.Inbox`'s `Push`/`Drain` contract is called from
THREE places today — the AG-UI HTTP steer route (Phase 52), the Telegram bot dispatch path
(Phase 52), and `drainSteer` in `internal/agent`. Amendment #154 confirms this reopens Phase 52
code merged 2026-08-25. If the Postgres-backed implementation's `Drain` semantics diverge even
slightly from the in-memory version's "atomically pop everything queued, a second Drain
returns empty" contract, all three callers silently change behavior. Because `SteerInbox` is
already an interface in `llm_agent_steer.go`, the blast radius is contained to implementations
of that interface, not the callers — this is the cheapest possible shape for this migration.
**Cost of a cheap reversal:** LOW-MEDIUM. Because the interface seam already exists, reverting
to the in-memory `Inbox` requires no caller changes — only re-pointing the composition root's
wiring. The EXPENSIVE part is not code reversal, it's DATA: once steer/delegation-result rows
live in Postgres with a TTL, reverting means deciding what happens to in-flight rows (drain
them into memory? drop them?). This is why CONTEXT.md marks it one-way — not because the code
is hard to revert, but because live data has no clean rollback.

### D-10 — `memory_upsert_fact` schema change: `RunID` leaves the model-facing schema (one-way)

**What it breaks if wrong:** This is a PUBLISHED MCP tool schema. Per CONTEXT.md, it "touches
the same caller set MEM-04/D-19 already covers": the bridge, the CLI, and host-driven writes.
Any external caller (a skill, a script, a future integration) that currently passes
`source.run_id` explicitly will have that field silently ignored (if the schema keeps it as an
accepted-but-unused input) or rejected (if the schema removes it entirely) — the planner must
pick one and document it, since CONTEXT.md does not specify which.
**Cost of a cheap reversal:** MEDIUM. Reverting means re-adding a `jsonschema:"required"` model
input and re-trusting model-asserted provenance — cheap in LOC, but expensive in TRUST: every
fact written in the interim under the new host-derived scheme has a real, host-verified
`run_id`; facts written after a revert would go back to being model-asserted, silently
degrading provenance quality history-wide without a visible marker distinguishing the two
eras.

### D-12 — pause fencing column on `aura.paused_states` (one-way)

**What it breaks if wrong:** Adds a guarded column and changes `CommitResumeBatch`'s argument
contract. Per CONTEXT.md this is folded together with the `approval-resume-defects` constraint
("New validation goes inside that transaction's front door, never as a second path around
it") — meaning the fencing check MUST live inside the same cross-store transaction
`CommitResumeBatch` already uses under sorted-token deadlock-free ordering, not as a
before/after check. Getting this wrong reintroduces exactly the double-drive/double-bill defect
LibreChat's own design note warns about ("a double-drive re-executes tools and double-bills").
**Cost of a cheap reversal:** LOW for the column itself (a nullable ADD COLUMN, trivially
droppable), but MEDIUM for the call-site contract: `CommitResumeBatch`'s new
`expectActionId`-shaped argument, once callers depend on it for correctness, cannot be silently
dropped without re-auditing every resume call site for the double-drive hazard it was added to
prevent.

### Not one-way, but worth flagging: the new claim-loop worker's placement

Not named as one-way in CONTEXT.md, but structurally similar in risk: wherever the new
delegation claim-loop worker is wired into `cmd/aura`'s composition root determines whether it
inherits the SAME `gateway.WithDelegatedDispatch` discipline the `791dcd7e0` fix established.
Getting this wrong reintroduces the exact orphaned-reservation defect this phase's own design
gate already found and fixed once. Low cost to fix if caught in review (it's a missing context
marker), but easy to miss because the failure mode (a false `indeterminate` stamp) only
surfaces 30 minutes later via the reconciler, not immediately.

## Sources

### Primary (HIGH confidence — live spikes on the running stack, or direct code reads)
- `.planning/spikes/100-durable-substrate-shape/README.md` — D-01 (verdict: validated)
- `.planning/spikes/101-message-channel-necessity/README.md` — D-02 (verdict: validated)
- `.planning/spikes/099-worker-duration-and-progress/README.md` — D-03 (verdict: validated)
- `.planning/spikes/098-steer-carries-worker-result/README.md` — D-04 amendment (verdict:
  validated, was PARTIAL)
- `prd.md:8504-8585` — PRD Amendment #154, closing the design gate
- `.planning/phases/51-durable-delegation/51-CONTEXT.md` — locked decisions D-00..D-14
- `.planning/REQUIREMENTS.md:140-150` — SWARM-01..11 canonical text
- `.planning/ROADMAP.md:505-577` — Phase 51 goal/rationale/success criteria (stale blocking
  banner noted and corrected)
- `docs/aura-quality-snapshot.md` — Swarm E2E row, re-measured 2026-08-27 (`0e7cc821a`)
- Direct reads: `internal/agent/tools/swarm_spawn.go`, `internal/swarm/swarm.go`,
  `internal/swarm/brief.go`, `internal/swarm/report.go`, `internal/agent/swarm_context.go`,
  `internal/steer/inbox.go`, `internal/agent/llm_agent_steer.go`,
  `internal/runner/approval_expiry.go`, `cmd/arcadedb-mcp/tool_memory.go`,
  `internal/arcadedb/memory.go`, `internal/agent/idempotency_operation.go`,
  `internal/documents/jobs_store.go`, `internal/config/config.go`,
  `internal/config/config_knobs_test.go`, `internal/db/migrations/` (directory listing)
- Commits inspected via `git show --stat`/full diff: `f60d109f4`, `6793cf6b3`, `56c7a5247`,
  `a798f6005`, `268580e23`, `70e4a0b43`, `83bae3f6e`, `791dcd7e0`, `8f58d5bb5`, `e92be2919`,
  `0e7cc821a`, `67d24aee4`

### Secondary (MEDIUM confidence)
- `docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md` — read for mechanics
  only (leases, fencing, backoff-with-jitter, `waiting_input` pattern), explicitly NOT as
  settled scope per D-00/D-01/D-02

### Tertiary (LOW confidence / not independently re-verified this session)
- LibreChat/hermes source file citations (`ApprovalLifecycle.ts`, `delegate_tool.py`,
  `async_delegation.py`, etc.) — taken as accurately quoted by CONTEXT.md and the spike
  READMEs, not independently re-read in this research pass (out of budget; the spikes already
  did this reading and cited line numbers). If the planner needs a fresh LibreChat/hermes
  citation, re-verify against `D:/tmp/LibreChat` / `D:/tmp/hermes-agent` directly.

## Metadata

**Confidence breakdown:**
- Gate status / D-01/D-02/D-03 answers: HIGH — each is a live measurement with a stated
  perimeter of what it does not prove, cross-checked against two reference implementations
- Already In The Tree inventory: HIGH — every row cites a specific file:line or commit hash
  read directly in this session
- Validation Architecture: MEDIUM-HIGH — test file paths for NEW tests are proposed, not
  discovered (they don't exist yet); the reused patterns (`jobs_store_test.go`,
  `swarm_test.go:464`) are HIGH confidence
- Risk Register: HIGH for what breaks (traced to specific caller sets in the code); MEDIUM for
  reversal cost estimates (reasoned from the code's structure, not measured)
- Security Domain: MEDIUM — ASVS mapping is reasoned from the phase's actual data flows, not
  independently audited; no live penetration test was run this session

**Research date:** 2026-08-27
**Valid until:** This research is tied to a specific measured commit state (`master` at
`0e7cc821a`). If Phase 51 planning is delayed and additional commits land on `master` touching
`internal/swarm/`, `internal/steer/`, `internal/gateway/`, or `internal/arcadedb/memory.go`
before planning starts, re-verify the "Already In The Tree" table against current `git log`
before trusting it — this document ages in days, not weeks, given how active this exact area
of the codebase is (16 commits touching this domain in the 48 hours preceding this research).
