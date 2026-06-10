# Infrastructure Audit — Aura `internal/agent`

Scope: `internal/agent/**` and its direct integration points (`internal/runner`, `internal/llm`, `internal/mcp`, `cmd/aura`, `compose.yaml`, CI). Line numbers verified against `tabula-rasa` @ `0ab722e5`.

---

## Observability gap matrix

| Signal | Exists? | Quality / Notes |
|---|---|---|
| **Logs** | Partial | slog in outer layers (cron 23, telegram 20, skills 13, conversations 9, swarm); **zero** in `internal/agent` core. Default Go text handler at Info → stderr; no JSON option, no `AURA_LOG_LEVEL`, no rotation. Correlation fields inconsistent (request_id absent from swarm/cron lines). |
| **Metrics** | **No** | No exporter, no counters anywhere. Only `cache_metrics` PG rows (cache-hit ratio) via `aura cache stats`. |
| **Traces** | Yes, dark by default | Real OTel SDK (`tracing.go`): per-LLM-call `llm.request` spans with model/provider/tokens/request_id. Default exporter `otlp→localhost:4317` with **no collector in compose** + a process-global no-op error handler → silently dropped. Tool executions and swarm children have **no spans**. |
| **Health** | No (daemon) / Yes (sidecars) | Every compose sidecar has a healthcheck; `aura serve` has **no `/healthz`**, no readiness, no supervision. A process alive with a wedged scheduler or hung MCP is indistinguishable from healthy. |
| **Profiling** | **No** | No `net/http/pprof` mount on the daemon. |
| **Debug trace** | Yes (opt-in) | `AURA_REASONING_TRACE` JSONL — rich but unbounded, temp-dir default, best-effort name-heuristic redaction. |
| **Audit** | Partial | `tool_invocations` + `skill_audit` PG tables (migrations 0010/0011); Event StateDelta carries termination_reason/limit_hit. Best-effort writes (not a pre-execution gate). |

## Configuration inventory — env vars actually read on `internal/agent` code paths

| Var | Default | Read at | Notes |
|---|---|---|---|
| `AURA_LOOP_MAX_STEPS` | 25 | budget.go:40 | fail-fast on malformed |
| `AURA_LOOP_MAX_WALLCLOCK_SEC` | 300 | budget.go:41 | **blocks new steps only — never cancels in-flight (P1)** |
| `AURA_LOOP_DEDUP_WINDOW` | 3 | budget.go:42 | |
| `AURA_LOOP_BRANCH_SOFT_FRACTION` | 1.0 | budget.go:43 | advisory only |
| `AURA_LOOP_DEDUP_RESULT_CAP` | 2048 | budget.go:44 | |
| `AURA_LOOP_NODE_TIMEOUT_SEC` | 0 | budget.go:138 | **DEAD — parsed, never consumed (P1)** |
| `AURA_LOOP_DEDUP_EXEMPT_TOOLS` | — | budget.go:145 | |
| `OPENROUTER_API_KEY` | — (fail-fast) | llm/config.go:151 | only secret; header-only use (D-28) |
| `AURA_LLM_MODEL`/`_BASE_URL`/`_TEMPERATURE`/`_MAX_TOKENS` | deepseek-v4-flash:exacto / openrouter / 0.7 / 4096 | llm/config.go:256–271 | 4-tier precedence incl. `~/.aura/llm.json` |
| `AURA_LLM_TOTAL_TIMEOUT_SEC` | 120 | llm/config.go:285 | per-LLM-call ctx ceiling (`llm_agent.go:176`) |
| `AURA_LLM_CONNECT_TIMEOUT_SEC` | 10 | llm/config.go:290 | dialer timeout |
| `AURA_LLM_ADAPTIVE_REASONING` | true | llm/config.go:272 | router call capped 8s |
| `AURA_MODEL_CONTEXT_WINDOW`/`_MAX_OUTPUT_TOKENS` | 1M / 32768 | llm/config.go:275–283 | L2 budget inputs; **1M hardcoded for DeepSeek (P2)** |
| `AURA_COMPLETION_GATE`/`_CRITIC_MODEL` | true / loop model | llm/config.go:167–168 | |
| `AURA_CONTEXT_PREVIEW_CAP_BYTES` | 2048 | config.go:218 | spillover threshold |
| `AURA_RUN_DIR` | OS-derived | config.go:217 | sidecar root; **grows unbounded (P2)** |
| `AURA_OTEL_EXPORTER`/`_ENDPOINT` | otlp / localhost:4317 | config.go:219–220 | **default silently drops (P2)** |
| `AURA_REASONING_TRACE`/`_FILE` | off / `$TMP/aura-reasoning-trace.jsonl` | reasoningtrace.go:15–16 | **unbounded when on (P2)** |
| `AURA_SWARM_MAX_GOALS`/`_CHILD_TIMEOUT_SEC`/`_MAX_CONCURRENT`/`_MAX_DEPTH` | 8 / 120 / 4 / (swarm_depth) | config.go:237–239 | enforced |
| `AURA_RISK_ALERT_THRESHOLD` | (scoring default) | tools/task.go:30 | |
| `SEARXNG_URL`, `AURA_WEB_*` (6 knobs) | config.go:229–235 | via `web.NewClient` | fetch 30s / search 20s — good |

**Missing knobs that should exist:** `AURA_LLM_RETRY_MAX_ATTEMPTS`/`_BASE_MS`, `AURA_MCP_CALL_TIMEOUT_SEC`, `AURA_SHELL_MAX_TIMEOUT_MS`, `AURA_SHELL_OUTPUT_CAP_BYTES`, `AURA_SHELL_BG_BUF_CAP`/`_MAX`, `AURA_LOOP_MAX_PARALLEL_TOOLS`, `AURA_LOG_LEVEL`/`_FORMAT`, `AURA_SHUTDOWN_TURN_GRACE_SEC`, `AURA_RUN_DIR_SIDECAR_TTL_DAYS`, `AURA_REASONING_TRACE_MAX_MB`, `AURA_SHELL_PATH` (Git-Bash override).

## Reliability surfaces

| Surface | Status | Gap |
|---|---|---|
| LLM call timeout | ✅ `TotalTimeoutSec` (120s) per call | — |
| LLM connect timeout | ✅ 10s dialer | — |
| LLM retry (429/5xx) | ❌ **none** | `Retry-After` parsed and discarded; turn dies on first 429 (P1) |
| LLM circuit breaker | ❌ **none** | outage hammers a dead provider (P1) |
| MCP call timeout | ❌ **none** | hangs forever, holds mutex (P0) |
| MCP reconnect (stdio) | ✅ on transport error | but re-executes the call (at-most-once violated, P2) |
| MCP reconnect (HTTP) | ❌ **none** | session expiry bricks the server (P2) |
| Subprocess timeout | ⚠️ default 120s | model can override unbounded (P3); wallclock doesn't cancel it (P1) |
| Subprocess kill | ✅ process-group on both OSes | well done |
| Subprocess output cap | ❌ **unbounded RAM** | OOM (P1) |
| Parallel tool fan-out | ❌ **uncapped** | model-controlled width (P1) |
| Swarm caps | ✅ depth/goals/concurrent/timeout | well done |
| Backpressure | ✅ in workflow/parallel (ack-per-event) | absent in `executeBatch` |
| Graceful shutdown | ⚠️ scheduler/HTTP drain | conversational turns NOT drained (P2) |
| Wallclock cancellation | ❌ **dead code** | `WithDeadline`/`NodeTimeout` unwired (P1) |

## Resource management

- **Goroutine lifecycle:** excellent — SSE stream goroutine closes on ctx-cancel with drain-to-close at every consumer-stop site; swarm waves use errgroup + spawn-loop guard + per-child timeout; scheduler joins workers; goleak gates in test mains.
- **File handles:** clean (defer Close).
- **Buffers:** **unbounded** on shell sync + background output (P1); reasoningtrace JSONL unbounded (P2).
- **Disk:** `$AURA_RUN_DIR` sidecars + swarm transcripts grow monotonically for live conversations; GC only covers orphans + tmp, only at boot (P2).

## Dependency posture (`go.mod`)

Clean: all modules version-pinned, **no replace directives**, modern OTel v1.44, pgx v5.9.2, goleak in main reqs (test-only use). Two flags:
1. `gopkg.in/telebot.v4 v4.0.0-beta.9` — the **primary user-facing channel rides a beta**; pin-watch for v4 GA and re-run HITL live tests on bump.
2. `ag-ui-protocol/ag-ui/sdks/community/go` at a pseudo-version (commit pin of a community SDK).
3. Indirect `araddon/dateparse` (archived upstream, via readability) — `govulncheck` gates CVEs, so acceptable.

## Deployment assumptions

- compose runs **only sidecars** (postgres, neo4j, embed, searxng, STT/TTS/OCR, markitdown, agent-memory-mcp), each with a healthcheck, loopback-published, `restart: unless-stopped`.
- The **aura daemon runs on the host, unsupervised**: no service unit, no restart policy, no healthcheck (pairs with the missing `/healthz`).
- **No `.env.example` in the repo** — onboarding depends on the PRD env catalog. *(Note: the IDE shows `.env.example` open; confirm whether it is tracked — the infra auditor found it absent at audit time.)*
- `aura-ocr-vl` requires the nvidia runtime — `compose up` fails wholesale on a GPU-less host unless deselected.
- Neo4j heap caps sized for the mini-PC (1G max) — good.

## Concrete recommendations

**P0/P1 (Phase 0–1):**
1. Per-call MCP timeout + ctx-aware read (`AURA_MCP_CALL_TIMEOUT_SEC`).
2. Wire `Budget.WithDeadline` into `runner.buildAgent` + `cmd/aura/agent.go`; wrap tools with `NodeTimeout`; clamp `shell_exec timeout_ms`.
3. Provider retry/backoff honoring `Retry-After` + a minimal circuit breaker in `internal/llm`.
4. Bound `executeBatch` with `AURA_LOOP_MAX_PARALLEL_TOOLS` (default 4–8).
5. `GET /healthz` (pool Ping + scheduler last-tick) + `expvar`/Prometheus counters at `ConsumeStep`/`dispatch`/`streamWithOpenRetry`.

**P2 (Phase 1–2):**
6. `slog.SetDefault(JSONHandler, AURA_LOG_LEVEL)` in `cmd/aura/main.go`; Warn logs at loop terminal decisions with `request_id`+`thread_id`; `llm.Config.LogValue()` redactor.
7. Default `AURA_OTEL_EXPORTER=none` or warn-on-unreachable; ship a collector behind a compose profile; add tool/swarm spans.
8. Ring-cap shell buffers; reasoningtrace rotation; sidecar TTL sweep moved into the scheduler.
9. Bounded conversational-turn drain on SIGTERM (`AURA_SHUTDOWN_TURN_GRACE_SEC`).
10. Background-shell `Shutdown()` wired into serve teardown + concurrency cap.

**P3 (Phase 4–5):**
11. Ship `.env.example`; document a host supervisor (NSSM/Task Scheduler on Windows, systemd unit for WSL/Linux) for `aura serve`.
12. `net/http/pprof` on the loopback bind behind a flag.

## What is done well

- Budget design (TOCTOU-safe shared atomic, wallclock-first gate, per-branch dedup rings, injectable clock, fail-fast env parsing).
- Goroutine lifecycle discipline (drain-to-close, `DisableKeepAlives` for goleak determinism, errgroup swarm waves, scheduler worker-join).
- Graceful-shutdown *ordering* in `aura serve` (signal ctx → scheduler drain → channels StopAll → bounded HTTP shutdowns → reverse-close MCP + pool); `flushPause` on `WithoutCancel` is the right durability carve-out.
- At-most-once on side effects (mutating tools never retried; transient-by-type classification).
- Secret discipline at the wire (API key header-only, span attrs key-free, MCP stderr redaction, error bodies bounded at 64 KiB).
- `mcp.Client.Close` hard-deadline (the 13-minute-hang lesson — now needs applying to the call path, the P0).
- Scheduler correctness (SKIP LOCKED + advisory locks + heartbeats + missed-fire catch-up) — replica-tolerant by construction.
