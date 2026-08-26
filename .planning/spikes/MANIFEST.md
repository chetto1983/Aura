# Spike Manifest

## Ideas

### durable-delegation

Phase 51's design gate. A delegated worker must get a brief worth acting on and limits it can
see, must be able to orchestrate workers of its own, and a top-level delegation must stop
holding the operator's turn hostage - results re-enter the conversation when the work is
actually done. ROADMAP section 51 forbids `/gsd-plan-phase` until an unresolved
inventory question is answered by measurement: *is background delegation a new execution path
or a second caller of `agent_job` / AG-UI run-detach?* The 2026-06-29 design doc that would
have answered it is stale - it targets migration slot `0026` while the tree is at `0102`, and
its "Current Context" predates `aura.ingestion_jobs`, which already implements most of what it
proposes. These spikes measure what the discussion deliberately refused to settle from the
armchair. Full decision record: `.planning/phases/51-durable-delegation/51-CONTEXT.md`.

**Requirements:**

- **LibreChat (`D:/tmp/LibreChat`) is the reference implementation; port its pattern rather
  than inventing one.** Operator directive, given twice: *"guarda sempre librechat non
  inventare niente"*, then *"copia da librechat che è un pattern industriale"*. Honest fit
  boundary to state wherever it bites: LibreChat is TypeScript over Mongo/Redis, Aura is Go
  over Postgres - port the mechanics (guarded status transitions, a flat top-level mirror so a
  CAS can compare it, lazy expiry on read, an injectable store seam), never the stack. Where
  LibreChat has no equivalent of an Aura problem, say so plainly instead of forcing the
  analogy. `D:/tmp/hermes-agent` is the second reference, named by ROADMAP section 51.
- **Delivery rides the shipped steer rail; no second delivery mechanism gets built.**
  `internal/steer` + `LlmAgent.drainSteer` already deliver at a round boundary behind a
  non-forgeable nonce envelope, budget-free, without touching `history[0..2]`.
- **The queue is Postgres-backed with a TTL.** One table, rows typed by kind
  (`steer` | `delegation_result`), two TTLs - short for a steer, long for a completion - one
  sweep, two knobs. In-memory and consume-once is what is being replaced.
- **An expired row is never silently dropped.** A readable trace is written in the same
  transaction that marks it expired, following the shipped approval-expiry precedent.
- **Memory provenance splits:** `RunID` and worker identity become host-derived and leave the
  model-facing schema entirely; `MemoryIDs` stays model-supplied. The model gets no field to
  lie in - LibreChat's `createMemoryTool` closes the actor at construction.
- **Workers add facts; they never supersede.** Corrections stay with the parent and the
  operator; an attempted supersede returns a model-readable error.
- **Each worker's question is its own pause, fenced by an action id kept in a column**, not a
  jsonb path - LibreChat keeps `pendingActionId` flat precisely so an atomic transition can
  guard on it. Answering one pause must never resume another.
- **A nested worker asks like anyone else** and its pause carries the identity of the level it
  came from, so the resume replays into exactly that level. No auto-deny, no bespoke
  chain-suspension.
- **A worker completion needs its OWN envelope, distinct from `<user_steer>`, granting
  tool-result trust rather than operator trust** (spike 098, 3/3 live runs). `<user_steer>`
  declares the operator as author; a worker report declares a worker; the model trusts the
  payload's self-declared authorship over the envelope and discounts the whole thing as
  spoofing. Reusing the rail is fine - reusing the envelope silently loses the result.
- **Worker tool dispatch was broken; it is fixed, and the phase can be built on it**
  (spike 099, live, deterministic, re-measured). `swarm_spawn` and the 10 agent-scoped tools
  share `OperationScope: OperationScopeAgent`, so `deriveToolOperationContext`'s
  `parent.Key.Scope == spec.OperationScope` early-return left `swarm_spawn`'s operation in
  the worker's context; the gateway then recomputed the fingerprint for the worker's actual
  tool and denied on mismatch. Measured 4/4 workers, 100% of dispatches; re-measured at 0
  denials and 3 real `shell_exec` executions after the fix. This IS SWARM-08's "verified
  against the live surface rather than assumed from registry inheritance", and the live
  surface now says workers both inherit the registry and dispatch through it.
- **A nested tool call's idempotency key cannot tell two sibling workers apart** (spike 099
  fix, characterized in `TestSiblingWorkersOfOneSpawnCollapseOnAnIdenticalCall`). The derived
  key is parent key + parent fingerprint + tool + args + round ordinal, because request and
  tool-call ids are audit-only by design (a same-round retry must collapse). Two siblings of
  ONE spawn issuing a byte-identical mutating call at the same ordinal therefore share a key
  and the second gets in-progress or a marked replay rather than a second mutation — safe,
  but it is D-10's call whether worker identity should join that key.
- **The child timeout is 120s nominal but 240s effective** (spike 099). The recovery turn
  deliberately severs the expired deadline (`context.WithoutCancel`, fix-plan 1.1) and its
  call is bounded by `AURA_LLM_TOTAL_TIMEOUT_SEC` (120 here). Observed once at 139.77s
  returning `ok`. Any lease or reclaim interval sized against 120s would reclaim a live worker.
- **A wall-clock cap is the wrong instrument, measured** (spike 099, third run, live). Four
  workers on four executable goals finished in **5.15 / 5.51 / 7.58 / 7.80s** against a 120s
  nominal cap — a 23x margin — with every answer verified against ground truth taken from the
  box beforehand. Across three runs the cap fired exactly ONCE, and it caught an OpenRouter
  `context deadline exceeded`, an upstream stall. The worker genuinely worth intervening on —
  70.31s spent calling tools that could never succeed — sailed under the cap and returned
  `ok`. The 23x spread inside one fan-out is itself the argument: no constant fits both ends.
- **A worker's tool call used to orphan its reservation; fixed in `791dcd7e0`** (spike 099,
  measured, explained, re-measured). The gateway wrote `start`, the Runner wrote `end` from
  turn frames a worker's stream never reaches, and the reconciler then stamped SUCCEEDED
  worker calls as indeterminate failures 30 minutes later. The gateway now closes what it
  opens, gated on a marker `runChild` sets: workers went 5 starts / 0 ends to 5 / 5, the
  reconciler's anti-join returns 0, and the parent's richer end is untouched. The rule is
  LibreChat's: whoever opens a record closes it.
- **Staleness still cannot be driven from `aura.tool_invocations`** (spike 099). The rows are
  honest now, but they say a call ENDED; staleness needs to know a worker is still ALIVE
  part-way through one. LibreChat fires `recordActivity` per emitted chunk, not per finished
  tool call, so the tick belongs in `runChild`'s event loop (`swarm.go:185`).
- **The substrate already exists: generalize `aura.ingestion_jobs`** (spike 100, measured).
  Its claim predicate handles all three of D-01's failure modes correctly — it re-claims a
  `running` row whose lease expired, refuses one at `attempt_count = max_attempts`, and
  leaves a live lease alone — and a daemon restart needs no recovery path because that same
  steady-state claim IS the recovery. `job_type` is already the discriminator; the file calls
  itself "The GENERIC asset queue" and has already survived one de-specialization (0098).
- **The delegation's intent is already durable; only the "this is owed" row is missing**
  (spike 100, SIGKILL two seconds into a live four-worker fan-out). What survived: the
  `swarm_spawn` `start` row carrying the FULL goals in `args_raw`. What did not: any worker
  row, any queue row, any terminal frame for the client — the stream simply truncates
  mid-sentence with neither `RUN_FINISHED` nor `RUN_ERROR`.
- **The two references disagree on durability, and LibreChat is the one that persists less**
  (spike 100 research). hermes keeps a durable SQLite delegation ledger with restart
  recovery; LibreChat keeps NOTHING for a failed non-paused turn — `LazyMongoSaver` persists
  only checkpoints carrying a resumable pending write, so *"an errored turn still leaves
  NOTHING durable (0 checkpoints, 0 write rows)"*. It makes durable exactly one thing: a
  pause somebody is expected to come back to, rebuilt by SEEDING the new owner with the old
  owner's whole state rather than reconciling.
- **Reap on inactivity, not on age** (spike 099 research). LibreChat refreshes `lastActivity`
  on every emitted chunk so *"a long but live stream is never reaped"*; hermes leaves an
  advancing child alone forever. Aura caps total age and consumes no liveness signal, although
  `runChild`'s event loop is exactly where the tick belongs.
- **Sequencing is spike -> PRD amendment (SWARM-11) -> one single Phase 51.** No phase split.
  The amendment must also carry the three work items ROADMAP section 51 does not account for:
  the steer queue moving to Postgres, the `memory_upsert_fact` schema change, and the pause
  fencing column.

## Spikes

| # | Idea | Name | Type | Validates | Verdict | Tags |
|---|------|------|------|-----------|---------|------|
| 098 | durable-delegation | steer-carries-worker-result | standard | Given a worker completion pushed into the steer inbox with a worker-attributed marker, when the parent turn reaches its next round boundary, then it lands in history without breaking role alternation or touching `history[0..2]`, and reads as spoken by a worker rather than by the operator | **PARTIAL** - rail validated, envelope invalidated: the model detects operator-envelope vs worker-payload mismatch and discounts the report as injection (3/3 live runs) | steer, delivery, kv-cache, attribution |
| 099 | durable-delegation | worker-duration-and-progress | standard | Given a real fan-out on the live stack, when workers run to completion, then measured durations show whether a 120s wall-clock ceiling is survivable, and whether per-worker progress is observable enough to drive hermes-style staleness | **validated** - healthy workers finish in 5.15-7.80s against a 120s cap (23x margin, answers verified against ground truth); the cap fired once in three runs and caught an upstream stall, while the 70s lost worker passed under it; staleness cannot come from `tool_invocations` (workers log `start`, never `end`) and belongs in `runChild`'s event loop. Also found and fixed a live deterministic defect: a swarm worker could dispatch NO agent-scoped tool (4/4 workers, 100% denied `operation fingerprint mismatch`; re-measured at 0 denials); corrected the child cap from 120s nominal to 240s effective | swarm, timeout, observability, defect |
| 100 | durable-delegation | durable-substrate-shape | standard | Given the three failure modes D-01 names — a worker dying mid-flight, a daemon restart, and delivery failing eight times — measured against the lease queue Aura already owns, then the substrate choice is decided by evidence rather than preference | **validated** - GENERALIZE `aura.ingestion_jobs`, do NOT create a delegation table: its claim predicate already handles all three scenarios correctly (measured), `job_type` is already the discriminator, a restart needs no recovery path because the lease expiry IS one, and a SIGKILL mid-fan-out showed the delegation's full intent is ALREADY durable in the `swarm_spawn` reservation's args — what is missing is a row saying the work is owed | substrate, durability, lease, crash-recovery, inventory |
| 100a | durable-delegation | substrate-single-table | comparison | Given a delegation in flight, when the worker dies mid-flight / the daemon restarts / delivery fails 8 consecutive times, then the delegation is never silently lost and converges to a readable terminal state | PENDING | postgres, durability, lease |
| 100b | durable-delegation | substrate-generalized-queue | comparison | The same question, generalizing the proven `aura.ingestion_jobs` lease engine instead of adding a new table | PENDING | postgres, durability, lease, reuse |
| 101 | durable-delegation | message-channel-necessity | standard | Given the delegation ledger plus the steer rail, when a background delegation must return a result AND its worker must ask the operator, then either no agent-to-agent message channel is needed, or the spike names exactly what cannot be expressed without one | PENDING | scope, messaging |
