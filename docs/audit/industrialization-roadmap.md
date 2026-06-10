# Industrialization Roadmap — Aura `internal/agent`

Step-by-step plan to take the agent runtime from strong-prototype to production-grade. Phases are ordered by risk-reduction-per-effort; later phases depend on earlier ones only where noted. Effort uses S (≤1 day), M (2–4 days), L (1–2 weeks).

**Guiding principle (project doctrine "no atomic bombs / minimal industrial shape"):** do not rewrite the loop core — it is correct. Add the smallest layers that close the perimeter.

---

## Phase 0 — Stabilization (stop the bleeding)

Goal: eliminate the data-corruption, system-hang, and accidental-OOM classes. All fixes are localized.

| # | Item | Priority | Effort | Impact | Dependencies | Acceptance criteria |
|---|---|---|---|---|---|---|
| 0.1 | Reject empty `old_string` in `fs_edit` | P1 | S | Stops host-file corruption | none | `TestFsEdit_EmptyOldStringRejected` passes; empty `old_string` returns an error and leaves the file unchanged |
| 0.2 | Per-call MCP timeout + ctx-aware `readResponse` | P0 | M | Stops daemon-wide hang + shutdown deadlock | none | `TestMCPCall_TimesOutAndPoisonsTransport` (fake hung server) returns within `AURA_MCP_CALL_TIMEOUT_SEC`; a second server's tools unaffected; `Close()` returns within its deadline; goleak-clean |
| 0.3 | Wire `Budget.WithDeadline` into runner + `cmd/aura/agent.go`; wrap tools with `NodeTimeout`; clamp `shell_exec timeout_ms` | P1 | M | Makes the 300s wallclock a real cancel | none | a tool that sleeps past the wallclock is cancelled at the deadline; a model `timeout_ms` above `AURA_SHELL_MAX_TIMEOUT_MS` is clamped |
| 0.4 | Ring/tail-cap shell output (sync + background); free background buffers | P1 | M | Stops accidental OOM on the shared host | none | a >cap command yields a bounded preview + `[output truncated]`; background `buf` length stays bounded across many polls; process RSS bounded |
| 0.5 | Filter the child env (strip secret-shaped vars) for `shell_exec` + MCP launch | P1 | M | Stops ambient secret broadcast | none | `mergeEnv` excludes a planted `FAKE_API_KEY`; the model's explicit `env` arg can re-add a name |
| 0.6 | Remove `approve` from the model-facing `task` enum | P1 | S | Closes self-approval bypass | none | a model `task approve` call is rejected/converts to a pause; only CLI + resume release a pending_approval task |
| 0.7 | Remove `/internal/agent/tools/` from the coverage-gate exclusion | P1 | S | Re-enables the floor on the riskiest package | none | `make coverage` runs with `agent/tools` in the profile and still passes |
| 0.8 | `GET /healthz` + basic counters (`expvar`) | P1 | M | Minimum operability | none | `/healthz` returns 503 when the pool ping fails, 200 otherwise; counters increment at `ConsumeStep`/`dispatch`/`stream` |

**Exit criteria for Phase 0:** all P0s closed; the four data-harm P1s (0.1, 0.4, 0.5, plus 0.3 making 0.2 enforceable) closed; CI floor covers the tool package; the daemon is health-probeable. Re-score target: **7.0**.

---

## Phase 1 — Observability and reliability

Goal: the system survives provider blips and is debuggable in production.

| # | Item | Priority | Effort | Impact | Dependencies | Acceptance criteria |
|---|---|---|---|---|---|---|
| 1.1 | Provider retry/backoff honoring `Retry-After` + minimal circuit breaker (`internal/llm`) | P1 | M | Survives 429/5xx; protects a degraded provider | none | table test: 429-with-Retry-After retries with sleep, 503 retries with jittered backoff, 400 does not retry; breaker opens after N consecutive failures |
| 1.2 | Cap parallel tool fan-out (`AURA_LOOP_MAX_PARALLEL_TOOLS`) | P1 | S | Bounds host load | none | N>cap tool calls run ≤cap concurrently (barrier test) |
| 1.3 | Structured slog: `JSONHandler` + `AURA_LOG_LEVEL`; Warn logs at loop terminal decisions with `request_id`+`thread_id`; `llm.Config.LogValue()` redactor | P2 | M | Production debuggability + log hygiene | none | a budget trip / dedup veto / finalize-stub emits a correlated log line; `%+v` of `llm.Config` does not leak the key |
| 1.4 | Surface finalize/critic mid-stream errors (`reasoningtrace.Record`/slog) | P2 | S | Diagnosability of the empty-answer mode | 1.3 | a forced-double-failure synthesis logs both errors before the stub |
| 1.5 | Per-event intra-turn persistence (journal assistant-tool_call + RoleTool turns) | P1 | L | Eliminates resume amnesia + duplicate side effects | 0.3 | `TestRunner_IntraTurnPersistedAcrossResume`: resume rehydrates prior tool rounds; no duplicate mutating dispatch |
| 1.6 | One-transaction pause writes (paused_states + assistant turn) + load-time orphan-pair repair | P1 | M | Closes conversation-bricking | none | crash-window simulation does not produce a 400-bound history; load repairs/refuses orphan pairs |
| 1.7 | Mid-stream LLM error → bounded re-issue from the loop | P1 | M | Survives transient SSE cuts on long runs | 1.1 | a fake client emitting text-then-error mid-stream is re-issued once and the run survives |

**Exit criteria:** the runtime tolerates provider failures and process restarts without data loss or duplicated side effects; every terminal decision is logged and correlated. Re-score target: **8.0**.

---

## Phase 2 — Architecture hardening

Goal: make the composition and persistence contracts explicit and safe.

| # | Item | Priority | Effort | Impact | Dependencies | Acceptance criteria |
|---|---|---|---|---|---|---|
| 2.1 | Define a single budget owner per agent tree; fix LoopAgent×LlmAgent double-spend | P1 | M | Correct composed budgets | none | composing LoopAgent over a budget-aware sub consumes steps once; documented on `Agent.Run` |
| 2.2 | LoopAgent ctx check + per-iteration budget (no hot-spin on non-tool subs) | P1 | S | No CPU-spin wedge | 2.1 | `maxIter=0` over a chat-only sub terminates on ctx cancel and wallclock |
| 2.3 | Duplicate-`text_response` handling (synthetic results for extras) | P2 | S | No dangling-tool_call 400 | none | a turn with two `text_response` calls produces a wire-valid next request |
| 2.4 | Fold the duplicated `truncateTailBytes`; property-test once | P2 | S | Reusable-code compliance | none | one helper, rapid test green |
| 2.5 | Stamp Timestamp/ThreadID on workflow terminal Events | P2 | S | Correct event ordering/retention | none | `terminalEvent` carries a non-zero Timestamp |
| 2.6 | `anyInt` accepts `json.Number`; per-thread AG-UI in-flight guard | P3 | S | Latent-bug + concurrency closure | none | token aggregates survive a decode boundary; concurrent same-thread runs serialize |

**Exit criteria:** no undefined composition behavior; the event model is consistent across layers. Re-score target: **8.3**.

---

## Phase 3 — Security hardening

Goal: close the prompt-injection blast radius.

| # | Item | Priority | Effort | Impact | Dependencies | Acceptance criteria |
|---|---|---|---|---|---|---|
| 3.1 | Provenance-tag untrusted tool output + control-token neutralization | P0 | L | Closes the keystone injection class | none | results from web/MCP/fs_read/shell are wrapped in a non-spoofable envelope before history; a forged `</assistant>` token is neutralized; system prompt frames envelope content as data |
| 3.2 | Gate `always:true` model skill creation; fix stale schema/comment; fire `Alerter` on auto-activation | P1 | M | Closes persistent-backdoor vector | none | a model `always:true` create is gated/alerted, not silently activated |
| 3.3 | Fence `send_file` to the workspace; approval for outside paths | P2 | M | Closes arbitrary-file exfil | none | `send_file` outside the workspace requires an `ask_user` approval |
| 3.4 | Destructive-shell-pattern gate behind `ask_user` (config-toggle) | P2 | M | Backstop on destructive actions under injection | 3.1 | a configured destructive pattern forces an approval pause |
| 3.5 | Default bridged MCP tools `Mutating` (honor `readOnlyHint`); conditional reconnect (no replay); provenance-frame + length-cap MCP descriptions | P2 | M | Closes MCP trust gaps | 3.1 | write-capable MCP tools arm the critic and are never auto-replayed; descriptions are framed + capped |
| 3.6 | Redact the model-facing tool preview; shape-based reasoningtrace redaction; sidecar `0o600` + allowlist id grammar | P2/P3 | M | Secret-leak defense-in-depth | 0.5 | a secret-named/secret-shaped value in tool output is redacted before reaching the provider |
| 3.7 | Blocking write-ahead audit row for mutating tools (or document observability-only) | P2 | M | Non-repudiation (if required) | none | a mutating dispatch refuses (or is explicitly documented to proceed) when the intent row can't persist |

**Exit criteria:** a prompt-injection payload from any ingress cannot reach host RCE, secret exfiltration, or persistent self-modification without an enforced gate. Re-score target: **8.7**.

---

## Phase 4 — Scalability and production operations

Goal: run unattended, observably, with bounded resources.

| # | Item | Priority | Effort | Impact | Dependencies | Acceptance criteria |
|---|---|---|---|---|---|---|
| 4.1 | Prometheus metrics (turns, tool dispatch/error/retry, budget trips, pause/resume, MCP reconnects, LLM latency/tokens/cost) | P1 | M | Real telemetry | 0.8 | `/metrics` exposes the named counters/histograms; a dashboard renders them |
| 4.2 | OTel: default exporter honesty + tool/swarm spans + a collector behind a compose profile | P2 | M | Usable tracing | 1.3 | spans land in a collector when enabled; default deployment doesn't silently drop |
| 4.3 | Bounded conversational-turn drain on SIGTERM | P2 | M | No dropped in-flight turns on restart | 1.5 | a turn in flight at SIGTERM completes or persists an "interrupted" marker within `AURA_SHUTDOWN_TURN_GRACE_SEC` |
| 4.4 | Background-shell `Shutdown()` + concurrency cap | P2 | S | No orphaned host processes | 0.4 | serve teardown kills all background jobs; `AURA_SHELL_BG_MAX` enforced |
| 4.5 | Disk lifecycle: sidecar TTL sweep in the scheduler; reasoningtrace rotation | P2 | M | Bounded disk | none | `$AURA_RUN_DIR` stops growing monotonically for live conversations |
| 4.6 | Ship `.env.example`; document a host supervisor (systemd/NSSM) + restart policy | P3 | S | Reproducible deploy | none | a fresh host boots `aura serve` under supervision from documented steps |
| 4.7 | `net/http/pprof` on loopback behind a flag | P3 | S | Production profiling | none | pprof reachable when enabled |

**Exit criteria:** the daemon runs unattended under a supervisor, exports metrics + health, drains gracefully, and has bounded disk/memory. Re-score target: **9.0**.

---

## Phase 5 — Long-term maintainability

Goal: lock in quality and adopt the best reference patterns.

| # | Item | Priority | Effort | Impact | Dependencies | Acceptance criteria |
|---|---|---|---|---|---|---|
| 5.1 | Add the ~18 tests from `testing-strategy.md`; Windows CI lane; fuzz the args boundary | P2 | L | Regression safety | most fixes | new tests in CI; Windows lane green; fuzz corpus runs in unit job |
| 5.2 | Mutation scores for the loop core (`llm_agent*.go`, `shell_exec.go`, `bridge_reconnect.go`) ≥70% | P2 | M | Gate-3 completeness | 5.1 | scores recorded in `docs/aura-quality-snapshot.md` |
| 5.3 | Adopt codex's per-tool parallel-safety gate (mutating ⇒ exclusive) | P2 | S | Closes concurrent-mutation hazard | 1.2 | two mutating tools in a batch serialize; reads stay concurrent |
| 5.4 | Adopt adk-go reflect-and-retry: per-tool failure counter + "stop using this tool" injection | P2 | M | No budget burned on a dead tool | 1.1 | after N failures of a tool, the model is told to route around it |
| 5.5 | Stream idle-timeout watchdog (config, not string-match); typed transient-error classification | P2 | M | Reliability + cross-platform correctness | 1.1 | an idle stream is detected by deadline, not error text |
| 5.6 | Cleanups: dead `renderShellOutput`, `_ = requestID`, fresh-per-turn guard, `Registry.Register` duplicate guard | P3 | S | Hygiene | none | dead code removed; duplicate registration fails loud |

**Exit criteria:** the package has a complete test pyramid, documented mutation scores, cross-platform CI, and has absorbed the high-value reference patterns. Re-score target: **9.3+**.

---

## Dependency graph (critical path)

```
0.2 (MCP timeout) ─┐
0.3 (WithDeadline) ┼─▶ enables real cancellation
0.1 0.4 0.5 0.6 0.7 0.8  (independent Phase-0 fixes)
        │
        ▼
1.1 (retry/breaker) ─▶ 1.7 (mid-stream re-issue)
1.5 (intra-turn persist) ─▶ 4.3 (turn drain)
        │
        ▼
3.1 (provenance) ─▶ 3.4 (destructive gate), 3.5 (MCP trust)
```

Phases 0–1 are the gate to "production-safe enough to pilot." Phase 3.1 (provenance tagging) is the single highest-leverage security item and should start in parallel with Phase 1 if a second engineer is available.
