# Risk Register — Aura `internal/agent` (2026-06-12)

Probability/Impact: H (high) / M (medium) / L (low). Status: OPEN (unmitigated), PARTIAL (some defense exists), TRACKED (accepted, decision pending), CLOSED (verified fixed).

**History.** 2026-06-10 closed R-01..R-17 (P0/P1 pass). 2026-06-11 closed R-18..R-25, R-35, R-43 (P2 boundary pass). This 2026-06-12 re-audit **re-opens two over-credited closures** (R-05 as B-01, R-09 as B-04) and adds new findings (`B-/O-/D-/M-` IDs) from the production-perimeter review.

## Active risks (2026-06-12)

| ID | Title | Sev | Prob | Impact | Area | Mitigation | Status |
|---|---|---|---|---|---|---|---|
| B-01 | Mutating tool side effects re-execute after intra-turn crash (R-05 was over-credited) | P1 | M | H | dispatch ↔ persistence ordering | Write-ahead intent row + recovery marker (AP-1) | OPEN |
| B-02 | Swarm child reports re-enter parent prompt with no untrusted envelope | P1 | M | H | `swarm/runner_adapter.go`, `trust.go` | Set `Provenance{Trust:Untrusted}` (AP-2) | OPEN |
| B-03 | No per-thread in-flight guard → conversation corruption + budget double-spend | P1 | M | H | `agui/server.go`, `runner.go` | Per-thread guard in Runner (AP-3) | OPEN |
| B-04 | Self-extension gate open for `always:false` + lying schema + lost alert (R-09 regressed by P5) | P1 | M | H | `skills/writer.go`, `tools/skill.go` | Honest schema + restore alert (AP-4) | OPEN |
| O-01 | `serve` never boots tracer + zero structured logging → prod blind | P1 | H | M | `serve.go`, agent core | `obs.Init` tracer + JSON slog (AP-5) | OPEN |
| O-02 | No latency/error/cost metrics, no Prometheus | P1 | H | M | `metrics.go` | Prometheus `/metrics` (AP-6) | OPEN |
| D-01 | No production container for the privileged agent binary | P1 | M | H | repo root, `compose.yaml` | Dockerfile + hardened compose (AP-7) | OPEN |
| M-01 | L1 microcompact destroys `ask_user` answers → dead pointer (R-28) | P2 | M | M | `conversations/context.go:208` | Pointer-rewrite only sidecar-backed turns (AP-8) | OPEN |
| M-02 | `SubmitAnswer` non-atomic → duplicate resume bricks the round (R-27) | P2 | L | H | `runner_resume.go:77` | One-transaction inject+mark (AP-9) | OPEN |
| M-03 | `hardCap<=0` silently disables all context protection (R-29) | P2 | L | H | `context.go:66,153` | Treat ≤0 as config error / per-model floor (AP-10) | OPEN |
| B-05 | Circuit breaker per-turn → no cross-turn protection | P2 | M | M | `llm_agent.go:117` | Hoist to Runner singleton (AP-11) | OPEN |
| B-06 | Breaker-open routes to error slot, not finalize | P2 | M | L | `llm_agent.go:225` | Route to finalize (AP-11) | OPEN |
| B-07 | `LoopAgent` maxIter=0 hot-spins on a budget-owning sub (latent, off prod path) | P2 | L | H | `workflow/loop.go:67` | Require >0 or break on no-progress (AP) | OPEN |
| O-03 | otel global no-op error handler silences all prod otel errors (R-31) | P2 | H | L | `tracing.go:63` | Rate-limited logging handler (AP-5) | OPEN |
| O-04 | No boot secret validation; no secret-redacting log handler (R-32 adj.) | P2 | M | M | `config.go`, `serve.go:130` | `Config.Validate()` + redact `ReplaceAttr` (AP-14) | OPEN |
| O-05 | `/healthz` PG-only; no Neo4j/provider readiness; no `/readyz` (R-14 residual) | P2 | M | M | `serve.go:178`, `agui/server.go` | Dep probes + `/readyz` (AP-14) | OPEN |
| B-08 | No stream idle-timeout watchdog (stall bounded only by total timeout) | P2 | M | M | `internal/llm`, `llm_agent.go:184` | Per-chunk idle watchdog (AP-12) | OPEN |
| B-09 | Divergent `secretEnvKey`; shell leaks bare `*_KEY` (R-07 divergence) | P2 | M | M | `shell_exec_env.go:22`, `mcp/client.go:164` | One shared `IsSecretEnvKey` (AP-13) | OPEN |
| B-10 | Destructive-shell gate regex-bypassable + off by default (R-19 residual) | P2 | M | M | `shell_exec_env.go:71` | Document advisory + default patterns (AP-15) | OPEN |
| B-11 | `shell_exec.go` 598/600 LOC god-class | P2 | H | L | `tools/shell_exec.go` | Pre-emptive split (AP-15) | OPEN |
| M-04 | Sidecar spill outside tx → orphan-on-rollback unreclaimed in live conv | P2 | M | L | `conversations/store.go:296` | Spill-inside-tx or sweep reconciliation (AP-16) | OPEN |
| M-05 | `dropOldestRound` can drop the newest user turn (R-30 residual) | P2 | L | M | `context.go:281` | Never drop the newest round (AP-10) | OPEN |
| M-06 | `$AURA_RUN_DIR` + reasoningtrace grow monotonically (R-33) | P2 | M | M | `orphan_scan.go`, `reasoningtrace.go` | Periodic TTL sweep + rotation (AP-16) | OPEN |
| O-06 | SIGTERM hard-cancels in-flight turns (asymmetric drain) (R-34) | P2 | M | M | `serve.go` | Bounded turn drain (AP-17) | OPEN |
| O-07 | No Windows CI lane; OS-specific kill code untested (R-36) | P2 | M | M | `.github/workflows/ci.yml` | `windows-latest` lane (AP-17) | OPEN |
| R-41 | Per-session tool state never evicted in the daemon | P2 | M | L | `todo.go`, `shell_bg.go` | `Evict(sessionID)` hook (AP-16) | OPEN |
| T-01 | No fuzz/bench + no mutation score for agent core | P2 | H | M | agent test suite | Fuzz+bench+mutation (AP-21) | OPEN |
| M-07 | `anyInt` rejects `json.Number` (dormant token-zeroing) (R-42) | P3 | L | M | `runner_persist.go:412` | Add `json.Number` case (AP-22) | OPEN (dormant) |
| B-12 | Mid-stream retry replays partial chunks to the user (cosmetic) | P3 | M | L | `llm_agent.go:245`, `chat_render.go` | Buffer chunks until clean completion | OPEN |
| B-13 | Stream-open retry classifies by substring fallback (R-38 residual) | P3 | M | L | `llm_agent_stream_retry.go:96` | Typed sentinels (`ECONNRESET`/`ErrUnexpectedEOF`) | OPEN |
| O-08 | Span coverage `llm.request`-only; no turn/tool spans | P3 | M | L | `tracing.go`, `llm_agent.go` | `agent.turn` + `tool.execute` spans (AP-20) | OPEN |
| B-14 | `Registry.Register` silent overwrite on duplicate (R-45) | P3 | L | M | `tools/registry.go:102` | Fail-loud on duplicate | OPEN |
| B-15 | Unframed/uncapped MCP argument-schema descriptions (R-22 residual) | P3 | L | H | `bridge.go:140`, `search.go:177` | Cap+frame arg descriptions | OPEN |
| B-16 | `fs_grep`/`fs_glob` no node/time budget (`path:/` full-disk scan) | P3 | L | M | `fs_grep.go`, `fs_glob.go` | Node-count/deadline cap | OPEN |
| T-02 | `foldToASCII` 23.5% covered (primary channel filename folding) | P3 | L | L | `send_file.go:193` | Table test | OPEN |
| T-03 | Deferred-tool `Spec()` 0% covered | P3 | L | L | `fs_*`/`search.go` | Golden well-formed-spec test | OPEN |
| T-04 | `agenttest` dilutes the coverage floor | P3 | M | L | `agenttest`, `coverage_gate.sh:44` | Exclude from denominator | OPEN |
| M-08 | `EnsureConversation` race reconciliation masks real create failure | P3 | L | L | `runner.go:186` | Classify `23505` before swallowing | OPEN |
| R-26 | Ledger best-effort, not a pre-execution audit gate | P2 | M | M | `runner_persist.go` | Subsume into write-ahead intent log (AP-19) | TRACKED |
| R-40 | Primary channel on telebot v4 beta | P3 | L | M | `go.mod` | Pin-watch GA; re-run HITL tests on bump | TRACKED |

## Verified CLOSED (do not re-open)

R-01 (untrusted envelope, direct feeders), R-02 (MCP per-call timeout), R-03 (`fs_edit` empty `old_string`), R-04 (`WithDeadline`/`NodeTimeout` wired + shell clamp), R-06 (one-tx pause + load-time repair), R-07-shell (child-env denylist + preview redaction — see B-09 for the divergence), R-08 (subprocess ring/tail cap), R-10 (model-facing `task approve` removed), R-11 (retry + breaker checked before call — see B-05 for lifetime), R-12 (parallel fan-out cap), R-13 (mid-stream bounded re-issue), R-15 (`OwnsBudget` single owner), R-16 (ctx check + empty-pass charge — see B-07 for the budget-owner edge), R-17 (coverage gate covers `agent/tools`), R-18 (`send_file` fence), R-20 (MCP reconnect-no-replay), R-21 (MCP default `Mutating`), R-23/R-24/R-43 (sidecar id grammar + perms + clamp), R-35 (bg-shell `Shutdown`/cap/prune), R-37 (finalize/critic errors recorded), R-39 (terminal-after-batch partition).

## Top 10 by exposure (severity × probability × impact)

1. **B-01** Mutating re-execution on crash (P1, M/H)
2. **B-02** Swarm prompt-injection laundering (P1, M/H)
3. **B-03** Conversation corruption under concurrency (P1, M/H)
4. **B-04** Self-extension open + lying contract (P1, M/H)
5. **O-01** Production untraced + unlogged (P1, H/M)
6. **O-02** No latency/error/cost metrics (P1, H/M)
7. **D-01** No production container (P1, M/H)
8. **M-01** `ask_user` answers destroyed by compaction (P2, M/M)
9. **M-03** Small-window context protection silently off (P2, L/H)
10. **M-02** Non-atomic resume bricks the round (P2, L/H)
