# Target Architecture — Aura `internal/agent`

The proposed industrial-grade design. **The loop core is kept intact** (it is correct); the target adds thin, independently-shippable layers that close the perimeter: real cancellation, a resilient LLM client, a provenance-aware tool boundary, per-event durability, and an operational surface.

---

## Design principles

1. **Preserve the core.** `LlmAgent`'s ReAct loop, the budget tree, the dedup ring, the `iter.Seq2` contract, and the cache-prefix discipline are sound. No rewrite — only additive layers and surgical fixes at named seams.
2. **The trust boundary is a type, not a convention.** Untrusted data is tagged at ingestion and rendered as a non-spoofable envelope; the model is told envelope content is data.
3. **Every limit must actually cancel.** A budget that only refuses *new* steps is not a limit. The run ctx derives from the budget deadline; every external call (LLM, MCP, subprocess) has a real, ctx-propagated timeout.
4. **Durability is per-event.** The persistence seam already observes every event; it should write them, so resume reconstructs from a faithful log, not a lossy projection.
5. **Resilience is shared, not per-call-site.** One retry/backoff/breaker in `internal/llm`, used by loop/finalize/critic/router alike.
6. **Observability is non-optional.** Metrics + health + structured logs are part of the runtime, not an add-on.

## Recommended module boundaries

```
cmd/aura (composition root)
  ├─ boot: slog JSONHandler(level), metrics registry, OTel exporter (honest default)
  ├─ supervisor-friendly: /healthz, /metrics, graceful drain
  └─ constructs: llm.Client (resilient), tool Registry (provenance-aware), Runner

internal/llm  (resilience lives HERE, shared)
  ├─ Client: stream open/consume
  ├─ retry: typed transient classification + jittered backoff + Retry-After honoring
  ├─ breaker: consecutive-failure cooldown per provider
  └─ Config.LogValue(): secret-redacting

internal/runner  (turn driver — durability + cancellation live HERE)
  ├─ derives ctx := Budget.WithDeadline(parent)   ← real cancel
  ├─ journals EVERY event (assistant-tool_call, RoleTool, terminal)
  ├─ one-transaction pause writes  + load-time orphan-pair repair
  └─ bounded SIGTERM turn-drain

internal/agent  (the loop — UNCHANGED CORE + two seam fixes)
  ├─ LlmAgent.Run         (keep)
  ├─ + mid-stream retry re-issue (1.7)
  ├─ + executeBatch semaphore (parallel cap)
  └─ Budget / dedup / events / prompt builder (keep)

internal/agent/tools  (the boundary — provenance + bounds live HERE)
  ├─ ToolResult { Preview, Bytes, Provenance, Trust }   ← new fields
  ├─ shell_exec: filtered child env, ring-capped output, clamped timeout, destructive gate
  ├─ fs_edit: empty-old_string guard
  ├─ send_file: workspace fence
  ├─ task: no model-facing approve
  └─ result.go: turn-seq sidecar key, 0o600, allowlist id grammar

internal/agent/mcptools + internal/mcp  (external trust boundary)
  ├─ per-call timeout + ctx-aware read   ← closes the P0 hang
  ├─ filtered subprocess env
  ├─ bridged tools default Mutating (honor readOnlyHint)
  ├─ conditional reconnect (no replay)
  └─ provenance-framed, length-capped descriptions

internal/agent/workflow  (composition — one budget owner)
  ├─ LoopAgent: ctx-check + per-iteration budget; observational when sub is budget-aware
  └─ documented composition contract on Agent.Run
```

## Interfaces (new / changed)

```go
// Tool results carry provenance so the prompt boundary can frame untrusted data.
type ToolResult struct {
    Preview    string
    Bytes      int
    Sidecar    string
    Provenance Provenance // NEW
}
type Provenance struct {
    Source string // "shell_exec" | "web_fetch" | "mcp:<ns>" | "fs_read" | "skill" | "builtin"
    Trust  Trust  // Trusted | Untrusted
}

// Resilient client policy (internal/llm), shared by every LLM call site.
type RetryPolicy struct {
    MaxAttempts int
    BaseDelay   time.Duration
    Jitter      bool
    HonorRetryAfter bool
}
type Breaker interface {
    Allow() bool            // false when open (cooling down)
    RecordSuccess()
    RecordFailure(retryAfter time.Duration)
}

// Budget gains real teeth at the runner seam (existing methods, now WIRED):
ctx, cancel := budget.WithDeadline(parent)   // run-tree deadline
toolCtx, _  := context.WithTimeout(ctx, budget.NodeTimeout()) // per tool
```

## Runtime flow (target)

```
SIGTERM ──▶ daemon: stop accepting new turns; drain in-flight ≤ grace; then cancel
   │
turn ──▶ Runner:
   1. ctx, cancel = Budget.WithDeadline(parent)          // real cancel
   2. append user turn (tx)
   3. LoadManagedHistory + orphan-pair repair             // wire-safe load
   4. for each event from LlmAgent.Run(ic):
        • journal it (assistant-tool_call / RoleTool / terminal)   // per-event durability
        • increment metrics; emit correlated slog at terminal decisions
        • on pause: one-transaction pause-write
   5. LlmAgent loop (unchanged) — but:
        • LLM call → llm.Client{retry+breaker}             // survives 429/5xx
        • mid-stream error → bounded re-issue               // survives SSE cut
        • tool dispatch → executeBatch{semaphore}           // bounded fan-out
        • each tool → toolCtx with NodeTimeout              // real per-tool cancel
        • tool result → Provenance-tagged                   // trust boundary
        • MCP call → per-call timeout                       // no hang
```

## Observability model

| Signal | Target |
|---|---|
| **Logs** | slog JSON, `AURA_LOG_LEVEL`, every terminal decision + error logged with `request_id`+`thread_id`+`branch`; `llm.Config` redacted via `LogValue()` |
| **Metrics** | Prometheus: `aura_turns_total{outcome}`, `aura_tool_dispatch_total{tool,outcome}`, `aura_tool_retries_total`, `aura_budget_trips_total{reason}`, `aura_pause_total`/`aura_resume_total`, `aura_mcp_reconnects_total`, `aura_llm_latency_seconds`, `aura_llm_tokens_total{kind}`, `aura_llm_cost_usd_total`, `aura_breaker_state` |
| **Traces** | OTel spans for LLM calls (exist) + tool executions + swarm children, linked to the Event SpanID tree; honest default exporter (off or warn-on-unreachable); collector behind a compose profile |
| **Health** | `GET /healthz` (pool ping + scheduler last-tick + breaker state) → 200/503 |
| **Profiling** | `net/http/pprof` on loopback behind a flag |
| **Audit** | append-only `tool_invocations` + (optional) blocking write-ahead intent row for mutating tools; redacted at the boundary AND on the model-facing preview |

## Failure-handling model

| Failure | Today | Target |
|---|---|---|
| Provider 429/5xx | turn dies | retry w/ Retry-After + backoff; breaker on sustained outage |
| Mid-stream SSE cut after tools ran | turn dies, side effects orphaned | bounded re-issue from intact history |
| Hung MCP server | daemon-wide hang | per-call timeout → poison transport → reconnect |
| Hung/long tool | runs past wallclock | `NodeTimeout` + budget-derived ctx cancel |
| Wide parallel batch | host saturation | semaphore cap |
| Crash mid-turn | progress lost, side effects re-run | per-event journal → faithful resume |
| Crash in pause window | conversation bricked | one-transaction pause + load-time repair |
| SIGTERM mid-turn | turn dropped | bounded drain or "interrupted" marker |
| Prompt injection | reaches host RCE | provenance envelope + destructive gate + env filter |

## Persistence / checkpointing strategy

- **Source of truth:** `aura.conversation_turns` (PK `(conversation_id, seq)`, seq under a row-lock tx). Keep.
- **Add per-event journaling:** assistant-tool_call and RoleTool turns are written as they occur (the ledger end-event already carries call ID, args, preview, sidecar path), making resume a faithful replay and giving L1 microcompact its real population.
- **Pause atomicity:** paused_states row(s) + the combined assistant ask_user turn in **one transaction** at round end; resume injects answers and `MarkResumed` in **one transaction**.
- **Load-time integrity guard:** before building a request, validate tool_call↔tool_result pairing; repair or refuse-with-hint orphans (closes the brick class).
- **Idempotency for scheduler jobs:** until full journaling lands, scheduler `agent_job` payloads should be idempotent by contract (documented), since at-least-once is the current reality.
- **Disk lifecycle:** sidecar TTL sweep tied to the eviction horizon, run periodically by the scheduler (not only at boot); reasoningtrace rotation.

## What stays exactly as-is

The budget core, dedup ring, `iter.Seq2` discipline, parallel-agent choreography, cache-prefix builder, SSRF hardening, process-group kill, namespaced MCP mounts, and the redaction-at-the-ledger-boundary architecture. These are production-quality today; the target builds around them, not over them.
