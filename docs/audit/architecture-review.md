# Architecture Review — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`

## 1. Current architecture

`internal/agent` is the agent runtime: an open `Agent` interface, the `LlmAgent` reasoning loop, a tool registry + tool implementations, an MCP bridge, workflow composites (loop/sequential/parallel), a shared budget tree, a prompt builder with KV-cache discipline, a reasoning-tier classifier, a hooks extension surface, and OTel/expvar observability.

```
                         ┌──────────────────────────────────────────────┐
   Runner (out of pkg) → │            LlmAgent.Run (iter.Seq2)            │
   sessionID, ctx,       │  [llm_agent.go]                                │
   history, budget       │                                                │
                         │  loop:                                         │
                         │   1 budget gate  ── trip ─▶ maybeRecover /     │
                         │     (ConsumeStep)            finalize()        │
                         │   2 build req (PromptBuilder, cache-stable)    │
                         │   3 hooks.BeforeModel                          │
                         │   4 stream (streamWithOpenRetry + Breaker)     │
                         │   5 consume → text|calls|finish|usage          │
                         │   6 classifyToolTruncation                     │
                         │   7 pauseCalls (ask_user → AwaitingInput)      │
                         │   8 dispatch(calls) ─────────────┐             │
                         └──────────────────────────────────┼────────────┘
                                                            │
              ┌─────────────────────────────────────────────┘
              ▼
     dispatch [llm_agent_dispatch.go]
       • partition terminal (text_response) vs runnable
       • serial: hooks.BeforeTool + dedup gate (BeforeToolCall)
       • executeBatch (concurrent runTool, sem-bounded) ──▶ tools.Registry.Get → tool.Execute
       • serial: history append + AfterToolResult + result Event
       • terminal: runTerminal → finalEvent

     Cross-cutting:
       Budget tree [budget.go, budget_dedup.go]  — shared *atomic.Int32 + per-branch dedup ring
       PromptBuilder [prompt/]                    — byte-stable messages[0] + tail-injected hints + cache_control
       MCP bridge [mcptools/]                     — namespaced bridged tools, reconnect-on-transport-error
       Trust [trust.go]                           — nonce-fenced <tool_output trust="untrusted"> envelope
       Tracing/metrics [tracing.go, metrics.go]   — OTel spans + expvar/Prometheus counters
       Hooks [hooks.go, hooks_command.go]         — in-process + trust-gated out-of-process command hooks
       Workflow [workflow/]                       — Loop/Sequential/Parallel composites over Agent
       Swarm [swarm_context.go + internal/swarm]  — swarm_spawn fan-out via ctx-carried seam
```

### Main components and responsibilities

| Component | File(s) | Responsibility |
|---|---|---|
| `Agent` interface | `agent.go` | Open contract: `Name/Description/Run/SubAgents/FindAgent`; `BudgetOwner` opt-in. `InvocationContext` is single-Run-scoped, copy-on-`With*`. |
| `LlmAgent` | `llm_agent.go` (+ `_dispatch/_finalize/_completion/_pause/_consume/_retry/_stream_retry/_parallel/_truncation/_reasoning/_args/_events`) | The budget-gated tool-dispatch loop. |
| Budget | `budget.go`, `budget_dedup.go` | Shared `*atomic.Int32` step counter + wallclock gate + per-branch dedup ring (period-1/2 loop guard). |
| Tools | `tools/*` | `shell_exec`, `fs_*`, `web_*`, `task`, `skill_*`, `send_file`, `swarm_spawn`, `ask_user`, `todo`, `read_tool_output`, `search` (semantic tool discovery), `text_response`. Registry + deferred-tool manifest. |
| MCP bridge | `mcptools/*` | Mount external MCP servers as namespaced bridged tools; reconnect-on-transport-error; description capping. |
| Prompt | `prompt/*`, `prompt.go` | Byte-stable system prompt; cache_control injection; reasoning-tier classifier + LLM router fallback. |
| Trust | `trust.go` | Wrap untrusted tool output (web/fs/shell) in a crypto-nonce envelope; HTML-escape + NFKC. |
| Hooks | `hooks.go`, `hooks_command.go` | `BeforeModel/BeforeTool/AfterTool/OnTurn*` extension points; in-process + SHA-gated command hooks. |
| Workflow | `workflow/*` | `LoopAgent`, `SequentialAgent`, `ParallelAgent` composites; budget-aware, escalate-aware. |
| Observability | `tracing.go`, `metrics.go`, `event.go` | OTel spans (turn/llm/tool), expvar+Prometheus counters, the `Event` wire model. |

## 2. Agent-loop design analysis

The loop (`llm_agent.go:189–441`) is the strongest part of the system. Properties verified by reading:

- **Deterministic control flow, bounded recovery.** Every `continue` is gated by a per-run counter: `recoveryAttempts ≤ 1` (budget/dedup/empty-response nudge), `completionAttempts ≤ 1` (completion-critic veto), `truncatedToolTurns ≤ 2` (truncated-tool-call nudge), `streamRetryUsed` one-shot. There is **no unbounded `continue`** — the "203-turn thrash" failure mode is provably closed.
- **Non-empty terminal contract.** Termination is always an explicit `Event`, never the `iter.Seq2` error slot (which carries only real infra failures). Forced finalization (`llm_agent_finalize.go`) issues one tool-free synthesis turn, retries once, then falls back to a deterministic Italian stub digesting gathered tool results — so the user *always* gets prose.
- **Budget gate before every call.** `ConsumeStep` (decrement-check-restore) is TOCTOU-safe across concurrent branches; recovery rides *outside* the gate via `skipBudgetGate` (ceiling = `max_steps + 2`, asserted by `TestFinalizeOutsideBudget`).
- **Side-effect-aware completion critic** (`llm_agent_completion.go`): on a *mutating* turn, a cheap critic grades the deliverable by **observed tool results**, vetoing once if "a script written but never executed" — a genuinely good guard against false "done." Fails *open* (critic outage never wedges a turn).
- **Cache-stable assembly** (`prompt/builder.go`): volatile hints (budget, workspace, current time) are tail-injected to a *copy* of history; `messages[0]` is a package constant, never templated. Date-sensitive turns stay deterministic without poisoning the cached prefix.
- **Parallel tool dispatch** (`llm_agent_dispatch.go`, `llm_agent_parallel.go`): runnable calls execute concurrently (sem-bounded) but shared-state mutations (history append, dedup ring, result Events) stay serial in original call order, preserving the wire contract and cache stability.
- **HITL pause** (`llm_agent_pause.go`): name-gated to `ask_user` only; emitted as an `Actions.AwaitingInput` Event; persistence/resume is the Runner's job. Clean separation.

### Loop weaknesses (where it bites)

- **Concurrency safety is convention, not enforcement.** The dedup ring is mutated lock-free under a documented "serial caller" invariant that spans three files, directly adjacent to the concurrent `executeBatch` (AG-002). Goroutines have no panic recovery (AG-001).
- **Soft wall-time.** The budget wallclock is a *step-boundary* gate; `Budget.WithDeadline` exists but is not wired into the run ctx, so total wall-time can overshoot by one per-call timeout per step (AG-041).
- **No checkpointing.** All run state is in-memory (D-26). Pause/resume is durable, but a crash mid-run loses everything (AG-042).
- **Latency cliff on classifier miss.** The reasoning-tier router falls back to a synchronous LLM round-trip (≤8s) per turn when the embedding sidecar degrades (AG-008).

## 3. Architecture weaknesses (system level)

1. **No panic firewall.** A long-lived multi-channel daemon executing arbitrary tools has no goroutine-level isolation; one panic = process death (AG-001).
2. **MCP integration lacks resilience primitives.** No backoff, no circuit breaker, lock-during-IO reconnect, ctx-coupled recovery, `=0`-disables-timeout (AG-005/AG-006).
3. **Trust is binary at the server boundary, not per-call.** Mutating MCP tools and self-authored skills run with no capability-grant gate (AG-007/AG-011), even though the PRD defines `capability_grants` (Slice 1.7).
4. **Observability is span-and-counter, not log-and-metric.** No structured logs; no latency/error/cost/outcome metrics; tracing fails silently (AG-012/AG-013).
5. **Secret boundary is name-heuristic.** `IsSecretEnvKey` is a substring denylist that misses DSN-shaped vars (AG-010); command hooks inherit the full env (AG-003).
6. **Extension surfaces (hooks, skills) are powerful but unsafe-by-default.** TOCTOU, no fail-soft, unvalidated rewrites, ungated activation.

## 4. Suggested target architecture (summary)

Keep the loop core intact — it is verified correct. Add **thin, independently-shippable layers** around it:

- A **panic firewall**: a `safeGo` helper wrapping every spawned goroutine with recover→error; a Runner-level per-turn recover backstop.
- An **MCP resilience layer**: single-flight reconnect off-lock, `WithoutCancel` + dedicated timeout, exponential backoff + per-server circuit breaker, boot-validated timeouts.
- A **capability gate**: consult `capability_grants` for `Mutating && Untrusted` tool dispatch (MCP + skill activation); default-untrusted provenance for unknown tools and `swarm_spawn` reports.
- A **unified observability surface**: `slog` at turn/llm/tool boundaries + a complete Prometheus metric set + never-panic telemetry + a daemon tracer-boot log.
- A **durability boundary**: incremental checkpoint of history+counters keyed by sessionID for crash-resume; an active wallclock deadline ctx.
- A **secret boundary**: DSN-aware `IsSecretEnvKey`; minimal-env command hooks; value-scan redaction.

See `target-architecture.md` for the detailed design, interfaces, and failure-handling model.
