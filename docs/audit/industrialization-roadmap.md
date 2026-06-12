# Industrialization Roadmap — Aura `internal/agent` (2026-06-12)

Step-by-step plan to take the agent runtime from strong-prototype to production-grade. The 2026-06-10/11 cycle closed both P0s and most P1s; this roadmap addresses what remains. Phases are ordered by risk-reduction-per-effort. Effort: S (≤1 day), M (2-4 days), L (1-2 weeks).

**Guiding principle (project doctrine "no atomic bombs / minimal industrial shape"):** the loop core is correct — do not rewrite it. Add the smallest layers that close the perimeter. Every item below is localized.

---

## Phase 0 — Stabilization (durability + trust + concurrency)

The three correctness/security holes that can lose data or breach the boundary today.

| ID | Task | Effort | Impact | Depends on | Acceptance |
|---|---|---|---|---|---|
| B-01 | Write-ahead tool-intent row in the same tx as the assistant tool-call turn; recovery surfaces unmatched intents as "verify before re-running" instead of dropping | M | **High** — stops duplicated mutating side effects on crash | — | Kill-before-result-commit integration test: reload does NOT silently drop the mutating call |
| B-02 | Set `Provenance{Trust:Untrusted}` on swarm child reports | S | High — closes the trust-boundary hole | — | A swarm result with `</tool_output>` bytes returns HTML-escaped inside an untrusted envelope in the parent history |
| B-03 | Per-thread in-flight guard in `Runner` (shared by AG-UI + Telegram) | M | High — stops conversation corruption | — | Concurrent runs on one thread → 409 or provable serialization; race test green |
| B-04 | Fix the `skill` tool schema + comments to the true auto-activate policy; fire the alert/audit row on the ungated path | S | High — honest contract + restored operator signal | — | Schema-snapshot test matches live gating; an audit row is emitted on ungated auto-activate |

**Phase 0 exit:** no path can duplicate a mutating side effect on crash, launder untrusted content past the envelope, corrupt a conversation under concurrency, or self-extend silently with a lying contract. ~1.5 engineer-weeks.

---

## Phase 1 — Observability & reliability (make it operable)

The system is currently un-runnable in production because you can't see it.

| ID | Task | Effort | Impact | Depends on | Acceptance |
|---|---|---|---|---|---|
| O-01a | Shared `obs.Init(cfg)` called by both `serve` and `chat`: boot the tracer + install JSON `slog` default with `service`/`version` + `request_id`/`thread_id` correlation | S/M | High | — | A turn under `serve` produces a span (stdout exporter capture) and correlated JSON logs |
| O-02 | Prometheus `/metrics`: turn/tool/LLM latency histograms, error + SSE-drop counters, in-flight + SSE gauges, cost | M | High | O-01a | `/metrics` returns Prometheus text; a turn moves the histograms/counters |
| O-03 | Replace the process-global no-op otel error handler with a rate-limited logging handler (or scope to Shutdown) | S | Medium | O-01a | An injected export error is observable, not silently dropped |
| B-05 | Hoist the circuit breaker to a process-lifetime `Runner` singleton, injected into each `LlmAgent` | S | Medium | — | Two consecutive turns vs a 503 client → second short-circuited by `ErrBreakerOpen`, no network call |
| B-06 | Route breaker-open to `finalize(reason="breaker_open")` instead of the error slot | S | Medium | B-05 | Pre-opened breaker → non-empty terminal Event, no error slot |
| B-08 | Stream idle-timeout watchdog (`AURA_LLM_STREAM_IDLE_TIMEOUT_SEC`) | M | Medium | — | A fake client that opens then stalls → turn aborts within the idle window, not the total window |
| O-05 | Extend `/healthz` (Neo4j + embed probe) + add `/readyz` | S | Medium | — | `/healthz` 503 with Neo4j down; `/readyz` distinct from `/healthz` |
| O-08 | `agent.turn` root span + `tool.execute` per-call spans | S | Medium | O-01a | Span-tree: one turn span parents N `llm.request` + M `tool.execute` |

**Phase 1 exit:** production emits correlated logs, latency/error/cost metrics, distributed traces, and real health — a recurrence of the P0 MCP-hang class is detectable and alertable. ~2 engineer-weeks.

---

## Phase 2 — Architecture hardening (context + state lifecycle)

| ID | Task | Effort | Impact | Depends on | Acceptance |
|---|---|---|---|---|---|
| M-01 | L1 microcompact: only pointer-rewrite `RoleTool` turns that actually have a sidecar; leave `ask_user`/small results inline | M | High — stops silent answer loss | — | An `ask_user` answer older than `evictAfter` survives verbatim |
| M-02 | One-transaction `SubmitAnswer` (inject + mark-resumed in one tx, `RowsAffected==0` guard) | M | Medium | — | A resume retry yields exactly one answer turn + `ErrPauseNotFound` |
| M-03 | Treat `hardCap<=0` as a config error / per-model floor, not "protection off" | S | Medium | — | Small-window over-cap history → error/compaction, never raw unprotected history |
| M-05 | Never drop the newest round in `dropOldestPairs` | S | Low | — | Newest oversized user turn survives or an error is returned |
| B-07 | `LoopAgent` maxIter=0 + budget-owner: require >0 or break on no-progress | S | Low (latent) | — | `NewLoop("t",0,budgetOwningStub)` terminates in bounded Events |
| B-11 | Pre-emptively split `shell_exec.go` (598/600) | S | Maintainability | — | File-size gate green; no file >600 LOC |
| B-09 | One shared `secret.IsSecretEnvKey` across the 3 sites | S | Medium (security hygiene) | — | `PRIVATE_KEY` redacts identically in shell + MCP |

**Phase 2 exit:** context compaction is lossless for HITL answers and small results; resume is atomic; small-window models are protected; no god class. ~1.5 engineer-weeks.

---

## Phase 3 — Security hardening

| ID | Task | Effort | Impact | Depends on | Acceptance |
|---|---|---|---|---|---|
| O-04 | `Config.Validate()` boot fail-fast + secret-redacting log `ReplaceAttr` | S/M | Medium | O-01a | Empty `NEO4J_PASSWORD` → non-zero exit; a DSN-bearing log line is sanitized |
| B-10 | Document the destructive gate as advisory; ship a conservative default pattern set; (optional) intent-layer gate | M | Medium | — | Gate on by default; docs state it is not a sandbox |
| B-15 | Cap+frame bridged MCP argument-schema descriptions (or document trusted-by-mount) | S | Low | — | An injection-laden property description is framed/capped in the loaded spec |
| B-16 | Node/deadline budget on `fs_grep`/`fs_glob` walks | S | Low | — | A deep tree with no match aborts at the node cap |

---

## Phase 4 — Scalability & production operations

| ID | Task | Effort | Impact | Depends on | Acceptance |
|---|---|---|---|---|---|
| D-01 | Production `Dockerfile` (non-root, distroless) + hardened `aura` compose service (`read_only`, `cap_drop`, `mem_limit`, healthcheck) + sidecar limits | M | High | O-05 | Image runs non-root; container memory-bounded; healthcheck green |
| O-06 | Bounded in-flight-turn drain on SIGTERM | M | Medium | — | A turn during SIGTERM reaches a terminal frame within the grace window |
| O-07 | `windows-latest` CI lane (build + vet + tools units) | S | Medium | — | The Windows kill-path runs in CI |
| M-06 | Periodic sidecar/reasoningtrace TTL sweep + rotation; stop logging full history per turn | M | Medium | — | reasoningtrace rotates at cap; archived-conversation sidecars reclaimed |
| R-41 | `Evict(sessionID)` on session-scoped tools, called from `ConversationCleaner` | S | Medium | — | Deleting N conversations frees their `todo`/`shell_bg` entries |

---

## Phase 5 — Long-term maintainability & test apex

| ID | Task | Effort | Impact | Depends on | Acceptance |
|---|---|---|---|---|---|
| T-01 | Fuzz (tool-args, MCP framing) + bench (budget/dedup hot path) + recorded mutation score for the agent core | M | Medium | — | Fuzz/bench in CI; mutation ≥70% documented for `budget*.go`, `llm_agent_completion.go` |
| — | Chaos tests: provider-500 storm, crash mid-turn, MCP hang, SIGTERM drain | M | Medium | B-01, B-08 | The B-01/M-02/M-03 regression tests in §4 of the testing strategy exist and pass |
| M-07 | `anyInt` `json.Number` case (kill the dormant token-zeroing) | S | Low | — | `anyInt(json.Number("100"))==100`; final-Event round-trip recovers tokens |
| T-04 | Exclude `agenttest/` from the coverage-gate denominator | S | Low | — | Owned-surface floor reflects real surface |
| O-08+ | State-machine turn tracer (BUDGET_GATE→…→FINALIZE, per-state duration) — adopt the nanobot pattern | L | Medium | O-08 | Per-phase latency visible in traces |

---

## Sequencing summary

Phase 0 (durability/trust/concurrency) and Phase 1 (observability/reliability) are the go-live gate — together ~3.5 engineer-weeks — and re-score the system to **7.5-8**. Phases 2-5 are the path to a durable industrial product and can proceed in parallel by area once Phase 0/1 land. Nothing here touches the verified-correct loop core.
