# Risk Register — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`
Probability / Impact: **H** (high) / **M** (medium) / **L** (low). Status: **OPEN** (unmitigated) · **PARTIAL** (some defense, acceptance not fully met) · **TRACKED** (accepted, decision pending) · **GOOD** (verified strong, no action).

| ID | Title | Sev | Prob | Impact | Affected area | Mitigation | Status |
|---|---|---|---|---|---|---|---|
| AG-001 | Unrecovered panic in spawned goroutine crashes the daemon | P0 | M | H | `executeBatch`, `workflow/parallel`, `swarm.runWave`, `shell_bg` | `recover()` in every goroutine + Runner backstop; panic→error | OPEN |
| AG-002 | `dedupRing` concurrent-map-write fatal (latent) | P1 | L | H | `budget_dedup.go` | `sync.Mutex` on the ring | OPEN |
| AG-003 | Hook exec TOCTOU + full-env secrets + unvalidated rewrite | P1 | M | H | `hooks_command.go` | minimal-env, exec-by-fd, validate rewrites, audit | OPEN |
| AG-004 | A failing/slow hook aborts the whole turn | P1 | M | M | `hooks_command.go`, `hooks.go` | per-hook fail-soft policy + recover | OPEN |
| AG-005 | MCP reconnect: lock-during-IO, ctx-coupled, no backoff/breaker | P1 | M | H | `mcptools/bridge_reconnect.go` | single-flight off-lock, `WithoutCancel`, backoff+breaker | OPEN |
| AG-006 | `AURA_MCP_CALL_TIMEOUT_SEC=0` → unbounded hang + held mutex | P1 | M | H | `mcptools/timeout.go`, `bridge.go` | `0`→default; bounded turn ctx; boot-validate | OPEN |
| AG-007 | No capability gate on mutating MCP tools; reconnect flips Mutating | P1 | M | H | `llm_agent_dispatch.go`, `bridge_reconnect.go` | wire `capability_grants` at dispatch; warn on flip | OPEN |
| AG-008 | Reasoning-router LLM fallback = ≤8s extra round-trip/turn on sidecar degrade | P1 | M | M | `llm_agent_reasoning.go` | static-tier fallback; embed breaker; opt-in router | OPEN |
| AG-009 | Full prompts/history/PII logged to reasoning trace | P1 | M | M | `reasoningtrace/*`, `llm_agent.go:290` | don't dump history by default; cap fields; doc PII | OPEN |
| AG-010 | DB password leaks into `shell_exec` children via DSN env | P1 | M | H | `shell_exec.go`, `secret/envkey.go` | DSN markers + value-scan redaction | OPEN |
| AG-011 | Skill self-activation ungated despite gated-looking spec (prior B-04) | P1 | M | H | `skill_write.go`, `skill.go` | gate or honestly document + alert; delete dead schema | OPEN |
| AG-012 | No latency/error/cost/outcome metrics — no SLOs (prior O-02) | P1 | H | M | `metrics.go` | full metric set; non-default registry | OPEN |
| AG-013 | No structured logs; tracing silent-drop + panic risk (prior O-01) | P1 | H | M | `tracing.go`, agent core | slog; never-panic span ID; boot-log exporter | OPEN |
| AG-014 | No fs size cap → OOM/turn-wedge on large file | P2 | M | M | `tools/fs_*` | `AURA_FS_MAX_READ_BYTES`; auto-page | OPEN |
| AG-015 | Background-shell buffers leak in long-lived daemon | P2 | M | M | `tools/shell_bg.go` | `SessionEvictor`; prune on poll/kill | OPEN |
| AG-016 | Deferred `agent_job` gated only by keywords | P2 | L | M | `tools/task.go` | gate all schedules or surface at fire time | OPEN |
| AG-022 | Cross-server MCP name collision drops whole server silently | P2 | L | M | `mcptools/name.go`, boot loop | boot-validate namespace uniqueness; escalate WARN | OPEN |
| AG-024 | After-send MCP transport fail → mutating tool double-exec | P2 | L | M | `mcptools/bridge_reconnect.go` | failed-after-send sentinel; non-retryable | OPEN |
| AG-025 | Hostile MCP schema (1000s props) bloats manifest/index | P2 | L | M | `mcptools/bridge.go` | total-size + property-count cap | OPEN |
| AG-027 | Per-call env parse; malformed value = loop-fatal MCP calls | P2 | L | M | `mcptools/bridge.go`, `timeout.go` | resolve+validate at boot | OPEN |
| AG-028 | Dead code `openManagedServer` (NEEDS CONFIRMATION) | P2 | L | L | `mcptools/mount.go` | `deadcode`; remove if unref | OPEN |
| AG-029 | Registry "immutable" relies on undocumented atomic-swap | P2 | L | M | `mcptools/bridge.go`, `tools/spec.go` | document + `-race` reconnect-during-dispatch test | OPEN |
| AG-030 | Non-zero hook exit + parseable decision swallows runErr | P2 | L | M | `hooks_command.go` | require exit 0 for rewrite | OPEN |
| AG-031 | No runtime cache-prefix invariant; hook rewrite busts cache silently | P2 | M | M | `prompt/hash.go` (test-only use) | per-turn PrefixHash compare + drift metric | OPEN |
| AG-032 | classifier `ensureAnchors` holds lock across network calls | P2 | M | M | `prompt/reasoning_classifier.go` | build off-lock / singleflight | OPEN |
| AG-033 | `mintSpanID` panics on entropy failure | P2 | L | M | `tracing.go` | zero-ID fallback; never panic | OPEN |
| AG-034 | `ToolInvocation` raw args/preview/Meta — ledger redaction unconfirmed | P2 | M | M | `event.go` + persistence | redact + cap before DB write | NEEDS CONFIRMATION |
| AG-035 | `maxIterations==0` loop bounded only by budget/no-progress | P2 | L | M | `workflow/loop.go` | default iteration ceiling; require wallclock ctx | OPEN |
| AG-036 | `max_steps`/`wallclock` not validated `>0`; `=0` disables runtime | P2 | L | M | `budget.go` | reject `<1` at `NewBudget` | OPEN |
| AG-037 | `findInTree` no cycle guard → stack overflow on cyclic tree | P2 | L | M | `workflow/workflow.go` | visited-set / depth bound | OPEN |
| AG-038 | Swarm `budgetReserve=3` is best-effort (TOCTOU), not reserved | P2 | M | L | `budget.go`, `swarm.go` | atomic reserve/restore or document | OPEN |
| AG-039 | Dedup `results` map grows unbounded over a run | P2 | L | L | `budget_dedup.go` | prune on eviction / cap | OPEN |
| AG-040 | Dedup misses period-3+ cycles | P2 | L | M | `budget_dedup.go` | repeated-subsequence detection | OPEN |
| AG-041 | Wallclock is step-boundary soft gate; `WithDeadline` unwired | P2 | M | M | `budget.go` (unwired) | wire into run ctx | NEEDS CONFIRMATION |
| AG-042 | No crash-resume checkpoint (in-memory state) | P2 | M | M | `llm_agent.go` (Runner) | incremental snapshot keyed by sessionID | TRACKED |
| AG-043 | Parallel result-closer goroutine leak edge | P2 | L | M | `workflow/parallel.go` | goleak stress test | NEEDS CONFIRMATION |
| AG-052 | Unknown-tool output defaults trusted; swarm reports unwrapped (prior B-02) | P3→P1* | M | H* | `trust.go`, swarm | default-untrusted; propagate provenance | OPEN |
| AG-018 | Model `cwd` unvalidated; approval digest not normalized | P3 | L | L | `tools/shell_exec_session.go` | stat + clean + normalize | OPEN |
| AG-019 | `send_file` fence silently off when WorkspaceRoot empty | P2 | L | M | `tools/send_file.go` | fail closed in non-CLI | OPEN |
| AG-020 | MCP-reconnect stale embedding degrades tool selection | P2 | M | L | `tools/search.go` | per-tool desc hash; rebuild on change | OPEN |
| AG-044 | Dead duplicate `skillParamsSchema` | P3 | L | L | `tools/skill.go` | delete | OPEN |
| AG-045 | Non-atomic in-place fs writes (crash-truncate, edit race) | P3 | L | M | `tools/fs_edit.go` | temp+rename | OPEN |
| AG-046 | Inconsistent glob semantics (fs_grep vs fs_glob) | P3 | L | L | `tools/fs_grep.go` | unify or document | OPEN |
| AG-049 | SSRF gate has no destination-port restriction | P3 | L | L | `internal/web` | optional port policy | TRACKED |
| AG-054 | Bare hook command resolved against runtime PATH | P3 | L | M | `hooks_command.go` | require absolute paths | OPEN |
| AG-055 | Reasoning seeds Italian-only | P3 | L | L | `prompt/reasoning_classifier.go` | multilingual seeds / document | OPEN |
| AG-057 | Duplicated expvar+Prometheus globals; promauto panics on re-reg | P3 | L | L | `metrics.go` | custom registry | OPEN |
| AG-058 | Hook short-circuit undocumented; no recover around in-proc hooks | P3 | L | M | `hooks.go` | document + recover | OPEN |
| AG-062 | `SwarmContextValue` concurrent-read contract implicit | P3 | L | M | `swarm_context.go` | document + `-race` fan-out test | OPEN |
| — | SSRF hardening (pinned-IP, scheme allowlist, metadata block, redirect re-val) | — | — | — | `internal/web` | — | GOOD |
| — | `trust.go` nonce-fenced untrusted-output envelope | — | — | — | `trust.go` | — | GOOD |
| — | KV-cache stable-prefix discipline | — | — | — | `prompt/builder.go` | — | GOOD |
| — | Shared-atomic budget defeats `max_steps^depth` fan-out bomb | — | — | — | `budget.go` | — | GOOD |
| — | Non-empty terminal contract (synthesis → Italian stub) | — | — | — | `llm_agent_finalize.go` | — | GOOD |
| — | Bounded recovery/completion/truncation counters (no infinite loop) | — | — | — | `llm_agent*.go` | — | GOOD |

\* **AG-052** is rated P3 within the strict single-operator model but **P1 with H impact** once the daemon synthesizes swarm-worker output that may carry attacker-controlled web/file content — the realistic deployment. Treat as P1 for planning.

## Top-10 risks (by severity × probability × impact)

1. **AG-001** — daemon-wide crash from one panicking tool (P0).
2. **AG-005** — MCP reconnect livelock / head-of-line freeze (P1, M·H).
3. **AG-003** — hook TOCTOU + secret-env exposure (P1, M·H).
4. **AG-007** — no capability gate on mutating MCP tools (P1, M·H).
5. **AG-010** — DB password leaks to shell children (P1, M·H).
6. **AG-011** — ungated skill self-activation / injection persistence (P1, M·H).
7. **AG-052** — swarm/unknown-tool output not enveloped → indirect injection (P1*, M·H).
8. **AG-006** — `=0` timeout unbounded hang (P1, M·H).
9. **AG-012/AG-013** — production blindness, no SLO/alerting (P1, H·M).
10. **AG-002** — latent dedup concurrent-map-write fatal (P1, L·H).
