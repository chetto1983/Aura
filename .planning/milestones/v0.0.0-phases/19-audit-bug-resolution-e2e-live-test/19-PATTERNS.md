# Phase 19: Audit Bug Resolution + E2E Live Test - Pattern Map

**Mapped:** 2026-06-10
**Files analyzed:** 26 findings → ~24 files touched (most existing-edit), 5 net-new files
**Analogs found:** 26 / 26 (every finding has an in-repo or in-stdlib analog)

## How to read this map

This is a **correctness** phase. For most findings the "closest analog" is **the existing
function being modified plus the sibling that already does it right** (e.g. the HTTP path that
already sanitizes vs the Fanout path that does not). Two classes:

- **existing-edit / reuse-seam** — modify an existing function; the analog is a *sibling already
  doing the right thing* (sanitizeErr, redactEvent, streamWithOpenRetry, isLifecycleFrame). The
  planner's `<action>` is "apply seam X at site Y".
- **net-new** — genuinely new files (H4 OS-split, H6/H7 migration+sqlc, H10 reconnecting wrapper).
  The high-value mapping work is here; each new file is anchored to its closest in-repo template.

All `file:line` are verified against HEAD `0e453c7a` (branch `tabula-rasa`). RESEARCH's three
precision corrections are folded in: **migration head is `0012_telegram` → new is `0013`**; the
stale H2 comment is at **`serve_channels.go:142`** (not :145); Go is **1.26.4**.

---

## File Classification

| Finding | File(s) to create/modify | Role | Data Flow | Class | Closest Analog | Match |
|---------|--------------------------|------|-----------|-------|----------------|-------|
| H1 | `internal/agui/server.go`, `fanout.go` | transport | streaming/event | existing-edit | `isLifecycleFrame` (server.go:264) — widen the same fn | exact (self) |
| H2 | `internal/channels/telegram/renderer.go`; `serve_channels.go:142` (comment) | renderer/component | event-driven | existing-edit + reuse-seam | sibling `sanitizeErr`/`sanitizeString` (agui/server.go:392,403) | role+seam |
| H3 | `internal/channels/telegram/bot_dispatch.go:388` | renderer/component | event-driven (async cb) | existing-edit | sync sibling at bot_dispatch.go:400 (`t.reply(c, convertFailMessage)`) | exact (sibling) |
| H4 | `shell_exec.go` + **new** `shell_exec_unix.go`, `shell_exec_windows.go` | tool / OS-syscall | request-response (process) | **net-new** + edit | spike `replace_{posix,windows}.go` (build-tag shape); Go stdlib `os/exec` Cancel/WaitDelay | role-match (no internal/ precedent) |
| H5 | `internal/agent/tools/result.go`, `shell_exec.go`; `llm_agent_completion.go:157` | utility / tool | transform (truncate) | existing-edit | `truncatePreview`/`NewResult` (result.go:75,94) — add tail-reserving variant | exact (self) |
| H6 | **new** migration `0013`, **new** query file, `cron/dispatch.go`, `store_runs.go` | model/migration + service | CRUD + event-driven | **net-new** + edit | migration `0009_scheduler.{up,down}.sql`; `agent_job_runs.sql`; `store_runs.go` call sites | exact (template) |
| H7 | same as H6 + `cron/notify.go` | service | CRUD + retry | **net-new** + edit | shares H6 migration/sweep; `Notifier.Notify` contract (notify.go:65-72) | exact (template) |
| H8 | `internal/conversations/context.go:237-263` | utility | transform (compaction) | existing-edit | `dropOldestPairs` itself (the protected-head split at :242-249 stays) | exact (self) |
| H9 | `internal/llm/client.go` (Chunk), `openai_compat/client.go:145`, `sse.go`, main-loop consumer | model + transport | streaming | existing-edit (cross-pkg) | trailing-Usage-chunk emit pattern (openai_compat/client.go:162-165) | exact (sibling) |
| H10 | **new** wrapper type in `mcptools/bridge.go` (or `bridge_reconnect.go`); reuse `mcp/client.go` | service/provider | request-response (RPC) | **net-new** + reuse | `bridgedTool` (bridge.go:35-60); `mcp.Open`/`initialize`/`Ping` (client.go:64,121,185) | role-match |
| M-a | `llm_agent_completion.go:94`, `llm_agent_finalize.go:217`, `llm_agent_reasoning.go:45` | service (agent loop) | streaming | reuse-seam | `streamWithOpenRetry` (llm_agent_stream_retry.go:20) | exact (seam) |
| M-b | `llm_agent.go:259-266`, `llm_agent_finalize.go:117,202` | service (agent loop) | event-driven (policy) | existing-edit | the veto append site itself (llm_agent.go:260) | exact (self) |
| M-c | `internal/agui/fanout.go:85-95` | transport | streaming | reuse-seam | HTTP path `redactEvent`/`sanitizeString` (server.go:403,426) | exact (sibling) |
| M-d | `internal/agui/server.go:318-328` | transport | request-response | existing-edit | `lastUserMessage` itself + `payloadString` shape-switch (server.go:300) | exact (self/sibling) |
| M-e | `telegram/bot_dispatch.go:114-122`, `commands.go:167-177`, `bot_dispatch_hitl.go:24` | renderer/component | event-driven | existing-edit | `PendingFor` seam + `SubmitAnswer(…ActionCancel)` (resumeAnswers @ server.go:285) | role-match |
| M-f | `internal/agent/tools/shell_exec.go:123-125` | tool | streaming (I/O) | existing-edit | single synchronized writer (the `safeBuffer` mutex pattern, mcp/client.go:317) | role-match |
| M-g | `internal/cron/recover.go:55-82` | service | event-driven | existing-edit | `HandlerMeta.ReschedulesOnRecovery` (dispatch.go:32); `catchUpMissed` itself | exact (self) |
| M-h | `internal/cron/dispatch.go:120-134` | service | CRUD | existing-edit + reuse-seam | `context.WithTimeout(context.Background(), …)` detach (serve.go:119) | exact (sibling) |
| M-i | `cmd/aura/main.go:35` (one line) | config / composition-root | — | existing-edit + reuse-seam | `_ = godotenv.Load()` (internal/config/config.go, internal/llm/config.go) | exact (sibling) |
| M-j | `internal/knowledge/client.go:272-285` (+ `mcp/client.go:317-334` sibling) | service | streaming (I/O buffer) | existing-edit | `safeBuffer.Write` itself — make it a bounded ring | exact (self) |
| L1 | `cmd/aura/skills_snippet.go:112` | CLI/config | request-response (process) | existing-edit | `shell_exec` timeout (`context.WithTimeout`, shell_exec.go:105) | role-match |
| L2 | `internal/skills/writer_activate.go:24`, `resume.go:66` | service | file-I/O | existing-edit | sibling methods that already call `SanitizeName` before `os.RemoveAll` | exact (sibling) |
| L3 | `internal/agent/tools/shell_exec.go:138-145` (folds into H4/H5) | tool | request-response | existing-edit | `renderShellOutput` status switch (shell_exec.go:301-310) | exact (self) |
| L4 | `internal/conversations/store.go:482` | model (store) | CRUD (query) | existing-edit (guarded, NOT SQL rewrite) | status-filter shape elsewhere in store.go | role-match |
| L5 | `internal/cron/store_runs.go:81-94` + `scheduler_tasks.sql:34` | service / query | CRUD | existing-edit (drop or fold) | `pg_try_advisory_lock` claim (claim.go) already holds correctness | role-match |
| L6 | `internal/agent/tools/web_search.go:76`, `web_fetch.go:65` | tool | request-response | existing-edit | `shell_exec.go:94` empty-arg reject (`command is required`) | exact (sibling) |
| INFO | none (D-01a) | — | — | doc-only | n/a — document trust boundary | n/a |

---

## NET-NEW Code (the high-value mapping)

### 1. H4 — Cross-platform process-group kill (FIRST `//go:build` OS-split in `internal/`)

**Files to create:**
- `internal/agent/tools/shell_exec_unix.go`  (filename suffix `_unix` → all non-Windows GOOS)
- `internal/agent/tools/shell_exec_windows.go` (filename suffix `_windows` → Windows GOOS)
- regression: the existing `internal/agent/tools/shell_exec_test.go` (`TestShellExecTimesOut`
  rewritten — D-04) + a cross-platform grandchild-liveness helper.

**Closest in-repo analog for the build-tag split:** `.planning/spikes/038-profile-store-atomic-contract/replace_{posix,windows}.go`.
This is the ONLY existing `_posix.go`/`_windows.go` pair in the repo. It is in `.planning/spikes/`
(not shipped `internal/`), but it is the exact pattern the new files copy:

```go
// .planning/spikes/038-profile-store-atomic-contract/replace_posix.go
//go:build !windows
package main
import "os"
func replaceFile(src, dst string) error { return os.Rename(src, dst) }
```
```go
// replace_windows.go
//go:build windows
package main
import "golang.org/x/sys/windows"
func replaceFile(src, dst string) error { /* MoveFileEx … */ }
```

**Confirmed:** grep for `SysProcAttr|Setpgid|CreationFlags|//go:build (windows|unix|…)` in
`internal/` returns **zero** shipped files. This is the first OS-split under `internal/`. Both halves
MUST compile (CI = WSL/Linux; dev box = Windows w64devkit).

**The exact seam the new files own** (per-OS helpers the shared `shell_exec.go` calls):
- `setProcessGroup(cmd *exec.Cmd)` — POSIX `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`;
  Windows `&syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}`.
- `killProcessGroup(cmd *exec.Cmd) error` — POSIX `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`
  (negative pid = the group); Windows `taskkill /F /T /PID <pid>` (A1: minimal-real choice; no Job
  Object). RESEARCH §Pattern 1, [ASSUMED].

**The shared `shell_exec.go` edit site** (the analog being modified — `Execute`, shell_exec.go:119,126):
```go
// shell_exec.go:119-126 TODAY — relies on the DEFAULT CommandContext Cancel (kills bash ONLY = H4 root):
cmd := exec.CommandContext(runCtx, name, args...)
cmd.Dir = s.workdir(ctx, a.Cwd)
cmd.Env = mergeEnv(a.Env)
var stdout, stderr strings.Builder   // M-f: TWO separate builders (de-interleave defect)
cmd.Stdout = &stdout
cmd.Stderr = &stderr
runErr := cmd.Run()
```
Fix shape (RESEARCH §Pattern 2): after `exec.CommandContext`, call `setProcessGroup(cmd)`, set
`cmd.Cancel = func() error { return killProcessGroup(cmd) }` and `cmd.WaitDelay = 5 * time.Second`.

**L3 + M-f ride this same edit (D-01b, no double-touch):**
- **L3** — add a `context.Canceled` branch to `renderShellOutput` (shell_exec.go:301-310) for a
  distinct `[command cancelled]` status (today the switch only branches `DeadlineExceeded` /
  `*exec.ExitError` / `[command failed: %v]`).
- **M-f** — point both `cmd.Stdout` and `cmd.Stderr` at ONE synchronized writer so temporal
  interleave survives. Closest writer analog = the `safeBuffer` mutex-guarded writer in
  `internal/mcp/client.go:317-334` / `internal/knowledge/client.go:272-285`.
- **New error class to classify (Pitfall 2):** `cmd.Run()` can now return `exec.ErrWaitDelay`. Add
  `errors.Is(runErr, exec.ErrWaitDelay)` → render as timeout/kill (set `timed_out: true`), NOT a
  crash. `exitCodePtr` (shell_exec.go:324) + `renderShellOutput` both need the new branch.

**Regression analog (Pitfall 1):** `shell_exec_test.go` already has the `shellIsCmd()` OS switch
precedent (RESEARCH cites :109) — gate the POSIX grandchild-PID-death assertion (`syscall.Kill(pid,0)`
→ ESRCH) behind it and provide the cmd.exe equivalent (`tasklist`). Fixture:
`sh -c 'sleep 30 & echo $! > pidfile; sleep 30'`, 200ms timeout, then assert the grandchild PID is dead.

---

### 2. H6/H7 — Durable notification state (new Postgres migration `0013` + sqlc query file)

**Files to create:**
- `internal/db/migrations/0013_<name>.up.sql`
- `internal/db/migrations/0013_<name>.down.sql`
- `internal/db/queries/<name>.sql`  (sqlc source)
- regenerated `internal/db/sqlc/<name>.sql.go` (+ `models.go`/`querier.go` deltas) via `sqlc generate`
- new store methods in `internal/cron/store_runs.go` (the sweep + status writers)

**Confirmed migration head = `0012_telegram`** (`internal/db/migrations/` listing). The new migration
is **`0013`** (audit/CONTEXT said 0012 — RESEARCH correction folded in).

**Closest analog — migration template `0009_scheduler.up.sql`** (the role-separated grants + partial
index pattern the new migration MUST copy):
```sql
-- 0009_scheduler.up.sql:48-63 — partial index on the sweep predicate + role grants:
CREATE INDEX scheduler_tasks_due_idx ON aura.scheduler_tasks (next_run_at)
    WHERE status = 'active';                                  -- partial index on the poll predicate
-- DML grants (NEVER TRUNCATE/DROP/CREATE to aura_app); audit-forever = no DELETE:
GRANT SELECT, INSERT, UPDATE         ON aura.agent_job_runs   TO aura_app;
GRANT ALL                            ON aura.agent_job_runs   TO aura_migrate;
```
```sql
-- 0009_scheduler.down.sql — drop child (FK) first, indexes drop with their tables:
DROP TABLE IF EXISTS aura.agent_job_runs;
DROP TABLE IF EXISTS aura.scheduler_tasks;
```

**Schema choice (A2 — planner picks; RESEARCH recommends the new table):**
- **(recommended)** a new `aura.pending_notifications` table keyed by run_id with
  `(notify_after timestamptz, attempts int DEFAULT 0, last_error text, status text CHECK (IN ('pending','failed','delivered')))`
  + partial index `WHERE status = 'pending'`. The sweep query gets a **REAL**
  `FOR UPDATE SKIP LOCKED` (it runs in a tx — unlike the inert L5 one on the autocommit pool).
- (alt) columns on `agent_job_runs` (has an UPDATE grant; but it is audit-forever/append-leaning).

**Closest analog — sqlc query style `internal/db/queries/agent_job_runs.sql`** (copy the
`-- name:`/`RETURNING`/`make_interval` conventions exactly):
```sql
-- name: InsertRun :one
INSERT INTO aura.agent_job_runs (id, task_id, status, step_budget)
VALUES ($1, $2, 'running', $3)
RETURNING id, task_id, status, …;          -- full column list on RETURNING (precedent)

-- name: ScanStaleRuns :many
SELECT id, task_id FROM aura.agent_job_runs
WHERE status = 'running'
  AND last_heartbeat_at < now() - make_interval(secs => $1);   -- interval param idiom

-- name: MarkUnknownRecovery :exec
UPDATE aura.agent_job_runs SET status = 'unknown_recovery', missed_since = now() WHERE id = $1;
```
The new query file needs: an `InsertPendingNotification :one`, a `SweepDueNotifications :many`
(`WHERE status='pending' AND notify_after <= now()` … `FOR UPDATE SKIP LOCKED` — model on
`scheduler_tasks.sql:26-34 DueTasks`), a `MarkNotificationDelivered :exec`, and a
`MarkNotificationFailed :exec` (increment `attempts`, set `last_error`).

**Closest analog — the generated-client call sites in `internal/cron/store_runs.go`** (the new
store methods copy this shape exactly):
```go
// store_runs.go:81-94 — DueTasks: clamp at the boundary + rows→[]Task loop. The new
// SweepDueNotifications store method copies the clamp + the rows.Err-after-loop discipline.
func (s *Store) DueTasks(ctx context.Context, limit int) ([]Task, error) {
    if limit <= 0 || limit > math.MaxInt32 { limit = 1 }       // int4OrNull/clamp precedent
    rows, err := s.q.DueTasks(ctx, int32(limit))
    if err != nil { return nil, fmt.Errorf("due tasks: %w", err) }
    out := make([]Task, 0, len(rows))
    for _, r := range rows { out = append(out, taskFromRow(r)) }
    return out, nil
}
// store_runs.go:23-38 insertRunOnConn — `sqlc.New(conn)` + RETURNING→struct mapping
// is the template for InsertPendingNotification. int4OrNull(stepBudget) is the null-clamp idiom.
```

**The dispatcher edit sites (H6/H7 — the existing functions modified):**
```go
// dispatch.go:154-156 (H6) — TODAY logs+returns, NO persistence:
if !immediate && runErr == nil && task.Kind != KindReminder && d.deferred(task) {
    slog.Info("notification deferred to quiet-hours window end", "task", task.ID)
    return                                  // ← H6: the notification is LOST here
}
// dispatch.go:158-160 (H7) — TODAY logs "bound-retry on a later tick" with NO backing impl:
if err := d.deps.Notifier.Notify(ctx, NotifyRoute(task.NotifyRoute), "", text); err != nil {
    slog.Warn("dispatch notify undelivered (bound-retry on a later tick)", "task", task.ID, "err", err)
}                                           // ← H7: "bound-retry" is fiction (D-22 unbacked)
```
H6 fix: persist a `pending_notifications` row with `notify_after` = window end. H7 fix: persist a
`failed` row + a tick sweep that re-invokes `Notifier.Notify` bounded by `attempts < N`. The sweep
is a new dispatcher pass — wire it into the tick loop (the same place the catch-up/orphan recovery
runs). `Notifier.Notify` (notify.go:65-72) already returns the undelivered error → the sweep just
re-calls it.

**Honor pgx lazy-error discipline** (CONTEXT established pattern, store_runs.go precedent):
`rows.Err()` after the sweep loop, SQLSTATE re-classification, `WithTx` rollback.

---

### 3. H10 — MCP reconnecting Server wrapper (lazy reconnect-on-use, study §43)

**Files to create/modify:**
- a new `reconnectingServer` type — lands in `internal/agent/mcptools/bridge.go` (or a new
  `internal/agent/mcptools/bridge_reconnect.go` if bridge.go nears the 600-LOC cap; it is currently
  187 LOC, so in-file is fine).
- reuse (no new file) `internal/mcp/client.go` primitives: `Open`, `initialize`, `Ping`.
- composition-root wiring in `cmd/aura` (where `ServerConfig` + name are known).

**Closest analog — the `bridgedTool` that captures one `Server` at mount** (the exact defect site,
bridge.go:35-60):
```go
// bridge.go:35-39 — bridgedTool holds ONE srv captured at mount; never re-opened:
type bridgedTool struct {
    srv  Server      // ← H10: a possibly-dead Server, called forever
    name string
    spec tools.Spec
}
// bridge.go:48-60 Execute — calls b.srv.CallTool against a dead stdin, no reconnect:
text, err := b.srv.CallTool(ctx, b.name, args)
if err != nil {
    return tools.NewResult(ctx, "error: "+err.Error())   // ← the clean inline tool error to mirror
}
```

**The `Server` interface the wrapper must satisfy** (bridge.go:25-28 — narrow, consumer-declared):
```go
type Server interface {
    ListTools(ctx context.Context) ([]mcp.ToolDef, error)
    CallTool(ctx context.Context, name string, args map[string]any) (string, error)
}
```
The `reconnectingServer` wraps the mounted `*mcp.Client`, satisfies this same interface, and holds
the `name` + `mcp.ServerConfig` needed to re-`Open`.

**Closest analog — the re-open + handshake primitives in `internal/mcp/client.go`** (reused as-is):
```go
// client.go:64 — Open is re-callable from name+cfg; initialize runs INSIDE it:
func Open(ctx context.Context, name string, cfg ServerConfig) (*Client, error) { … c.initialize() … }
// client.go:121 — initialize is the MCP handshake (runs once inside Open):
func (c *Client) initialize() error { … }
// client.go:185 — Ping: the transport-health primitive, today wired ONLY to `aura mcp doctor`:
func (c *Client) Ping(ctx context.Context) error { … }
```

**Fix shape (RESEARCH §Pattern 5 — NO supervisor, NO ping ticker):**
- On `CallTool`/`ListTools` transport error: re-`mcp.Open(ctx, name, cfg)` (which re-runs
  `initialize`), then retry the call once. A SECOND consecutive failure → clean inline tool error
  (mirror bridge.go:57 `"error: "+err.Error()`).
- **Refresh the dead tool's boot-time description on reconnect:** re-`ListTools` after reconnect and
  update `bt.spec` (the audit notes the dead tool keeps its stale boot-time description in the
  registry). Do NOT regress the deferred-tool BM25 registry (CONTEXT established pattern).
- `Ping` is optional pre-call liveness; reconnect-on-error alone is the minimal-real shape.

**M-j rides the same MCP cluster** (D-01b): `safeBuffer` (the unbounded stderr buffer) is identical
in `internal/knowledge/client.go:272-285` (the finding's cited site) and `internal/mcp/client.go:317-334`.
Make `safeBuffer.Write` a bounded ring (cap the buffer); `stderrTail()` (knowledge/client.go:250)
already reads only the last 200B, so trimming is transparent to it. Fix BOTH copies (or extract a
shared bounded-ring type) since the type is duplicated.

---

## Shared Patterns (cross-cutting seams — reuse, never hand-roll)

### Error/secret sanitization
**Source:** `internal/agui/server.go:392` (`sanitizeErr`), `:403` (`sanitizeString`), `:426` (`redactEvent`).
**Apply to:** H2 (Telegram renderer), M-c (Fanout path).
```go
// server.go:403 — the chokepoint: DSN→scheme marker, URL userinfo→[redacted]@, bearer/api-key/token→[redacted]:
func sanitizeString(msg string) string { … secretPattern … urlUserinfoPattern … tokenPattern … }
// server.go:426 — redactEvent: sanitize a RUN_ERROR Message in-flight (the HTTP path uses this; Fanout bypasses it):
func redactEvent(ev events.Event) events.Event { re.Message = sanitizeString(re.Message); … }
```
`sanitizeErr` takes an `error`; the renderer (H2) and fanout (M-c) carry a `string`. **OQ3 (RESEARCH):
export a shared `SanitizeString`** so both the renderer's `RunErrorEvent.Message` and the Fanout's
`err.Error()`/trace JSON route through the one redaction contract. One commit coordinates the export
across the agui + telegram clusters.

### Stream-open bounded retry
**Source:** `internal/agent/llm_agent_stream_retry.go:20` (`streamWithOpenRetry`) + `retryableStreamOpenError` (:57).
**Apply to:** M-a — the 3 bypass sites (`llm_agent_completion.go:94`, `llm_agent_finalize.go:217`,
`llm_agent_reasoning.go:45`) call `a.client.Stream` directly; replace each with
`a.streamWithOpenRetry(callCtx, req, requestID)`. The helper already classifies retryable network
errors (wsarecv/reset/EOF, :77-93). Critic/finalize/router keep their fail-open behavior on the FINAL error.

### Detach-from-cancelled-root for a terminal write
**Source:** `cmd/aura/serve.go:119` — `context.WithTimeout(context.Background(), aguiShutdownTimeout)`
(a fresh ctx + short deadline because the root ctx is already signal-cancelled).
**Apply to:** M-h — `CompleteRun(ctx,…)` at `dispatch.go:120-134` runs on the cancelled root ctx and
is rejected by pgx. Write the terminal run state on a detached ctx + short deadline. (RESEARCH cites
`context.WithoutCancel`; the actual shipped precedent is the `WithTimeout(Background(), …)` form at
serve.go:119 — either yields a non-cancelled ctx; the planner picks. `WithoutCancel` preserves ctx
values, `Background()` does not — irrelevant here since `CompleteRun` reads no ctx values.)

### Central `.env` load at the composition root
**Source:** existing `_ = godotenv.Load()` in `internal/config/config.go` and `internal/llm/config.go`
(load-first-wins; does NOT override already-set vars — idempotent with the per-command loads).
**Apply to:** M-i + free-rider env LOWs — add ONE `_ = godotenv.Load()` at the top of `main()`
(`cmd/aura/main.go:35`, before the dispatch switch). Closes `aura mcp` `AURA_MCP_CONFIG` invisibility,
`aura mcp doctor` whatsapp-URL, and `agent dry-run`/`swarm-demo` `AURA_LOOP_*`/`AURA_SWARM_MAX_DEPTH`
in one line. **One commit** (D-01b).

### Empty-required-arg rejection
**Source:** `shell_exec.go:94` — `if strings.TrimSpace(a.Command) == "" { return …, "command is required" }`.
**Apply to:** L6 — `web_search.go:76` (`query required`), `web_fetch.go:65` (`url required`) before
reaching SearXNG.

### SanitizeName guard before `os.RemoveAll`
**Source:** the sibling skills `Writer` methods that already call `SanitizeName` before destructive FS ops.
**Apply to:** L2 — `writer_activate.go:24` (`Activate`), `resume.go:66` (`DiscardPending`) skip the guard.

---

## Existing-Edit findings — analog = the function itself (or a sibling doing it right)

| Finding | Edit site | Sibling-already-right analog | Concrete excerpt anchor |
|---------|-----------|------------------------------|-------------------------|
| H1 | `isLifecycleFrame` (server.go:264) | the fn is too narrow — widen it | Today: only `RunStarted/Finished/Error` non-droppable (server.go:266). Widen to ALL START/END/RESULT/CUSTOM/SNAPSHOT (RESEARCH H1 classifier list); only `*_CONTENT`/`TOOL_CALL_ARGS`/`STATE_DELTA` drop. Shared by pump (server.go:241) + fanout (fanout.go:113) — fix once. |
| H2 | `renderer.consume` switch (renderer.go:81-97) | sibling `RunFinished` case (renderer.go:92) | Switch has NO `*events.RunErrorEvent` case → error text dropped. Add `case *events.RunErrorEvent:` that sends a `SanitizeString`-cleaned failure. Also fix stale comment `serve_channels.go:142`. |
| H3 | async `asyncResult` cb (bot_dispatch.go:388-395) | sync sibling (bot_dispatch.go:400) | Async cb logs `convErr` + `return`s silently; the sync ≤5MB path does `t.reply(c, convertFailMessage)`. On `convErr != nil`, send `convertFailMessage` via the captured `sender`/`notifier` (both already in the closure, bot_dispatch.go:386-387). |
| H5 | `truncatePreview`/`NewResult` (result.go:75-84,94) | the head-first truncation is the defect | `NewResult` cuts head-first (`content[:cut]`, result.go:104); the shell footer is appended at the TAIL (shell_exec.go:140-145). Add a tail-reserving variant (truncate body to `cap - len(footer)` then append footer). Also fix the same defect on the critic digest (`criticResultCap=400`, completion.go:157). |
| H8 | `dropOldestPairs` (context.go:237-263) | the protected-head split (context.go:242-249) stays | `body = body[2:]` (context.go:258) assumes user/assistant alternation; a tool round `assistant(tool_calls)→tool→tool→assistant` is sliced mid-round → orphan `RoleTool` head → provider 500. Drop by advancing to the next `RoleUser`; skip a dangling `RoleTool` head after reducing. |
| H9 | `Chunk` struct (client.go:75-81); producer (openai_compat/client.go:145-166) | trailing-Usage-chunk emit (openai_compat/client.go:162-165) | Add `Err error` to `Chunk`. The producer already `emit`s a trailing chunk for Usage — `emit(llm.Chunk{Err: parseErr})` before close on parse error OR EOF-without-finish_reason (covers sse.go:114-122 clean-EOF). Main loop checks `c.Err`; critic/finalize/router may ignore (already fail-open). Blast radius: 1 producer + 5 consumers (RESEARCH H9 table). |
| M-b | veto append (llm_agent.go:259-266) | the text_response/tool path uses RoleTool (a separate, non-leaking seam) | Today appends the vetoed prose as durable `RoleAssistant`; `finalize` copies it forward (finalize.go:117,202). Fix: on a content-stop veto append ONLY the user-role nudge, not the vetoed prose (A3 — verify the next request stays wire-valid). |
| M-d | `lastUserMessage` (server.go:318-328) | `payloadString` shape-switch (server.go:300-313) | Accepts only `string` content (server.go:323); a `[]InputContent` last user message is silently skipped. Reject explicitly or project text parts (mirror `payloadString`'s type-switch over `any`). |
| M-e | `/cancel` intercept (bot_dispatch.go:114-122), `cancel` (commands.go:167-177) | the "Annulla" button → `SubmitAnswer(…ActionCancel)` (resumeAnswers, server.go:285-293) | `/cancel` only cancels a turn ctx (none during a pause). Route through `SubmitAnswer(…ActionCancel)` when `PendingFor` (bot_dispatch_hitl.go:24) is non-empty so the `paused_states` row resolves + keyboard clears. |
| M-g | `catchUpMissed` (recover.go:55-82) | `HandlerMeta.ReschedulesOnRecovery` (dispatch.go:32) — defined, never consulted | `catchUpMissed` re-fires every overdue task; the `ReschedulesOnRecovery` flag is never read. Look up the handler's `Meta()` and skip the catch-up fire when the flag is false. (The dispatcher map is the composition root's; recover.go needs a handler-meta lookup seam.) |
| L1 | snippet exec (skills_snippet.go:112) | `shell_exec` timeout (shell_exec.go:105) | `context.Background()` with no timeout/SIGINT. Wrap with `context.WithTimeout` + `signal.NotifyContext` (mirror shell_exec's `WithTimeout`). |
| L4 | `SearchConversationTurns` (store.go:482) | — (LOCKED FTS contract — guard, do NOT rewrite SQL) | Add a `status <> 'deleted'` guard only. RESEARCH anti-pattern: the SQL at store.go:478-481 is a locked cross-slice contract; this is a minimal guarded addition, latent (no command sets `StatusDeleted` today). |
| L5 | `DueTasks` / `scheduler_tasks.sql:34` | `pg_try_advisory_lock` claim (claim.go) holds correctness | `FOR UPDATE SKIP LOCKED` on the autocommit pool is inert (lock released at SELECT return). Drop it or run select+claim in one tx. (Note: the H6/H7 new-table sweep gets a REAL SKIP LOCKED — it runs in a tx.) |

---

## No Analog Found

None. Every finding maps to an existing in-repo seam, an existing function being modified, or a Go
stdlib idiom. The closest-to-greenfield items are anchored as:

| Item | Why no internal/ analog | Anchor used instead |
|------|-------------------------|---------------------|
| H4 OS-split files | First `//go:build` OS-split under `internal/` (grep-confirmed zero) | `.planning/spikes/038-…/replace_{posix,windows}.go` (only in-repo build-tag pair) + Go stdlib `os/exec` Cancel/WaitDelay (canonical) |
| H10 `reconnectingServer` | No reconnecting wrapper exists today | `bridgedTool` (the captured-`Server` defect) + `mcp.Open`/`initialize`/`Ping` primitives (re-callable as-is) |

---

## Metadata

**Analog search scope:** `internal/agent/tools`, `internal/agui`, `internal/channels/telegram`,
`internal/cron`, `internal/conversations`, `internal/llm` + `internal/llm/openai_compat`,
`internal/agent` (llm_agent*), `internal/agent/mcptools`, `internal/mcp`, `internal/knowledge`,
`internal/skills`, `internal/db/{migrations,queries,sqlc}`, `cmd/aura`, `.planning/spikes`.
**Files scanned:** ~22 source files read at the relevant ranges; migration + query dirs enumerated.
**Pattern extraction date:** 2026-06-10
**Key corrections folded in:** migration head `0012` → new `0013`; H2 stale comment at
`serve_channels.go:142`; Go `1.26.4` (Cancel/WaitDelay/AsType[T] all available); M-h shipped
precedent is `WithTimeout(Background(),…)` at serve.go:119 (RESEARCH's `WithoutCancel` is equivalent).
