# Infrastructure Audit — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`
**Scope:** observability, configuration, secrets, health, persistence, runtime isolation, dependency management for the agent runtime and its `serve`-daemon deployment shape.

## 1. Logging

**Current:** The agent core has **no `slog` structured logging**. Observability is three other mechanisms: (a) `reasoningtrace.Record(...)` JSONL — rich, but **off by default** (`AURA_REASONING_TRACE`) and a privacy hazard when on (AG-009); (b) OTel spans; (c) expvar/Prometheus counters.

**Gap:** Production (`aura serve`) runs without queryable, level-filterable, correlation-keyed logs. When a turn fails you have a span (if a collector is attached) and a counter increment, but no `slog.Error("llm stream failed", "request_id", …, "thread_id", …, "err", …)` line to grep.

**Recommendation:** Add `slog` at the turn/LLM/tool boundaries (start, terminal `turnReason`, tool error, hook decision) keyed by `request_id`/`thread_id`. Keep the JSONL reasoning-trace as the deep-debug tier, but redact PII and don't dump full history by default (AG-009). One log line per terminal outcome is the minimum.

## 2. Metrics

**Current (`metrics.go:11–68`):** expvar + Prometheus counters for budget steps, tool-dispatch count, LLM-stream-open, LLM-stream-retry; one tool-duration histogram.

**Gaps (AG-012, prior O-02):**
- No **LLM-call latency** histogram (the dominant latency is span-only).
- No **turn-outcome** counter — the rich `turnReason` taxonomy is exported nowhere.
- No **tool-error**, **token/cost**, or **hook** counters.
- expvar and Prometheus counters are **duplicated globals** registered at package init; `promauto` panics on duplicate registration → the package can't be instantiated twice (AG-057).

**Recommendation:** `aura_agent_turn_total{outcome}`, `aura_agent_llm_call_duration_seconds`, `aura_agent_llm_errors_total{kind}`, `aura_agent_tool_errors_total{tool}`, `aura_agent_tokens_total{kind}`, `aura_agent_hook_total{event,decision}`, `aura_agent_span_export_failures_total`. Use a non-default registry to decouple from global state.

## 3. Tracing

**Current (`tracing.go`):** OTel span tree is well-built — `agent.turn` wraps the loop, `llm.request` and `tool.execute` nest under it via ctx; attributes include model/provider/tokens/cache-hit and **deliberately never an api_key** (good, D-28).

**Gaps:**
- Default OTLP target `localhost:4317` insecure; with no collector, spans batch/drop **silently** — no boot log of exporter mode/endpoint, no export-failure metric (AG-056).
- `mintSpanID` **panics the process** on a `crypto/rand` failure (AG-033) — a telemetry ID must never crash the daemon.
- **NEEDS CONFIRMATION:** whether `aura serve` actually boots the tracer (composition root in `cmd/aura`, out of this package's scope; prior O-01 flagged it did not). If not, production traces are absent regardless of code quality here.

**Recommendation:** Boot-log the exporter mode + endpoint; export-failure metric; never panic in `mintSpanID` (fall back to zero ID). Confirm tracer boot in the daemon.

## 4. Configuration management

**Current:** Config is env-driven (`AURA_<DOMAIN>_<UNIT>` convention, ~60 vars; third-party sidecar vars keep upstream naming). The agent reads several directly on hot paths.

**Gaps:**
- **Hot-path env reads with loop-fatal failure:** `configuredMCPCallTimeout` re-reads + parses env per MCP call; a malformed value makes **every** MCP call a hard Go error mid-run (AG-027/F-18). Config should be resolved + validated **once at boot** (fail loud there), matching the project's "no hard-coded env, fail loud at boot" doctrine.
- **No positivity validation:** `AURA_LOOP_MAX_STEPS=0` / negative silently disables the runtime ("budget exhausted before first step") instead of erroring at `NewBudget` (AG-036).
- **`AURA_MCP_CALL_TIMEOUT_SEC=0` overloaded** to mean "infinite" rather than "default" (AG-006).

**Recommendation:** Centralize env resolution at boot into typed config with range + positivity validation; the agent reads from the struct, not `os.Getenv`.

## 5. Secrets handling

**Current:** `secret.IsSecretEnvKey` substring denylist strips secret-named vars from `shell_exec` children; OTel never emits api_key; reasoning-trace redacts named env-var secrets; MCP child env is an allowlist.

**Gaps:**
- **DSN-shaped secrets leak** (AG-010): `AURA_DB_URL=postgres://u:PASS@h` passes the denylist and is inherited by shell children.
- **Command hooks inherit the full `os.Environ()`** including every provider/DB secret (AG-003).
- Reasoning-trace redaction is name-heuristic only — misses typed secrets and PII (AG-009).

**Recommendation:** DSN-aware markers + value-scan redaction; minimal-env command hooks; treat the reasoning trace as sensitive-at-rest.

## 6. Health checks

**Current:** A `/healthz` exists at the daemon layer (prior cycle R-14). No readiness signal distinguishing "process up" from "dependencies (LLM provider, embed sidecar, MCP servers, Neo4j) reachable."

**Recommendation:** A `/readyz` that checks provider reachability (or breaker state), embed-sidecar liveness (ties to AG-008 fallback decision), and MCP server health; expose breaker-open and span-export-failure as health signals.

## 7. Queues / backpressure

**Current:** The loop is pull-based (`iter.Seq2`), so the event stream has natural backpressure (a slow consumer stops the iterator). Parallel tool dispatch is sem-bounded. Swarm fan-out is `MaxGoals`-capped and workers cannot re-spawn.

**Gaps:** No backpressure or rate-limit on MCP reconnect (crash-loop storm, AG-005); no global rate limit on outbound LLM/tool calls beyond the per-turn budget. The **NEEDS CONFIRMATION** per-thread in-flight guard (prior B-03, in `agui`/`runner`) is out of this package but materially affects whether concurrent turns on one thread corrupt history.

## 8. Persistence & checkpointing

**Current:** Run state (history, budget counters, dedup ring) is **in-memory only** (D-26). The HITL pause path *is* durable (Runner persists `aura.paused_states`). Tool spillover/sidecar persists large outputs to `$AURA_RUN_DIR`.

**Gap (AG-042):** No crash-resume — a process crash mid-run loses the conversation and partial swarm progress. Acceptable for CLI; a gap for a long-autonomous-run daemon.

**Recommendation:** Incremental snapshot of history + budget counters keyed by sessionID (a Runner concern, flagged here for completeness).

## 9. Runtime isolation & deployment

**Current:** By design (amendment #50/D-15c) the runtime executes `shell_exec` with the operator's full host privileges — no sandbox, no path fence. This is coherent for the single-operator model.

**Gaps:**
- **NEEDS CONFIRMATION** (prior D-01): no production container (non-root, read-only rootfs, resource-bounded) for a runtime that runs arbitrary shell. For any deployment beyond the operator's own machine this is a hard requirement.
- No goroutine-level panic isolation (AG-001) — a crash takes the whole daemon down.

## 10. Dependency management

**Current:** Standard Go modules; OTel, Prometheus, uuid, pgx (via other packages). MCP servers are external subprocesses (stdio) or remote HTTP. Embed sidecar (granite) and LLM provider (OpenRouter) are network dependencies.

**Gaps:** External-dependency degradation handling is uneven: the LLM provider has a circuit breaker (good); the **embed sidecar has none** (every miss → LLM-router round-trip, AG-008); **MCP servers have no breaker** (AG-005). `govulncheck` is wired in CI (good).

**Recommendation:** Extend the breaker pattern to the embed sidecar and per-MCP-server.

---

## Infrastructure scorecard

| Capability | State | Priority |
|---|---|---|
| Structured logging (slog) | ❌ absent in agent core | P1 |
| Latency/error/cost/outcome metrics | ❌ minimal counters only | P1 |
| Distributed tracing | ⚠️ spans good; silent-drop + panic risk; daemon boot unconfirmed | P1/P2 |
| Config validation at boot | ⚠️ hot-path env reads, no positivity check | P2 |
| Secrets hygiene | ⚠️ DSN leak + full-env hooks | P1 |
| Health / readiness | ⚠️ `/healthz` only | P2 |
| Backpressure / rate-limit | ⚠️ no MCP reconnect throttle | P1 |
| Crash-resume / checkpoint | ❌ in-memory only | P2 |
| Runtime isolation (container) | ❓ unconfirmed; no panic firewall | P0/P1 |
| Dependency degradation | ⚠️ LLM breaker only | P1 |
