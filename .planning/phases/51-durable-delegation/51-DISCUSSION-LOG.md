# Phase 51: Durable delegation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md - this log preserves the alternatives considered.

**Date:** 2026-08-26
**Phase:** 51-durable-delegation
**Areas discussed:** Durable substrate, Turn return path (SC#1), Concurrent memory writes
(SWARM-07), Worker-to-operator relay (SWARM-06), Design gate and phase slicing

The operator selected all four offered gray areas and added a fifth instruction inline:
*"controlla hermes e librechat in d:/tmp"*. Reference reading was therefore done before
the questions, as ROADMAP section 51 requires, not after.

---

## Todo cross-reference

| Option | Description | Selected |
|--------|-------------|----------|
| Fold as constraint | Enters CONTEXT.md as a constraint on SWARM-06: the relay must go through the `CommitResumeBatch`/`MarkResumed` transaction, never a second path around it; inherits `AURA_ASKUSER_PAUSE_TTL_SEC` | Y |
| Note as reference only | Cited in canonical refs without being promoted to a phase constraint | |
| Ignore | Closed and out of scope | |

**User's choice:** Fold as constraint.
**Notes:** All three defects in `approval-resume-defects` were closed 2026-08-25; what
survives is the transaction-front-door rule and the 48h pause TTL precedent.

---

## Durable substrate

| Option | Description | Selected |
|--------|-------------|----------|
| Single table, hermes-shaped | `aura.swarm_delegations`: one row per delegation with owner fencing, state, delivery_state, delivery_attempts, self-contained task payload, boot recovery. No messages/artifacts/channel_threads | |
| The doc as approved (4 tables) | Build the design doc's slice-1: channel_threads + tasks + messages + artifacts, own claim/lease/heartbeat/retry, plus `agent_message_send` beside `swarm_spawn` | |
| Generalize `aura.ingestion_jobs` | Extract the proven queue engine (SKIP LOCKED, locked_by/locked_until, attempt_count, lease_generation, expired-lease reclaim, event log) into a reusable package; delegation becomes a second job_type | |
| Decide by measurement | A short spike compares the candidates on the real failure cases and CONTEXT records the measured winner | Y |

**User's choice:** Decide by measurement.
**Notes:** Presented alongside the measured fact that the approved design doc targets
migration slot `0026` while the tree is at `0102`, and that its "Current Context" section
predates `aura.ingestion_jobs`, which already implements most of what it proposes.

---

## Phase perimeter: `agent_message_send`

| Option | Description | Selected |
|--------|-------------|----------|
| Out - amend the doc | No second model-facing tool; the 4-table messaging falls with it since it existed to serve that tool | |
| In - as per doc | Build `agent_message_send` with its pinned Normal gateway tier, as the approved document prescribes | |
| Out, but keep the exit open | Do not build it now, but avoid a substrate schema that would foreclose a generic messaging layer later | |
| The spike decides | The spike also measures whether background delegation needs a separate message channel at all | Y |

**User's choice:** The spike decides.
**Notes:** None of SWARM-01..11 names `agent_message_send`; the doc's slice-1 explicitly
declined to touch `swarm_spawn`, which is the opposite of what SWARM-01..05 require.

---

## Worker termination model

| Option | Description | Selected |
|--------|-------------|----------|
| Progress, not wall-clock | Adopt hermes' progress-based staleness: a worker that advances is left alone forever; no progress past threshold triggers interrupt, a grace window to deliver partials, then terminal `stalled` | |
| Wall-clock, higher in background | Keep the wall-clock ceiling with two values, one for synchronous/nested and a much larger one for background | |
| Unchanged for now | Move the turn to background with today's 120s and treat duration as a separate phase | |
| Measure it in the spike | The spike records how long workers actually run on a real fan-out and the termination model follows that number | Y |

**User's choice:** Measure it in the spike.
**Notes:** hermes rejects wall-clock explicitly - *"legitimate heavy subagent work must never
be killed for taking long"*. Aura's `AURA_SWARM_CHILD_TIMEOUT_SEC=120` is exactly the model
hermes abandoned.

---

## Turn return path (SC#1)

| Option | Description | Selected |
|--------|-------------|----------|
| Queue, new turn when idle | hermes' rail: the completion waits in a durable queue and becomes a NEW turn only once the current turn ends, never spliced between a tool result and the next message | |
| The Phase 52 drain point | Reuse the just-shipped steering delivery: append to the last tool result of the in-flight batch behind an attribution marker | |
| Origin-channel delivery | Reuse cron's `deliverToOrigin`, as an `agent_job` already does | |
| Queue plus delivery | Both: a new turn when idle AND a channel notification | |

**User's choice:** free text - *"la steer e gia fatta"*.
**Notes:** Read as: the delivery rail already exists and no second one gets built. Confirmed
after reading the shipped code - `internal/steer` + `drainSteer` deliver at a round boundary
behind a non-forgeable nonce envelope, with lookalike scrubbing, budget-free, never touching
`history[0..2]`, and `Message.Source` already carries the producing channel. Two boundaries
were recorded rather than treated as objections: the inbox is in-memory and consume-once
(addressed by the next question), and the `user_steer` marker tells the model the content
came from the operator, so a worker report needs a distinct marker or SC#4's attribution
inverts.

---

## Channel capability gate

| Option | Description | Selected |
|--------|-------------|----------|
| Refuse explicitly | Return a model-readable error when the channel cannot deliver after the turn ends, as hermes' `async_delivery_supported()` does | |
| Degrade to synchronous | Fall back to today's blocking delegation on such channels | |
| No channel is excluded | Delivery is server-side durable, not session-bound, so the gate solves a problem Aura does not have | |

**User's choice:** free text - *"la steer e gia fatta"*.
**Notes:** Read as "no channel is excluded": steer already arrives from both
`internal/agui/server_run_steer.go` and `internal/channels/telegram/bot_dispatch_steer.go`.

---

## Durability of the delivery queue

**User's choice:** free text - *"io metterei tutto su postgres con una ttl piu sicuro e meno
fragile"*. Not offered as options; raised by the operator.
**Notes:** Closes both boundaries recorded above. Consequence named at the time: this
reopens `internal/steer`, Phase 52 code merged 2026-08-25, and must appear in the PRD
amendment rather than being discovered by the planner.

---

## TTL shape

| Option | Description | Selected |
|--------|-------------|----------|
| Two TTLs, one table | Rows typed (steer / delegation_result), short TTL for steers, long for completions, one sweep, two knobs | Y |
| One long TTL | Everything expires on the 48h pause horizon; one knob, no row typing | |
| Two separate queues | Distinct tables with their own TTL and sweep each | |
| The spike decides | Measure real row ages first | |

**User's choice:** Two TTLs, one table.
**Notes:** A steer delivered hours late is actively harmful; a worker report stays valid for
hours. Precedent surfaced during the question: `AURA_ASKUSER_PAUSE_TTL_SEC` = 172800, with
`<=0` disabling expiry, swept by `ExpirePendingApprovals(ctx, cutoff, limit)` without driving
a model turn.

---

## Expiry visibility

| Option | Description | Selected |
|--------|-------------|----------|
| Follows precedent, never silent | A readable trace is written in the same transaction that marks the row expired, mirroring how approval expiry persists an `expired` refusal plus its `RoleTool` answer | Y |
| Silent but queryable | Terminal and undelivered, inspectable from CLI/audit only, like hermes' `dropped` after 8 attempts | |
| Depends on the type | Expired steers die silently, expired completions leave a trace | |

**User's choice:** Follows precedent, never silent.
**Notes:** LibreChat does the same on its expire transition
(`error: 'Approval expired before a decision was made'` written with `completedAt`).

---

## Memory provenance (SWARM-07 / SC#5)

Raised by the operator inline: *"poi abbiamo anche arcadedb con mcp di memoria a lungo
termine"*.

| Option | Description | Selected |
|--------|-------------|----------|
| Host-stamped, like MEM-04 | Worker identity rides the MCP call context and the server stamps it before `arcadedb.Fact` is built, where `canonicalSubject` already rewrites the subject | |
| New model-filled field | Widen `FactSource` with a worker field the worker's brief tells it to fill | |
| RunID by convention | No schema change; each worker reuses its own run id as attribution | |

**User's choice:** free text - *"guarda come fa librechat"*.
**Notes:** LibreChat's answer proved stronger than the offered host-stamping option:
`createMemoryTool({ userId, setMemory, ... })` closes the actor at construction and the
model-facing schema takes only `key`+`value` - the model gets no field to lie in, rather
than a field the host overwrites. Beneath it, `tenantContext.ts` carries
`{tenantId, userId, requestId}` in an `AsyncLocalStorage`, and even `runAsSystem()`
preserves `userId`/`requestId`.

### Follow-up: where to cut the provenance

| Option | Description | Selected |
|--------|-------------|----------|
| Cut there | `RunID` + worker identity become host-derived and leave the model-facing schema; `MemoryIDs` stays model-supplied | Y |
| Remove all of Source | `MemoryIDs` also becomes host-derived from what was injected that turn | |
| Only add worker, remove nothing | Keep `RunID` and add a host-stamped worker field beside it | |

**User's choice:** Cut there.
**Notes:** Rejected the third option's failure mode explicitly - two truths about the same
write. `MemoryIDs` stays because which retrieved memories support a fact is genuinely the
model's knowledge.

---

## Concurrent supersede

| Option | Description | Selected |
|--------|-------------|----------|
| Workers do not supersede | A worker ADDS facts; corrections stay with parent and operator; an attempted supersede returns a model-readable error | Y |
| Atomic via `Script` | Convert `UpsertFact` from N separate `Command` calls to one BEGIN/COMMIT script - the method exists in the client and is unused here | |
| Serialized memory writes | One worker writes at a time per identity | |
| Measure it in the spike | Run N workers concurrently against one graph first | |

**User's choice:** Workers do not supersede.
**Notes:** Removes the race by scope rather than by locking. Accepted cost stated at the
time: a worker cannot rectify what it finds wrong. The measured basis: `UpsertFact` is a
sequence of separate transactions and only the create leg has a race compensation;
`closeSuperseded` has none, and Phase 45's fix to it was semantic, not concurrency-safe.

---

## Worker-to-operator relay (SWARM-06)

| Option | Description | Selected |
|--------|-------------|----------|
| All of them, each named | Every question is its own `ask_user` pause attributed to the worker that raised it; no cap | |
| One at a time, others queued | Serialize worker pauses; the rest park in `waiting_input` | |
| Configurable cap, then auto-deny | Up to N live questions; past the cap the worker gets hermes' recoverable refusal | |
| Only the first may ask | One worker per fan-out holds the right to interrupt the operator | |

**User's choice:** free text - *"guarda librechat"*.
**Notes:** LibreChat's `ApprovalLifecycle` supplied the mechanism none of the options
named: the run is the unit that pauses, holds at most one pending action, and
`pendingActionId` is a flat top-level mirror of `pendingAction.actionId` kept flat
*specifically* so an atomic transition can guard on it. `expectActionId` prevents a stale
decision resuming a run that has since paused for a different action; `resolve` returns
true to exactly one caller because a double-drive re-executes tools and double-bills; and
`peek` applies lazy expiry so a stale prompt is never surfaced. Aura consequence recorded:
`resume_context` is jsonb, so the fencing id must be a column.

---

## Nested worker asking the operator

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, pauses the whole chain | The question rises as an `ask_user` pause and grandchild, worker and parent all wait | |
| No, recoverable refusal | Only a top-level background worker may reach the operator | |
| Asks its parent, not the operator | The question rises one level to the orchestrating worker | |

**User's choice:** free text - *"guarda librechat"*.
**Notes:** LibreChat identifies a pause by `runId` = the LangGraph `checkpoint_ns`; a nested
subgraph owns its namespace, so the pause names its level and the resume replays into that
level. No chain-suspension concept and no auto-deny is needed. `PendingAction` also carries
`interruptId`/`threadId` for cross-process resume and `requestFingerprint`/`resumeContext`
because the resume request cannot reconstruct the original.

---

## Phase slicing

| Option | Description | Selected |
|--------|-------------|----------|
| Spike -> amendment -> one Phase 51 | Spike is the design gate, its outcome enters the PRD amendment, then one phase carries all 11 requirements | Y |
| 51a surface, 51b durable | Split the spike-independent requirements (01, 02, 05, 07, 08) from background/durability | |
| Decide after the spike | Do not choose the cut now | |
| Extract memory into its own phase | SWARM-07 leaves 51 and becomes a memory-correctness phase | |

**User's choice:** Spike -> amendment -> one Phase 51.
**Notes:** SWARM-11 already requires the PRD amendment before any code, so the ordering was
constrained; the open question was only whether to split the phase. The three work items
ROADMAP section 51 does not account for (steer to Postgres, `memory_upsert_fact` schema,
pause fencing column) must therefore ride in that same amendment.

---

## Standing instruction

Given by the operator mid-discussion, after the second reference-driven answer:
*"guarda sempre librechat non inventare niente"*. Recorded in CONTEXT.md as D-00, a
constraint on every subsequent decision, not a note.

## Claude's Discretion

D-01, D-02 and D-03 - substrate shape, `agent_message_send` perimeter, and termination model
- were delegated to a measurement rather than to a preference. Claude designs the spike and
reports the numbers; the numbers decide and the PRD records them with date and evidence.

## Deferred Ideas

- Making `UpsertFact` atomic via the existing `Script` BEGIN/COMMIT method - belongs to a
  memory-correctness phase, since it touches every caller of the write path.
- Serializing per-identity memory writes - rejected as a bottleneck in the fan-out this
  phase exists to enable.
- Extracting SWARM-07 into its own phase - rejected by the slicing decision, recorded because
  the argument stays valid if Phase 51 grows too large during planning.
