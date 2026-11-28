# Scheduled report reaches the live turn Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A finished `agent_job` run reaches the turn running in its origin conversation. Its stored record names its author. Reload no longer drops a tool result that a record landed in front of. `task action=history` reads what the runs produced.

**Architecture:**
- `cron.Dispatch.recordToOrigin` moves to its own file. It prefixes the record and, for `agent_job`, pushes the same text on the steer queue under a new runtime source `scheduler`. `LlmAgent.drainSteer` already delivers runtime sources in the untrusted envelope after every tool round.
- `conversations.repairToolPairs` moves messages that sit between a call and its results to after the results.
- The task tool gains a `history` action, backed by an owner-scoped join in a new `cmd/aura` adapter file.

**Tech Stack:** Go, pgx/sqlc, Postgres (`aura.agent_job_runs`, `aura.scheduler_tasks`).

**Spec:** `docs/superpowers/specs/2026-11-28-scheduled-report-reaches-live-turn-design.md`

## Global Constraints

- Steer source: `steer.SourceScheduler = "scheduler"`; envelope name `"scheduled_run"`.
- Record prefix, exactly: `[Scheduled task report — <kind> <first 8 chars of task id>]\n` followed by the text. It applies to success and failure texts alike.
- Only `KindAgentJob` pushes on the steer queue. Every kind keeps its record, as today.
- A push failure is `slog.Warn`, never a run failure and never a lost record.
- `history`: `task_id` required, `limit` default 5, max 20. Each run renders as one block: start time (RFC3339, UTC), status, summary bounded to 1500 runes, `last_error` if set. Newest first. Cancelled tasks are readable.
- `run_now` result, exactly: `running task <id> now. Its report is recorded in this conversation and reaches this turn after your next tool call; task action=history reads it. Do not sleep or poll to wait for it.`
- No file over 600 LOC. Tests run in WSL with `-race`.

## Review Focus

- **A summary bigger than the steer cap (32 KiB by default).** Push refused with `ErrTooLarge`: a WARN, and the record still lands. Task 1 covers this with a publisher fake that returns the error.
- **A report pushed after the turn's last drain point.** `Runner.deliverLeftoverSteer` runs one follow-on turn to deliver it. That is a paid model call, and the design wants it ("Aura notices"). Task 4 reports it if it is seen on the VM. No test, because the behaviour already exists.
- **A `pim_watch` with an origin conversation.** It must record and must NOT push, or a watch every few minutes injects notes into live turns and triggers follow-on turns. Task 1 tests it.
- **Two records between one call and its results, or a record before a partial result set.** Both records are moved when all results are found. A missing result keeps today's recovery path. Task 2 tests both.
- **`history` on another identity's task id.** It returns "no runs" (not found) and never another owner's summaries. Task 3's integration test covers it.

## Decisions taken while writing this plan

- **Steer push limited to `agent_job`.** The spec said every recorded run pushes. Reading `Runner.deliverLeftoverSteer` showed that a steer row left at turn end starts a follow-on turn. `pim_watch` (`KindPIMWatch`, a person kind) would then trigger paid turns. Task 1 amends the spec's Goals line.
- **The pushed text is the same prefixed record text.** The envelope says "scheduler", and the prefix adds kind and task id, which the model needs to match the report to its `run_now`. The spec's "only the stored record" line is amended in Task 1.
- **No regeneration of `testdata/deferred_manifest.json`.** It is a dump of the deployed manifest, used only as the search-gate corpus (`search_gate_test.go`), and no test compares it to the live spec.

## File structure

- `internal/steer/inbox.go`: the new source constant and `IsRuntimeSource`.
- `internal/agent/llm_agent_steer.go`: `runtimeSteerEnvelopes` entry.
- `internal/cron/dispatch_record.go` (new): `recordToOrigin` moved from `dispatch.go`, plus the prefix and the push.
- `internal/cron/dispatch_deps.go`: the `Steer` field and the `SteerPublisher` interface.
- `cmd/aura/serve_dispatch.go`: wiring of `chat.steer`.
- `internal/conversations/store_helpers.go`: the reorder.
- `internal/agent/tools/task_history.go` (new): the action and its rendering.
- `cmd/aura/serve_adapters_task_runs.go` (new): the owner-scoped query.

---

### Task 1: The scheduled report is attributed and steered

**Files:**
- Modify: `internal/steer/inbox.go`, `internal/agent/llm_agent_steer.go`, `internal/cron/dispatch.go` (remove `recordToOrigin`), `internal/cron/dispatch_deps.go`, `cmd/aura/serve_dispatch.go`, the spec (Goals line + Design 3)
- Create: `internal/cron/dispatch_record.go`
- Test: `internal/cron/record_origin_test.go`, `internal/steer/` unit test for `IsRuntimeSource`, `internal/agent/llm_agent_steer_worker_test.go`

**Interfaces:**
- Produces:
  - `steer.SourceScheduler = "scheduler"`;
  - `cron.SteerPublisher interface { Push(conv, source, text string) error }`;
  - `cron.DispatchDeps.Steer SteerPublisher` (nil = no push);
  - `func scheduledRecordText(task Task, text string) string`, the prefix.

- [ ] **Step 1: Write the failing tests**
  - `record_origin_test.go`:
    - `TestCompletedRunLandsInItsOriginConversation` asserts the stored text equals `"[Scheduled task report — reminder task-1]\nSPIKE101-DELIVERED"`. This is an intended behaviour change; say so in the commit.
    - `TestAgentJobRecordIsSteeredToItsConversation`: an `agent_job` with origin `conv-1` → one push, `conv="conv-1"`, `source="scheduler"`, text equal to the record.
    - `TestWatchRecordIsNotSteered`: a `pim_watch` with an origin → one record, zero pushes.
    - `TestSteerRefusalKeepsTheRecord`: a publisher returning `steer.ErrTooLarge` → record present, `Dispatch` returns nil.
    - `TestNilSteerPublisherRecordsOnly`: `Steer` unset → record present, no panic.
  - `steer`: `IsRuntimeSource("scheduler") == true`.
  - `llm_agent_steer_worker_test.go`: `MarkSteer(steer.Message{Source: "scheduler", Text: "<b>x</b>"})` returns envelope `"scheduled_run"`, and the marked text is the untrusted tool-output envelope with the text HTML-escaped.

- [ ] **Step 2: Run them and see them fail**
  Run: `go test -race ./internal/cron/ ./internal/steer/ ./internal/agent/ -run 'Origin|Steered|SteerRefusal|NilSteer|RuntimeSource|MarkSteer'`
  Expected: FAIL (undefined `SourceScheduler`, prefix mismatch).

- [ ] **Step 3: Implement**
  - Move `recordToOrigin` to `dispatch_record.go` unchanged, then add the prefix and, when `task.Kind == KindAgentJob && d.deps.Steer != nil`, the push after the append.
  - Add `SourceScheduler` to `IsRuntimeSource` and `runtimeSteerEnvelopes`, and update both comments that list the runtime sources.
  - In `serve_dispatch.go`, set `Steer: chat.steer` only when `chat.steer != nil`. A nil `*steer.PostgresStore` boxed into the interface is non-nil: this is the trap `newCronAgentDeps` documents.
  - Amend the spec: the Goals line says `agent_job`, and Design 3 says the steer copy carries the same prefix. One sentence each, why included.

- [ ] **Step 4: Run them and see them pass, plus the packages**
  Run: `go vet ./internal/cron/ ./internal/steer/ ./internal/agent/ ./cmd/aura/ && go build ./... && go test -race ./internal/cron/ ./internal/steer/ ./internal/agent/`
  Expected: PASS.

- [ ] **Step 5: Commit**
  `git commit -- <the files above>` (pathspec: the index holds another session's staged files). Subject: `feat(cron): steer a finished agent_job's report to the turn waiting for it`.

### Task 2: Reload keeps a result that a record landed in front of

**Files:**
- Modify: `internal/conversations/store_helpers.go`
- Test: `internal/conversations/store_helpers_edges_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: no new exported names; `repairToolPairs` behaviour only (model context and display snapshot alike).

- [ ] **Step 1: Write the failing tests** (on `repairManagedToolMessagePairs`)
  - `TestRepairMovesRecordAfterItsToolResult`: shaped like the VM turns #129–#131. `assistant{tool_calls:[call-1 shell_exec]}`, `assistant{"[Scheduled task report — agent_job 01a221d9]\nNessuna anomalia"}`, `tool{call-1,"waited"}`. Output: call, tool `"waited"` (the real one, not `RecoveryToolResultContent`), then the record.
  - `TestRepairMovesTwoRecordsAndKeepsTheirOrder`: two records between a two-call group and both results → call, result 1, result 2, record A, record B.
  - `TestRepairWithMissingResultKeepsRecovery`: two calls, a record, and only one result → today's output, byte-identical (synthetic recovery for the missing call).
  - The same #129–#131 fixture through `DisplayMessages(repairDisplayTurns(...))` shows the real result.

- [ ] **Step 2: Run them and see them fail**
  Run: `go test -race ./internal/conversations/ -run 'TestRepair'`
  Expected: FAIL (the real result is dropped as an orphan).

- [ ] **Step 3: Implement**
  In `repairToolPairs`, before the recovery branch: from `i+1`, skip messages that are role `assistant` with no tool calls, or role `user`. If the next `len(calls)` messages form a valid result group for `calls` (reuse the `validToolResultGroup` logic on that offset), emit the call, the results, then the skipped messages, and continue after the results. Otherwise fall through to `appendRecoveredToolResultGroup`. Extract a helper; the loop stays flat.

- [ ] **Step 4: Run them and see them pass, plus the package**
  Run: `go vet ./internal/conversations/ && go test -race ./internal/conversations/`
  Expected: PASS, existing repair tests unchanged.

- [ ] **Step 5: Commit**
  Subject: `fix(conversations): keep a tool result that a record landed in front of`.

### Task 3: `task action=history`, and `run_now` says where the report goes

**Files:**
- Create: `internal/agent/tools/task_history.go`, `cmd/aura/serve_adapters_task_runs.go`
- Modify: `internal/agent/tools/task.go` (enum + description at the `action` schema field, dispatch map, `taskStore`, `actionRunNow` text)
- Test: `internal/agent/tools/task_history_test.go`, `cmd/aura/serve_adapters_task_runs_integration_test.go` (`//go:build db_integration`)

**Interfaces:**
- Produces:
  - `tools.TaskRun{StartedAt time.Time; Status, Summary, LastError string}`;
  - on `taskStore`: `ListTaskRuns(ctx context.Context, id string, limit int) ([]TaskRun, error)`;
  - `taskArgs.Limit int` (`json:"limit"`).

- [ ] **Step 1: Write the failing tests**
  - `task_history_test.go` with the existing fake store:
    - `TestHistoryRendersNewestFirst`: two runs → both blocks present, the newer first, with status and summary.
    - `TestHistoryBoundsSummary`: a 3000-rune summary → 1500 runes plus an ellipsis marker.
    - `TestHistoryLimitDefaultsAndCaps`: limit 0 → the store receives 5; limit 50 → 20.
    - `TestHistoryRequiresTaskID`: empty → error `task history: task_id is required`.
    - `TestHistoryNoRuns`: empty → `no runs for task <id>`.
    - `TestRunNowSaysWhereTheReportGoes`: the exact `run_now` text from Global Constraints.
  - Integration test, on the disposable coverage database: a task of identity A with two runs, one cancelled task with one run → listed. The same id read as identity B → zero runs.

- [ ] **Step 2: Run them and see them fail**
  Run: `go test -race ./internal/agent/tools/ -run 'History|RunNowSays'`
  Expected: FAIL (unknown action).

- [ ] **Step 3: Implement**
  - `history` does not mutate, so it uses its own `task_id` check, not `requireTaskID`'s mutation guard.
  - The adapter query joins `aura.agent_job_runs r` to `aura.scheduler_tasks t` on `t.id = r.task_id`. It filters on `t.id = $1::uuid AND t.identity_id = $2` (identity from `taskIdentity(ctx)`) and has no status filter. Order is `r.started_at DESC`, with `LIMIT $3`.

- [ ] **Step 4: Run them and see them pass, plus the packages**
  Run: `go vet ./internal/agent/tools/ ./cmd/aura/ && go test -race ./internal/agent/tools/ && bash scripts/coverage_docker.sh` (the integration tier, disposable `aura_cov` only).
  Expected: PASS; the new integration test executes (not skipped).

- [ ] **Step 5: Commit**
  Subject: `feat(task): read a task's past runs, and tell run_now where its report lands`.

### Task 4: Measure on the lab VM, then record it

**Files:**
- Create: `docs/superpowers/verification/2026-11-28-scheduled-report-reaches-live-turn.md`

- [ ] **Step 1: Ship.** Push with the WSL recipe from CLAUDE.md, but only once the other session's staged work is committed or set aside: the pre-push gates run on the whole tree. Wait until every job on the pushed SHA is green, "Publish Aura edge image" included, and the VM updater has applied it.
- [ ] **Step 2: The operator runs it in the cockpit** (paid: the operator drives it). In a new conversation: "schedula un agent_job di prova, lancialo con run_now e poi dimmi cosa ha prodotto".
- [ ] **Step 3: Read it back from the VM's Postgres.** For that conversation, in `aura.conversation_turns`:
  - the record starts with `[Scheduled task report — agent_job`;
  - no assistant text sits between a tool call and its result in what reload builds;
  - the model's reply quotes the report without `sleep` polling.

  In `aura.steer_queue`, the `scheduler` row is drained. `task action=history` is answered with the run. Also reload conversation `01a221cc…` and check that #131's `waited` result is present.
- [ ] **Step 4: Write the verification file:** what was measured, the SHAs, and what it does not prove: `pim_watch` was not exercised, and one run is not a rate. Commit it with pathspec.
