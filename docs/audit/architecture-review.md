# Architecture Review — Aura `internal/agent`

## 1. Current architecture

Aura's agent runtime is a **single-goroutine ReAct loop** exposed as a Go 1.23 push iterator (`iter.Seq2[*Event, error]`), composed over an open `Agent` interface, driven by per-context turn drivers, and bounded by a shared budget tree.

### Component map

| Component | File(s) | Responsibility |
|---|---|---|
| `Agent` interface | `agent.go` | Open contract: `Name`/`Description`/`Run`/`SubAgents`/`FindAgent`. Termination is an Event, never the error slot (D-04). |
| `InvocationContext` | `agent.go:47–74` | Single-Run-scoped, passed by value; carries `Ctx`, `RequestID` (UUIDv7), `SpanID`, `Branch`, shared `*Budget`. `WithContext`/`WithSubAgent` copy, never mutate (D-24). |
| `LlmAgent` (the loop) | `llm_agent.go` + `llm_agent_*.go` (11 files) | The ReAct run-loop: budget gate → prompt build → streamed LLM call → tool dispatch → terminate. Owns in-memory `history`. |
| `Budget` | `budget.go`, `budget_dedup.go` | Shared-atomic step counter + per-branch dedup ring + wallclock deadline + injectable clock. The DoS/loop-storm control. |
| Workflow agents | `workflow/loop.go`, `sequential.go`, `parallel.go` | adk-derived orchestrators composing the `Agent` interface. |
| Tool registry | `tools/registry.go`, `spec.go`, `manifest.go` | Name→tool map, deferred-spec manifest, `Without()` derivation. |
| Tools | `tools/*.go` (~24 tools) | shell_exec/fs_*/web_*/skill/task/ask_user/send_file/swarm_spawn/read_tool_output/… |
| Tool search | `tools/search.go`, `bm25.go` | The `tool_search` hook tool — BM25 retrieval over deferred specs. |
| Result normalization | `tools/result.go`, `read_tool_output.go` | Preview cap + sidecar spillover to `$AURA_RUN_DIR` + paging. |
| MCP bridge | `mcptools/bridge.go`, `mount.go`, `name.go`, `bridge_reconnect.go` | Mounts external MCP servers as namespaced tools; reconnect-on-use (stdio). |
| Prompt assembly | `prompt/builder.go`, `prompt.go`, `hash.go`, `cache_anthropic.go` | Byte-stable system prompt + volatile-hint tail injection + cache-control seam. |
| Events | `event.go`, `llm_agent_events.go` | Forward-compat Event/Actions/LLMResponse model; byte-identical JSON round-trip. |
| Tracing | `tracing.go` | OTel span per LLM call; root span IDs via crypto/rand. |
| Swarm seam | `swarm_context.go` | Cycle-free ctx key letting `swarm_spawn` reach the parent budget/registry/client. |

### Control flow (one turn)

A turn driver — the `Runner` (conversations), `swarm.runChild` (workers), `cron/handlers.agentjob` (scheduled), or `cmd/aura agent dry-run` (mock) — constructs a **fresh `LlmAgent` per round**, seeded with DB-rehydrated history whose `messages[0]` is the byte-stable system prompt (never mutated), and an `InvocationContext` carrying a UUIDv7 RequestID and a shared `*Budget`.

Each loop iteration (`LlmAgent.Run`, `llm_agent.go:139–293`):

1. **Budget gate** (`ConsumeStep`): wallclock check, then TOCTOU-safe atomic decrement-restore. A trip routes to counter-gated recovery (one nudged extra turn) or forced finalization.
2. **Prompt assembly** (`builder.Build`): the single chokepoint; tail-injects volatile hints (budget/workspace/time) onto a *copy* of history — `messages[0]` untouched.
3. **Streamed LLM call**: ctx-bounded by `TotalTimeoutSec` (120s), open-retried twice, consumed chunk-by-chunk (`consume`), re-emitting chunk/reasoning/tool-call Events.
4. **Tool dispatch** (`dispatch`): partition (first `text_response` = terminal); per-branch two-tier dedup gate; runnable calls executed **concurrently** (`executeBatch`); results collected in original order, appended to history, yielded serially.
5. **Terminate**: `text_response`, content-stop fallback, budget trip (explicit Event), or a real infra failure (error slot).

`ask_user` pauses are name-gated, rewrite the assistant message to ask_user-only calls, and emit `AwaitingInput` Events; the `Runner` is the sole `paused_states` writer; resume is a fresh run over rehydrated history (an iterator cannot be suspended — durability lives entirely in the stores).

### Lifecycle contract

- **Construction:** caller owns; one `LlmAgent` per turn (fresh-per-turn is enforced only by convention — see P3).
- **Persistence:** event-sourced but lossy — only the user turn, terminal answer, pause turn, and resume answers reach `conversation_turns`; intra-turn tool work lives only in memory + the observability ledger.
- **Cancellation:** `ic.Ctx` propagation only; the wallclock budget does not derive a deadline'd ctx (dead code).
- **Concurrency:** the loop is single-goroutine; the only fan-out is `executeBatch` (uncapped) and the swarm/workflow layers (capped).

## 2. Agent-loop design analysis

**Strengths (confirmed by adversarial reading):**

- The `iter.Seq2` discipline is airtight — every yield honors yield-after-false, with `stopped` plumbed through `consume`/`dispatch`, and drain-to-close preventing producer-goroutine leaks. `go vet` clean, `-race` green.
- Recovery is a **counter, never a latch** (the CrewAI #1656 anti-pattern is explicitly designed out); every recovery/finalize/critic path is bounded. There is **no infinite-loop path inside `LlmAgent`**.
- Dedup is fail-*safe*, not fail-open: args-only fingerprint pre-execution + result-preview progress veto means volatile-result tools look like progress instead of escaping the guard.
- Mutating tools are never retried; transient classification for tool retries is strictly typed.
- Cache-prefix hygiene is enforced end-to-end (builder copies, finalize copies, no in-place mutation of LLM-visible history).

**Weaknesses (architectural):**

- **Cancellation is half-built.** `Budget.WithDeadline`/`NodeTimeout` exist but are unwired; in-flight tool/LLM work cannot be cancelled by the wallclock budget. The "hard cap" is soft.
- **The composition contract is undefined.** Wrapping `LlmAgent` in `LoopAgent` double-spends the budget and double-counts dedup (P1); `LoopAgent` with `maxIter=0` hot-spins on a non-tool sub (P1). The two layers have overlapping, additive budget semantics over shared state with no documented ownership.
- **The trust boundary is unmodeled.** Tool output of every provenance is appended identically; the prompt builder has no concept of "untrusted data" vs "instruction".
- **Durability is per-turn-terminal, not per-event.** The persistence seam *sees* every event but writes only terminals; resume reconstructs from a lossy projection.
- **No backpressure on intra-turn fan-out.** `executeBatch` width is model-controlled and uncapped.
- **No operational surface.** No metrics, no health, no structured logs in the core; tracing is dropped by default.

## 3. Main components and responsibilities

(See the component map above.) The package is cleanly factored: every file is ≤600 LOC, concerns are split (`llm_agent_completion.go`, `_finalize.go`, `_retry.go`, `_events.go`, `_parallel.go`, `_pause*.go`), and decisions are documented inline with `D-NN`/`WR-NN`/`SC-N` references. Ownership boundaries are clear for the *happy path*; they blur at (a) the workflow↔LlmAgent budget seam, (b) the runner↔agent persistence seam, and (c) the tool↔untrusted-input seam.

## 4. Architecture weaknesses (ranked)

1. **Trust boundary not represented in the type system.** A `RoleTool` message carries no provenance. *Fix:* a `Provenance` field on tool results, rendered as a non-spoofable envelope by the builder.
2. **Cancellation is advisory.** *Fix:* derive the run ctx from `Budget.WithDeadline`; wrap each tool with `NodeTimeout`.
3. **Composition semantics undefined.** *Fix:* a single budget owner per tree; document on `Agent.Run`.
4. **Persistence is lossy.** *Fix:* journal intra-turn tool turns; one-transaction pause writes.
5. **External-call resilience is ad-hoc.** *Fix:* a shared retry/backoff/breaker in `internal/llm`; a per-call MCP timeout.
6. **No operational telemetry.** *Fix:* `expvar`/Prometheus counters + `/healthz` + structured slog.

## 5. Suggested target architecture

See [`target-architecture.md`](target-architecture.md) for the full design. In brief, the target keeps the loop core intact and adds five thin layers:

```
                       ┌─────────────────────────────────────────┐
   SIGTERM ──drain──▶  │  Daemon (aura serve): /healthz, /metrics │
                       │  graceful turn-drain, slog JSON           │
                       └───────────────┬───────────────────────────┘
                                       │
        ┌──────────────────────────────▼──────────────────────────┐
        │  Turn driver (Runner / swarm / cron)                     │
        │  • derives ctx from Budget.WithDeadline (real cancel)    │
        │  • journals EVERY event (intra-turn durability)          │
        │  • one-transaction pause writes                          │
        └──────────────────────────────┬──────────────────────────┘
                                        │
        ┌──────────────────────────────▼──────────────────────────┐
        │  LlmAgent loop  (UNCHANGED CORE — keep as-is)            │
        │  budget gate → build → stream → dispatch → terminate     │
        │  + mid-stream retry  + parallel-tool semaphore           │
        └───────┬───────────────────────────────┬──────────────────┘
                │                                │
   ┌────────────▼─────────┐          ┌───────────▼──────────────────┐
   │ llm.Client + shared  │          │ Tool layer                    │
   │ retry/backoff/breaker│          │ • provenance-tagged results   │
   │ (429/5xx, Retry-After)│         │ • filtered child env          │
   └──────────────────────┘          │ • bounded output buffers      │
                                      │ • MCP per-call timeout        │
                                      │ • destructive-action gate     │
                                      └───────────────────────────────┘
```

The principle (per the project's "no atomic bombs / minimal industrial shape" directive): **do not touch the loop core — it is correct.** Add the smallest layers that close the perimeter: a real cancellation ctx, a resilient LLM client, a provenance-aware tool boundary, per-event journaling, and an operational surface. Each is independently shippable.
