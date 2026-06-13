# Risk Register — Aura `internal/agent`

Probability/Impact: H (high) / M (medium) / L (low). Status: OPEN (unmitigated), PARTIAL (some defense exists, acceptance not fully met), TRACKED (accepted, decision pending), CLOSED (verified fixed + test).

> **Status verified 2026-06-13** against the `tabula-rasa` working tree — see [`reconciliation-2026-06-13.md`](reconciliation-2026-06-13.md) for method + evidence. The 2026-06-12 table below was written before commit `ec7fe2f6 "fix(audit): close P1 items"`; the **Status** column now reflects re-verification, not the morning snapshot.

**History.** 2026-06-10 closed R-01..R-17 (P0/P1 pass). 2026-06-11 closed R-18..R-25, R-35, R-43 (P2 boundary pass). The 2026-06-12 re-audit re-opened two over-credited closures (R-05→B-01, R-09→B-04) and added `B-/O-/D-/M-` findings. **2026-06-12 PM `ec7fe2f6` closed the entire P1 gate** (B-01–B-04, O-01, O-02, D-01), each with a passing acceptance test; 2026-06-13 verification confirms it and re-checks every P2/P3.

## Active risks (Status as of 2026-06-13)

| ID | Title | Sev | Prob | Impact | Area | Mitigation | Status |
|---|---|---|---|---|---|---|---|
| B-01 | Mutating tool side effects re-execute after intra-turn crash (R-05 was over-credited) | P1 | M | H | dispatch ↔ persistence ordering | Write-ahead intent row + recovery marker (AP-1) | **CLOSED** |
| B-02 | Swarm child reports re-enter parent prompt with no untrusted envelope | P1 | M | H | `swarm/runner_adapter.go`, `trust.go` | Set `Provenance{Trust:Untrusted}` (AP-2) | **CLOSED** |
| B-03 | No per-thread in-flight guard → conversation corruption + budget double-spend | P1 | M | H | `agui/server.go`, `runner.go` | Per-thread guard in Runner (AP-3) | **CLOSED** |
| B-04 | Self-extension gate open for `always:false` + lying schema + lost alert (R-09 regressed by P5) | P1 | M | H | `skills/writer.go`, `tools/skill.go` | Honest schema + restore alert (AP-4) | **CLOSED** |
| O-01 | `serve` never boots tracer + zero structured logging → prod blind | P1 | H | M | `serve.go`, agent core | `obs.Init` tracer + JSON slog (AP-5) | **CLOSED** |
| O-02 | No latency/error/cost metrics, no Prometheus | P1 | H | M | `metrics.go` | Prometheus `/metrics` (AP-6) | **CLOSED** |
| D-01 | No production container for the privileged agent binary | P1 | M | H | repo root, `compose.yaml` | Dockerfile + hardened compose (AP-7) | **CLOSED** |
| M-01 | L1 microcompact destroys `ask_user` answers → dead pointer (R-28) | P2 | M | M | `conversations/context.go` (applyL1) | Pointer-rewrite only sidecar-backed turns (AP-8) | **CLOSED** |
| M-02 | `SubmitAnswer` non-atomic → duplicate resume bricks the round (R-27) | P2 | L | H | `runner_resume.go:77` | Gate-first reorder: MarkResumed before inject (full single-tx deferred) | **CLOSED** |
| M-03 | `hardCap<=0` silently disables all context protection (R-29) | P2 | L | H | `context.go` (hardCap gate) | Nanobot-style floor: a non-positive SPEC formula clamps to a positive `ContextWindow/2` so L2.5 stays active (only `ContextWindow<=0` yields 0) | **CLOSED** |
| B-05 | Circuit breaker per-turn → no cross-turn protection | P2 | M | M | `llm_agent.go:131` | Hoist to Runner singleton (AP-11) | **CLOSED** |
| B-06 | Breaker-open routes to error slot, not finalize | P2 | M | L | `llm_agent.go` (streamWithOpenRetry) | Route to finalize (AP-11) | **CLOSED** |
| B-07 | `LoopAgent` maxIter=0 hot-spins on a budget-owning sub (latent, off prod path) | P2 | L | H | `workflow/loop.go` | No-progress guard | **CLOSED** |
| O-03 | otel global no-op error handler silences all prod otel errors (R-31) | P2 | H | L | `tracing.go:63` | Rate-limited slog handler in `obs.Init` (AP-5): logs each OTel error WARN, ≤1/window (60s default), next emission carries the suppressed count; covers every exporter mode (no-op deleted from the otlp branch) | **CLOSED** |
| O-04 | No boot secret validation; no secret-redacting log handler (R-32 adj.) | P2 | M | M | `config.go`, `obs/init.go` | `Config.Validate()` + redact `ReplaceAttr` (AP-14) | **CLOSED** |
| O-05 | `/healthz` PG-only; no Neo4j/provider readiness; no `/readyz` (R-14 residual) | P2 | M | M | `serve.go`, `agui/server.go` | `/readyz` readiness endpoint over an injectable `ReadinessProbe` seam; serve wires PG-pool `Ping` + native-driver Neo4j `VerifyConnectivity` (`knowledge.VerifyConnectivity`, bounded 3s/probe); 503 (naming the failed dep, sanitized) when any required backend is unreachable; `/healthz` stays a cheap PG-only liveness (AP-14) | **CLOSED** |
| B-08 | No stream idle-timeout watchdog (stall bounded only by total timeout) | P2 | M | M | `internal/llm`, `llm_agent.go` | Per-chunk idle watchdog (AP-12) | **CLOSED** |
| B-09 | Divergent `secretEnvKey`; shell leaks bare `*_KEY` (R-07 divergence) | P2 | M | M | `shell_exec_env.go:22`, `mcp/client.go:164` | One shared `secret.IsSecretEnvKey` (AP-13): canonical superset-marker predicate in new `internal/secret/envkey.go` now called by all 3 sites (shell child-env, MCP child-env, MCP managed-config export); the 3 divergent local copies deleted; `PRIVATE_KEY` (and every `*_KEY`) now redacts identically on shell + MCP — RED→GREEN cross-site acceptance tests | **CLOSED** |
| B-10 | Destructive-shell gate regex-bypassable + off by default (R-19 residual) | P2 | M | M | `shell_exec_env.go` | Document advisory + default patterns (AP-15) | **CLOSED** |
| B-11 | `shell_exec.go` god-class risk | P2 | H | L | `tools/shell_exec.go` | Pre-emptive split (AP-15) | CLOSED* |
| M-04 | Sidecar spill outside tx → orphan-on-rollback unreclaimed in live conv | P2 | M | L | `conversations/store.go` | Cleanup-on-rollback: best-effort `os.Remove` the just-spilled sidecar when the turn-append tx errors (AP-16); boot orphan scan remains the backstop | **CLOSED** |
| M-05 | `dropOldestRound` can drop the newest user turn (R-30 residual) | P2 | L | M | `context.go` | Never drop the newest round (AP-10) | CLOSED |
| M-06 | `$AURA_RUN_DIR` + reasoningtrace grow monotonically (R-33) | P2 | M | M | `orphan_scan.go`, `reasoningtrace.go`, `sweeper.go` | reasoningtrace rotation [part 1] + periodic sidecar sweep/reclaim [part 2] now both done: a `conversations.Sweeper` interval worker (`AURA_RUN_DIR_SWEEP_INTERVAL_SEC`, default 1h, ≤0 disables) re-runs the boot `ScanOrphans` (orphan dirs + tmp TTL + audit size WARN) without a restart, Start/Stopped on the serve daemon's goleak-clean drain (AP-16) | **CLOSED** |
| O-06 | SIGTERM hard-cancels in-flight turns (asymmetric drain) (R-34) | P2 | M | M | `serve.go` | Bounded turn drain (AP-17): the per-turn work ctx is a background-derived `workCtx`, NOT the signal ctx, so a SIGTERM no longer hard-kills a turn mid-stream; on the signal the daemon stops accepting new work (poller + HTTP listener) and joins in-flight turns under a `AURA_SERVE_SHUTDOWN_GRACE_SEC` grace (default 25s) via the testable `drainWithGrace` helper, THEN `workCancel` fires as the final backstop (never wedges past the grace) | **CLOSED** |
| O-07 | No Windows CI lane; OS-specific kill code untested (R-36) | P2 | M | M | `.github/workflows/ci.yml` | `windows-latest` lane (AP-17) | OPEN |
| R-41 | Per-session tool state never evicted in the daemon | P2 | M | L | `todo.go`, `shell_exec.go`, `shell_approval.go` | `SessionEvictor.Evict(sessionID)` hook; `Runner.Stop` ranges the registry and evicts the todo list + shell cwd + approval ledger when a conversation ends (AP-16) | **CLOSED** |
| T-01 | No fuzz/bench + no mutation score for agent core | P2 | H | M | agent test suite | Fuzz+bench+mutation (AP-21) | OPEN |
| M-07 | `anyInt` rejects `json.Number` (dormant token-zeroing) (R-42) | P3 | L | M | `runner_persist.go` + `chat_render.go` | Add `json.Number` case (AP-22) | **CLOSED** |
| B-12 | Mid-stream retry replays partial chunks to the user (cosmetic) | P3 | M | L | `llm_agent.go`, `chat_render.go` | Buffer chunks until clean completion | OPEN |
| B-13 | Stream-open retry classifies by substring fallback (R-38 residual) | P3 | M | L | `llm_agent_stream_retry.go` | Typed sentinels (`ECONNRESET`/`ErrUnexpectedEOF`) | **CLOSED** |
| O-08 | Span coverage `llm.request`-only; no turn/tool spans | P3 | M | L | `tracing.go`, `llm_agent.go` | `agent.turn` + `tool.execute` spans (AP-20) | **CLOSED** (`agent.turn` wraps the Run loop, `tool.execute` per dispatch; `llm.request`+`tool.execute` nest under `agent.turn` via the threaded turn ctx) |
| B-14 | `Registry.Register` silent overwrite on duplicate (R-45) | P3 | L | M | `tools/spec.go:102` | Fail-loud on duplicate | **CLOSED** |
| B-15 | Unframed/uncapped MCP argument-schema descriptions (R-22 residual) | P3 | L | H | `bridge.go`, `search.go` | Cap arg-schema descriptions | **CLOSED** |
| B-16 | `fs_grep`/`fs_glob` no node/time budget (`path:/` full-disk scan) | P3 | L | M | `fs_grep.go`, `fs_glob.go` | Node-count/deadline cap | **CLOSED** |
| T-02 | `foldToASCII` primary-channel filename folding undertested | P3 | L | L | `send_file.go` | Table test | CLOSED |
| T-03 | Deferred-tool `Spec()` golden coverage | P3 | L | L | `fs_*`/`search.go` | Golden well-formed-spec test | **CLOSED** |
| T-04 | `agenttest` dilutes the coverage floor | P3 | M | L | `agenttest`, `coverage_gate.sh:44` | Exclude from denominator | **CLOSED** |
| M-08 | `EnsureConversation` race reconciliation masks real create failure | P3 | L | L | `runner.go` | Classify `23505` before swallowing | **CLOSED** |
| R-26 | Ledger best-effort, not a pre-execution audit gate | P2 | M | M | `runner_persist.go` | Subsume into write-ahead intent log (AP-19) | TRACKED |
| R-40 | Primary channel on telebot v4 beta | P3 | L | M | `go.mod` | Pin-watch GA; re-run HITL tests on bump | TRACKED |

\* **B-11** sits at 599/600 LOC — it satisfies "no file >600 LOC" only by one line and re-breaches on the next touch; treat as nominal, not resolved.

## Verified CLOSED (do not re-open)

**P1 gate (`ec7fe2f6`, 2026-06-12, each with a passing acceptance test — see [`reconciliation-2026-06-13.md`](reconciliation-2026-06-13.md)):** B-01 (write-ahead ordering + recovery marker), B-02 (swarm untrusted provenance), B-03 (per-thread 409 guard), B-04 (honest skill schema + restored alert), O-01 (`obs.Init` tracer + JSON slog), O-02 (Prometheus `/metrics`), D-01 (non-root Dockerfile + hardened compose).

**P2/P3:** M-05 (tail-preserving drop + `ErrContextWindowExceeded`), T-02 (`foldToASCII` table test), B-11 (599 LOC — nominal).

**Prior cycles:** R-01 (untrusted envelope, direct feeders), R-02 (MCP per-call timeout), R-03 (`fs_edit` empty `old_string`), R-04 (`WithDeadline`/`NodeTimeout` wired + shell clamp), R-06 (one-tx pause + load-time repair), R-07-shell (child-env denylist — see B-09 for the divergence), R-08 (subprocess ring/tail cap), R-10 (model-facing `task approve` removed), R-11 (retry + breaker checked before call — see B-05 for lifetime), R-12 (parallel fan-out cap), R-13, R-15 (`OwnsBudget`), R-16 (see B-07 for the budget-owner edge), R-17, R-18 (`send_file` fence), R-20/R-21 (MCP reconnect-no-replay + default Mutating), R-23/R-24/R-43 (sidecar id grammar + perms), R-35, R-37, R-39.

## Top exposure now (P1 gate closed; open P2/P3 only)

1. **M-01** `ask_user` answers destroyed by L1 compaction (P2, M/M)
2. **M-03** small-window context protection silently off (P2, L/H)
3. **M-02** non-atomic resume bricks the round (P2, L/H)
4. **B-05/B-06** breaker per-turn + ungraceful open (P2, M)
5. **B-08** no stream idle-timeout watchdog (P2, M/M)
6. **M-04** sidecar orphan-on-rollback (P2, M/L)
7. **B-09** divergent secret redaction — shell leaks `*_KEY` (P2, M/M)
8. **O-05 / O-06 / O-07** no `/readyz`, no SIGTERM drain, no Windows CI (P2, M/M)
9. **B-16** unbounded `fs_grep`/`fs_glob` walk (P3, L/M)
10. **T-01** no fuzz/bench/mutation for the agent core (P2, H/M)
