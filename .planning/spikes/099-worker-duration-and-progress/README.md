---
spike: 099
idea: durable-delegation
name: worker-duration-and-progress
type: standard
validates: "Given a real fan-out on the live stack, when workers run to completion, then measured durations show whether a 120s wall-clock ceiling is survivable, and whether per-worker progress is observable enough to drive hermes-style staleness"
verdict: BLOCKED
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

## Verdict — BLOCKED

D-03's question cannot be answered until worker tool dispatch works. The blocking fact is
worth more than the number the spike set out to collect:

- **New, live, deterministic defect:** a swarm worker cannot dispatch any agent-scoped tool.
  Measured 4/4 workers, 100% of dispatches, root cause read in the code.
- **Correction to a number Phase 51 would otherwise have trusted:** the child timeout is 120s
  nominal, 240s effective.
- **The design direction for D-03 is nonetheless clear** and does not depend on the blocked
  measurement: both references reap on inactivity, Aura reaps on age, and Aura already owns
  the event loop where the liveness tick belongs.

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

- No claim about worker durations under a *working* dispatch path — that measurement is still
  owed once the defect is fixed.
- One deployment, one routed model (`deepseek/deepseek-v4-flash-0731:nitro`, read from
  `aura.settings`), one profile (`single_user_hardened`). The denial is structural, so it
  should not be deployment-specific, but that has not been checked elsewhere.
- The 240s effective bound is arithmetic from two configured values plus one observation at
  139.77s; the full 240s was not itself observed.
- Nothing here says whether the fix belongs in `deriveToolOperationContext`, in the scope
  taxonomy, or in how the swarm builds the worker's context. That is a design decision for the
  phase, not a spike finding.
