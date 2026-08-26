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
- **Sequencing is spike -> PRD amendment (SWARM-11) -> one single Phase 51.** No phase split.
  The amendment must also carry the three work items ROADMAP section 51 does not account for:
  the steer queue moving to Postgres, the `memory_upsert_fact` schema change, and the pause
  fencing column.

## Spikes

| # | Idea | Name | Type | Validates | Verdict | Tags |
|---|------|------|------|-----------|---------|------|
| 098 | durable-delegation | steer-carries-worker-result | standard | Given a worker completion pushed into the steer inbox with a worker-attributed marker, when the parent turn reaches its next round boundary, then it lands in history without breaking role alternation or touching `history[0..2]`, and reads as spoken by a worker rather than by the operator | **PARTIAL** - rail validated, envelope invalidated: the model detects operator-envelope vs worker-payload mismatch and discounts the report as injection (3/3 live runs) | steer, delivery, kv-cache, attribution |
| 099 | durable-delegation | worker-duration-and-progress | standard | Given a real fan-out on the live stack, when workers run to completion, then measured durations show whether a 120s wall-clock ceiling is survivable, and whether per-worker progress is observable enough to drive hermes-style staleness | PENDING | swarm, timeout, observability |
| 100a | durable-delegation | substrate-single-table | comparison | Given a delegation in flight, when the worker dies mid-flight / the daemon restarts / delivery fails 8 consecutive times, then the delegation is never silently lost and converges to a readable terminal state | PENDING | postgres, durability, lease |
| 100b | durable-delegation | substrate-generalized-queue | comparison | The same question, generalizing the proven `aura.ingestion_jobs` lease engine instead of adding a new table | PENDING | postgres, durability, lease, reuse |
| 101 | durable-delegation | message-channel-necessity | standard | Given the delegation ledger plus the steer rail, when a background delegation must return a result AND its worker must ask the operator, then either no agent-to-agent message channel is needed, or the spike names exactly what cannot be expressed without one | PENDING | scope, messaging |
