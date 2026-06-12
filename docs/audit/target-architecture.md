# Target Architecture — Aura `internal/agent` (2026-06-12)

The proposed industrial-grade design. **The loop core is kept intact** — it is verified correct. The target adds thin, independently-shippable layers that close the perimeter: a durability boundary, a provenance-by-default tool boundary, process-lifetime reliability state, a concurrency guard, and a unified observability surface.

---

## Design principles

1. **Preserve the core.** `LlmAgent`'s ReAct loop, the budget tree, the dedup ring, the `iter.Seq2` contract, the parallel `executeBatch`, and the cache-prefix discipline are sound. No rewrite — only additive layers and surgical fixes at named seams.
2. **The trust boundary is a type, not a tool-name allowlist.** Untrusted data is tagged at ingestion (`ToolResult.Provenance`) and the prompt builder wraps on that property. A new ingress cannot forget to opt in.
3. **Durability brackets every side effect.** A mutating tool's effect is bracketed by a write-ahead intent record (before) and a result record (after), both transactional. Recovery never silently drops a half-completed call.
4. **Reliability state outlives the turn.** The circuit breaker (and any cross-turn mutating-call dedup) lives on the process-lifetime `Runner`, not the per-turn `LlmAgent`.
5. **One observability init, both entry points.** Tracer + structured logger + metrics registry are installed once by a shared `obs.Init(cfg)` called by `serve` and `chat` alike.

---

## Runtime flow (target)

```
 channel/AG-UI/cron ─▶ Runner.Turn(threadID)
                         │  ① per-thread in-flight guard (singleflight)        [B-03]
                         │  ② rehydrate history (DB) + microcompact ladder
                         │  ③ build LlmAgent  ── inject: process-lifetime breaker [B-05]
                         │                              + shared obs logger/meter
                         ▼
                   LlmAgent.Run  (iter.Seq2, unchanged core)
                         │  budget gate ─▶ build prompt (wraps on Provenance.Trust) [B-02/AP-18]
                         │  stream-open (breaker.Allow + retry + idle watchdog)      [B-08]
                         │  consume (chunk/reasoning/tool-call events)
                         │  dispatch:
                         │     ├─ serial dedup gate
                         │     ├─ WRITE-AHEAD intent rows (tx) ──────────────┐       [B-01]
                         │     ├─ executeBatch (bounded concurrent)          │
                         │     └─ persist results (tx) + AfterToolResult ◀───┘
                         ▼
                   terminate: text_response | content-stop | finalize | error
                         │
            spans: agent.turn ▷ {llm.request, tool.execute}  + JSON logs + Prometheus  [O-01/02/08]
```

Everything in the inner box is the current, verified-correct loop. The bracketed annotations are the only additions.

---

## Module boundaries

| Module | Today | Target change |
|---|---|---|
| `agent` (loop) | `LlmAgent` owns history, breaker, dedup ring | Breaker injected from Runner; rest unchanged |
| `runner` | builds a fresh `LlmAgent` per turn, journals per-event | + per-thread in-flight guard; + write-ahead intent log; owns the process-lifetime breaker |
| `tools` | `ToolResult` with optional `Provenance` | `Provenance` default `Trust=Untrusted` for externally-sourced results; `Registry.Register` fail-loud on dup |
| `agent` prompt assembly (`trust.go` → builder) | tool-name allowlist decides wrapping | builder wraps on `ToolResult.Provenance.Trust` — applies to swarm, MCP, future ingresses uniformly |
| `swarm` | child report → unwrapped parent result | sets `Provenance{Trust:Untrusted}` |
| `conversations/context` | L1 rewrites all old `RoleTool`; `hardCap<=0` disables L2.5 | L1 only rewrites sidecar-backed turns; `hardCap<=0` is a config error |
| `obs` (new) | tracer in REPL only; 4 expvars; text logs | `obs.Init(cfg)` → tracer + JSON slog + Prometheus, called by both entry points; `agent.turn`/`tool.execute` spans |
| `llm` | per-call timeout, breaker, retry | + stream idle-timeout watchdog |
| deployment | host binary + sidecars | non-root distroless image + resource-bounded compose service |

---

## Observability model

- **Logs:** JSON `slog`, base attrs `service`/`version`/`env`, per-turn `request_id`/`thread_id`/`identity`; WARN/ERROR at the loop chokepoints (stream retry, empty-response recovery, dedup trip, finalize-on-budget, breaker trip). Secret-redacting `ReplaceAttr`.
- **Metrics (Prometheus):** `aura_turn_duration_seconds`, `aura_tool_duration_seconds{tool}`, `aura_llm_request_duration_seconds` (histograms); `aura_errors_total{kind}`, `aura_sse_dropped_total` (counters); `aura_inflight_turns`, `aura_sse_connections` (gauges); `aura_tokens_total{kind}` + cost.
- **Traces:** `agent.turn` root span per turn parenting `llm.request` and `tool.execute(tool)` spans; honest exporter error handler.
- **Health:** `/healthz` (liveness) vs `/readyz` (PG + Neo4j + embed reachability); `/metrics`; optional `/debug/pprof`.

---

## Failure-handling model

| Failure | Today | Target |
|---|---|---|
| Provider 429/5xx | retry + per-turn breaker | retry + **process-lifetime** breaker; breaker-open → graceful finalize |
| Stalled stream (open, no chunks) | bounded by whole-call timeout | per-chunk idle watchdog → retryable transport error |
| Crash mid-turn (mutating tool) | result lost, call dropped, **re-executed** | write-ahead intent → recovery surfaces "verify before re-running" |
| Crash in pause window | one-tx pause + load-time repair (CLOSED) | unchanged |
| Resume retry | non-atomic, can duplicate | one-transaction inject+mark |
| Concurrent runs on one thread | interleave + corrupt | per-thread guard (409 or serialize) |
| SIGTERM mid-turn | hard cancel | bounded completion grace |
| Hung MCP server | per-call timeout + reconnect-no-replay (CLOSED) | unchanged |

---

## Persistence / checkpointing strategy

The durability boundary is the central new invariant:

```
decide tool call
  └─▶ BEGIN tx: persist assistant tool_calls turn + intent row(s) (tool_call_id, canon args, status=pending)  COMMIT
        └─▶ execute (host side effect happens here)
              └─▶ BEGIN tx: persist RoleTool result + intent.status=done  COMMIT
```

On reload, any `intent.status=pending` with no matching result is a half-completed call: the recovery path injects a synthetic `RoleTool` ("previous result unknown — verify before re-running") rather than dropping the group, so the model reconciles instead of blindly re-issuing. This single mechanism closes B-01, subsumes the best-effort ledger (R-26 → audit gate), and implements the nanobot checkpoint-recovery pattern in Aura's transactional idiom. Oversized turn content continues to spill to `0o600` sidecars, but the spill write moves inside the commit path (or is reconciled by the periodic sweep) to close the orphan-on-rollback leak (M-04).

---

## What explicitly does NOT change

The budget tree (shared atomic + dedup ring + injectable clock), the `iter.Seq2` yield-after-false discipline, the bounded recovery/completion counters, the byte-stable `messages[0]` cache prefix, the parallel `executeBatch` semaphore, the sidecar id grammar + perms, the `send_file` symlink fence, the MCP reconnect-no-replay, and the process-group kill paths are all verified correct and are preserved as-is. The target is the same engine with a hardened chassis.
