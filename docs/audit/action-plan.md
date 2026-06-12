# Action Plan — Aura `internal/agent` (2026-06-12)

Concrete engineering backlog, by priority. Each task: title · description · owner role · expected outcome · acceptance criteria. Cross-references [`bug-report.md`](bug-report.md).

> **Status note.** The 2026-06-10/11 cycle closed both P0s and most P1s (see the bug-report appendix). This plan covers the open P1/P2/P3 set found in the 2026-06-12 re-audit, including two over-credited prior closures (B-01⊃R-05, B-04⊃R-09).

---

## Immediate (P1 — go-live blockers)

### AP-1 · Close the crash re-execution hazard (B-01)
- **Description:** Insert a write-ahead tool-intent row (`tool_call_id` + canonical args) inside the *same* transaction that persists the assistant `tool_calls` turn, *before* `executeBatch`. On reload, surface an unmatched intent as a synthetic `RoleTool` ("previous result unknown — verify before re-running") instead of dropping the dangling group. Add idempotency tokens to mutating tools where the underlying op supports it.
- **Owner:** Backend / runtime engineer.
- **Expected outcome:** A crash between tool execution and result persistence never causes the model to blindly re-issue the mutating call.
- **Acceptance:** Integration test — persist the tool-call turn, kill before the result commit, reload; assert the repaired history contains a recovery marker, not a silent drop, and the model does not re-execute.

### AP-2 · Wrap swarm child reports in the untrusted envelope (B-02)
- **Description:** Set `res.Provenance = &tools.ToolResultProvenance{Source:"swarm", Trust:tools.TrustUntrusted}` in `internal/swarm/runner_adapter.go` (or add `swarm_spawn` to `untrustedToolNames`).
- **Owner:** Security / runtime engineer.
- **Expected outcome:** Swarm-laundered content can no longer reach the parent prompt as trusted text.
- **Acceptance:** A swarm result containing `</tool_output>` bytes returns HTML-escaped inside an untrusted envelope in the parent history.

### AP-3 · Per-thread in-flight guard (B-03)
- **Description:** Add a `sync.Map[threadID]*sync.Mutex` (or singleflight) to `Runner`, shared by AG-UI and Telegram; reject a second concurrent run on the same thread with `409 Conflict`, or serialize.
- **Owner:** Backend engineer.
- **Expected outcome:** Concurrent runs on one conversation can no longer interleave history appends.
- **Acceptance:** Concurrent `handleRun` on one thread → second returns 409 (or serializes provably); race-detector test on interleaved turns is green.

### AP-4 · Make the self-extension contract honest + restore the alert (B-04)
- **Description:** Update the `skill` tool schema `description` + doc comments to state the true auto-activate policy (in-box create/update auto-activate; `always:true` and delete differ); fire the `Alerter`/audit row on the ungated path; decide whether unattended `delete` is intended.
- **Owner:** Security / runtime engineer.
- **Expected outcome:** The model and operators are no longer misled; self-extension is auditable.
- **Acceptance:** Schema-snapshot test matches live gating; an audit/alert row is emitted on ungated model auto-activate.

### AP-5 · Boot the tracer in `serve` + JSON structured logging (O-01)
- **Description:** A shared `obs.Init(cfg)` called by both `serve` and `chat`: boot the OTel tracer (deferred bounded `Shutdown`) and install `slog.SetDefault(JSONHandler)` with base `service`/`version` and `request_id`/`thread_id` correlation threaded into the loop's WARN/ERROR chokepoints.
- **Owner:** Platform / SRE engineer.
- **Expected outcome:** Production turns are traced and emit correlated machine-parseable logs.
- **Acceptance:** A turn under `serve` produces a span (stdout-exporter capture) and JSON logs carrying `request_id`/`thread_id`.

### AP-6 · Prometheus `/metrics` (O-02)
- **Description:** Adopt `prometheus/client_golang`; add turn/tool/LLM latency histograms, error + SSE-drop counters, in-flight + SSE-connection gauges, and a token/cost counter; mount `promhttp.Handler()` at `GET /metrics`.
- **Owner:** Platform / SRE engineer.
- **Expected outcome:** SLO dashboards + alerting on latency/error/cost.
- **Acceptance:** `/metrics` returns Prometheus text; a turn moves the histograms and error counters.

### AP-7 · Production container + hardened compose (D-01)
- **Description:** Multi-stage distroless/alpine `Dockerfile` for `aura` (non-root `USER`); an `aura` compose service with `read_only`, `cap_drop:[ALL]` (+ minimal add-back), `mem_limit`/`cpus`, and a `/healthz` healthcheck; add `cap_drop`/`mem_limit` to sidecars.
- **Owner:** Platform / DevOps engineer.
- **Expected outcome:** The privileged agent runs isolated and resource-bounded.
- **Acceptance:** Image builds in CI, runs non-root, is memory-capped; healthcheck green.

---

## Short-term (P2 — pre-scale)

### AP-8 · Fix L1 microcompact answer destruction (M-01)
Only pointer-rewrite `RoleTool` turns with a real sidecar; leave `ask_user`/small results inline. **Owner:** runtime. **Acceptance:** an `ask_user` answer older than `evictAfter` survives verbatim.

### AP-9 · One-transaction `SubmitAnswer` (M-02)
Inject + mark-resumed in one `db.WithTx` with a `RowsAffected==0` no-op guard. **Owner:** backend. **Acceptance:** a resume retry → exactly one answer turn + `ErrPauseNotFound`.

### AP-10 · `hardCap<=0` is a config error, not "protection off" (M-03)
Return `ErrContextWindowExceeded` (or a per-model floor) instead of skipping L2.5. **Owner:** runtime. **Acceptance:** small-window over-cap history → error/compaction, never raw history.

### AP-11 · Hoist the breaker + graceful breaker-open (B-05, B-06)
Process-lifetime breaker on `Runner`; route open to `finalize`. **Owner:** runtime. **Acceptance:** second turn vs 503 short-circuits; pre-open → non-empty terminal Event.

### AP-12 · Stream idle-timeout watchdog (B-08)
Per-chunk idle deadline (`AURA_LLM_STREAM_IDLE_TIMEOUT_SEC`) treated as retryable transport error. **Owner:** llm-client. **Acceptance:** an open-then-stall client aborts within the idle window.

### AP-13 · Unify `secretEnvKey` (B-09)
One `secret.IsSecretEnvKey` (with `"key"`) across the 3 sites. **Owner:** security. **Acceptance:** `PRIVATE_KEY` redacts identically in shell + MCP.

### AP-14 · `/healthz` dep probes + `/readyz` (O-05); `Config.Validate()` + log redaction (O-04)
**Owner:** SRE. **Acceptance:** `/healthz` 503 with Neo4j down; empty required secret → non-zero boot exit; DSN-bearing log sanitized.

### AP-15 · Split `shell_exec.go` (B-11); destructive-gate docs + defaults (B-10)
**Owner:** runtime. **Acceptance:** no file >600 LOC; destructive gate on by default + documented as advisory.

### AP-16 · Lifecycle hygiene (M-06, R-41, M-04)
Periodic sidecar/reasoningtrace TTL sweep + rotation; `Evict(sessionID)` on session-scoped tools; spill-inside-tx (or sweep reconciliation). **Owner:** runtime/SRE. **Acceptance:** reasoningtrace rotates; archived sidecars + dead session state reclaimed.

### AP-17 · Windows CI lane (O-07); SIGTERM turn drain (O-06)
**Owner:** DevOps. **Acceptance:** Windows kill-path runs in CI; a turn during SIGTERM reaches a terminal frame within the grace window.

---

## Medium-term (architecture)

### AP-18 · Provenance-by-default tool boundary
Make `Trust=Untrusted` the default for any externally-sourced `ToolResult`; the prompt builder wraps on `Provenance.Trust`, not a tool-name allowlist. Structurally subsumes B-02 and prevents the next ingress from forgetting. **Owner:** architecture. **Acceptance:** adding a new external tool with no provenance still gets wrapped.

### AP-19 · Write-ahead intent log as the audit gate
Generalize AP-1's intent row into the pre-execution audit gate, upgrading the best-effort ledger (R-26). **Owner:** architecture. **Acceptance:** every mutating call has a durable pre-execution intent row.

### AP-20 · State-machine turn tracer (adopt nanobot pattern)
BUDGET_GATE→LLM_CALL→CONSUME→DISPATCH→FINALIZE with per-state `duration_ms` on the Event + trace. **Owner:** runtime/observability. **Acceptance:** per-phase latency visible in a trace tree.

---

## Long-term (industrialization)

### AP-21 · Test apex (T-01 + chaos)
Fuzz (tool-args, MCP framing), bench (budget/dedup), recorded mutation score for the agent core, and the chaos/regression suite from the testing strategy §4. **Owner:** QA/runtime. **Acceptance:** fuzz+bench in CI; mutation ≥70% documented; B-01/M-01/M-02/M-03/B-03/B-05 regression tests exist and pass.

### AP-22 · Dependency GA tracking (R-40), `anyInt` json.Number (M-07), coverage hygiene (T-04)
Low-effort cleanups. **Owner:** maintenance. **Acceptance:** telebot GA tracked; `anyInt(json.Number)` handled; `agenttest` out of the floor denominator.

---

## Priority ladder (one screen)

1. **AP-1 → AP-7** (P1 go-live blockers): crash re-execution, swarm envelope, in-flight guard, skill contract, tracer+logs, metrics, container.
2. **AP-8 → AP-17** (P2 pre-scale): context/resume correctness, breaker, idle watchdog, secret unify, health/config, lifecycle, Windows CI.
3. **AP-18 → AP-20** (architecture): provenance-by-default, intent-log audit gate, state-machine tracer.
4. **AP-21 → AP-22** (industrialization): test apex, cleanups.
