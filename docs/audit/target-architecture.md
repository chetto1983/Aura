# Target Architecture — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`

The proposed industrial-grade design. **The loop core is kept intact** — it is verified correct (bounded recovery, non-empty terminal contract, cache-stable assembly, shared-atomic budget). The target adds **thin, independently-shippable layers** that close the operational perimeter without touching the reasoning algorithm.

## Design principles

1. **The loop is the asset — wrap it, don't rewrite it.** Every change below is additive or a boundary swap; none alters the `iter.Seq2` Event contract or the termination semantics.
2. **Fail-soft by default, fail-loud at boot.** Transient runtime faults (panicking tool, hung MCP, down sidecar, failing hook) degrade to a model-visible error or a graceful finalize; configuration faults crash at startup, not mid-run.
3. **Provenance and capability are per-call, not per-boundary.** Trust decisions ride with the data and the action, not with a server-admission flag set once.
4. **Observability is a first-class output, not a side effect.** Every turn emits a structured log line + metrics + a span; telemetry can never crash the process.

## Target module map

```
┌─────────────────────────── Runner (composition root) ───────────────────────────┐
│  per-turn recover backstop · slog root · tracer boot · checkpoint store           │
└───────────────────────────────────────────┬──────────────────────────────────────┘
                                             │
                 ┌───────────────────────────▼───────────────────────────┐
                 │                  LlmAgent.Run (UNCHANGED)               │
                 │  budget gate → build → hooks → stream → consume →       │
                 │  truncation → pause → dispatch → finalize               │
                 └───┬───────────┬───────────┬───────────┬───────────┬─────┘
                     │           │           │           │           │
        ┌────────────▼──┐ ┌──────▼─────┐ ┌───▼──────┐ ┌──▼────────┐ ┌▼───────────────┐
        │ panic firewall│ │ capability │ │ MCP      │ │ secret    │ │ observability  │
        │  safeGo(...)  │ │ gate       │ │ resilience│ │ boundary  │ │ surface        │
        │  recover→err  │ │ (grants)   │ │ layer    │ │           │ │                │
        └───────────────┘ └────────────┘ └──────────┘ └───────────┘ └────────────────┘
            NEW (P0)         NEW (P1)        NEW (P1)      HARDEN(P1)     NEW (P1)
```

## Layer specifications

### L1 — Panic firewall (NEW, AG-001/AG-002)
- **Boundary:** `safeGo(label string, fn func() error) error` wrapper used by `executeBatch`, `workflow/parallel.runSub`, `swarm.runWave`, the `shell_bg` reaper, and in-process hook calls.
- **Behavior:** `defer recover()` → translate to a typed error (per-call `toolRunResult{Err}` / per-child `{status:failed}` / hook error routed by fail-soft policy). Increment `aura_agent_panic_total{site}`. A Runner-level per-turn `recover` is the last backstop.
- **Note:** concurrent-map-writes are Go *fatals* not panics — the `dedupRing` mutex (AG-002) is the separate, required fix.
- **Failure handling:** a panic becomes a model-visible error preview; the daemon and sibling turns/channels are unaffected.

### L2 — Capability gate (NEW, AG-007/AG-011/AG-052)
- **Interface:** `Authorize(ctx, ToolCall, spec) (Decision, error)` consulted in `dispatch` for `spec.Mutating && provenance==Untrusted`.
- **Backing:** `capability_grants` (Slice 1.7). Decisions: `allow` / `confirm-via-ask_user` / `deny`. Skill activation routes through the same gate.
- **Provenance default:** unknown-tool output and `swarm_spawn` child reports default to **untrusted** (wrapped in the nonce envelope) unless explicitly marked trusted — the fail-safe direction.
- **Failure handling:** no grant → confirm or deny, never silent execution.

### L3 — MCP resilience layer (NEW, AG-005/AG-006/AG-024/AG-027)
- **Reconnect:** `singleflight` keyed per server, executed *outside* `s.mu`; swap `s.client` under the lock only to publish. Reconnect ctx = `context.WithoutCancel(parent)` + dedicated timeout.
- **Backoff + breaker:** exponential backoff with ceiling; per-server circuit breaker (open after N consecutive reconnect failures, cooldown).
- **Timeouts:** resolved + validated once at mount/boot; `0` → default; an explicit `-1` sentinel for infinite; the agent turn always carries a hard ceiling ctx.
- **Mutating semantics:** after-send transport failures marked non-retryable to the model; reconnect Mutating-flip logged.
- **Failure handling:** a flapping/hung server degrades to `ErrTransport` for a cooldown window; never freezes the runtime or thrashes spawns.

### L4 — Secret boundary (HARDEN, AG-010/AG-003/AG-009)
- **`IsSecretEnvKey`:** add URL/DSN markers (`url,dsn,uri,conn,pwd,cookie,session,jwt`) + a `scheme://user:pass@` value-scan; output redactor gains a DSN pattern.
- **Command hooks:** `cmd.Env` defaults to a minimal allowlist (`PATH` + explicit `cfg.Env`); exec-by-fd to close the TOCTOU; validate hook-supplied requests; audit every rewrite.
- **Reasoning trace:** treat as sensitive-at-rest; don't dump full history by default; cap per-field size before redaction.
- **Failure handling:** secrets never reach shell children, hook subprocesses, or the trace by default.

### L5 — Observability surface (NEW, AG-012/AG-013/AG-033)
- **Logs:** `slog` at turn-start, terminal `turnReason`, tool error, hook decision — keyed by `request_id`/`thread_id`.
- **Metrics:** `aura_agent_turn_total{outcome}`, `llm_call_duration_seconds`, `llm_errors_total{kind}`, `tool_errors_total{tool}`, `tokens_total{kind}`, `hook_total{event,decision}`, `span_export_failures_total`, `panic_total{site}` — on a non-default registry.
- **Tracing:** `mintSpanID` falls back to zero-ID on entropy failure (never panics); boot-log the exporter mode + endpoint; confirm the daemon boots the tracer.
- **Failure handling:** a missing collector or entropy hiccup degrades telemetry, never the process.

### L6 — Durability boundary (NEW, AG-042/AG-041)
- **Checkpoint:** incremental snapshot of `history` + budget counters + dedup ring keyed by `sessionID`, written by the Runner at step boundaries; resume reconstructs the loop on restart.
- **Active deadline:** `Budget.WithDeadline(parent)` threaded into the root `ic.Ctx` so the wallclock is an active cancellation, not just a step-boundary gate.
- **Failure handling:** a crash mid-run resumes from the last checkpoint; total wall-time is hard-bounded.

## Runtime flow (target)

1. Runner mints `request_id`, opens a span, sets up `slog` context, registers the per-turn recover backstop, and (if resuming) loads the checkpoint.
2. `LlmAgent.Run` proceeds unchanged, but: tool dispatch goes through L1 (panic firewall) + L2 (capability gate); MCP calls go through L3; shell/hook env goes through L4.
3. Every terminal `turnReason` emits an L5 log + metric; the checkpoint (L6) is updated at step boundaries.
4. On any transient fault (panic, hung MCP, down sidecar, failing hook) the system degrades to a model-visible error or a graceful finalize — the existing non-empty terminal contract guarantees the user still gets prose.

## Persistence / checkpointing strategy

| State | Today | Target |
|---|---|---|
| Conversation history | in-memory (D-26) | + incremental checkpoint (L6), Runner-owned |
| Budget counters / dedup ring | in-memory | + checkpoint snapshot |
| HITL pause | durable (`aura.paused_states`) | unchanged (already correct) |
| Large tool outputs | sidecar in `$AURA_RUN_DIR` | unchanged |
| Skill activations / scheduled jobs | persisted | + capability-gated (L2) |

## What deliberately stays the same

The loop's control flow, the `iter.Seq2` Event contract, the budget tree's shared-atomic design, the cache-stable prompt assembly, the SSRF hardening, and the nonce untrusted-output envelope are all verified-correct and are **not** changed. The target is a perimeter, not a rewrite.
