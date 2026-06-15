# Aura Orchestrator — Design Spec

**Date:** 2026-06-15
**Status:** Draft — awaiting user review, then PRD-amendment (O0) before any code.
**Author:** Claude (brainstorming session with Davide)

## 1. Problem & motivation

Aura's multi-agent capability today is a *static fan-out*: the parent `LlmAgent`
calls `swarm_spawn` once with N goals, the [`internal/swarm`](../../../internal/swarm)
engine runs N identical workers in budget-bounded leak-safe waves, each worker
returns a free-text `Summary`, and the parent re-reads those summaries. It is
well-hardened (D-02 failure isolation, budget partitioning, dedup, panic
firewall, transcripts) but it cannot **plan**, **verify**, or **synthesize** —
the three things that make a Claude-Code-style orchestrator strong.

The gap vs a Workflow-style orchestrator is three missing layers, **not** the
execution primitives (Aura already has `SequentialAgent`/`ParallelAgent`/
`LoopAgent` in [`internal/agent/workflow`](../../../internal/agent/workflow),
with budget threading, dedup rings, escalate signalling, and OTel spans solved):

1. Nothing turns a user objective into a task graph dynamically.
2. Workers emit free text, not typed results.
3. There is no verify / synthesize stage.

## 2. Goals / non-goals

**Goals**
- An `orchestrate` capability that takes an objective and runs
  **plan → fan-out → verify → synthesize**, deterministically executed.
- Reuse Aura's existing primitives (swarm engine, workflow agents, Budget,
  dedup, OTel) rather than rebuild them.
- Typed, schema-validated artifacts end to end (`Plan`, `TaskResult`).
- Preserve every current swarm guarantee: D-02 sibling isolation, budget caps,
  leak safety, panic firewall.

**Non-goals (this design)**
- Replacing `swarm_spawn`. It stays for simple parallel fan-out, exactly as I
  keep both the **Agent** tool (ad-hoc) and the **Workflow** tool (planned).
- Nested worker spawning. Flat-v1 (D-08/D-10) is preserved — the *executor*
  grows the graph, workers never spawn workers.
- A general scripting DSL. The plan is a typed DAG, not arbitrary code.

## 3. Control-flow model: Planner → deterministic executor

The chosen model (confirmed with the user — "like you") mirrors the Workflow
tool: an LLM authors the plan, a deterministic Go engine executes it with typed
agent slots.

```
objective
   │
[Planner LLM]  ── emits Plan{tasks, deps, verify}  (schema-validated, retry-on-invalid)
   │
[Executor (Go, deterministic)]
   ├─ topological waves; independent tasks fan out (no-barrier pipeline)
   ├─ each task = worker → typed TaskResult (structured output)
   ├─ optional per-task verify (skeptic agent); failures retry/drop, siblings never cancelled
   └─ optional bounded replan loop (loop-until-dry / completeness critic)
   │
[Synthesizer] ── aggregates verified TaskResults → final answer
   │
final answer (+ structured trace)
```

**Determinism boundary:** the LLM decides *what* (the plan and each task's
answer); the Go executor guarantees *how* (ordering, concurrency, budget,
verification, failure isolation). This is what makes the orchestration both
adaptive and reliable.

## 4. Components & file layout

New package `internal/orchestrator` (imports `internal/swarm` and
`internal/agent/workflow` for reuse). Cycle-free seam via a `RunnerAdapter`,
identical pattern to `swarm.RunnerAdapter`, so the tool package imports neither
`agent` nor `config`.

| File | Responsibility | LOC budget |
|---|---|---|
| `internal/orchestrator/orchestrator.go` | package doc, `RunConfig`, `Run` entrypoint, preflight | ≤300 |
| `internal/orchestrator/plan.go` | `Plan`/`Task`/`TaskResult`/`Verdict` types + JSON schema | ≤250 |
| `internal/orchestrator/executor.go` | DAG → topological waves, no-barrier pipeline scheduling | ≤400 |
| `internal/orchestrator/planner.go` | planner `LlmAgent` wrapper, schema-validate + retry | ≤250 |
| `internal/orchestrator/verify.go` | per-task skeptic verify (O4) | ≤200 |
| `internal/orchestrator/synthesize.go` | aggregator agent → final answer | ≤200 |
| `internal/orchestrator/runner_adapter.go` | cycle-free seam (reads deps off ctx) | ≤120 |
| `internal/agent/tools/orchestrate.go` | deferred tool spec + arg schema | ≤200 |

The fan-out of independent tasks **delegates to the existing swarm engine**
(`swarm.Run` / a refactored entry) so D-02, budget partitioning, dedup, panic
firewall, and transcripts are inherited, not duplicated.

## 5. Data types

```go
// plan.go
type Plan struct {
    Objective string `json:"objective"`
    Tasks     []Task `json:"tasks"`
}

type Task struct {
    ID        string   `json:"id"`                   // unique within plan; "t1".."tN"
    Goal      string   `json:"goal"`                 // self-contained brief
    Role      string   `json:"role,omitempty"`       // O5: maps to tool-subset/framing
    DependsOn []string `json:"depends_on,omitempty"` // task IDs that must complete first
    Verify    bool     `json:"verify,omitempty"`     // O4: run a skeptic on this result
}

type TaskResult struct {
    TaskID   string `json:"task_id"`
    Status   string `json:"status"`            // ok | failed | needs_user_input
    Output   string `json:"output,omitempty"`  // worker's structured/answer payload
    Error    string `json:"error,omitempty"`
    Verified bool   `json:"verified,omitempty"`// O4
    // needs_user_input proxy fields mirror swarm.ChildReport (D-04/D-05)
    Question   string   `json:"question,omitempty"`
    Options    []string `json:"options,omitempty"`
    ToolCallID string   `json:"tool_call_id,omitempty"`
}

type Verdict struct { // O4
    TaskID string `json:"task_id"`
    Real   bool   `json:"real"`
    Reason string `json:"reason,omitempty"`
}
```

`TaskResult` is intentionally `swarm.ChildReport` + `task_id`/`verified`; O1 may
extend `ChildReport` rather than fork it, decided during planning.

## 6. Data flow & error handling

- **Preflight** (mirrors `swarm.preflight`): depth/total-task cap/budget snapshot
  before any worker is constructed; rejections return a model-readable
  `error: ...` string (D-15), never a Go error.
- **Planner failure:** schema-invalid plan → retry up to N (config); exhausted →
  model-readable error so the parent self-corrects or answers directly.
- **Per-task failure:** captured into its `TaskResult` slot; siblings never
  cancelled (D-02). A panic → `{failed, "panic: …"}` via the panic firewall.
- **Dependency failure policy:** a task whose dependency failed is marked
  `skipped` (not run) and surfaced in the trace; the rest of the DAG proceeds.
- **Budget:** total task count must fit `ParentBudget.Remaining()` minus a
  synthesis reserve (same shape as swarm's `budgetReserve`). Each task gets an
  equal `Budget.Child` share; verify/replan ride a bounded sub-reserve.
- **needs_user_input:** proxied up exactly as swarm does today (D-04/D-05).
- **Replan loop (O4):** bounded by iteration cap *and* budget; reuses
  `LoopAgent`'s no-progress + dedup guards so it cannot hot-spin.

## 7. Handoff / dynamic routing (reframed — no flat-v1 amendment)

Mid-task handoff (worker → worker) is forbidden by D-08/D-10. Dynamic delegation
is expressed instead as **conditional edges in the plan**: a "router" task whose
typed `Output` tells the *executor* which downstream tasks to expand, or a
replan step that appends tasks. Workers still never spawn workers; the executor
owns graph growth. Result: the strong capability without a nested-spawn PRD
amendment. The only PRD work is adding the `orchestrate` capability itself.

## 8. Slice decomposition (build order)

| Slice | Delivers | Depends on |
|---|---|---|
| **O0** | PRD amendment cataloging the capability (decisions, env, file targets, acceptance). Mandatory first. | — |
| **O1** | `Plan`/`Task`/`TaskResult` types + optional structured worker output (schema → validated JSON). | O0 |
| **O2** | Deterministic executor over hand-authored Plans (topological waves, no-barrier pipeline, dep waiting). | O1 |
| **O3** | Planner agent + `orchestrate` tool (planner→executor→synthesize). | O2 |
| **O4** | Per-task skeptic verify + bounded replan loop. | O3 |
| **O5** *(future)* | Per-task roles (tool-subset/framing) + conditional routing. | O4 |

## 9. Testing strategy

- **Unit (FakeClient):** plan schema validation; executor DAG shapes (chain,
  diamond, independent fan-out); dependency-skip on upstream failure; budget
  exhaustion; needs_user_input proxy.
- **Property-based:** random DAGs (gopter/rapid) → executor never deadlocks,
  never runs a task before its deps, never exceeds budget.
- **Concurrency:** `-race` + goleak on the executor (inherits swarm's
  invariants; re-assert at the new layer).
- **Integration / live:** planner produces a valid plan for a real objective
  (cot_eval-style, `OPENROUTER_API_KEY`-gated, not CI), reusing the existing
  `internal/eval` harness pattern.
- **Coverage:** owned-surface ≥85% (project hard floor), measured across the tag
  matrix. Mutation spot-check on `executor.go` and `plan.go`.

## 10. Project gates before code

1. **PRD-amendment (O0)** — the orchestrator is not in `prd.md`; PRD-first
   forbids code until the amendment commit lands.
2. **Milestone fit** — current milestone is the v1.0.0 Deep Search Web Cockpit
   (phases 22–29). This capability is milestone-scale and outside that roadmap;
   timing is a user decision (new milestone vs interleave vs design-only).
3. **Build workflow** — GSD (`/gsd-spec-phase` → discuss → plan → execute) is
   the project's official path and aligns with PRD-first; the superpowers
   `writing-plans` path is the alternative.

## 11. Open questions (defaulted, adjustable at review)

- **Synthesis:** dedicated aggregator agent (default) vs returning the raw
  verified `[]TaskResult` to the parent for it to synthesize. Default chosen for
  consistent quality; cheap to make configurable.
- **TaskResult vs ChildReport:** extend `swarm.ChildReport` (default) vs a new
  type. Decided in O1 planning.
- **Plan size cap:** new env `AURA_ORCH_MAX_TASKS` (mirrors
  `AURA_SWARM_MAX_GOALS`); default value set in O0.
