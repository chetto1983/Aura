# Action Plan — Aura `internal/agent`

Concrete engineering backlog, organized by priority. Each task: title · description · owner role · expected outcome · acceptance criteria. Cross-references to [`bug-report.md`](bug-report.md) findings.

---

## Closure update - 2026-06-10

All P0 and P1 items in this action plan are closed in code. Validation run:

- `go test ./internal/agent ./internal/llm ./internal/runner ./internal/conversations ./internal/agui ./internal/cron ./internal/agent/workflow ./internal/agent/tools ./internal/skills ./internal/mcp`

| Action | Risk(s) | Status | Closure evidence |
|---|---|---|---|
| A-1 | R-03 | CLOSED | `fs_edit` rejects empty `old_string`; regression in `internal/agent/tools/fs_test.go`. |
| A-2 | R-02 | CLOSED | MCP ctx-aware stdio reads, per-call timeout, reconnect mutex fix; see `p0-validation-2026-06-10.md`. |
| A-3 | R-04 | CLOSED | Runner/CLI derive budget deadline ctx; `runTool` applies `NodeTimeout`; shell timeout is clamped. |
| A-4 | R-08 | CLOSED | Sync/background shell buffers are capped with truncation markers and poll redaction. |
| A-5 | R-07 | CLOSED | Shell and MCP child envs strip inherited secret-shaped keys while preserving explicit env. |
| A-6 | R-10 | CLOSED | `task approve` removed from model-facing schema/router; operator approval path remains. |
| A-7 | R-17 | CLOSED | `internal/agent/tools` restored to the coverage gate scope. |
| A-8 | R-14 | CLOSED | `GET /healthz`, scheduler last-tick detail, `/debug/vars` expvar counters. |
| A-9 | R-11, R-13 | CLOSED | HTTP 429/5xx retry, bounded Retry-After, mid-stream retry salvage, breaker in `internal/llm`. |
| A-10 | R-12 | CLOSED | `AURA_LOOP_MAX_PARALLEL_TOOLS` semaphore cap with fan-out regression coverage. |
| A-11 | R-01 | CLOSED | Untrusted tool-output provenance envelope and prompt contract; see `p0-validation-2026-06-10.md`. |
| A-12 | R-05 | CLOSED | ToolInvocation start/end now journals assistant tool_calls plus RoleTool turns. |
| A-13 | R-06 | CLOSED | Load-time tool-pair repair prevents orphan tool results from bricking provider history. |
| A-25 | R-15, R-16 | CLOSED | `BudgetOwner` contract, LoopAgent ctx checks, no double-charge for budget-owning children, empty-pass budget charge. |

## P2 boundary/lifecycle closure update - 2026-06-11

The boundary and lifecycle P2 cluster is closed in code. Validation evidence:

- `go test ./internal/agent/tools ./internal/agent/mcptools ./internal/mcp ./cmd/aura -count=1`
- `go test -race ./internal/agent/tools ./internal/agent/mcptools ./internal/mcp ./cmd/aura -count=1`
- `go test ./internal/agent ./internal/llm ./internal/runner ./internal/conversations ./internal/agui ./internal/cron ./internal/agent/workflow ./internal/agent/tools ./internal/skills ./internal/mcp ./cmd/aura -count=1`

Coverage caveat:

- `scripts/coverage_gate.sh` ran to completion but failed the global floor: `77.2% < 85%`.
- This leaves a repo-wide coverage remediation item outside the P2 boundary/lifecycle risk closure.

## Immediate (P0/P1) — start now

### A-1 · Reject empty `old_string` in `fs_edit`
- **Description:** Add a guard after unmarshal in `tools/fs_edit.go:Execute` rejecting `OldString == ""`. (Bug P1 / F-06.)
- **Owner:** Backend engineer (tools).
- **Expected outcome:** A common model mistake can no longer corrupt host files.
- **Acceptance:** `fs_edit` with `old_string:""` returns an error and leaves the file byte-unchanged, with and without `replace_all`, on empty and non-empty files; table test added.

### A-2 · Per-call MCP timeout + ctx-aware read
- **Description:** Wrap `bridgedTool.Execute` in `context.WithTimeout(ctx, AURA_MCP_CALL_TIMEOUT_SEC)`; make `internal/mcp/client.go:readResponse` ctx-aware (reader goroutine + select on `ctx.Done()`), treating timeout as `ErrTransport`; stop holding `s.mu` across blocking I/O. (Bug P0 / F-01.)
- **Owner:** Backend engineer (MCP/reliability).
- **Expected outcome:** A hung MCP server no longer wedges the daemon or deadlocks shutdown.
- **Acceptance:** a fake server that accepts-and-never-replies causes `CallTool` to return a timeout within the ceiling; a second server's tools remain usable; `Close()` returns within its deadline; goleak-clean.

### A-3 · Make the wallclock cancel in-flight work
- **Description:** Derive `ic.Ctx, cancel := bud.WithDeadline(ctx)` in `runner.buildAgent` and `cmd/aura/agent.go`; wrap each `runTool` with `NodeTimeout()` when >0; clamp `shellExecArgs.TimeoutMs` to `AURA_SHELL_MAX_TIMEOUT_MS`. (Bug P1 / F-03.)
- **Owner:** Backend engineer (loop/runtime).
- **Expected outcome:** The 300s wallclock becomes a real cap; no tool can outlive it indefinitely.
- **Acceptance:** a tool sleeping past the wallclock is cancelled at the deadline; an over-ceiling `timeout_ms` is clamped.

### A-4 · Cap subprocess output buffers
- **Description:** Ring/tail-cap `shellOutputCapture` (head + last M bytes, `AURA_SHELL_OUTPUT_CAP_BYTES`); have `bgShell.snapshot` drop consumed bytes and enforce `AURA_SHELL_BG_BUF_CAP` with a truncation marker; stream overflow to the existing sidecar. (Bug P1 / F-07.)
- **Owner:** Backend engineer (tools).
- **Expected outcome:** No accidental OOM from high-volume or long-lived subprocess output on the shared host.
- **Acceptance:** a >cap command yields a bounded preview + `[output truncated]`; background `buf` length stays bounded across many polls.

### A-5 · Filter the child environment
- **Description:** In `mergeEnv` (`shell_exec.go`) and the MCP launch (`mcp/client.go:83`), strip secret-shaped vars (`*_API_KEY`/`*_TOKEN`/`*_PASSWORD`/`*_SECRET`/`OPENROUTER_*`/`TELEGRAM_*`/`POSTGRES_*`/`NEO4J_*`) unless the operator opts a name in; let the model's explicit `env` arg re-add. (Bug P1 / F-05.)
- **Owner:** Security-minded backend engineer.
- **Expected outcome:** Secrets are no longer broadcast to every child process.
- **Acceptance:** `mergeEnv` output excludes a planted `FAKE_API_KEY`; MCP subprocess receives only declared env + a minimal base.

### A-6 · Remove model-facing `task approve`
- **Description:** Drop `approve` from the `task` tool's model-visible enum and Description; keep it on `aura task approve` CLI + the ask_user resume path. (Bug P1 / F-09.)
- **Owner:** Backend engineer (scheduler/tools).
- **Expected outcome:** The model can no longer release its own gated destructive scheduled task.
- **Acceptance:** a model `task approve` call is rejected or converts to a pause; only CLI/resume releases a `pending_approval` task.

### A-7 · Re-enable the coverage floor on `agent/tools`
- **Description:** Remove `/internal/agent/tools/` from `scripts/coverage_gate.sh:44`; re-check `/internal/sandbox/` + `/internal/llm/client.go:` for the same staleness. (Bug P1 / coverage gate.)
- **Owner:** QA / build engineer.
- **Expected outcome:** The riskiest tool surface is protected by the 85% floor.
- **Acceptance:** `make coverage` runs with the package in the profile and still passes.

### A-8 · `/healthz` + basic counters
- **Description:** Add `GET /healthz` (pool `Ping` + scheduler last-tick) to the AG-UI mux; add `expvar` counters at `ConsumeStep`/`dispatch`/`streamWithOpenRetry`. (Bug P1 / metrics.)
- **Owner:** SRE / backend engineer.
- **Expected outcome:** The daemon is health-probeable and minimally observable.
- **Acceptance:** `/healthz` returns 503 on pool failure, 200 otherwise; counters increment.

### A-9 · Provider retry/backoff + circuit breaker
- **Description:** In `retryableStreamOpenError`, `errors.As` to a provider-neutral HTTP error; retry 429 honoring `RetryAfterSec` (capped) and 5xx with jittered exponential backoff (`AURA_LLM_RETRY_*`); add a consecutive-failure breaker in `internal/llm`. (Bug P1 / F-08.)
- **Owner:** Backend engineer (LLM client).
- **Expected outcome:** Turns survive routine provider blips; a provider outage doesn't hammer a dead endpoint.
- **Acceptance:** table test: 429-with-Retry-After retries with sleep, 503 with backoff, 400 no retry; breaker opens after N failures.

### A-10 · Cap parallel tool fan-out
- **Description:** Bound `executeBatch` with a semaphore (`AURA_LOOP_MAX_PARALLEL_TOOLS`, default 4–8). (Bug P1 / F / fan-out.)
- **Owner:** Backend engineer (loop).
- **Expected outcome:** Model-emitted wide batches can't saturate the host.
- **Acceptance:** N>cap tool calls run ≤cap concurrently (barrier test).

### A-11 · Provenance-tag untrusted tool output
- **Description:** In `runTool`, wrap results from `web_fetch`/MCP/`fs_read`/`shell_exec` in a non-spoofable `<tool_output source=… trust="untrusted">…</tool_output>` envelope; neutralize chat-template control tokens (reuse `skills/validator.go` NFKC stripping); add a system-prompt clause that envelope content is data. (Bug P0 / F-02.)
- **Owner:** Security + prompt engineer (pair).
- **Expected outcome:** Indirect prompt injection can no longer impersonate instructions.
- **Acceptance:** a contract test asserts each untrusted-source result is wrapped before `a.history`; a forged `</assistant>` token is neutralized.

### A-12 · Per-event intra-turn persistence
- **Description:** Journal assistant-tool_call + RoleTool turns through `persistEvent` (the ledger end-events already carry the data), or flush in-memory round history on pause like `flushPause`. (Bug P1 / F-04.)
- **Owner:** Backend engineer (runner/persistence).
- **Expected outcome:** Pause/resume and crash no longer lose completed tool work or re-run mutating tools.
- **Acceptance:** resume rehydrates prior tool rounds; no duplicate mutating dispatch on retry (integration test).

### A-13 · One-transaction pause writes + load-time repair
- **Description:** Write paused_states row(s) + the combined assistant turn in one `db.WithTx` at round end; add a `LoadManagedHistory` guard that repairs/refuses orphan tool_call/result pairs. (Bug P1 / F / orphan brick.)
- **Owner:** Backend engineer (runner/persistence).
- **Expected outcome:** A crash in the pause window can't permanently brick a conversation.
- **Acceptance:** crash-window simulation does not yield a 400-bound history.

---

## Short-term improvements (P2)

### A-14 · Structured logging
- slog `JSONHandler` + `AURA_LOG_LEVEL` in `cmd/aura/main.go`; Warn logs at loop terminal decisions with `request_id`+`thread_id`; `llm.Config.LogValue()` redactor. Owner: backend. Acceptance: terminal decisions are logged and correlated; `%+v` of config doesn't leak the key.

### A-15 · Surface finalize/critic mid-stream errors
- `reasoningtrace.Record`/slog in `synthesizeWithFallback` and `runCompletionCritic` ok=false branches. Owner: backend. Acceptance: a forced double-failure logs both errors before the stub.

### A-16 · Duplicate-`text_response` handling
- Synthetic results for second-and-later terminal calls in `dispatch`. Owner: backend. Acceptance: a two-`text_response` turn yields a wire-valid next request.

### A-17 · MCP trust hardening
- Default bridged tools `Mutating: true` (honor `readOnlyHint`); conditional reconnect (no replay on recv-side error); provenance-frame + length-cap descriptions; wrap HTTP transports in the reconnect decorator + `io.LimitReader`. Owner: backend (MCP). Acceptance: write-capable MCP tools arm the critic and are never auto-replayed; HTTP servers reconnect; bodies are capped.

### A-18 · `send_file` fence + destructive-shell gate
- Default-fence `send_file` to workspace (`ask_user` for outside); operator-configurable destructive-pattern detector forcing an approval pause. Owner: security backend. Acceptance: outside-workspace delivery and a configured destructive pattern require approval.

### A-19 · `read_tool_output` limit clamp + bounded read
- Clamp `limit` to ~4–8× previewCap; `os.Open`+`Seek` instead of `ReadFile`. Owner: backend. Acceptance: a huge `limit` returns a bounded preview; a small window doesn't load the whole sidecar.

### A-20 · Sidecar key collision fix
- Key sidecars by `<turnSeq>_<toolCallID>` or a host-minted UUID stamped in the footer. Owner: backend. Acceptance: two truncated outputs in one session don't overwrite; paging returns the correct bytes. (Confirm provider id-reuse first.)

### A-21 · Disk lifecycle
- Sidecar TTL sweep in the scheduler (tied to eviction horizon); reasoningtrace rotation + boot WARN + drop the duplicate `history` field. Owner: SRE. Acceptance: `$AURA_RUN_DIR` stops growing for live conversations.

### A-22 · Background-shell shutdown + cap
- `BackgroundShells.Shutdown()` wired into serve teardown; `AURA_SHELL_BG_MAX`; evict finished shells. Owner: backend. Acceptance: serve teardown kills background jobs; cap enforced.

### A-23 · Bounded conversational-turn drain on SIGTERM
- WaitGroup-gated drain (`AURA_SHUTDOWN_TURN_GRACE_SEC`) before cancelling turn ctxs, or persist an "interrupted" assistant turn. Owner: backend. Acceptance: an in-flight turn at SIGTERM completes or persists a marker within the grace window.

### A-24 · Test gaps + Windows CI
- The ~18 tests in `testing-strategy.md`; a `windows-latest` shell lane; fuzz the args boundary. Owner: QA. Acceptance: new tests green; Windows lane green; fuzz corpus in CI.

---

## Medium-term architecture work (P1/P2 design)

### A-25 · Single budget owner per agent tree
- Resolve LoopAgent×LlmAgent double-spend; LoopAgent ctx-check + per-iteration budget. Document on `Agent.Run`. Owner: senior backend. Acceptance: composed budgets consume once; `maxIter=0` over a chat-only sub terminates.

### A-26 · OTel productionization
- Default exporter honesty; tool/swarm spans; collector behind a compose profile. Owner: SRE. Acceptance: spans land in a collector; default deployment doesn't silently drop.

### A-27 · Prometheus metrics
- turns, tool dispatch/error/retry, budget trips, pause/resume, MCP reconnects, LLM latency/tokens/cost. Owner: SRE. Acceptance: `/metrics` exposes them; a dashboard renders.

### A-28 · Blocking write-ahead audit (if non-repudiation required)
- For `Mutating` tools, refuse dispatch when the intent row can't persist; else document observability-only. Owner: security backend. Acceptance: documented decision + enforced behavior.

---

## Long-term industrialization (P2/P3)

### A-29 · Adopt codex per-tool parallel-safety gate
- Mutating ⇒ exclusive in the batch; reads concurrent. Owner: backend. Acceptance: two mutating tools serialize. (Ref: `D:\tmp\codex\codex-rs\core\src\tools\parallel.rs`.)

### A-30 · Adopt adk-go reflect-and-retry
- Per-tool failure counter + "stop using this tool" injection past N. Owner: backend. Acceptance: a repeatedly-failing tool gets routed around instead of burning budget. (Ref: `D:\tmp\adk-go-study\plugin\retryandreflect`.)

### A-31 · Stream idle-timeout watchdog + typed transient classification
- Config-driven idle deadline; `errors.Is(ECONNRESET/io.ErrUnexpectedEOF)` over substring matching. Owner: backend. Acceptance: idle streams detected by deadline; classification is typed.

### A-32 · Mutation scores for the loop core
- `go-mutesting` on `llm_agent*.go`/`shell_exec.go`/`bridge_reconnect.go` ≥70%, recorded in the quality snapshot. Owner: QA. Acceptance: scores documented.

### A-33 · Hygiene sweep
- Remove dead `renderShellOutput` + `_ = requestID`; `Registry.Register` duplicate guard; fresh-per-turn one-shot guard; tighten sidecar id grammar + `0o600`; `anyInt` `json.Number`; per-thread AG-UI guard. Owner: backend. Acceptance: dead code gone; duplicate registration fails loud.
