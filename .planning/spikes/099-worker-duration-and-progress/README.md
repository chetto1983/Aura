---
spike: 099
idea: durable-delegation
name: worker-duration-and-progress
type: standard
validates: "Given a real fan-out on the live stack, when workers run to completion, then measured durations show whether a 120s wall-clock ceiling is survivable, and whether per-worker progress is observable enough to drive hermes-style staleness"
verdict: validated
related: [098]
tags: [swarm, timeout, observability, defect]
---

# Spike 099: worker-duration-and-progress

## What This Validates

D-03 delegates the worker termination model to a measurement: are real worker durations
survivable under `AURA_SWARM_CHILD_TIMEOUT_SEC=120`, and is per-worker progress observable
enough to drive hermes-style staleness instead of a wall-clock cap?

## Research

### Both reference implementations reap on inactivity; Aura reaps on age

| | How it decides a worker is dead | Refreshed by |
|---|---|---|
| **LibreChat** | `now - max(lastActivity, lastActiveAt, createdAt) > staleJobTimeout` — *"reaps on inactivity (a hung generation) rather than age (a long but live stream)"*, *"so a long but live stream is never reaped"* (`InMemoryJobStore.ts:40-44`, `225-245`) | `recordActivity(streamId)` on **every emitted chunk** (`GenerationJobManager.ts:1142`) |
| **hermes** | progress-based staleness: api-call count + current tool; *"a child that is advancing is left alone forever"*; idle 450s, in-tool 1200s, 120s grace after interrupt | a `progress_fn` polled by one monitor thread |
| **Aura today** | `context.WithTimeout(egCtx, SwarmChildTimeoutSec)` — total age from spawn (`swarm.go:124`, `139`) | **nothing** |

Two independent industrial implementations converge against Aura's current design. LibreChat's
rationale is stated in the code and is exactly D-03's question.

### Aura already has both halves of the fix

- **The refresh point exists**: `runChild`'s `for ev, err := range worker.Run(ic)` loop
  (`swarm.go:185`) sees every worker event. That is precisely where LibreChat calls
  `recordActivity`. Nothing consumes it for liveness today.
- **The measurement exists**: `slog.Info("swarm.child.completed", ..., "dur", time.Since(started))`
  (`swarm.go:224`) already logs per-child duration, so this spike needed no instrumentation.

## How to Run

```
MSYS_NO_PATHCONV=1 docker run --rm --env-file .env --network aura_default \
  -v "$PWD/.planning/spikes/099-worker-duration-and-progress:/work" \
  --entrypoint sh curlimages/curl:latest /work/drive.sh
```

Then read durations from the daemon:

```
docker logs aura --since <start> | grep -oE '"child":"w[0-9]","status":"[a-z_]+","dur":[0-9]+'
```

## Results

### The measurement could not be taken, because workers cannot execute tools at all

The fan-out ran: `tool_search` → `swarm_spawn` with four goals, each requiring `shell_exec`.
Four workers spawned, all four returned `status:"ok"`, and the parent's final answer admitted
it had done the work itself. Ground truth from the per-child transcripts under
`$AURA_RUN_DIR/<conv>/swarm/w*.jsonl` — not the model's prose:

```
error: tool dispatch denied by policy: operation fingerprint mismatch
```

| worker | shell_exec calls | denials |
|---|---|---|
| w1 | 4 | 11 |
| w2 | 4 | 8 |
| w3 | 3 | 12 |
| w4 | 3 | 10 |

**100% of worker tool dispatches were denied.** Not one worker executed a single tool.

### Root cause, established by reading the code rather than inferring it

`swarm_spawn` and `shell_exec` declare the **same** operation scope
(`OperationScope: OperationScopeAgent` — `swarm_spawn.go:89`, `shell_exec.go:115`; 10 tools
share that scope). In `deriveToolOperationContext` (`idempotency_operation.go:41-45`):

```go
parent, ok := idempotency.OperationFromContext(ctx)
if !ok || parent.Key.Scope == spec.OperationScope {
    return ctx, nil          // fires: both are OperationScopeAgent
}
```

The early return means **no child operation is derived for the worker's tool call**. The
worker's context still carries `swarm_spawn`'s operation, whose `Fingerprint` was computed
over `swarm_spawn`'s own `{goals:[...]}` arguments. The gateway then recomputes and compares
(`gateway/reserve.go:64-67`):

```go
expectedFingerprint, err := tools.OperationFingerprint(spec, rawArgs)
if err != nil || expectedFingerprint != operation.Fingerprint {
    return Verdict{Decision: Deny, Tier: tier, Reason: "operation fingerprint mismatch"}, false
}
```

`shell_exec`'s fingerprint can never equal `swarm_spawn`'s. The denial is **deterministic**,
not a race, and applies to every agent-scoped tool a worker calls.

### Why this is Phase 51's problem, precisely

SWARM-08 requires that workers *"reason over the same flattened tool surface the parent does,
**verified against the live surface** rather than assumed from registry inheritance."* The
verification just ran. The registry inheritance is real — `Without(reg, "swarm_spawn")` hands
the worker the tools — but **dispatch is dead**. Phase 51 is built entirely on workers doing
real work; making delegation durable and backgrounded on top of workers that cannot call a
single tool would produce a very reliable pipeline for delivering nothing.

### Durations measured, and why they are not the answer to D-03

Contaminated by the defect — these are workers looping on denials and recovery, not workers
working:

| fan-out | worker durations |
|---|---|
| haiku (spike 098 iter. 2, LLM-only, no tools needed) | 12.58s, 14.60s |
| this spike (4 goals, all tool dispatches denied) | 15.76s, 21.25s, 36.59s, **139.77s** |

Two things survive the contamination and are worth carrying forward:

1. **Variance within one fan-out is ~9×** (15.76s → 139.77s for four comparable goals). A
   single fixed wall-clock cap cannot be set well against that spread — which is the argument
   hermes and LibreChat both act on.

2. **`AURA_SWARM_CHILD_TIMEOUT_SEC` is not a hard bound.** w1 ran **139.77s** against a
   nominal 120s cap and returned `status:"ok"` (spawned 18:30:43.966, completed 18:33:03.730;
   `AURA_SWARM_CHILD_TIMEOUT_SEC=120` confirmed inside the container). This is by design, not a
   bug: on a budget trip the recovery turn **severs the expired deadline** —
   `callParent = context.WithoutCancel(ic.Ctx)` (`llm_agent.go:307-314`, fix-plan 1.1) —
   because otherwise the recovery call would be dead on arrival. That call is then bounded by
   `AURA_LLM_TOTAL_TIMEOUT_SEC`, measured at 120 in this deployment.

   **So the effective worst-case child duration is `SwarmChildTimeoutSec + LLMTotalTimeoutSec`
   = 240s, twice the nominal cap.** Any lease or reclaim interval in spike 100's substrate
   that is sized against the nominal 120s would reclaim a worker that is still alive.

## Re-measurement after the fix (2026-08-26)

The defect is closed and the closure was measured on the same four-goal fan-out, on the
live stack, with the rebuilt image. Ground truth is `aura.tool_invocations` and the daemon
log — not the parent's prose, which lied the first time.

| | first run (defect present) | after the fix |
|---|---|---|
| `operation fingerprint mismatch` | **41** across 4 workers, 100% of dispatches | **0** |
| `shell_exec` actually executed | 0 | 3 rows `event_kind=end, status=ok, exit_code=0`, with real captured output |
| worker outcomes | 4 `ok` having done nothing | 3 `ok`, 1 failed on an **OpenRouter** `context deadline exceeded` — upstream, not policy |

The fix is in `deriveToolOperationContext` (`internal/agent/idempotency_operation.go`): the
passthrough guard now keys on the whole identity of the call (scope AND fingerprint) instead
of scope alone, and `ScopeAgentTool` is accepted as a PARENT scope so a nested call derives
its own child key. Which of the three candidate sites to fix was left open by this spike;
this one was chosen because it is the site that actually holds the wrong premise — the guard
is a re-entry guard, and re-entry was being tested for with half of the call's identity.

### The second run's durations were still not the answer, for an unrelated reason

That run's goals asked for counts under `internal/agent` and `internal/db/migrations`, and
the sandbox answered `ls: cannot access '/workspace/internal/agent'`. Ten `start` rows
against three `end` rows were workers burning rounds searching for a repo that is not in the
box — not workers being denied. The goals now live in `goals.txt` beside the driver, are
executable inside `/workspace`, and are what a re-run measures.

## D-03 answered (2026-08-26, third run, `01a03f9f-05ad-78c4-8a33-a0e2faf1c92e`)

Four workers, four executable goals, all four `ok`, and every answer verified against ground
truth taken independently from the box before the run:

| worker | duration | goal | answer | ground truth |
|---|---|---|---|---|
| w3 | **5.15s** | lines of README.md + create_docx.py | 17, 39 | 17, 39 |
| w2 | **5.51s** | files + subdirs under documents/ | 8, 8 | 8, 8 |
| w1 | **7.58s** | bytes of faccia_divertente.png | 60650 | 60650 |
| w4 | **7.80s** | sha256 of every top-level file | `8aa31133d523797f` first | `8aa31133d523797f` |

### Is a 120s wall-clock ceiling survivable? Yes — and that is not the same as adequate.

Every worker doing real, feasible work finished in **5-8s**, a 23× margin under the nominal
cap. The cap is not the binding constraint on a healthy worker. But the three runs together
show what the cap actually catches, and it is not slowness:

| observation | run | what the 120s cap did |
|---|---|---|
| 5.15 - 7.80s, all ok | 3rd | nothing; margin was never approached |
| 70.31s, ok | 2nd | nothing, yet the worker was *lost* the whole time doing `find /` for absent files |
| 120.00s, failed | 2nd | fired — on an **OpenRouter** `context deadline exceeded`, an upstream stall |

The cap fired exactly once across three runs, and what it caught was a stalled upstream call,
not a long computation. The 70s worker — the one genuinely worth intervening on, because it
was making tool calls that could never succeed — sailed under the cap and returned `ok`.
**A wall-clock ceiling is the wrong instrument for the failure that actually occurs**, which
is what LibreChat's comment says in as many words and what hermes' progress counter acts on.
The spread across a single fan-out (5.15s to 120s, 23×) is itself the argument: no single
constant is right for both ends of it.

### Is per-worker progress observable enough for hermes-style staleness? Yes, but not from the ledger.

Measured, not inferred — `aura.tool_invocations` for the third run:

| tool | start rows | end rows |
|---|---|---|
| `shell_exec` | **6** | **1** |
| `swarm_spawn` | 1 | 1 |
| `tool_search` | 1 | 1 |

The one `end` that landed carries the parent's own combined command (the table above is read
from its preview). Grouping the same conversation by request id turns it into a controlled
comparison — one conversation, one ledger, two event paths:

| | request id | starts | ends |
|---|---|---|---|
| **parent** | `…05d4-76f2` | 3 | **3** |
| **workers** (×4) | `…264f-760e`, `…264f-76b7`, `…264f-798f`, `…2650-75f3` | 5 | **0** |

**The cause: the two halves of a ledger row have different writers, and a worker only
traverses one of them.**

| row | writer | worker passes through it? |
|---|---|---|
| `start` | the **gateway**, in the dispatch path (`reserve.go:302`) | **yes** — so the start is written |
| `end` | the **Runner**, from `agent.Event` frames as the turn streams (`runner_persist.go:110`, `:201`) | **no** — `runChild` consumes `worker.Run(ic)` itself (`swarm.go:185`) and dumps to a per-child JSONL, so the frames never reach the Runner |

Every worker tool call therefore opens a reservation that nothing will ever close.

### The orphaned starts are not inert: the reconciler writes a falsehood over them

This was caught live rather than predicted. `gateway.Reconciler` is wired at
`cmd/aura/serve.go:498` and sweeps `start ∧ ¬end` after a grace of at least 30 minutes
(`reconcile.go:31,38,78-83`), appending `syntheticEnd` — `status='error'`, `meta.indeterminate`.
At **20:10:31**, half an hour after the second run, it fired on exactly those rows:

```
reconcile: appended indeterminate end for crash-orphaned reservation   ×7
  status=error  "crash-orphaned in-flight tool call (indeterminate outcome, never re-invoked)"
```

Seven worker `shell_exec` calls that **had succeeded** are now permanently recorded as
indeterminate failures, in a ledger the migration header calls append-only and the code
never updates or deletes (`reconcile.go:41-43`). The reconciler is not at fault — it is
correctly closing what looks exactly like a crash orphan. The ledger simply has no way to
tell a crashed call from a call whose end-writer was never on the path.

The third run then reproduced it exactly, and the same request-id grouping shows the split
is total rather than incidental:

| | starts | ends | of which `reconciled` |
|---|---|---|---|
| parent (`…05d4-76f2`) | 3 | 3 | **0** |
| workers (×4) | 5 | 5 | **5** |

Every worker call, no exceptions; no parent call, no exceptions.

So a staleness reaper built on this ledger would not merely see live workers as in-flight;
it would be reading a record that actively contradicts what happened.

### Fixed, and re-measured

`791dcd7e0` gives the gateway the other half of the row: it closes the reservation at its
own terminal hooks (`CompleteOperation` / `MarkOperationIndeterminate`), gated on a context
marker `runChild` sets, so a Runner-observed dispatch is untouched and keeps the Runner's
richer end. The rule is LibreChat's (D-00) — whoever opens a record closes it, so no
component ever has to find and close a row another one opened.

Same fan-out, rebuilt image, live:

| | starts | ends | orphans |
|---|---|---|---|
| workers (×4), before | 5 | **0** | 5, all later stamped indeterminate |
| workers (×4), after | 5 | **5**, `meta.delegated` | **0** |
| parent, after | 2 | 2 | 0 — unchanged |

The reconciler's own anti-join over that conversation returns **0**. The delegated end rows
carry the real output (`60650`, `17`/`39`, `FILE_REGOLARI=8`, `NUMERO_FILE=15 … 8aa…`).

**This does not change D-03's design answer below.** The ledger now records that a worker's
call ENDED; staleness needs to know a worker is still ALIVE part-way through one, and that
signal is still only in `runChild`'s event loop — LibreChat fires `recordActivity` per
emitted chunk, not per finished tool call. What the fix removes is the ledger actively
lying, which is a precondition for spike 100 measuring anything on top of it.

The signal that *does* exist is the one already named in the research above:
`runChild`'s `for ev, err := range worker.Run(ic)` loop (`swarm.go:185`) sees every worker
event, which is precisely where LibreChat calls `recordActivity`. **The liveness tick belongs
in that loop, not in a query over `tool_invocations`.** That is the design answer D-03 owed,
and it now rests on a measurement rather than on the reference implementations alone.

## Verdict — validated (was BLOCKED)

The blocking fact was worth more than the number the spike set out to collect, and it has
since been fixed and re-measured (above). What the spike delivered:

- **New, live, deterministic defect, now fixed:** a swarm worker could not dispatch any
  agent-scoped tool. Measured 4/4 workers, 100% of dispatches, root cause read in the code,
  re-measured at 0 denials after the fix.
- **Correction to a number Phase 51 would otherwise have trusted:** the child timeout is 120s
  nominal, 240s effective.
- **D-03 answered on measurement, agreeing with both references:** a healthy worker finishes
  in 5-8s against a 120s cap the spread makes meaningless, the one cap firing caught an
  upstream stall rather than slow work, and the ledger cannot drive staleness while the
  event loop can.

## Investigation Trail

1. Read `runWave`/`runChild` for the timeout shape; found the hard per-child
   `context.WithTimeout` and, unexpectedly, that duration is already logged.
2. Read LibreChat's `InMemoryJobStore` reaper and `recordActivity`; found the idle-vs-age
   rationale stated verbatim in its comments.
3. Ran the live 4-goal fan-out and pulled durations from the daemon log.
4. Saw a 139.77s child reported `ok` against a 120s cap; verified the env inside the container
   before calling it anything, then found the deliberate deadline-severing recovery turn.
5. Read the parent's final message — it claimed the workers were "blocked by policy". Did not
   trust the prose; went to `$AURA_RUN_DIR/<conv>/swarm/w*.jsonl` and found the real error
   string and its count per worker.
6. Traced `operation fingerprint mismatch` to its single raise site, then to the scope
   early-return that leaves `swarm_spawn`'s operation in the worker's context.

## What This Spike Does NOT Prove

- **The durations are four workers on four small goals, not a load characterization.** They
  prove a healthy worker finishes far under the cap; they say nothing about a worker doing
  genuinely heavy work, which no run here produced.
- **The missing worker `end` rows were explained, their consequence caught live, and the
  defect fixed and re-measured** (`791dcd7e0`: workers 5/5 closed, anti-join 0).
  The blast radius is small so far and was counted rather than guessed: **22 reconciled
  `end` rows exist in the whole ledger** (`meta->>'reconciled'='true'`), 18 of them written
  today by these three runs and only 4 before them (2026-08-15/16). What is NOT established
  is how many of the 4 older ones are swarm workers rather than genuine crash orphans —
  the row itself cannot tell the two apart, which is the defect restated.
- One deployment, one routed model (`deepseek/deepseek-v4-flash-0731:nitro`, read from
  `aura.settings`), one profile (`single_user_hardened`). The denial is structural, so it
  should not be deployment-specific, but that has not been checked elsewhere.
- The 240s effective bound is arithmetic from two configured values plus one observation at
  139.77s; the full 240s was not itself observed.
- Nothing here says whether the fix belongs in `deriveToolOperationContext`, in the scope
  taxonomy, or in how the swarm builds the worker's context. That is a design decision for the
  phase, not a spike finding.
