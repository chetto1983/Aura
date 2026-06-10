# Risk Register — Aura `internal/agent`

Probability/Impact: H (high) / M (medium) / L (low). Status: OPEN (unmitigated), PARTIAL (some defense exists), TRACKED (accepted with a documented decision pending). All risks are OPEN at audit time unless noted.


**Closure update - 2026-06-10:** R-01 through R-17 are closed in code by the P0/P1 remediation pass. Residual P2/P3 hardening remains tracked below.

| ID | Title | Severity | Probability | Impact | Affected area | Mitigation | Status |
|---|---|---|---|---|---|---|---|
| R-01 | Prompt injection via unmarked untrusted tool output → host RCE + exfiltration | P0 | H | H | prompt assembly / tool boundary (`llm_agent.go:361`) | Provenance envelope + control-token neutralization (A-11); env filter (A-5); destructive gate (A-18) | CLOSED |
| R-02 | Hung MCP server wedges every turn + deadlocks shutdown | P0 | M | H | `mcp/client.go`, `mcptools/bridge_reconnect.go` | Per-call timeout + ctx-aware read (A-2) | CLOSED |
| R-03 | `fs_edit` empty `old_string` corrupts host files | P1 | M | H | `tools/fs_edit.go` | Reject empty `old_string` (A-1) | CLOSED |
| R-04 | Wallclock cap is inert; in-flight tool/LLM work runs unbounded | P1 | M | H | `budget.go` (dead `WithDeadline`), `runner.go` | Wire `WithDeadline` + `NodeTimeout` + clamp `timeout_ms` (A-3) | CLOSED |
| R-05 | Intra-turn persistence loss → duplicated mutating side effects on resume/crash | P1 | H | H | `runner_persist.go`, `llm_agent.go` | Per-event journaling (A-12) | CLOSED |
| R-06 | Crash in pause window → orphan tool_result → conversation 400s forever | P1 | L | H | `runner_persist.go` (two-phase pause write) | One-transaction pause + load-time repair (A-13) | CLOSED |
| R-07 | All secrets broadcast to every shell + MCP child; preview unredacted | P1 | H | H | `shell_exec.go:393`, `mcp/client.go:83` | Child-env filter (A-5) + preview redaction (A-21/3.6) | CLOSED |
| R-08 | Subprocess output buffered unbounded → OOM on the shared host | P1 | M | H | `shell_exec.go`, `shell_bg.go` | Ring/tail cap (A-4) | CLOSED |
| R-09 | Auto-activating `always:true` skill self-extension → persistent backdoor | P1 | M | H | `skills/writer.go`, `tools/skill_write.go` | Gate `always:true` + alert + fix stale contract (A-... / 3.2) | CLOSED |
| R-10 | Model self-approves its own gated destructive scheduled task | P1 | M | M | `tools/task.go:106` | Remove model-facing `approve` (A-6) | CLOSED |
| R-11 | No retry/breaker on provider 429/5xx → turns die on routine blips | P1 | H | M | `llm_agent_stream_retry.go` | Retry/backoff + breaker (A-9) | CLOSED |
| R-12 | Uncapped model-controlled parallel tool fan-out saturates the host | P1 | M | M | `llm_agent_parallel.go` | Semaphore cap (A-10) | CLOSED |
| R-13 | Mid-stream LLM error kills a near-complete turn, orphans side effects | P1 | H | M | `llm_agent.go:consume` | Bounded re-issue from the loop (A-... / 1.7) | CLOSED |
| R-14 | Operationally blind: no metrics, no `/healthz` — P0 hang undetectable | P1 | H | M | `agui/server.go`, agent core | `/healthz` + counters (A-8), Prometheus (A-27) | CLOSED |
| R-15 | LoopAgent×LlmAgent composition double-spends budget + double dedup | P1 | L | M | `workflow/loop.go` + `llm_agent.go` | Single budget owner (A-25) | CLOSED |
| R-16 | LoopAgent `maxIter=0` hot-spins on a non-tool sub (100% CPU, no escape) | P1 | L | H | `workflow/loop.go:64–129` | Ctx check + per-iteration budget (A-25 / 2.2) | CLOSED |
| R-17 | Coverage floor doesn't gate `agent/tools` (shell/fs/skill) | P1 | H | M | `scripts/coverage_gate.sh:44` | Remove the exclusion (A-7) | CLOSED |
| R-18 | `send_file` unfenced → arbitrary host-file exfiltration to channel | P2 | M | H | `tools/send_file.go` | Workspace fence + approval (A-18) | OPEN |
| R-19 | No enforced backstop on destructive shell commands | P2 | M | H | `shell_exec.go` | Destructive-pattern approval gate (A-18) | PARTIAL (prompt-only) |
| R-20 | MCP reconnect replays a possibly-completed side effect | P2 | M | M | `bridge_reconnect.go` | Conditional reconnect (A-17) | OPEN |
| R-21 | Bridged MCP tools never `Mutating` → skip critic, eligible for replay | P2 | M | M | `mcptools/bridge.go` | Default `Mutating` + `readOnlyHint` (A-17) | OPEN |
| R-22 | MCP tool descriptions trusted verbatim → tool poisoning | P2 | L | H | `mcptools/bridge.go`, `search.go` | Provenance frame + length cap (A-17) | OPEN |
| R-23 | Sidecar `tool_call_id` collision → wrong data paged as ground truth | P2 | M | M | `tools/result.go` | Turn-seq-prefixed key (A-20) | OPEN (confirm provider id-reuse) |
| R-24 | `read_tool_output` unbounded `limit` re-inflates truncated output | P2 | M | M | `tools/read_tool_output.go` | Clamp + bounded read (A-19) | OPEN |
| R-25 | Streamable-HTTP MCP: no reconnect, no body cap → brick / OOM | P2 | L | M | `mcptools/mount.go`, `mcp/http_client.go` | Reconnect decorator + LimitReader (A-17) | OPEN |
| R-26 | ledger is best-effort, not a pre-execution audit gate | P2 | M | M | `runner_persist.go` | Write-ahead intent row or document (A-28) | TRACKED |
| R-27 | `SubmitAnswer` non-atomic → duplicate tool_results | P2 | L | H | `runner_resume.go` | One-transaction inject+mark (A-13) | OPEN |
| R-28 | L1 microcompact destroys ask_user answers, points to absent sidecars | P2 | M | M | `conversations/context.go:202` | Exempt sidecar-less ids (revisit w/ A-12) | OPEN |
| R-29 | `hardCap` clamps to 0 → disables all context protection (small models) | P2 | L | H | `conversations/context.go:66` | Treat cap≤0 as config error; per-model window | OPEN (future Slice-13) |
| R-30 | Oversized final round silently discarded → model sees no user request | P2 | L | M | `conversations/context.go:274` | Return `dropped=false` when empty (route to error) | OPEN |
| R-31 | Tracing dropped by default + global no-op error handler | P2 | H | L | `tracing.go:63`, `config.go` | Exporter honesty + collector profile (A-26) | OPEN |
| R-32 | No structured logging in the agent core | P2 | H | M | agent core | slog JSONHandler + correlated logs (A-14) | OPEN |
| R-33 | `$AURA_RUN_DIR` + reasoningtrace grow monotonically | P2 | M | M | `orphan_scan.go`, `reasoningtrace.go` | TTL sweep + rotation (A-21) | OPEN |
| R-34 | SIGTERM drops in-flight conversational turns (asymmetric drain) | P2 | M | M | `serve.go`, `bot_dispatch.go` | Bounded turn drain (A-23) | OPEN |
| R-35 | Background shells: no shutdown kill, no cap, never pruned | P2 | M | M | `shell_bg.go`, `cmd/aura/main.go` | `Shutdown()` + cap + eviction (A-22) | OPEN |
| R-36 | No Windows CI lane → Windows kill-path code untested | P2 | M | M | `.github/workflows/ci.yml` | Windows shell lane (A-24) | OPEN |
| R-37 | Finalize/critic mid-stream errors silently swallowed | P2 | M | L | `llm_agent_finalize.go`, `_completion.go` | Record both errors (A-15) | OPEN |
| R-38 | Stream-open retry classifies by substring; double-submit risk | P2 | M | L | `llm_agent_stream_retry.go` | Typed classification (A-31) | OPEN |
| R-39 | Duplicate `text_response` → dangling tool_call → 400 | P2 | L | M | `llm_agent.go:dispatch` | Synthetic results for extras (A-16) | OPEN |
| R-40 | Primary user channel rides telebot v4 beta | P3 | L | M | `go.mod` | Pin-watch GA; re-run HITL live tests on bump | TRACKED |
| R-41 | Per-session tool state never evicted in the daemon | P3 | M | L | `todo.go`, `shell_exec.go`, `shell_bg.go` | Evict via ConversationCleaner / TTL (A-33) | OPEN |
| R-42 | `anyInt` rejects `json.Number` (dormant token-zeroing) | P3 | L | M | `runner_persist.go:334` | Add `json.Number` case (A-33) | OPEN (dormant) |
| R-43 | Sidecar id permits `:` (Windows ADS) + world-readable perms | P3 | L | M | `tools/result.go` | Allowlist grammar + `0o600` (A-33) | OPEN |
| R-44 | AG-UI gateway: no per-thread in-flight guard | P3 | L | M | `agui/server.go` | Per-thread singleflight (A-33) | OPEN |
| R-45 | `Registry.Register` silent overwrite on duplicate name | P3 | L | M | `tools/spec.go:84` | Fail-loud on duplicate (A-33) | OPEN |

## Top 10 by exposure (severity × probability × impact)

1. **R-01** Prompt injection (P0, H/H)
2. **R-05** Intra-turn persistence loss → duplicate side effects (P1, H/H)
3. **R-07** Secrets broadcast to children (P1, H/H)
4. **R-02** MCP hang wedges daemon (P0, M/H)
5. **R-03** `fs_edit` file corruption (P1, M/H)
6. **R-09** Auto-activating skill backdoor (P1, M/H)
7. **R-08** Subprocess OOM (P1, M/H)
8. **R-04** Inert wallclock cap (P1, M/H)
9. **R-11** No provider retry/breaker (P1, H/M)
10. **R-14** Operationally blind (P1, H/M)
