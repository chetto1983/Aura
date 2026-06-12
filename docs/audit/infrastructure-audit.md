# Infrastructure Audit — Aura `internal/agent` (2026-06-12)

Scope: `internal/agent/**` and its integration points (`internal/runner`, `internal/llm`, `internal/mcp`, `internal/agui`, `internal/config`, `cmd/aura`, `compose.yaml`, CI). The previous cycle (R-14) added `/healthz` and four counters; this cycle finds the daemon still operationally blind in the ways that matter for go-live.

---

## Observability gap matrix

| Signal | Exists? | Reality |
|---|---|---|
| **Logs** | Partial → effectively no | `slog` in outer layers (cron/telegram/skills/conversations/swarm) but **zero** in the `internal/agent` core; failures surface only via debug-gated `reasoningtrace`. `main()` never calls `slog.SetDefault` → Go's default **text** handler to stderr, no JSON, no `service`/`request_id`/`thread_id` correlation. (O-01) |
| **Metrics** | Minimal | Four monotonic expvar counters (`metrics.go`); no latency histograms, no error rates, no in-flight gauge, no cost, **no Prometheus**. expvar at `/debug/vars` is not scrapeable. (O-02) |
| **Traces** | REPL-only, dark in the daemon | Real OTel SDK exists, but `aura serve` **never boots it** (only `chat_repl.go:32` does) → the single `llm.request` span is dropped in production. Default exporter `otlp→localhost:4317`, no collector in compose, **process-global no-op error handler** swallows all otel errors. Only `llm.request` spans — no turn/tool spans. (O-01, O-03, O-08) |
| **Health** | Liveness-ish only | `/healthz` exists (`agui/server.go:82`) but checks **only Postgres** (`pool.Ping`); no Neo4j/embed/provider probe, no `/readyz` liveness/readiness split. (O-05) |
| **Profiling** | No | No `net/http/pprof` on the daemon. |
| **Config validation** | LLM key only | Only the API key fail-fasts at boot; empty `NEO4J_PASSWORD`/`POSTGRES_PASSWORD` fail late; numeric/bool knobs silently fall back on parse error. (O-04) |
| **Graceful shutdown** | Asymmetric | HTTP + scheduler tick drain on SIGTERM, but an in-flight `Runner.Turn` is hard-cancelled mid-call (no bounded turn-completion grace). (O-06) |
| **Containerization** | Sidecars only | No `Dockerfile` for the agent binary; it ships as a host binary. Sidecars run root, full caps, mostly no `mem_limit`. (D-01) |
| **CI** | Linux-only | Every job `ubuntu-latest`; no `windows-latest` lane despite OS-specific kill code in shipped Windows binaries. (O-07) |

---

## Prior-claim verification

| ID | Claim | Status | Evidence |
|---|---|---|---|
| R-14 | `/healthz` + counters + Prometheus | **PARTIAL** | `/healthz` exists (`agui/server.go:82`) but PG-only; still 4 expvars, **no Prometheus** |
| R-31 | tracing dropped by default + global no-op handler | **OPEN** | `tracing.go:55-66` default otlp→localhost:4317; `tracing.go:63` no-op handler; **and the daemon never boots the tracer at all** |
| R-32 | no structured logging in agent core | **OPEN** | zero `slog` in `internal/agent/*.go`; default text handler, no correlation |
| R-34 | SIGTERM drops in-flight turns | **PARTIAL** | HTTP/scheduler drained; conversational turn body hard-cancelled |
| R-36 | no Windows CI lane | **OPEN** | all jobs `ubuntu-latest` |
| R-44 | AG-UI no per-thread in-flight guard | **OPEN** | `server.go`/`runner.go` have no per-`convID` lock (also a data-integrity bug, B-03) |

---

## What's missing to be industrial-grade

### Logging (O-01)
Install a JSON `slog` default in `main()` with base `service`/`version` attrs and an `AURA_LOG_LEVEL` knob; thread a `slog.Logger` (with `request_id`/`thread_id`) into the loop at the chokepoints that currently call `reasoningtrace.Record` — at minimum for WARN/ERROR (stream retry, empty-response recovery, dedup trip, finalize-on-budget, breaker trip). Wrap the handler with a `SanitizeString` `ReplaceAttr` so no line can leak a DSN/key (O-04).

### Metrics (O-02)
Adopt `prometheus/client_golang`; mount `promhttp.Handler()` at `GET /metrics`. Minimum set:
- Histograms: `aura_turn_duration_seconds`, `aura_tool_duration_seconds{tool}` (timing already at `llm_agent.go:366`), `aura_llm_request_duration_seconds`.
- Counters: `aura_errors_total{kind}` (tool_failure, stream_error, finalize_on_budget, breaker_open, empty_response_recovery), `aura_sse_dropped_total`.
- Gauges: `aura_inflight_turns`, `aura_sse_connections`.
- Cost: a counter sourced from `llm.Usage` (prompt/completion/cached tokens, $).

### Tracing (O-01, O-03, O-08)
Boot the tracer in `runServe` (mirror `chat_repl.go:32`) with a deferred bounded `Shutdown`. Replace the process-global no-op error handler with a rate-limited logging handler (or scope suppression to `Shutdown`). Add an `agent.turn` root span parenting the `llm.request` spans, and a `tool.execute` span per dispatched call.

### Health (O-05)
Extend `HealthCheck` to probe Neo4j (`knowledge/ping`) + embed reachability; add `/readyz` requiring all deps; keep `/healthz` as pure liveness. Provider reachability = a shallow cached check, not a per-probe token burn.

### Config & secrets (O-04)
`Config.Validate()` called by `bootServe`: fail-fast on empty required secrets when the subsystem is enabled; WARN (don't silently default) on a parse fallback. Apply log redaction as above.

### Deployment (D-01)
A multi-stage distroless/alpine `Dockerfile` for `aura` with a non-root `USER`; an `aura` compose service with `read_only: true`, `cap_drop: [ALL]` (then add back only what `shell_exec` legitimately needs, or run shell in a sub-sandbox), `mem_limit`/`cpus`, and a `/healthz` healthcheck. Add `cap_drop`/`mem_limit` to the sidecars.

### Shutdown (O-06)
Give in-flight turns a bounded completion grace on SIGTERM (derive the turn ctx so shutdown applies a bounded `WithTimeout` rather than immediate cancel), within `aguiShutdownTimeout`.

### CI (O-07)
Add a `windows-latest` job: `go build`, `go vet`, `internal/agent/tools` unit tests (race if feasible); gate the `taskkill` kill-path tests to actually run on Windows.

---

## Disk & resource lifecycle (cross-ref memory audit)

- **`$AURA_RUN_DIR` sidecars + reasoningtrace grow monotonically** (M-06/R-33): the orphan scan is boot-only and whole-conversation-orphan only; no per-conversation TTL/byte budget; reasoningtrace has no rotation and logs the full history per turn. Add a periodic sweep + rotation.
- **Per-session tool state never evicted** (R-41): `todo`/`shell_bg` singleton maps keyed by session id are never pruned on conversation delete. Add an `Evict(sessionID)` hook from the `ConversationCleaner`.
- **Spill-outside-transaction orphans** (M-04): a rolled-back oversized turn leaks a `<seq>.content` file the boot scan never reclaims in a live conversation.

---

## Dependency posture

- `telebot` v4 **beta** on the primary user channel (R-40, tracked): pin-watch for GA; re-run HITL live tests on bump.
- OTel trace train pinned; `prometheus/client_golang` is the one net-new dependency required by O-02 (well-established, low risk).

---

## Bottom line

The daemon is **shippable as a prototype, not operable as a product**: you cannot trace a request, alert on an error spike, or distinguish a healthy daemon from one with a wedged dependency. None of this is hard — it is the standard observability/deployment surface, currently bolted to the REPL instead of the daemon. The single highest-leverage move is a shared `obs.Init(cfg)` called by both entry points (tracer + JSON slog + Prometheus registry), which closes O-01/O-02/O-03/O-08 together.
