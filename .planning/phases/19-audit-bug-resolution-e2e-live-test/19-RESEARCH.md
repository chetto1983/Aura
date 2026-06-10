# Phase 19: Audit Bug Resolution + E2E Live Test - Research

**Researched:** 2026-06-10
**Domain:** Go correctness/robustness remediation (process-group lifecycle, SSE/AG-UI protocol integrity, durable scheduler state, MCP reconnect, context-window compaction) + two-layer validation architecture
**Confidence:** HIGH (every claim verified against repo file:line + Go stdlib docs + AG-UI SDK source in module cache)

## Summary

This is a **correctness + validation phase**, not a feature phase. The 2026-06-10 deep audit already prescribes the mechanical `fix:` for all 26 findings (10 HIGH, 10 MEDIUM, 6 LOW). My job was to verify the audit's `file:line` claims against the live tree, pin the exact Go APIs/SDK signatures the fixes need, and design the two-layer proof model. **Every audit claim I checked was accurate**, with three precision corrections the planner must know:

1. **Migration head is `0012_telegram`, NOT `0011`.** The audit and CONTEXT both say "current head 0011 → next 0012". The tree at HEAD has `0012_telegram.{up,down}.sql`. **H6/H7's new migration is `0013_*`.** (`internal/db/migrations/`)
2. **The stale comment H2 cites is at `serve_channels.go:142`, not `:145`** (the "user sees a generic ❌ Errore" string in `ensuringTurn`'s doc comment). Same finding, off-by-3.
3. **Go is `1.26.4`** (`go.mod:3`) — so `cmd.Cancel`/`cmd.WaitDelay` (Go 1.20+) and `errors.AsType[T]` (Go 1.26 generic, already used at `shell_exec.go:318`) are both available. The OS-process-group split is the **first** `//go:build` OS-split file in `internal/` (grep confirms zero exist today).

**Primary recommendation:** Execute in the audit's priority order, but cluster by **shared file** to obey refactor-on-touch and one-commit-per-cluster. The single highest-leverage cluster is **H4+H5+L3+M-f** (all in `shell_exec.go`/`result.go`) — it kills the "shell never answers" bug. For the H9 streaming contract, **add `Err error` to `llm.Chunk` and emit-before-close** (Option a) — it has a small, enumerable blast radius (6 consumers) and uniquely also covers the clean-EOF-no-finish_reason case at `sse.go:114-122`.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01 — Fix EVERYTHING, zero audit residue.** All 10 HIGH + all 10 MEDIUM + all 6 LOW are in scope. No tier deferred.
- **D-01a — INFO item = logged/accepted, no code change.** Self-installed skill *bundled scripts* not blocklist-scanned (`loader.go:213-220`) is confirmed-deliberate per the full-host-terminal trust model (PRD amendment #50 / D-15c). Resolution = document the trust boundary; do NOT add a scanner.
- **D-01b — Free-rider LOWs ride existing work.** M-i's central `godotenv.Load()` at `main()` auto-fixes the env-ordering LOWs (`aura mcp doctor` whatsapp-URL, `agent dry-run`/`swarm-demo` `AURA_LOOP_*`/`AURA_SWARM_MAX_DEPTH`). L3 (opaque cancel status) lands in the SAME `shell_exec.go` edit as H4/H5. Plan accordingly to avoid double-touch.
- **D-02 — Build minimal-real, all four contract findings (H6/H7/H10/M-g). NO doc-downgrade.** "Minimal" governs the *shape* (smallest thing that genuinely works, no supervisor/ping), not the *ambition* (it must actually function).
  - **H6**: persist a notification-defer state (`notify_after` / equivalent) + a tick sweep that flushes deferred notifications at quiet-hours window end.
  - **H7**: persist undelivered-state for a failed MCP self-send + bounded re-attempt on a later tick.
  - **H10**: build the lazy reconnecting Server wrapper — re-open + `initialize` once on transport error, then retry; clean tool error on a second failure. Refresh the dead tool's boot-time description on reconnect.
  - **M-g**: wire `ReschedulesOnRecovery` into `catchUpMissed`.
- **D-03 — Two-layer proof: regression for ALL, live real-agent repro for user-visible.**
  - **Layer 1 (every finding):** a fails-before / passes-after regression test = the committed CI proof. No-skip-as-green.
  - **Layer 2 (user-observable findings only):** a live before/after repro driven by the real paid agent with a real user prompt — no babysitting / no canned scripted inputs. Surfaces: `aura chat` host loop, Telegram CDP, cron tick, AG-UI SSE.
  - **Non-observable findings → regression-only** (M-j, L5, L4, M-h).
  - **Live pass = required operator sign-off gate, NOT CI-automated.** Mirror Phase 13-10 / Phase 8 live sign-off doc pattern; CI runs only Layer 1.
- **D-04 — Rewrite the named false-green tests as broken + wire the orphan fixture.** Targets: `TestFanoutSlowSubscriberDropped` (H1), `TestShellExecTimesOut` (H4), `context_boundary_test.go` fixtures (H8), the orphaned `testdata/premature_close.sse` (H9 — consume it, do NOT delete), the stale `serve_channels.go` comment (H2).

### Claude's Discretion
- **H9 streaming-contract approach:** add `Err error` to `llm.Chunk` and emit-before-close vs. treat a no-`finish_reason`+non-nil-parse-error stream as retryable infra failure. **→ RESEARCH RECOMMENDS Option (a): add `Err error` to `llm.Chunk`** (see H9 section + blast-radius table).
- **Wave sequencing:** the audit names H4+H5+M-b as the single highest-leverage bundle. Strong candidate for wave 1.
- **Commit granularity:** one commit per finding, or per tight cluster.
- **M-a / M-b stream-retry consolidation:** route completion-critic, finalize, and reasoning-router stream-opens through the shared `streamWithOpenRetry` helper.

### Deferred Ideas (OUT OF SCOPE)
- None — the phase deliberately absorbs the entire audit (D-01). Any net-new capability that surfaces during planning belongs in its own phase.
</user_constraints>

<phase_requirements>
## Phase Requirements

There are no `REQUIREMENTS.md`-style IDs for this phase; **each audit finding IS a requirement to clear.** The planner should treat the finding id (H1…H10, M-a…M-j, L1…L6) as the requirement id. Mapping of each finding to the research support that enables its fix:

| ID | Description (audit) | Research Support |
|----|---------------------|------------------|
| H1 | SSE backpressure drops protocol BOUNDARY frames | AG-UI `EventType` constants enumerated (events.go:14-71); `ValidateSequence` signature confirmed (events.go:229); translator-emitted frame set enumerated; `isLifecycleFrame` shared by both `server.go:264` and `fanout.go:113` |
| H2 | Turn errors swallowed on Telegram (no RunErrorEvent case) | `renderer.consume` switch confirmed lacking a `*events.RunErrorEvent` case (renderer.go:81-97); `sanitizeErr` reuse seam at server.go:392; stale comment located at serve_channels.go:142 |
| H3 | Async doc-conversion failure swallowed | async callback `asyncResult` logs+returns (bot_dispatch.go:388-395); sync path uses `convertFailMessage` (line 400); `sender`/`notifier` already captured in the closure |
| H4 | Timeout kills only the shell, not children → orphan + hang | Go 1.26 `cmd.Cancel`/`cmd.WaitDelay` semantics verified; no OS-split files exist yet; exact `SysProcAttr` fields per-OS documented |
| H5 | Exit code + stderr dropped on output > preview cap | `NewResult` truncates head-first (result.go:75-84,104); footer appended at tail (shell_exec.go:139-145); `criticResultCap=400` digest path same defect (completion.go:157) |
| H6 | Quiet-hours-deferred notifications permanently dropped | `notify` returns after WARN with no persistence (dispatch.go:154-156); migration pattern + `agent_job_runs` schema + sqlc conventions captured |
| H7 | Failed self-send never retried | `notify` WARN-and-moves-on (dispatch.go:158-160); `Notifier.Notify` returns undelivered error (notify.go:90-102); shares H6's persistence model |
| H8 | microcompact 2-stride drop corrupts tool history | `dropOldestPairs` body[2:] assumes user/assistant alternation (context.go:253-260); `role='tool'` is first-class (applyL1); all boundary-test fixtures are user/assistant-only |
| H9 | SSE parse error swallowed → partial answer delivered as complete | `llm.Chunk` has no error field (client.go:75-81); parse error captured to trace only then `close(out)` (client.go:145-166); 6 consumers enumerated; orphan fixture confirmed |
| H10 | MCP reconnect-on-use never implemented | `bridgedTool` captures one `Server` at mount (bridge.go:35-60); `mcp.Client.Open` re-spawnable from `ServerConfig`+name; `Ping` primitive exists (client.go:185); decided design = study §43 |
| M-a | 3 stream-opens bypass `streamWithOpenRetry` | direct `a.client.Stream` at completion.go:94, finalize.go:217, reasoning.go:45; helper at llm_agent_stream_retry.go:20 |
| M-b | veto re-surfaces vetoed prose on budget trip | veto appends durable RoleAssistant (llm_agent.go:260-261); finalize copies history forward (finalize.go:117,202) |
| M-c | Fanout emits/traces UN-sanitized RUN_ERROR | `redactEvent`/`sanitizeString` exist (server.go:403,426); Fanout forwards `err.Error()` verbatim + traces raw JSON (fanout.go:85-95) |
| M-d | `lastUserMessage` drops structured/multimodal content | only `string` content accepted (server.go:323) |
| M-e | `/cancel` during pause doesn't cancel the pause | `/cancel` intercepted before HITL (bot_dispatch.go:114-122); `cancel` only cancels turn ctx (commands.go:167-177); `PendingFor` seam at bot_dispatch_hitl.go:24 |
| M-f | stderr de-interleaved (all after all stdout) | two separate `strings.Builder`s (shell_exec.go:123-125) |
| M-g | `ReschedulesOnRecovery` dead control | flag never consulted in `catchUpMissed` (recover.go:55-82); `HandlerMeta.ReschedulesOnRecovery` defined (dispatch.go:32) |
| M-h | Shutdown mid-run completes on cancelled ctx | `CompleteRun(ctx,…)` with cancelled ctx (dispatch.go:120-134); `context.WithoutCancel` pattern at serve.go:119 |
| M-i | `aura mcp` reads `AURA_MCP_CONFIG` without `.env` | `ManagedConfigPath` reads env at managed_config.go:88; no central `godotenv.Load()` at `main()` (main.go:35) |
| M-j | mcp-neo4j-cypher stderr unbounded buffer | `safeBuffer` never trims (knowledge/client.go:69,317-334); identical `safeBuffer` in mcp/client.go:317 |
| L1 | host snippet exec no timeout | `cmd/aura/skills_snippet.go:112` `context.Background()` |
| L2 | `Writer.Activate`/`DiscardPending` skip `SanitizeName` | writer_activate.go:24, resume.go:66 |
| L3 | ctx-cancel reports opaque status | `shell_exec.go:138-145` — folds into H4/H5 edit |
| L4 | `SearchConversationTurns` no status filter | store.go:482; SQL is a LOCKED cross-slice contract (do not rewrite SQL casually) |
| L5 | `DueTasks` SKIP LOCKED is inert | scheduler_tasks.sql:34 on autocommit pool; `pg_try_advisory_lock` holds correctness |
| L6 | web_search/web_fetch don't reject empty required args | web_search.go:76, web_fetch.go:65 |
</phase_requirements>

---

## Architectural Responsibility Map

Aura is a single-binary Go agent (no multi-tier web stack). The "tiers" here are the internal package boundaries the audit findings cross. Mapping each finding cluster to its owning subsystem prevents the planner from mis-assigning a fix to the wrong package (e.g. trying to fix H1 in the translator when it belongs in the transport/fanout layer).

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Host shell execution (H4/H5/L3/M-f) | `internal/agent/tools` (shell_exec, result) | OS syscall layer (`syscall.SysProcAttr`) | The keystone host tool owns process lifecycle + output rendering; the truncation defect is in the shared `NewResult` it calls |
| AG-UI protocol transport (H1/M-c/M-d) | `internal/agui` (server, fanout) | AG-UI SDK (`events.ValidateSequence`) | Frame drop/redaction/input-validation are transport concerns; the translator stays pure (boundary-tested) |
| Telegram rendering (H2/H3/M-e) | `internal/channels/telegram` | `internal/agui` sanitizeErr (reused) | User-facing surface owns error/notice rendering + pause cancel |
| LLM streaming contract (H9/M-a) | `internal/llm` (Chunk) + `internal/llm/openai_compat` | `internal/agent` (consumers) | The error-reportability gap is a *contract* gap in `llm.Chunk`; the wire layer produces, the agent consumes |
| Agent completion/finalize gate (M-a/M-b) | `internal/agent` (llm_agent*) | `internal/llm` | The veto-prose and bypass-retry defects are agent-loop policy |
| Scheduler durable state (H6/H7/M-g/M-h/L5) | `internal/cron` (dispatch, notify, recover, store_runs) | `internal/db` (new migration 0013 + sqlc) | Persistence of notification/recovery state needs DB schema; the autocommit-lock semantics live in the store |
| MCP reconnect (H10/M-j) | `internal/agent/mcptools` (bridge) | `internal/mcp` (client) | The bridge owns the mounted `Server`; the client owns `Open`/`initialize`/`Ping`/`safeBuffer` |
| Conversation compaction (H8) | `internal/conversations` (context) | — | Pure-function ladder; tool-role awareness is a compaction-algorithm fix |
| Env-load ordering (M-i + env LOWs) | `cmd/aura` (main) | `internal/mcp` (managed_config) | One central `godotenv.Load()` at `main()` is a composition-root concern |
| Operator-CLI hygiene (L1/L2/L4/L6) | `cmd/aura/skills_snippet`, `internal/skills`, `internal/conversations/store`, `internal/agent/tools` | — | Independent small guards, no cross-coupling |

---

## Standard Stack

This phase uses **only the existing stack** — no new third-party dependencies. The "stack" relevant to the fixes is the Go stdlib + the already-vendored SDKs.

### Core (already in go.mod, used by the fixes)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `os/exec` | go 1.26.4 | `cmd.Cancel` + `cmd.WaitDelay` for H4 process-group kill | The canonical, idiomatic way to bound child-process teardown since Go 1.20 [CITED: pkg.go.dev/os/exec] |
| Go stdlib `syscall` | go 1.26.4 | `SysProcAttr{Setpgid}` (POSIX) / `SysProcAttr{CreationFlags}` (Windows) for process-group creation | No third-party process-group lib needed; stdlib covers both OSes [CITED: pkg.go.dev/syscall] |
| Go stdlib `errors` | go 1.26.4 | `errors.AsType[T]` (generic) already used at shell_exec.go:318 | Go 1.26 generic errors helper, already in the codebase |
| `github.com/ag-ui-protocol/ag-ui/sdks/community/go` | v0.0.0-20260514093510 | `events.ValidateSequence` (H1 test), `events.EventType*` constants (H1 classifier) | Already vendored; the conformance check the rewritten fanout test must call [VERIFIED: module cache] |
| `github.com/joho/godotenv` | v1.5.1 | central `_ = godotenv.Load()` at `main()` (M-i) | Already used at config.go:130 + llm/config.go:130; **does NOT override already-set env vars** (load-first-wins) so a central call is idempotent with the existing per-command calls [VERIFIED: go.mod:15] |
| `github.com/jackc/pgx/v5` + sqlc | (in go.mod) | H6/H7 new migration 0013 + generated queries | The locked persistence stack; lazy-error discipline already applied (rows.Err after loops) |
| `golang-migrate` | (dev tool) | 0013_* up/down migration | The locked migration tool; sequential numbering enforced |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `syscall.SysProcAttr` per-OS split | A third-party process-group lib (e.g. `go-cmd`, `gopsutil`) | Adds a dependency for ~30 LOC of stdlib; **rejected** — violates "no new deps" + the audit's mechanical fix is pure stdlib |
| H9 `Err error` field on `Chunk` | Sentinel "retryable infra failure" inference at the consumer | See H9 section — the field is cleaner, smaller blast radius, and covers the clean-EOF case the inference misses |
| New `0013` migration for H6/H7 | Reuse `agent_job_runs` columns (no migration) | The audit explicitly says "no notification-state column exists" (grep-confirmed); a column is required for D-02's "actually function" bar |

**Installation:** None. `go.mod` is unchanged by this phase.

## Package Legitimacy Audit

> **Not applicable.** This phase installs **zero new external packages.** Every library used by the fixes is already in `go.mod` (verified: `go.mod:15` godotenv, AG-UI + pgx + sqlc + tiktoken all present). No registry verification or slopcheck is required because no `go get` / `npm install` / `pip install` runs in this phase. (slopcheck was therefore not run — there is nothing to scan.)

---

## Architecture Patterns

### System Architecture Diagram — the "shell never answers" failure path (H4+H5+M-b)

```
user prompt ──> aura chat host loop (LlmAgent.Loop)
                      │
                      ▼
            assistant emits shell_exec tool_call
                      │
                      ▼
   ShellExec.Execute  ── exec.CommandContext(/bin/sh -c "<cmd>") ──> bash (direct child)
                      │                                                  └─> grandchild (go run / python / npm)
                      │                                                         │ keeps stdout pipe OPEN
              ctx timeout fires                                                 │
                      │                                                         ▼
        [H4 BUG] default Cancel = Kill(bash only) ───X grandchild orphaned, pipe never closes
                      │                                  cmd.Wait() blocks FOREVER (no WaitDelay)
                      ▼                                  ── TURN NEVER RETURNS ──
        [H4 FIX] Setpgid + cmd.Cancel kills WHOLE group + cmd.WaitDelay(2-5s) force-closes fds
                      │
                      ▼
        renderShellOutput + appendShellFooter  (status/[exit code N]/[aura_shell {...}] at TAIL)
                      │
                      ▼
   NewResult(content)  ── len > previewCap(2048)? ──> truncatePreview(content[:cut]) HEAD-FIRST
                      │                                  [H5 BUG] tail footer + exit_code DROPPED
                      ▼                                  model sees head-only stdout, can't see failure
        [H5 FIX] reserve tail bytes for footer before truncating the body
                      │
                      ▼
   model reasons over RoleTool result ──> completion critic (criticResultCap=400, same H5 defect)
                      │
              budget trip ──> finalize() copies history forward
                      │            [M-b BUG] vetoed "here's a script, you run it" prose was appended
                      ▼            durable RoleAssistant → re-emitted as the answer
        [M-b FIX] on content-stop veto, append ONLY the feedback nudge, not the vetoed prose
                      │
                      ▼
              terminal finalEvent (the real answer)
```

### Pattern 1: Build-tag-free OS split via `_unix.go` / `_windows.go` (H4)
**What:** Go's filename-suffix build constraints select the file per-OS without any `//go:build` line. `shell_exec_unix.go` compiles on all non-Windows; `shell_exec_windows.go` on Windows. A shared `shell_exec.go` calls a small per-OS function.
**When to use:** H4 process-group setup + group-kill (`Setpgid` vs `CREATE_NEW_PROCESS_GROUP`/`taskkill`). This is the FIRST OS-split in `internal/` (grep: zero `//go:build (windows|unix|...)` files exist). **Both halves MUST compile** — Aura's CI builds on WSL/Linux AND the dev box runs Windows w64devkit.
**Example:**
```go
// shell_exec_unix.go  (filename suffix _unix → all non-Windows GOOS)
//go:build !windows
package tools
import "os/exec"
import "syscall"
func setProcessGroup(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
// killProcessGroup sends the signal to the whole group: negative pid = the group.
func killProcessGroup(cmd *exec.Cmd) error {
    if cmd.Process == nil { return nil }
    return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // -pgid kills the group
}

// shell_exec_windows.go  (filename suffix _windows → Windows GOOS)
//go:build windows
package tools
import "os/exec"
import "syscall"
func setProcessGroup(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
// killProcessGroup uses taskkill /T to terminate the tree (Windows has no -pgid kill).
func killProcessGroup(cmd *exec.Cmd) error { /* exec taskkill /F /T /PID <pid> */ }
```
[ASSUMED] — the exact Windows group-kill mechanism (taskkill /T vs a Job Object) is an implementation choice; both are documented Windows idioms. The planner should pick taskkill /T for minimal-real (no Job Object machinery), matching D-02's "smallest thing that genuinely works". Verify the chosen mechanism reaps grandchildren in the live H4 test.

### Pattern 2: `cmd.Cancel` + `cmd.WaitDelay` composition (H4 — VERIFIED Go semantics)
**What:** Wire a custom `cmd.Cancel` that kills the whole group, and set `cmd.WaitDelay` so `Wait()` force-closes inherited fds even when an orphan keeps the pipe open.
**Verified Go stdlib semantics** [CITED: pkg.go.dev/os/exec]:
- `Cancel` is called when the command's Context is done (requires `CommandContext`). `CommandContext`'s DEFAULT `Cancel` is `cmd.Process.Kill()` (kills the direct child ONLY — this is the H4 root cause).
- `WaitDelay` timer starts when the Context is done OR `Wait` observes the child exited, whichever first.
- When `WaitDelay` expires: (1) if the child hasn't exited it is `os.Process.Kill`-ed; (2) **then any still-open I/O pipes are closed to unblock goroutines blocked on Read/Write** — this is what unblocks the hung `cmd.Wait()`.
- If pipes are closed due to WaitDelay, no Cancel occurred, and the command otherwise exited successfully, `Wait` returns `exec.ErrWaitDelay` instead of nil.
**Example:**
```go
cmd := exec.CommandContext(runCtx, name, args...)
setProcessGroup(cmd)                       // Pattern 1
cmd.Cancel = func() error { return killProcessGroup(cmd) }  // kill the GROUP, not just bash
cmd.WaitDelay = 5 * time.Second            // force-close fds if a grandchild keeps the pipe
runErr := cmd.Run()
// Note: runErr may now be exec.ErrWaitDelay on a forced-close path — the existing
// exitCodePtr/renderShellOutput must treat that as a timeout/kill, not a crash.
```
**Anti-pattern caught:** the current code (`shell_exec.go:119,126`) relies on the default `CommandContext` Cancel which kills only `bash`. The audit's grep ("zero hits for SysProcAttr/WaitDelay/Cancel") is verified — none exist in the tree.

### Pattern 3: Reserve-tail-before-truncate (H5)
**What:** `NewResult`/`truncatePreview` cut HEAD-first (`content[:cut]`). The shell footer (`[exit code N]`, `[aura_shell {...}]`, stderr block) is appended at the TAIL by `appendShellFooter`/`renderShellOutput`. On output > 2048 bytes the footer is sliced off.
**Fix shape (audit-prescribed):** truncate the stdout *body* before appending the always-keep footer, so the footer survives within the cap. Two viable seams:
  - (a) in `shell_exec.go`: pre-truncate `out`/`stderr` so `rendered` (body+footer) fits the cap before calling `NewResult`. Needs the cap, available via `toolCallCtx(ctx).cap` (result.go:21) — but that's unexported; the cleaner seam is (b).
  - (b) add a `NewResultReservingTail(ctx, body, footer string)` helper in `result.go` that truncates `body` to `cap - len(footer)` then appends `footer`, so the footer is always present. The existing `NewResult` stays for non-shell tools.
**Also fix `criticResultCap=400`:** `sideEffectDigest` (completion.go:157) calls `truncateBytes(results[id], criticResultCap)` head-first on the SAME tool result, so a large *failed* run's exit-code footer is sliced off before the critic sees it → grades DONE. The critic digest must keep the tail too (or the body+footer must already be tail-preserving by the time it reaches the digest).

### Pattern 4: Drop-by-conversational-boundary (H8)
**What:** `dropOldestPairs` does `body = body[2:]` assuming strict user/assistant alternation (context.go:258). But a tool round is `assistant(tool_calls) → tool → tool → assistant` — a 2-stride slices mid-round, leaving an orphan `tool` head with no preceding `assistant` tool_call → provider 500.
**Fix shape (audit-prescribed):** drop by advancing to the next `RoleUser` boundary (so a whole round leaves together), and after reducing, skip any dangling `RoleTool` at the head. The protected-head split (system L0 + always-block, context.go:242-249) is preserved.
**Fixture the test must add (D-04):** a history with `assistant(tool_calls) → tool → tool → assistant` so the 2-stride bug is exercised — every current `context_boundary_test.go` fixture is user/assistant-only (verified: `sizedTurns` and all hand-built `[]Turn` use only System/User/Assistant).

### Pattern 5: Reconnecting Server wrapper (H10 — lazy reconnect-on-use, study §43)
**What:** `bridgedTool` captures one `Server` at mount (bridge.go:36, `srv Server`). `Execute` calls `b.srv.CallTool` against a possibly-dead stdin forever. The decided design [CITED: mcp-sidecar-lifecycle-study.md:43]: "On a tool call whose backing process has exited, attempt one re-spawn+re-init, else a clean tool error. Exactly Claude Code's behavior; no background loop." NO supervisor, NO ping ticker (memory: `reference_mcp_sidecar_lifecycle_and_openclaw_host`).
**Fix shape:**
  - Wrap the mounted `srv` in a `reconnectingServer` that holds the `name` + `mcp.ServerConfig` needed to `mcp.Open` again (the client's `Open(ctx, name, cfg)` is re-callable; `initialize` runs inside it — client.go:64-97).
  - On `CallTool`/`ListTools` transport error: re-open + the new client's `initialize` runs once inside `Open`, then retry the call; a SECOND failure returns a clean tool error (inline `error: ...`, mirroring bridge.go:57).
  - Refresh the dead tool's boot-time description in the registry on reconnect (the audit notes the dead tool keeps its stale description — re-`ListTools` after reconnect and update `bt.spec`).
  - **Reuse the `Ping` primitive** (client.go:185, wired only to `aura mcp doctor` today) as the optional transport-health signal if a pre-call liveness check is wanted — but reconnect-on-error is sufficient and is the minimal-real shape.
**Seam note:** the bridge's `Server` interface (bridge.go:25-28) is `ListTools` + `CallTool`. The wrapper must satisfy the same interface and hold the re-open inputs. The composition root (cmd/aura) is where `ServerConfig` + name are known.

### Pattern 6: Durable scheduler notification state (H6/H7 — new migration 0013)
**What:** `notify` (dispatch.go:143-161) returns after a WARN with NO persistence on two paths: quiet-hours defer (line 154-156) and self-send-undelivered (line 158-160). No notification-state column exists (grep-confirmed). The completed run is terminal and never re-selected.
**Fix shape (audit-prescribed, D-02 build-real):**
  - **New migration `0013_*.up.sql`** following the 0009_scheduler pattern (role-separated grants: `aura_app` DML-only, DDL to `aura_migrate`; partial index on the sweep predicate). Two viable schema shapes:
    - (a) Add columns to `agent_job_runs`: `notify_after timestamptz` (quiet-hours window end), `notify_status text CHECK (IN ('pending','delivered','failed'))`, `notify_attempts int DEFAULT 0`. — but `agent_job_runs` is audit-forever (no DELETE grant) and append-leaning; UPDATE grant exists so columns are workable.
    - (b) A new `aura.pending_notifications` table keyed by run_id with `(notify_after, attempts, last_error, status)` + a partial index `WHERE status='pending'`. Cleaner separation; the sweep query is `SELECT … WHERE status='pending' AND notify_after <= now() FOR UPDATE SKIP LOCKED` (and here SKIP LOCKED is REAL because it runs in a tx, unlike L5).
  - **Tick-sweep:** a new dispatcher pass each tick that selects pending notifications whose `notify_after` has passed (H6 window-end flush) or whose `notify_status='failed'` with `attempts < bound` (H7 bounded re-attempt), re-invokes `Notifier.Notify`, and updates status. Bound the re-attempt count (D-22 "bound-retry").
  - **sqlc conventions to match:** `-- name: X :one|:many|:exec`, `RETURNING` full column list (agent_job_runs.sql precedent), `make_interval(secs => $1)` for interval params (ScanStaleRuns precedent), `int4OrNull`/clamp at the store boundary (store_runs.go:82 precedent). Generated into `internal/db/sqlc` via `sqlc.yaml` (pgx/v5, emit_interface).
  - **Honor pgx lazy-error discipline** (CONTEXT established pattern): `rows.Err()` after the sweep loop, SQLSTATE re-classification, `WithTx` rollback discipline.

### Anti-Patterns to Avoid
- **Don't rewrite the L4 `SearchConversationTurns` SQL** — it is a LOCKED cross-slice FTS contract (store.go:478-481, "The SQL is never rewritten here"). L4's fix (`status <> 'deleted'`) must be added only "before any soft-delete path ships"; the finding is latent (no command sets `StatusDeleted` today). The planner should treat L4 as a minimal guarded addition, not a contract rewrite.
- **Don't add a background MCP supervisor/ping-ticker for H10** — the study §43 + memory explicitly forbid it (over-engineering trap; violates mini-PC no-busy-poll).
- **Don't delete `testdata/premature_close.sse`** (D-04) — the H9 test must consume it. It is a truncated mid-stream body (verified: ends mid-token `"senza\n` with no `[DONE]`, no `finish_reason`).
- **Don't widen the AG-UI translator for M-c** — sanitize at the Fanout boundary (the translator is boundary-tested and stays pure). `redactEvent`/`sanitizeString` already exist server-side; reuse on the Fanout path.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| AG-UI frame sequence validation (H1 test) | A custom "did the sub-sequence stay valid" checker | `events.ValidateSequence([]Event)` (events.go:229) | The SDK's public conformance check is exactly what a real client runs; it tracks active runs/messages/tool-calls and rejects orphaned deltas ("cannot add content to message that was not started") |
| Child-process group teardown (H4) | A manual PID-tree walker + per-pid kill loop | `syscall.SysProcAttr{Setpgid}` + `cmd.Cancel` + `cmd.WaitDelay` | Stdlib bounds the teardown and force-closes fds; a hand-rolled walker races the orphan and still can't unblock `Wait()` |
| Error redaction on the Fanout path (M-c) | A new redactor | existing `sanitizeString`/`redactEvent` (server.go:403,426) | DSN/token/bearer patterns already covered + tested; reuse the chokepoint |
| Telegram error message (H2) | A new error formatter | existing `sanitizeErr` (server.go:392) | Same sanitization chokepoint the HTTP path uses; one redaction contract |
| Stream-open retry (M-a) | Per-call-site retry loops | existing `streamWithOpenRetry` (llm_agent_stream_retry.go:20) | Already classifies retryable network errors (wsarecv/reset/EOF); the 3 bypass sites just need to call it |
| `.env` loading (M-i) | Per-subcommand env readers | one `_ = godotenv.Load()` at `main()` start | godotenv is load-first-wins (won't override set vars), so a single central call is idempotent and closes the whole class |

**Key insight:** Every fix in this phase has an **existing reusable seam** in the codebase or stdlib. This is a remediation phase — building anything bespoke is a smell. The only genuinely new code is the H6/H7 migration + sweep (which still follows the 0009_scheduler template) and the H10 reconnecting wrapper (which still uses the existing `mcp.Open`/`initialize`/`Ping`).

---

## Runtime State Inventory

> This is a remediation phase with ONE schema change (H6/H7 migration 0013). Most findings are code-only. The relevant runtime-state question: *after the code is fixed, what persisted/live state needs migration vs. code-only?*

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | **H6/H7 new notification-state** (`notify_after`/`pending_notifications`) — net-new schema in migration 0013. No EXISTING rows to backfill (the column/table doesn't exist yet; pre-fix deferred notifications were already lost, not recoverable). | New migration (code + DDL). No data migration of existing rows — there is no prior state to migrate. |
| Live service config | **None.** No external service config embeds anything renamed here. The MCP `servers.json` (M-i) is read differently after the fix but its CONTENT is unchanged. | None — verified: M-i changes WHICH file is read (`.env`'s `AURA_MCP_CONFIG`), not the file content. |
| OS-registered state | **None.** No Task Scheduler / pm2 / systemd state. H4 changes process-GROUP creation at runtime; nothing is OS-registered. | None — verified by grep (no scheduler/launchd/pm2 references in scope). |
| Secrets/env vars | **None renamed.** M-i makes `AURA_MCP_CONFIG` (and `AURA_LOOP_*`/`AURA_SWARM_MAX_DEPTH` for the free-rider LOWs) *visible* to operator subcommands via central `.env` load — same var NAMES, just loaded earlier. | Code-only (one `godotenv.Load()` line). No key rename. |
| Build artifacts / installed packages | **None.** No package rename, no egg-info, no binary-name change. `go.mod` is unchanged (no new deps). The new `0013` migration regenerates `internal/db/sqlc/*` via `sqlc generate` — a build-step, committed to git. | Run `sqlc generate` after writing 0013 + the new query file; commit the regenerated sqlc output. |

**The canonical question — after every file is fixed, what runtime systems still hold stale state?** Answer: only the DB schema, via the new 0013 migration (which must be applied with `aura db migrate` before the H6/H7 live test). Everything else is code-only. The live H6/H7 test must run against a freshly-migrated DB.

---

## Common Pitfalls

### Pitfall 1: Cross-platform process-group test must assert the GRANDCHILD is dead (H4)
**What goes wrong:** `TestShellExecTimesOut` only asserts the `[command timed out]` marker appears (shell_exec_test.go:190) — that marker fires even when the grandchild is still alive and the pipe is still held (the false-green). A naive rewrite that just checks the marker survives the bug.
**Why it happens:** the marker is set from `runCtx.Err()` (DeadlineExceeded), which is true the instant the timeout fires, independent of whether the kill worked.
**How to avoid:** spawn a command that backgrounds a grandchild writing its PID to a temp file (e.g. `sh -c 'sleep 30 & echo $! > pidfile; sleep 30'`), then after Execute returns, assert the grandchild PID is **not** alive (`syscall.Kill(pid, 0)` returns ESRCH on POSIX; `os.FindProcess`+signal-0 / `tasklist` on Windows). Cross-platform: gate the POSIX form behind `shellIsCmd()` and provide the cmd.exe equivalent (the test already has the `shellIsCmd()` switch precedent at shell_exec_test.go:109).
**Warning signs:** a sub-second "timeout" test runtime, or a test that passes without ever reading a pidfile.

### Pitfall 2: `exec.ErrWaitDelay` is a NEW error class the render path must classify (H4)
**What goes wrong:** after wiring `cmd.WaitDelay`, `cmd.Run()` can return `exec.ErrWaitDelay` on the forced-fd-close path. `renderShellOutput`/`exitCodePtr` (shell_exec.go:294-336) only branch on `DeadlineExceeded`, `*exec.ExitError`, and nil — `ErrWaitDelay` falls into the `[command failed: %v]` arm and leaks an internal Go error string to the model.
**How to avoid:** add an `errors.Is(runErr, exec.ErrWaitDelay)` branch that renders as a timeout/kill (it IS a timeout consequence), not a crash. Set `timed_out: true` for it.
**Warning signs:** the model sees `[command failed: exec: WaitDelay expired before I/O complete]`.

### Pitfall 3: Adding `Err` to `llm.Chunk` is a struct-field add — every consumer compiles unchanged, but only some HANDLE it (H9)
**What goes wrong:** adding `Err error` to the struct (client.go:75) is backward-compatible at compile time (existing `range ch` loops ignore the new field). The risk is the OPPOSITE: a consumer that should now check `c.Err` but doesn't, so the error is still swallowed.
**How to avoid:** the primary loop (llm_agent.go) and the 3 stream-drainers (completion.go:99, finalize.go:223, reasoning.go:51) must each be reviewed: which should propagate `c.Err`? The main loop must surface it (so the partial answer is NOT delivered as complete); the critic/router can fail-open as they already do. See the H9 blast-radius table.
**Warning signs:** the H9 live test still delivers a truncated answer as complete.

### Pitfall 4: H8 fixture must produce an ORPHAN tool head, not just a tool turn (H8)
**What goes wrong:** a fixture with a tool turn somewhere in the middle won't trip the bug — the bug is the 2-stride leaving a `RoleTool` at the HEAD of `body` with no preceding `assistant`. The fixture must be sized so the drop loop slices exactly between an `assistant(tool_calls)` and its `tool` results.
**How to avoid:** build `[system, assistant(tool_calls), tool, tool, assistant, user, assistant]` sized so dropping the oldest "pair" lands mid-round; assert the reduced `toMessages` output has NO leading orphan `RoleTool` (the wire-invalid shape). Assert the round stays intact.

### Pitfall 5: The H6/H7 live test needs a real cron tick + real quiet-hours window (Layer 2)
**What goes wrong:** unit-testing the sweep query proves Layer 1, but the live D-03 bar is "the user actually learns the job ran". A scripted test that calls the sweep directly is not the live repro.
**How to avoid:** Layer 2 = configure `AURA_SCHEDULER_QUIET_HOURS` to cover now, schedule a job, let the tick fire (run completes, notification deferred to window end), then advance past the window and confirm the deferred notification is delivered (DB `notify_status='delivered'` + the stdout/route sink received it). H7: induce a self-send failure (no MCP route mounted), confirm `notify_status='failed'`, then confirm a later tick re-attempts within the bound.

---

## Code Examples

### H1 frame classifier — enumerate the exact non-droppable set (verified constants)
```go
// Source: AG-UI SDK events.go:14-71 (module cache) + translator-emitted set
// isLifecycleFrame must treat ALL START/END/RESULT/CUSTOM (boundary) frames as
// non-droppable; ONLY repeatable delta frames may drop.
func isLifecycleFrame(t events.EventType) bool {
    switch t {
    // Run lifecycle (already covered):
    case events.EventTypeRunStarted, events.EventTypeRunFinished, events.EventTypeRunError,
        // Message boundaries (currently DROPPED — the H1 bug):
        events.EventTypeTextMessageStart, events.EventTypeTextMessageEnd,
        // Tool-call boundaries + result:
        events.EventTypeToolCallStart, events.EventTypeToolCallEnd, events.EventTypeToolCallResult,
        // Reasoning boundaries:
        events.EventTypeReasoningStart, events.EventTypeReasoningMessageStart,
        events.EventTypeReasoningMessageEnd, events.EventTypeReasoningEnd,
        // Custom (artifact aura.artifact) + state snapshot are boundary/whole frames:
        events.EventTypeCustom, events.EventTypeStateSnapshot:
        return true
    default:
        // DROPPABLE deltas only: TEXT_MESSAGE_CONTENT, TOOL_CALL_ARGS,
        // REASONING_MESSAGE_CONTENT, STATE_DELTA.
        return false
    }
}
```
**Translator-emitted frame set** (verified translator.go): `RUN_STARTED`, `RUN_ERROR`, `RUN_FINISHED`, `TOOL_CALL_RESULT`, `CUSTOM`(artifact), `STATE_DELTA`, `TEXT_MESSAGE_{START,CONTENT,END}`, `REASONING_{START, MESSAGE_START, MESSAGE_CONTENT, MESSAGE_END, END}`, `TOOL_CALL_{START,ARGS,END}`. Only `*_CONTENT`, `TOOL_CALL_ARGS`, `STATE_DELTA` are repeatable deltas safe to drop. **The classifier is shared** by both `server.go:264` (SSE pump) and `fanout.go:113` (in-process) — fix it once, both consume it.

### H1 test — call ValidateSequence on the surviving sub-sequence (D-04)
```go
// Source: events.ValidateSequence signature (events.go:229)
// The rewritten TestFanoutSlowSubscriberDropped must re-validate the survivors,
// not just assert len<=want + first/last.
read := drain(slow)                 // the frames the slow subscriber actually got
if err := events.ValidateSequence(read); err != nil {
    t.Fatalf("surviving sub-sequence is protocol-invalid (H1 regression): %v", err)
}
```

### H2 — add the missing RunErrorEvent case (verified consume switch lacks it)
```go
// Source: renderer.go:81-97 consume() — switch has NO *events.RunErrorEvent case today.
func (r *renderer) consume(ctx context.Context, ch <-chan events.Event) {
    for ev := range ch {
        if ctx.Err() != nil { return }
        switch e := ev.(type) {
        case *events.TextMessageContentEvent:
            r.buf.WriteString(e.Delta); r.flush(ctx, false)
        case *events.TextMessageEndEvent:
            r.flush(ctx, true)
        case *events.RunErrorEvent:               // <-- H2 FIX
            r.sendError(ctx, sanitizeErr-equivalent(e.Message)) // reuse the sanitize chokepoint
        case *events.RunFinishedEvent:
            r.flush(ctx, true)
        }
    }
    r.flush(ctx, true)
}
```
Note: `sanitizeErr` takes an `error`; the renderer has a `Message string` — call `sanitizeString` (the inner helper, server.go:403) or expose a string-taking variant. M-c needs the same string-level sanitize on the Fanout path, so a shared exported `SanitizeString` is the natural seam.

### M-a — route the 3 bypass sites through streamWithOpenRetry
```go
// Source: 3 direct calls — completion.go:94, finalize.go:217, reasoning.go:45.
// Each is `a.client.Stream(ctx, req)`; replace with the bounded-retry helper:
ch, err := a.streamWithOpenRetry(callCtx, req, requestID)  // requestID per call site
```
The helper already exists (llm_agent_stream_retry.go:20) and classifies retryable open errors. The critic/finalize/router keep their existing fail-open/stub behavior on the FINAL error.

### M-b — append only the nudge, not the vetoed prose (content-stop path)
```go
// Source: llm_agent.go:259-263 — today appends BOTH the answer (durable RoleAssistant)
// and the feedback. finalize.go:117,202 then copy that RoleAssistant forward.
if veto, feedback := a.gateCompletion(ic, answer); veto {
    // M-b FIX: do NOT append the vetoed answer as RoleAssistant. Append only the
    // user-role nudge so finalize() can't copy the vetoed prose forward.
    a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: feedback})
    continue
}
```
**Caution:** the wire must stay valid. The content-stop path has no tool_call to attach to, so removing the RoleAssistant is fine here (a user-role nudge after an assistant content turn is valid). The text_response/tool path (dispatch, llm_agent.go:315+) uses a RoleTool result instead — verify that path's veto is NOT the M-b leak (the audit names only the content-stop + finalize copy-forward path).

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `exec.CommandContext` default Cancel (kills direct child only) | `cmd.Cancel` + `cmd.WaitDelay` for bounded group teardown | Go 1.20 (WaitDelay/Cancel fields) | The idiomatic way to avoid the exact H4 hang since 2023; the codebase predates adopting it here |
| `errors.As(err, &target)` | `errors.AsType[T](err)` generic | Go 1.25/1.26 | Already used at shell_exec.go:318 — the new H4/H5 error classification should match this style |
| Per-command `godotenv.Load()` | central `_ = godotenv.Load()` at `main()` | M-i fix | Closes the whole operator-subcommand `.env`-invisibility class in one line |

**Deprecated/outdated:** none relevant — the stack is current (Go 1.26.4, AG-UI SDK 2026-05, pgx v5).

---

## Validation Architecture

> **Nyquist enabled** (`config.json: workflow.nyquist_validation: true`). This section is mandatory and is the core deliverable for D-03.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (+ `go.uber.org/goleak` for goroutine leak checks, already used in agui/fanout_test.go) |
| Config file | none (Go convention); tag-gated tiers via `//go:build` constraints |
| Quick run command | `go test ./internal/<package>/` (per-package, sub-second for unit tiers) |
| Race tier | `go test -race ./internal/<package>/` (WSL native CGO, or Windows w64devkit with `BASH_ENV=~/.aura-toolchain.sh`) |
| Full suite command | `go test -tags 'db_integration neo4j_integration' ./...` with composed DSNs + PATH for mcp-neo4j-cypher (see memory `reference_db_knowledge_integration_test_invocation`) |
| Coverage gate | `make coverage` → owned-surface floor **≥85%** (CLAUDE.md hard floor, overrides PRD 75/60) |

### Two-Layer Proof Model (D-03)

**Layer 1 — committed CI regression (EVERY finding, fails-before / passes-after).** No-skip-as-green: any integration tier `t.Fatal`s under `$CI` when its env is unset.

| Finding | Test tier | Realistic fixture | EXACT assertion (the fails-before / passes-after proof) | False-green to rewrite (D-04) |
|---------|-----------|-------------------|--------------------------------------------------------|-------------------------------|
| H1 | unit (agui) | slow subscriber overflowed past cap-64 | `events.ValidateSequence(survivors) == nil` AND a START frame is never among dropped | **`TestFanoutSlowSubscriberDropped`** (today only `len<=want`+first/last) |
| H2 | unit (telegram) | a `RunErrorEvent` on the consume channel | the user-facing send contains the sanitized reason (not a bare "Stato: errore"); golden `testdata/statuspane_run_error.golden` matches | stale comment `serve_channels.go:142` corrected |
| H3 | unit (telegram) | async convert callback with `convErr != nil` | `convertFailMessage` is sent via the captured sender (assert the fake sender received it) | — |
| H4 | unit+race (tools) | `sh -c 'sleep 30 & echo $!>pid; sleep 30'`, 200ms timeout | the grandchild PID is **dead** after Execute returns (`Kill(pid,0)`→ESRCH) AND Execute returns within timeout+WaitDelay | **`TestShellExecTimesOut`** (today only marker) |
| H5 | unit (tools) | stdout > 2048 bytes + non-zero exit | the RoleTool result CONTAINS the `[exit code N]`/`[aura_shell {...exit_code...}]` footer; the critic digest also keeps it | — |
| H6 | integration (db_integration, cron) | scheduled job + quiet-hours window covering now | after tick: a pending notification row with `notify_after`=window end; after window: sweep delivers it (`notify_status='delivered'`) | — |
| H7 | integration (db_integration, cron) | self-send failure (no MCP route) | `notify_status='failed'`, `attempts` increments; a later tick re-attempts within the bound; bound exhaustion stops re-selection | — |
| H8 | unit (conversations) | `[system, assistant(tool_calls), tool, tool, assistant, …]` over cap | reduced `toMessages` has NO leading orphan `RoleTool`; the round drops whole; `ValidateSequence`-equivalent wire shape holds | **`context_boundary_test.go`** fixtures (today user/assistant-only) |
| H9 | unit (openai_compat) | **`testdata/premature_close.sse`** (orphan fixture, truncated mid-stream, no finish_reason) | a stream that ends with a parse error / no finish_reason surfaces `c.Err != nil` (Option a) so the consumer does NOT accept partial text as complete | orphan fixture **consumed** (D-04) |
| H10 | unit (mcptools) | a fake `Server` that errors once then succeeds after re-open | `CallTool` re-opens+`initialize`+retries on the 1st transport error; a 2nd consecutive failure → clean inline tool error; description refreshed | — |
| M-a | unit (agent) | a fake client whose Stream fails transiently once | completion/finalize/reasoning each retry via the shared helper (assert 2 open attempts) | — |
| M-b | unit (agent) | content-stop veto then budget trip → finalize | finalize output does NOT contain the vetoed "you run it" prose; history has no durable vetoed RoleAssistant | — |
| M-c | unit (agui) | Fanout RUN_ERROR carrying a DSN/token | the emitted event Message + the reasoning trace are sanitized (no DSN/bearer) | — |
| M-d | unit (agui) | a `[]InputContent` (multimodal) last user message | explicit rejection or text-part projection (NOT a silent skip driving over rehydrated history) | — |
| M-e | unit (telegram) | `/cancel` while `PendingFor` is non-empty | `/cancel` routes through `SubmitAnswer(…ActionCancel)`; the `paused_states` row is resolved; inline keyboard cleared | — |
| M-f | unit (tools) | interleaved stdout+stderr command | output preserves temporal interleave (single synchronized writer), not all-stderr-after-all-stdout | — |
| M-g | unit (cron) | overdue task with `ReschedulesOnRecovery=false` | `catchUpMissed` does NOT re-fire it (flag consulted); `=true` still fires | — |
| M-h | integration (db_integration, cron) | shutdown mid-run (cancelled root ctx) | terminal state written via `context.WithoutCancel`+short deadline (run not stuck `running`) | — |
| M-i | unit (cmd/aura) | `.env` with `AURA_MCP_CONFIG` set, env unset | `aura mcp` dispatch reads the `.env` value (central `godotenv.Load()` ran); free-rider env LOWs also resolve | — |
| M-j | unit (knowledge) | a chatty sidecar writing > ring size to stderr | RSS bounded (ring buffer trims); `stderrTail()` still returns last 200B | — |
| L1 | unit (cmd/aura) | runaway snippet | `skills snippet exec` enforces a timeout + SIGINT | — |
| L2 | unit (skills) | `Activate`/`DiscardPending` with a traversal-shaped name | `SanitizeName` guard rejects before `os.RemoveAll` | — |
| L3 | unit (tools) | ctx-cancel (not deadline) | distinct `[command cancelled]` status (folded into H4/H5 edit) | — |
| L4 | unit (conversations) | a `status='deleted'` conversation's turns | excluded from search (guard added, SQL contract not rewritten) | — |
| L5 | unit (cron) | — | SKIP LOCKED dropped or select+claim in one tx; correctness still held by advisory lock | — |
| L6 | unit (tools) | empty `query`/`url` | rejected with "query required" / "url required" before reaching SearXNG | — |

**Layer 2 — operator-sign-off live repro (user-observable findings ONLY, NOT CI).** Real paid agent + real user prompt, no scripted inputs (D-03 + memory `feedback_no_unsolicited_paid_runs_batch_calls` — batch the live runs, get explicit go before paid runs).

| User-observable finding | Live surface | Harness | Live repro shape |
|-------------------------|--------------|---------|------------------|
| H4 / H5 / M-b ("shell never answers") | `aura chat` host loop | `reference_run_aura_binary_live_env_loading` + `reference_live_tool_selection_trace` (`· <toolname>` trace = ground truth) | prompt that triggers a shell command spawning a backgrounded grandchild; before: turn hangs / lies about success / hands off; after: real answer with correct exit-code awareness |
| H9 (SSE answer truncation) | `aura chat` host loop / AG-UI SSE | `aura chat` + a mid-stream-cut provider (or the orphan fixture in a live-shaped harness) | before: partial answer delivered as complete; after: error surfaced / retried, not silently truncated |
| H2 (error render) | Telegram | CDP harness `D:/tmp/tg_cdp.py` (`reference_cdp_telegram_live_test_harness`) | induce a turn error (model unavailable / DB error); before: bare "Stato: errore"; after: sanitized reason rendered; DB = ground truth |
| H3 (doc-conversion silence) | Telegram | CDP harness | send a 5–50MB doc that fails async conversion; before: "elaborando…" then silence forever; after: `convertFailMessage` arrives |
| M-e (/cancel during pause) | Telegram | CDP harness | trigger an ask_user pause, send `/cancel`; before: pause orphaned, keyboard stays live; after: pause cancelled, keyboard cleared |
| H6 / H7 (scheduler notify) | cron tick | live daemon + DB ground truth (`reference_e2e_full_matrix_invocation`) | schedule a job inside quiet hours (H6) / with a failing route (H7); before: notification lost; after: delivered at window end / retried within bound |
| H1 (frame drop) | AG-UI SSE | a deliberately-slow SSE client against `aura serve` | before: a conformant client rejects the whole turn ("cannot add content…"); after: `ValidateSequence` passes on the received stream |

**Non-observable findings → Layer-1 regression-only:** M-j (unbounded stderr — RSS, not user-visible), L5 (inert SKIP LOCKED — defense-in-depth), L4 (search status filter — latent, no command sets StatusDeleted), M-h (shutdown-ctx terminal write — internal run-state), L1/L2/L3/L6/M-c/M-d/M-f/M-g/M-a/M-b-regression-half (internal/latent). M-b's USER-observable half is bundled with H4/H5 above.

### Nyquist Sampling Dimensions Covered
- **Per-task commit:** `go test ./internal/<touched-package>/` + `go vet` + `go build` (CLAUDE.md post-edit validation) + `go test -race` on the touched package.
- **Per-wave merge:** the affected packages' full unit tier + `make quality` (vet/build/file-size/lint+dupl/test-race/vuln).
- **Phase gate:** `make quality-full` (incl. `db_integration`/`neo4j_integration` coverage, stack up) green + owned-surface coverage ≥85% across the full tag matrix + the Layer-2 live sign-off doc recorded.
- **Coverage floor:** 85% owned-surface (CLAUDE.md), reported across unit+integration+smoke — a bare unit-only number under 85% is not an acceptable closing metric.

### Wave 0 Gaps
- [ ] `internal/db/migrations/0013_*.{up,down}.sql` + `internal/db/queries/<notifications>.sql` — covers H6/H7 (the only net-new schema). Run `sqlc generate` and commit `internal/db/sqlc/*` regeneration.
- [ ] `internal/agent/tools/shell_exec_unix.go` + `shell_exec_windows.go` — the first OS-split files in `internal/` (H4). Both halves MUST compile on WSL/Linux CI AND Windows w64devkit.
- [ ] A cross-platform grandchild-liveness test helper (H4) — assert PID death, not just the marker.
- [ ] Wire `testdata/premature_close.sse` into a new `sse_test.go`/`client_test.go` case (H9, D-04) — currently referenced by zero `.go` files.
- [ ] An `assistant(tool_calls)→tool→tool→assistant` fixture in `context_boundary_test.go` (H8, D-04).

*(No new framework install needed — Go `testing` + goleak are already present.)*

### Sign-off evidence recording (mirror Phase 13-10 / Phase 8)
Record Layer-2 live evidence in a phase VALIDATION/sign-off doc under `docs/` (NOT `/tmp` — memory `feedback_no_docs_in_tmp`), mirroring `project_phase13_10_live_signoff_resume` and `reference_validate_phase_procedure`: before/after body inspection (≥1 visual print, mojibake scan), DB/filesystem ground-truth assertions (not r.Reply), and the `· <toolname>` trace for shell repros. CI runs only Layer 1; the live pass is the operator gate.

---

## Free-Rider Folding + Wave-Sequencing Input (D-01b)

**Findings that MUST be touched together (single commit per cluster, avoid double-touch):**

| Cluster | Findings | Shared file(s) | Rationale |
|---------|----------|----------------|-----------|
| **Wave-1 candidate (highest leverage)** | H4 + H5 + L3 + M-f | `shell_exec.go`, `result.go`, new `shell_exec_{unix,windows}.go` | All in the shell tool; refactor-on-touch means one edit pass. L3 (cancel status) + M-f (single writer) are tiny riders on the H4/H5 edit. |
| **Veto/never-answer** | M-b (+ M-a) | `llm_agent.go`, `llm_agent_finalize.go`, `llm_agent_completion.go`, `llm_agent_reasoning.go`, `llm_agent_stream_retry.go` | M-b is the prose-leak; M-a routes the 3 bypass stream-opens through the shared helper. Same agent-loop files; the audit bundles H4+H5+M-b but M-b's code lives with M-a. |
| **AG-UI transport** | H1 + M-c + M-d | `agui/server.go`, `agui/fanout.go` | One classifier (`isLifecycleFrame`) serves H1 on both pump+fanout; M-c sanitize + M-d input-validation are in the same two files. |
| **Telegram surface** | H2 + H3 + M-e | `telegram/renderer.go`, `bot_dispatch.go`, `commands.go` | H2 (RunErrorEvent render), H3 (async notice), M-e (/cancel→SubmitAnswer) — all user-facing telegram. H2 reuses the sanitize seam M-c also touches → coordinate the shared `SanitizeString` export. |
| **Scheduler contract** | H6 + H7 + M-g + M-h + L5 | `cron/dispatch.go`, `notify.go`, `recover.go`, `store_runs.go` + new migration 0013 + new query file | The whole scheduler-persistence cluster; H6/H7 share the migration; M-g/M-h/L5 are the same package. |
| **MCP reconnect** | H10 (+ M-j) | `mcptools/bridge.go`, `mcp/client.go`, `knowledge/client.go` | H10 reconnecting wrapper; M-j bounded ring is the same `safeBuffer` pattern in both `mcp/client.go:317` and `knowledge/client.go` (M-j cites knowledge). |
| **Env-load class** | M-i + free-rider env LOWs (mcp doctor whatsapp-URL, agent dry-run/swarm-demo `AURA_LOOP_*`/`AURA_SWARM_MAX_DEPTH`) | `cmd/aura/main.go` (one `godotenv.Load()` line) | D-01b: the central load auto-fixes the env LOWs. One commit. |
| **H9 stream contract** | H9 | `llm/client.go` (Chunk struct), `openai_compat/{client,sse}.go`, + main-loop consumer | Cross-package (the field add + producer emit + consumer handle); see blast-radius table. |
| **Independent LOWs** | L1, L2, L4, L6, H8 | one file each | No coupling; H8 is `conversations/context.go` alone. |

**Sequencing recommendation:** Wave 1 = shell cluster (H4+H5+L3+M-f) + the veto/never-answer cluster (M-b+M-a) — together they close the named "shell never answers" UX bug and are the user's stated top priority. Wave 2 = error-swallowing surfaces (telegram H2/H3 + agui H1/M-c + H9). Wave 3 = not-wired contracts (scheduler H6/H7/M-g + MCP H10). Wave 4 = env-ordering (M-i) + independent LOWs.

---

## H9 Streaming-Contract Recommendation (Claude's Discretion)

**RECOMMENDATION: Option (a) — add `Err error` to `llm.Chunk` and emit-before-close.**

**Blast radius of adding `Err error` to `llm.Chunk` (struct at client.go:75-81):**

| Consumer | File:line | Currently | Action under Option (a) |
|----------|-----------|-----------|-------------------------|
| Producer (the ONLY producer) | `openai_compat/client.go:134-166` | captures `parseErr` to reasoning trace only, then `close(out)` | `emit(llm.Chunk{Err: parseErr})` before close when `parseErr != nil` OR (EOF with no finish_reason) — also covers `sse.go:114-122` clean-EOF case |
| Main agent loop | `llm_agent.go` (range over Stream) | accepts partial text as final | check `c.Err`; on non-nil, treat as retryable infra failure / surface error (do NOT deliver partial as complete) |
| Completion critic | `llm_agent_completion.go:99-103` | drains Text only | may ignore `c.Err` (already fails-open) — but should stop accumulating on Err |
| Finalize synthesis | `llm_agent_finalize.go:223-230` | drains Text/Usage | may ignore `c.Err` (already retries+stubs) — should treat Err like an empty result |
| Reasoning router | `llm_agent_reasoning.go:51-53` | drains Text | may ignore `c.Err` (already falls back to ReasoningTierLow) |
| Eval harness | `internal/eval/*` (cot_eval, NOT CI) | drains | no change required (build-tag gated, advisory) |
| Fake client | `agent/agenttest/fakeclient.go` | scripted chunks | can now script an `Err` chunk for the H9/M-a tests |

**Why (a) over (b):**
1. **Smaller, enumerable blast radius** — 1 producer + 5 consumers, all in-repo and listed above. The struct-field add is compile-compatible (existing loops ignore the field).
2. **Uniquely covers the clean-EOF-no-finish_reason case** (`sse.go:114-122`): EOF returns nil error today, so option (b)'s "non-nil parse error" inference would MISS the clean-EOF-but-incomplete case. With (a), the producer can emit `Err` for both the parse-error AND the EOF-without-finish_reason path.
3. **Provider-neutral** — the error rides the same channel as Text/Reasoning/Usage; no out-of-band signaling, consistent with the existing trailing-Usage-chunk pattern (client.go:162-165).
4. **The fake client can script it** — making the H9 regression test (consuming `premature_close.sse`) and M-a's transient-failure test straightforward.

The H9 regression test MUST consume `testdata/premature_close.sse` (D-04): feed it through `parseSSE`, assert a chunk with non-nil `Err` is emitted before close, and assert the main-loop consumer does NOT treat the accumulated partial text as the final answer.

---

## Security Domain

> `security_enforcement` is absent from `config.json` (= enabled). This phase is remediation; several findings are themselves security-relevant.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface touched (AG-UI loopback-bind is the existing compensating control, amendment #35) |
| V3 Session Management | no | No session changes |
| V4 Access Control | partial | L2 (`SanitizeName` path-traversal guard before `os.RemoveAll`) is an access-control hardening |
| V5 Input Validation | yes | L6 (reject empty `query`/`url`), M-d (reject/-project multimodal content), L2 (sanitize names) — use the existing validation chokepoints, do not hand-roll |
| V6 Cryptography | no | No crypto |
| V7 Errors & Logging (data leak) | **yes** | M-c (Fanout RUN_ERROR leaks DSN/token into reasoning trace — **live today**), H2 (sanitize before user-facing send) — reuse `sanitizeString`/`redactEvent`, never hand-roll redaction |
| V12 File/Resource | yes | H4 (orphan-process resource leak), M-j (unbounded stderr buffer), L1 (runaway snippet no timeout) |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Credential leak via error string (M-c, H2) | Information Disclosure | `sanitizeString` / `redactEvent` at the boundary (server.go:403,426) — DSN/userinfo/bearer/api-key patterns already covered |
| Path traversal in skill name (L2) | Tampering | `SanitizeName` guard before `os.RemoveAll` (sibling methods already guard) |
| Orphan-process resource exhaustion (H4, L1) | Denial of Service | `cmd.WaitDelay` + process-group kill (H4); `WithTimeout`+`signal.NotifyContext` (L1) |
| Unbounded buffer growth (M-j) | Denial of Service | bounded ring buffer (cap the `safeBuffer`) |
| Protocol corruption from frame drop (H1) | Tampering (protocol integrity) | non-droppable boundary-frame classification + `ValidateSequence` conformance |
| Silent data loss in error path (H3/H6/H7/H9) | Repudiation / Information loss | surface the error/notice; persist deferred/undelivered state (H6/H7) |

**INFO item (D-01a):** the self-installed-skill-bundled-scripts-not-scanned finding is a DELIBERATE trust-boundary decision (amendment #50 / D-15c, full-host-terminal model). Document the boundary explicitly in the phase doc; do NOT add a scanner. This is the one finding with no code change.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all fixes | ✓ | 1.26.4 (go.mod:3) | — |
| Postgres (Docker stack) | H6/H7/M-h integration tier | ✓ (compose) | 5432 | — (integration tier `t.Fatal`s under $CI if down) |
| `mcp-neo4j-cypher` | M-j (knowledge client) + neo4j_integration | ✓ (pipx, WSL `~/.local/bin`) | 0.6.0 | PATH must be prepended |
| `sqlc` | H6/H7 query regeneration | ✓ (WSL `~/go/bin`) | (CI-pinned) | invoke by full path |
| `golang-migrate` | 0013 migration apply | ✓ (`aura db migrate`) | — | — |
| `go-mutesting` | mutation spot-check (Gate-3) | ✓ (WSL only) | go1.26 fork | — |
| Telegram CDP harness | Layer-2 H2/H3/M-e | ✓ | `D:/tmp/tg_cdp.py` + Chrome :9222 | QR login once |
| OpenRouter API key | Layer-2 live paid agent | ✓ (`.env`) | — | batch runs, explicit go before paid |
| SearXNG | (not needed this phase unless L6 live) | ✓ (compose, socat bridge) | — | `SEARXNG_URL=127.0.0.1:18080` |
| Windows w64devkit | H4 cross-platform compile + race | ✓ | — | `BASH_ENV=~/.aura-toolchain.sh` for race |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none blocking — the integration tiers require the Docker stack up (already the project norm); the live tier requires manual harness drive (by design, D-03 operator-sign-off).

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Windows H4 group-kill via `taskkill /F /T /PID` (vs a Job Object) is the minimal-real choice | Architecture Pattern 1 | Low — both are documented Windows idioms; the live H4 test on Windows will reveal if taskkill doesn't reap the tree. Planner verifies in the regression test. |
| A2 | H6/H7 schema choice (new `pending_notifications` table vs columns on `agent_job_runs`) | Architecture Pattern 6 | Low — both work; the new-table option keeps `agent_job_runs` audit-clean and gives a REAL `FOR UPDATE SKIP LOCKED` sweep. Planner picks; either honors the 0009 grant/index conventions. |
| A3 | Removing the durable RoleAssistant in M-b's content-stop path keeps the wire valid | Code Examples (M-b) | Medium — the audit prescribes "append only the feedback nudge". Verify the next-turn request is wire-valid (a user nudge after an assistant content turn is valid; no orphan tool_call). The text_response/tool path uses a RoleTool result and is a separate seam. |
| A4 | The H9 `Err` field is ignored-but-safe in the critic/finalize/router drainers (they already fail-open) | H9 recommendation | Low — they accumulate Text only; an Err chunk carries empty Text so existing logic degrades gracefully. Best practice: stop accumulating on Err. |
| A5 | M-i central `godotenv.Load()` is idempotent with the existing per-command loads (load-first-wins) | Standard Stack | Low — godotenv documented to NOT override already-set vars; verified two existing call sites. |

**If this table seems short:** every other claim in this research is `[VERIFIED]` against a specific repo file:line or `[CITED]` from pkg.go.dev/the AG-UI SDK source in the module cache. The assumptions above are the genuinely open implementation choices the planner/executor settle.

---

## Open Questions

1. **H6/H7 schema: new table vs columns on `agent_job_runs`?**
   - What we know: no notification-state exists; `agent_job_runs` is audit-forever with an UPDATE grant; a new table gives a clean tx-scoped `FOR UPDATE SKIP LOCKED` sweep.
   - What's unclear: which the planner prefers for minimal-real.
   - Recommendation: new `aura.pending_notifications` table — keeps the audit ledger clean and makes the sweep semantics obvious. Either way, follow 0009's role-separated grants + partial index.

2. **H4 Windows group-kill mechanism (taskkill /T vs Job Object)?**
   - What we know: POSIX is `Setpgid` + `Kill(-pgid)`; Windows has no `-pgid` kill.
   - What's unclear: taskkill /T (spawn a process) vs a Win32 Job Object (more code, cleaner).
   - Recommendation: taskkill /F /T /PID for minimal-real (D-02); the live H4 test on Windows is the proof. If taskkill proves flaky, escalate to a Job Object.

3. **M-c shared sanitize export.** H2 (renderer) and M-c (fanout) both need string-level sanitization; `sanitizeErr` takes an `error`, the renderer/fanout have a `string`. Recommendation: export `SanitizeString` (the existing inner helper at server.go:403) and reuse it on both paths — one redaction contract.

---

## Sources

### Primary (HIGH confidence)
- Repository source at HEAD `0e453c7a` (branch `tabula-rasa`) — every finding's `file:line` verified directly:
  - `internal/agent/tools/shell_exec.go`, `result.go`, `shell_exec_test.go`
  - `internal/agui/server.go`, `fanout.go`, `fanout_test.go`; AG-UI SDK `events.go` (module cache, ValidateSequence @229, EventType constants @14-71)
  - `internal/cron/dispatch.go`, `notify.go`, `recover.go`, `store_runs.go`; `internal/db/migrations/` (head = 0012_telegram), `queries/agent_job_runs.sql`, `scheduler_tasks.sql`, `sqlc.yaml`
  - `internal/conversations/context.go`, `context_boundary_test.go`, `store.go`
  - `internal/llm/client.go`, `openai_compat/client.go`, `sse.go`, `testdata/premature_close.sse`
  - `internal/agent/llm_agent.go`, `llm_agent_completion.go`, `llm_agent_finalize.go`, `llm_agent_reasoning.go`, `llm_agent_stream_retry.go`
  - `internal/agent/mcptools/bridge.go`, `internal/mcp/client.go`, `managed_config.go`, `internal/knowledge/client.go`
  - `internal/channels/telegram/renderer.go`, `bot_dispatch.go`, `commands.go`, `bot_dispatch_hitl.go`
  - `cmd/aura/main.go`, `serve_channels.go`; `go.mod` (Go 1.26.4)
- `docs/audit/deep-correctness-audit-2026-06-10.md` — the requirement source (all 26 findings).
- `docs/research/mcp-sidecar-lifecycle-study.md:30-69` — the H10 decided design (§43 lazy reconnect-on-use).
- pkg.go.dev/os/exec — `cmd.Cancel` / `cmd.WaitDelay` / `exec.ErrWaitDelay` semantics [CITED].
- `.planning/phases/19-.../19-CONTEXT.md` — D-01..D-04 + Claude's Discretion.
- `.planning/config.json` — nyquist_validation: true; security_enforcement absent (= enabled).

### Secondary (MEDIUM confidence)
- Project memory entries (live-test harnesses, env-loading gotchas, validation procedure): `reference_cdp_telegram_live_test_harness`, `reference_run_aura_binary_live_env_loading`, `reference_live_tool_selection_trace`, `reference_validate_phase_procedure`, `reference_db_knowledge_integration_test_invocation`, `reference_mcp_sidecar_lifecycle_and_openclaw_host`.

### Tertiary (LOW confidence)
- A1 (Windows taskkill /T mechanism) — documented idiom, not yet verified live in this repo.

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new deps; every library verified in go.mod + usage sites.
- Architecture / fix seams: HIGH — every `file:line` from the audit re-verified; Go stdlib semantics cited from pkg.go.dev.
- Validation architecture: HIGH — test tiers + assertions derived from the verified code + existing test patterns; D-04 false-green targets confirmed false-green by reading them.
- Cross-platform H4 Windows kill: MEDIUM — POSIX path is certain; the Windows group-kill mechanism is an implementation choice (A1).
- H6/H7 exact schema: MEDIUM — pattern certain (follow 0009), table-vs-columns is a planner choice (A2).

**Research date:** 2026-06-10
**Valid until:** 2026-07-10 (stable — internal-code remediation; the only external surface is the AG-UI SDK + Go stdlib, both pinned).

## RESEARCH COMPLETE

**Phase:** 19 - audit-bug-resolution-e2e-live-test
**Confidence:** HIGH

### Key Findings
- **Migration head is `0012_telegram`, not `0011`** — H6/H7's new migration is **`0013`** (audit/CONTEXT were one off). The stale H2 comment is at `serve_channels.go:142` (not :145). Go is **1.26.4** so `cmd.Cancel`/`cmd.WaitDelay`/`errors.AsType[T]` are all available.
- **H4 process-group fix needs the FIRST `//go:build` OS-split in `internal/`** (`shell_exec_{unix,windows}.go`) — both halves must compile on WSL/Linux CI and Windows w64devkit. Verified Go semantics: default `CommandContext` Cancel kills only the direct child (the root cause); `WaitDelay` force-closes the orphan-held pipe and can surface `exec.ErrWaitDelay` (a new error class the render path must classify).
- **H1 classifier scope is precisely enumerable** — the AG-UI SDK has 13 boundary frame types the translator emits; only `*_CONTENT` + `TOOL_CALL_ARGS` + `STATE_DELTA` may drop. `events.ValidateSequence` (events.go:229) is the conformance check the rewritten fanout test must call.
- **H9 recommendation: add `Err error` to `llm.Chunk` (Option a)** — 1 producer + 5 consumers (all enumerated), compile-compatible, and uniquely covers the clean-EOF-no-finish_reason case. The orphan `testdata/premature_close.sse` (confirmed referenced by zero `.go` files) must be consumed by the test.
- **Every fix has an existing reusable seam** (sanitizeErr, redactEvent, streamWithOpenRetry, Ping, context.WithoutCancel, mcp.Open). The only net-new code is the H6/H7 migration+sweep (follows 0009 template) and the H10 reconnecting wrapper (reuses mcp.Open/initialize). No new external packages → no slopcheck needed.

### File Created
`.planning/phases/19-audit-bug-resolution-e2e-live-test/19-RESEARCH.md`

### Confidence Assessment
| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | No new deps; all verified in go.mod + usage |
| Architecture | HIGH | Every audit file:line re-verified; Go semantics cited |
| Validation (Nyquist) | HIGH | Per-finding test tier+assertion mapped; D-04 false-greens confirmed |
| Cross-platform H4 | MEDIUM | Windows group-kill mechanism is an implementation choice (A1) |
| H6/H7 schema | MEDIUM | Pattern certain (0009); table-vs-columns is a planner choice (A2) |

### Open Questions
1. H6/H7 schema: new `pending_notifications` table (recommended) vs columns on `agent_job_runs`.
2. H4 Windows group-kill: taskkill /T (recommended, minimal-real) vs Job Object.
3. M-c/H2: export a shared `SanitizeString` (recommended) for both renderer + fanout paths.

### Ready for Planning
Research complete. The planner can map each finding (H/M/L) to a plan, cluster by shared file per the Free-Rider Folding table, sequence H4+H5+M-b first, and build the two-layer validation (Layer-1 CI regression for all 26 + Layer-2 live operator sign-off for the 7 user-observable clusters).
