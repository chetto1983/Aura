# Architecture Review — Aura `internal/agent` (2026-06-12)

## 1. Current architecture

Aura's agent runtime is a **single-goroutine ReAct loop** exposed as a Go push iterator (`iter.Seq2[*Event, error]`), composed over an open `Agent` interface, driven by per-context turn drivers, and bounded by a shared budget tree. Tool execution within a turn fans out to a bounded goroutine pool; everything else (history, dedup gate, yields) stays serial.

### Component map

| Component | File(s) | Responsibility |
|---|---|---|
| `Agent` interface | `agent.go` | Open contract: `Name`/`Description`/`Run`/`SubAgents`/`FindAgent`. Termination is an Event, never the error slot (D-04). |
| `InvocationContext` | `agent.go:47-74` | Single-Run-scoped, passed by value; carries `Ctx`, `RequestID` (UUIDv7), `SpanID`, `Branch`, shared `*Budget`. `WithContext`/`WithSubAgent` copy, never mutate. |
| `LlmAgent` (the loop) | `llm_agent.go` + `llm_agent_*.go` (11 files) | ReAct run-loop: budget gate → prompt build → streamed LLM call → tool dispatch → terminate. Owns in-memory `history`. |
| `Budget` | `budget.go`, `budget_dedup.go` | Shared-atomic step counter + per-branch dedup ring + wallclock deadline + injectable clock. The DoS/loop-storm control. |
| Parallel tool exec | `llm_agent_parallel.go` | `executeBatch` — bounded (semaphore, `AURA_LOOP_MAX_PARALLEL_TOOLS`, default 4) concurrent tool dispatch; results re-serialized in call order. |
| Workflow agents | `workflow/loop.go`, `sequential.go`, `parallel.go` | adk-derived orchestrators composing the `Agent` interface. |
| Tool registry | `tools/registry.go`, `spec.go`, `manifest.go` | Name→tool map, deferred-spec manifest, `Without()` derivation. |
| Tools | `tools/*.go` (~24 tools) | shell_exec/fs_*/web_*/skill/task/ask_user/send_file/swarm_spawn/read_tool_output/… |
| Tool search | `tools/search.go`, `bm25.go` | The `tool_search` hook tool — BM25 retrieval over deferred specs. |
| Result normalization | `tools/result.go`, `read_tool_output.go` | Preview cap + sidecar spillover to `$AURA_RUN_DIR` + paging. |
| Trust boundary | `trust.go` | Wraps untrusted tool output in `<tool_output trust="untrusted" nonce=…>` (NFKC + HTML-escape + crypto/rand nonce). |
| MCP bridge | `mcptools/bridge.go`, `mount.go`, `name.go`, `bridge_reconnect.go` | Mounts external MCP servers as namespaced tools; per-call timeout + reconnect-no-replay. |
| Reliability | `llm_agent_stream_retry.go`, `internal/llm/breaker.go` | Stream-open retry classification + circuit breaker checked via `Allow()` before each call. |
| Prompt assembly | `prompt/builder.go`, `prompt.go`, `hash.go`, `cache_anthropic.go` | Byte-stable system prompt + volatile-hint tail injection + cache-control seam. |
| Events | `event.go`, `llm_agent_events.go` | Forward-compat Event/Actions/LLMResponse model; byte-identical JSON round-trip. |
| Tracing / metrics | `tracing.go`, `metrics.go` | OTel span per LLM call; 4 expvar counters. |
| Swarm seam | `swarm_context.go` | Cycle-free ctx key letting `swarm_spawn` reach the parent budget/registry/client. |

### Control flow (one turn)

A turn driver — the `Runner` (conversations), `swarm.runChild` (workers), `cron/handlers.agentjob` (scheduled), or `cmd/aura agent dry-run` (mock) — constructs a **fresh `LlmAgent` per turn**, seeded with DB-rehydrated history whose `messages[0]` is the byte-stable system prompt (never mutated), and an `InvocationContext` carrying a UUIDv7 RequestID and a shared `*Budget`.

Each loop iteration (`LlmAgent.Run`, `llm_agent.go:147-313`):

1. **Budget gate** (`ConsumeStep`) — wallclock first, then TOCTOU-safe atomic decrement; a trip triggers one bounded recovery nudge, then `finalize`.
2. **Bounded call ctx** — `WithTimeout(TotalTimeoutSec)`; a per-LLM-call span starts.
3. **Prompt build** — the builder reproduces `messages[0]` byte-stable and tail-injects volatile hints (budget, workspace, time) to a *copy*; the cache prefix is never poisoned.
4. **Stream open with retry + breaker** — `Allow()` gate → `client.Stream`; classified retry on 429/5xx/transport.
5. **Consume** — drains the stream, re-emitting text/reasoning/tool-call Events; airtight yield-after-false + drain-to-close.
6. **Dispatch** — partition terminal (`text_response`) from runnable; serial dedup gate; `executeBatch` runs runnable calls concurrently; results re-serialized in call order with per-call history append + `AfterToolResult` + result Event; terminal handled last.
7. **Terminate** — `text_response` (with an optional completion-gate veto), content-stop fallback, budget-trip finalize, or the error slot for real infra failure.

### Persistence & context

History is **in-memory within a turn**; the `Runner` journals per-event to Postgres (`conversation_turns`) and rehydrates on the next turn. Oversized tool outputs spill to `$AURA_RUN_DIR/<session>/<seq>.{content,result}` sidecars (`0o600`, opaque-id grammar) and page back via `read_tool_output`. A microcompaction ladder (`conversations/context.go`) protects the provider context window: L1 evicts old tool results to `read_tool_output` pointers, L2.5 drops oldest pairs, the system prompt + always-block are protected.

---

## 2. Agent-loop design analysis

**Strengths (verified, preserve).** The loop is deterministic and debuggable in its happy path, with airtight resource bounds:

- **Budget** is TOCTOU-safe (decrement-then-check-then-restore), shared by pointer across the whole tree so a depth-3 fan-3 swarm consumes ≤ `max_steps` total, fail-fast on malformed env, with an injectable clock for deterministic wallclock tests.
- **`iter.Seq2`** discipline is exemplary: every `yield`→false path returns immediately or drains-to-close before reporting `stopped`, so the range-function contract is never violated; no goroutine leaks (goleak-guarded across 4 packages, `-race` green).
- **Recovery/dedup** are bounded *counters*, not one-shot latches — no CrewAI-style infinite-recovery path; the two-tier dedup ring uses a result-preview progress veto so a volatile-output tool isn't falsely suppressed.
- **Reliability primitives are real**: the breaker is consulted (`Allow()`) before each stream open; per-call (`TotalTimeoutSec`) and per-tool (`NodeTimeout`) deadlines are wired and the wallclock genuinely cancels in-flight work via `Budget.WithDeadline` (now called from the runner — the prior "dead code" finding is closed).

**Weaknesses (this cycle).** The loop is *correct*; the system around it is not yet *durable* or *observable*:

1. **Crash durability is half-built (B-01).** The loop persists per-event, but a mutating tool's side effect commits *before* its result turn is persisted, and crash-recovery *drops* the dangling call → re-execution on resume. The architecture needs a write-ahead intent record between "decided to call" and "executed."
2. **Concurrency boundary is unguarded (B-03).** The loop is single-goroutine *within* a turn, but nothing serializes *two turns on one thread*. The composition root (Runner) needs a per-thread in-flight guard.
3. **The trust envelope has a gap (B-02).** `trust.go` wraps the 8 direct untrusted feeders, but the swarm-report path re-enters the parent prompt outside it. Provenance must be set at the swarm adapter.
4. **The breaker's lifetime undercuts its purpose (B-05).** Reconstructed per turn, it can't span a multi-turn outage. It belongs on the Runner, not the per-turn `LlmAgent`.
5. **No turn/tool spans, no idle watchdog (O-08, B-08).** Tracing sees only `llm.request`; a stalled-but-open stream is bounded only by the whole-call timeout.

---

## 3. Architecture weaknesses (systemic)

| Weakness | Where | Consequence |
|---|---|---|
| Side-effect-before-durability ordering | `dispatch` ↔ `runner_persist` | Re-executed mutating tools on crash (B-01) |
| Per-turn-scoped reliability state | `LlmAgent.breaker`, `Budget.dedupRing` | No cross-turn breaker / dedup protection (B-05) |
| No composition-root concurrency control | `Runner.Turn` | Conversation corruption on concurrent runs (B-03) |
| Observability bolted to the REPL, not the daemon | `serve.go` vs `chat_repl.go` | Production untraced + unlogged (O-01) |
| Trust envelope applied at the tool boundary, not the prompt-assembly boundary | `trust.go` keyed on a tool allowlist | New untrusted ingress (swarm) silently bypasses it (B-02) |
| Session-scoped tool state never evicted | `todo`, `shell_bg` singletons | Slow daemon leak (R-41) |
| Context ladder conflates "no cap" with "under cap" | `context.go:153` | Protection silently off on small-window models (M-03) |

---

## 4. Suggested target architecture (incremental, not a rewrite)

The current design is sound; the target is the same loop with the perimeter hardened. See [`target-architecture.md`](target-architecture.md) for the full diagram. The four structural moves:

1. **Durability boundary.** Introduce a write-ahead tool-intent log: `decide → persist intent (tx) → execute → persist result (tx)`. Recovery reads unmatched intents and surfaces them as "verify before re-running" markers instead of dropping them. This closes B-01 and subsumes the best-effort ledger (R-26) and the nanobot checkpoint-recovery pattern.
2. **Trust at the prompt-assembly boundary.** Move the untrusted-envelope decision from a tool-name allowlist (`trust.go`) to a property of every `ToolResult` (`Provenance`), defaulting `Trust=Untrusted` for any externally-sourced result (web, MCP, fs, shell, **swarm**). The prompt builder wraps on `Provenance.Trust`, so a new ingress can't forget to opt in. Closes B-02 structurally.
3. **Reliability state on the Runner.** Hoist the circuit breaker (and optionally a cross-turn dedup of *mutating* calls) to the process-lifetime `Runner`, injected into each `LlmAgent`. Add a per-thread in-flight guard there too. Closes B-03 and B-05.
4. **Observability as a first-class boot concern.** A single `obs.Init(cfg)` called by *both* `serve` and `chat` that installs the tracer, the `slog` JSON handler (with `service`/`version` base attrs), and the Prometheus registry; spans for `agent.turn` and `tool.execute`; a stream idle watchdog. Closes O-01/O-02/O-08, B-08.

These four are independent and individually shippable, each closing a cluster of findings without touching the verified-correct loop core.
