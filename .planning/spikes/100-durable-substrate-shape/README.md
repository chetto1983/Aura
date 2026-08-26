---
spike: 100
idea: durable-delegation
name: durable-substrate-shape
type: standard
validates: "Given the three failure modes D-01 names — a worker dying mid-flight, a daemon restart, and delivery failing eight times — measured against the lease queue Aura already owns, then the choice between a new hermes-style delegation table and generalizing aura.ingestion_jobs is decided by evidence rather than preference"
verdict: validated
related: [098, 099]
tags: [substrate, durability, lease, crash-recovery, inventory]
---

# Spike 100: durable-substrate-shape

## What This Validates

D-01 delegates the substrate's shape to a measurement: one new delegation table, or the
lease queue already in the tree? The rule that governs the question is CLAUDE.md's
**INVENTORY BEFORE INVENTION** — enumerate what the dependency already does before writing
the component, and answer BUILD vs REUSE with a probe, never with an assumption in either
direction.

## Research

### The two references disagree, and the disagreement is the whole question

| | durable record of in-flight delegated work | what happens to a run whose owner died |
|---|---|---|
| **hermes** | a SQLite delegation ledger with restart recovery | reclaimed and recovered |
| **LibreChat** | **none** | reaped, reported as an error, never resumed |

LibreChat's position is not an omission, it is a design, and it is stated in the code. Its
durable substrate is a checkpointer, and `LazyMongoSaver`
(`packages/api/src/agents/checkpointer.ts:64-120`) persists

> *"ONLY checkpoints carrying a resumable pending write — an interrupt (a HITL pause) or a
> real-channel/delta anchor — and discards both the no-write checkpoint LangGraph writes on
> a CLEAN exit and the bookkeeping-only checkpoint of a failed (non-paused) turn."*

with the consequence spelled out: *"an errored turn still leaves NOTHING durable (0
checkpoints, 0 write rows)"*. The only thing LibreChat makes durable is **a pause somebody
is expected to come back to**. Its detached run is a plain async closure that outlives the
HTTP response (`api/server/controllers/agents/request.js:245-252`, `:875`), and when it
dies the reaper aborts it and emits `'Generation timed out'`
(`GenerationJobManager.ts:1836-1855`). Resumption happens at one place only — a HITL pause,
rebuilt from the checkpoint: *"the original run lives in a detached background task that
exits when the run pauses, so this REBUILDS the run from the durable checkpoint (same
`thread_id`)"* (`resume.js:364-373`).

So LibreChat contributes the *principle* (persist the pause, hand the new owner the old
owner's whole state — seeding, not reconciliation) and hermes contributes the *mechanism*
(a durable ledger with lease reclaim). Neither settles which table Aura should use, which is
why this spike measures rather than cites.

### Aura already owns a generic lease queue, and it does not hide it

`internal/db/queries/ingestion_jobs.sql` opens on its own first line: **"The GENERIC asset
queue."** The file was renamed once already, when the document-specific statements left with
the catalog they wrote (migration 0098) — the queue was always the other half and *"is the
only half with callers"*. The table carries every column a delegation ledger needs:

| need | column |
|---|---|
| what kind of work | `job_type` (already generic — the reason a new table is not obviously needed) |
| lease + owner fencing | `locked_by`, `locked_until`, `lease_generation` |
| retry budget | `attempt_count`, `max_attempts` (default 5) |
| backoff | `next_attempt_at` |
| dedupe | UNIQUE `(identity_id, job_type, idempotency_key)` |
| tenant scoping | `identity_id` (FK to `aura.identities`, RLS-relevant) |
| the work itself | `payload` jsonb |
| failure detail | `error_code`, `error_message` |

And `ClaimIngestionJobs` already re-claims a dead owner's row:

```sql
(status = 'queued' AND next_attempt_at <= now()
 AND (locked_until IS NULL OR locked_until < now()))
OR (status = 'running' AND locked_until < now())     -- an owner that died
... AND attempt_count < max_attempts
ORDER BY next_attempt_at, created_at
FOR UPDATE SKIP LOCKED
```

## How to Run

The predicate probe, against the live queue under a synthetic `job_type` no processor
dispatches (`ProcessorSet.For` routes on modality and has no `spike100` entry), so nothing
it writes can be claimed by real work:

```
docker cp .planning/spikes/100-durable-substrate-shape/probe.sql aura-postgres:/tmp/probe.sql
MSYS_NO_PATHCONV=1 docker exec aura-postgres psql -U aura -d aura \
  -v ident="$(docker exec aura-postgres psql -U aura -d aura -t \
              -c 'SELECT id FROM aura.identities LIMIT 1' | tr -d ' \r\n')" \
  -f /tmp/probe.sql
docker exec aura-postgres psql -U aura -d aura -c "DELETE FROM aura.ingestion_jobs WHERE job_type='spike100';"
```

The crash test reuses spike 099's driver (same fan-out; no second copy), backgrounded, then
SIGKILLs the daemon once four workers have spawned:

```
docker run --rm --env-file .env --network aura_default \
  -v "$PWD/.planning/spikes/099-worker-duration-and-progress:/work" \
  --entrypoint sh curlimages/curl:latest /work/drive.sh &
# wait for 4x swarm.child.spawned, then:
docker kill --signal=SIGKILL aura
docker compose up -d aura
docker compose up -d --force-recreate arcadedb-mcp aura-pim-mcp whatsapp   # netns re-attach
```

## Results

### The queue already answers all three of D-01's scenarios

Measured, not read — the claim predicate applied verbatim as a SELECT over three rows built
to be each scenario:

| D-01 scenario | row | reclaimed? | right? |
|---|---|---|---|
| a worker dies mid-flight | `running`, lease expired | **yes** | ✓ the work is picked up again |
| delivery failing 8 times | `queued`, `attempt_count = max_attempts = 8` | **no** | ✓ it stops, rather than looping forever |
| a worker still working | `running`, lease live | **no** | ✓ a live worker is not robbed |

The third row is the one worth naming: it is the same distinction spike 099 found LibreChat
and hermes both making — reap on *inactivity*, not on *age*. The queue expresses it as a
lease that a live owner keeps renewing, which is the durable form of `recordActivity`.

### The daemon restart needs no recovery path, because the lease IS the recovery

There is no boot-time orphan sweep to write for this queue and none is missing: a restarted
daemon's ordinary claim picks up `running` rows whose lease has expired. Recovery is the
steady-state query, which is exactly why a lease queue is the shape to reuse.

### What a crash actually costs today — SIGKILL two seconds into a four-worker fan-out

| | observed |
|---|---|
| client | the SSE stream **truncates mid-sentence** — no `RUN_FINISHED`, no `RUN_ERROR`, no terminal frame at all |
| ledger | `swarm_spawn` `start` with no end; `tool_search` start+end; **zero worker rows** |
| delegation record | **none** — no row in `ingestion_jobs`, none in `agent_job_runs`, no swarm table exists |
| resumability | nothing reclaims it; the four goals are simply gone |

But one thing DID survive, and it is the seed the design needs:

```
args_raw: {"goals": ["Obiettivo: riportare la dimensione esatta in byte del file
           /workspace/faccia_divertente.png. Usa shell_exec …
```

**The delegation's full intent is already durable** — it rides the `swarm_spawn`
reservation's own arguments. What is missing is not the data. What is missing is a row that
says this work is *owed*, and something that claims it.

That is precisely LibreChat's seeding pattern read in Aura's terms: the new owner does not
hunt for what the dead owner was doing, it is handed the whole intent and re-runs it.

## Verdict — validated

**Generalize `aura.ingestion_jobs`. Do not create a delegation table.** The evidence:

1. The three failure modes D-01 named are already handled correctly by the existing claim
   predicate — measured, all three.
2. `job_type` is already the discriminator a second kind of work needs; the file calls
   itself the generic queue and has already survived one de-specialization.
3. A restart needs no new recovery path — the lease expiry already is one.
4. The intent to delegate is already durable in the `swarm_spawn` reservation's arguments,
   so enqueueing is a matter of writing a row that says the work is owed, not of inventing
   somewhere to keep it.

A new table would re-implement `locked_by`/`locked_until`/`attempt_count`/`next_attempt_at`/
`idempotency_key` and the `FOR UPDATE SKIP LOCKED` claim beside a tested implementation of
the same thing — the failure this project has already paid for once, when 8,640 LOC of
ingestion bookkeeping duplicated what CocoIndex did.

## Investigation Trail

1. Read the table before the code: `\d aura.ingestion_jobs` showed `job_type`, a lease, a
   retry budget and three fencing generations — the shape of a delegation ledger, not of an
   ingestion-specific one.
2. Read `internal/db/queries/ingestion_jobs.sql`, whose first line calls it the generic
   queue and whose history records an earlier de-specialization.
3. Built the three D-01 scenarios as real rows under a synthetic `job_type` and applied the
   claim predicate as a SELECT, so the probe reported without mutating; deleted the rows.
4. Read LibreChat for the substrate question and found the opposite of the premise: a
   checkpointer that deliberately persists nothing for a failed non-paused turn.
5. SIGKILLed the daemon two seconds into a live four-worker fan-out, restarted, and read
   what was left — including the discovery that the goals were already durable.

## What This Spike Does NOT Prove

- **The claim predicate was measured, not the Go claim path.** The probe applied the
  predicate as a SELECT over rows built by hand; no job flowed through `ClaimIngestionJobs`
  and its store code under a real restart. That end-to-end run is owed before the phase
  relies on it.
- **Nothing here says how a swarm worker becomes a queue row**, only that the queue is the
  right place to put it. Payload shape, who enqueues, and whether the parent turn blocks are
  design questions this spike deliberately did not answer.
- **The crash test killed the daemon at 2 seconds**, before any worker had dispatched a
  tool. A crash *after* partial side effects is a different and harsher case, and it was not
  measured.
- One deployment, one profile (`single_user_hardened`), one routed model.
