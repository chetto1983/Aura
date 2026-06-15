---
phase: 10-scheduler
plan: 05
subsystem: scheduler
tags: [scheduler, agent-job, llm-spawn, ask-user-auto-reject, backup, docker-exec, notifier, mcp-self-send, dispatch, risk-gate, db_integration]

# Dependency graph
requires:
  - phase: 10-scheduler
    plan: 03
    provides: Dispatcher seam (Dispatch(ctx, Task, *Claim) error) + held-conn claim + tick loop + DuringQuietHours predicate + Store.CompleteRun (23505-swallow)
  - phase: 10-scheduler
    plan: 04
    provides: scoring gate (ComputeTaskTier/GateRecommended/RequiresImmediateAlert) + the task tool surface
provides:
  - internal/cron/handlers — cron-free TaskKind handlers (Slice 0.9: Handler + HandlerMeta per kind, no dispatch switch)
  - internal/cron/handlers.ReminderHandler — verbatim payload-text delivery
  - internal/cron/handlers.AgentJobHandler — direct LlmAgent spawn mirroring swarm.runChild (budget-from-row D-24/SC#4 + ask_user auto-reject inject-and-continue D-25), NO internal/swarm import
  - internal/cron/handlers.BackupHandler — fixed-argv docker exec pg_dump/neo4j-admin (D-26) + retention sweep + MissedBackupAlert (SC#3)
  - internal/cron.Notifier + compositeNotifier — MCP self-send (send_message/send_email) via a cron-local SelfSendResolver + stdout fallback + notify-on-failure (D-19/D-21/D-22)
  - internal/cron.Dispatch — TaskKind→Handler routing satisfying the 10-03 Dispatcher seam; idempotent CompleteRun + risk-gated delivery (D-27)
affects: [10-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cron-free handlers package (D-24): handlers receive a plain Job value + return (summary, error); the cron dispatcher owns CompleteRun + notification. agent_job constructs agent.NewLlmAgent DIRECTLY (mirroring swarm.runChild), NEVER runner.Turn (amendment #23), with tools.Without(parentReg, swarm_spawn) — keeping internal/swarm out of the import graph."
    - "Budget-from-row (D-24/SC#4): agent.NewBudget(BudgetOptions{MaxSteps:&stepBudget}) inherits the agent_job_runs.step_budget so a job scheduled at 10 terminates at 10, not the default 25."
    - "ask_user auto-reject inject-and-continue (D-25): on Actions.AwaitingInput, synthesize the assistant ask_user turn + a RoleTool answer keyed to ai.ToolCallID carrying the auto-rejected marker, then re-Run a fresh LlmAgent within the SAME shared budget — bounded by maxAutoRejects so it completes <30s regardless of model behavior. ask_user STAYS in the registry (the model sees the rejection)."
    - "Consumer-declared cron-local interfaces break the tools→cron import cycle (the 10-04 taskStore pattern): cron declares Handler/Job/HandlerMeta + Notifier + SelfSendResolver; the real handlers (which import tools) + the MCP registry adapter wire in at the composition root. package cron stays free of an internal/agent/tools import."
    - "Composite Notifier (D-19/D-22): per-task route (whatsapp→send_message / email→send_email) on the mounted MCP self-send, stdout fallback + notification-undelivered signal on failure, fail-safe degrade-to-stdout for unknown/unset routes. Fail-soft mirrors the Phase-9 MCP boot posture."
    - "Fixed-argv docker exec backups (D-26, Phase-8 dockerCLI precedent): LookPath-gated, constant container+flag argv (NEVER model output, T-10-16), socket NEVER mounted (T-10-15); 14d/7d rolling retention; SC#3 24h-miss alert."

key-files:
  created:
    - internal/cron/handlers/handler.go
    - internal/cron/handlers/reminder.go
    - internal/cron/handlers/reminder_test.go
    - internal/cron/handlers/agentjob.go
    - internal/cron/handlers/agentjob_test.go
    - internal/cron/handlers/backup.go
    - internal/cron/handlers/backup_test.go
    - internal/cron/handlers/main_test.go
    - internal/cron/notify.go
    - internal/cron/notify_test.go
    - internal/cron/dispatch.go
    - internal/cron/dispatch_test.go
    - internal/cron/dispatch_integration_test.go
  modified: []
  deleted: []

key-decisions:
  - "Broke the would-be tools→cron→tools import cycle with cron-LOCAL consumer-declared interfaces instead of importing internal/cron/handlers or internal/agent/tools into package cron. 10-04 imported internal/cron into internal/agent/tools/task.go (its documented deviation #2); since handlers transitively imports tools (tools→cron), package cron CANNOT import handlers without a cycle. cron/dispatch.go therefore declares Handler/Job/HandlerMeta and cron/notify.go declares SelfSendResolver/SelfSendTool; the real handlers.{Reminder,AgentJob,Backup}Handler + a *tools.Registry→SelfSendResolver adapter wire in at the composition root (cmd/aura, 10-06). This keeps package cron tools-free (verified: go list -deps ./internal/cron has no internal/agent/tools) AND keeps the plan's stated file layout (notify.go/dispatch.go in package cron with the send_message/HandlerMeta literals)."
  - "The handlers package mirrors cron's Handler/Job/HandlerMeta with its own copies (handler.go) rather than importing cron — same interface-segregation rationale as 10-04's taskStore vs cron.Store. The composition root adapts handlers.Job↔cron.Job (trivial field copy)."
  - "agent_job summary collects the final LLMResponse.Content across the (possibly multi-pass) auto-reject loop, with the auto-rejected marker appended per rejection — so the audit summary always shows the rejection trail (D-25) even when the model then finalizes."
  - "Run-completion idempotency key = the run id (each claim opens a unique run; a redelivered completion of the SAME run trips completed_with_hash UNIQUE → swallowed as ErrAlreadyRunning by the store, logged by the dispatcher, never a crash — SC#2). Proven live in dispatch_integration_test."
  - "Backup destination path is passed to docker exec as the dump -f/--to-path target; the container must have AURA_BACKUP_DIR mounted for the dump to land host-visible (operator-config, RESEARCH Open Q2). The live docker-exec test is Manual-Only (AURA_BACKUP_LIVE=1) / t.Fatal-under-CI; the argv shape, LookPath gate, retention sweep, and 24h alert are unit-proven."
  - "env knobs (AURA_SCHEDULER_NOTIFY_DEFAULT/_RECIPIENT, AURA_BACKUP_DIR) read directly via os.Getenv in the cron/handlers packages (the 10-03 precedent — no config.Load struct touched; that composition wiring lands in 10-06)."

patterns-established:
  - "Cron-local consumer-declared Handler + SelfSendResolver interfaces (dispatch.go/notify.go) are the seam the 10-06 composition root adapts the real handlers + MCP registry onto — the dispatcher and Notifier unit-test against fakes with zero LLM/tool/DB wiring."

requirements-completed: [CAP-06]

# Metrics
duration: ~70min
completed: 2026-06-04
---

# Phase 10 Plan 05: Scheduler Handlers + Notifier + Dispatch Seam (6b) Summary

**Built the agent_job-shaped heart of CAP-06: the cron-free TaskKind handlers (reminder verbatim; agent_job a direct `agent.NewLlmAgent` mirroring `swarm.runChild` with its step budget INHERITED from the `agent_job_runs` row (SC#4/D-24) and `ask_user` auto-rejected via inject-and-continue so a scheduled job never blocks (D-25); backup via fixed-argv `docker exec pg_dump`/`neo4j-admin` (D-26) + a 24h-miss alert (SC#3)), the composite Notifier (WhatsApp/mail MCP self-send `send_message`/`send_email` + stdout fallback + notify-on-failure + bounded-retry signal, D-19/D-21/D-22), and the TaskKind→Handler dispatch seam the 10-03 tick loop drives (idempotent run completion + RISKY/DESTRUCTIVE immediate alert through the Notifier, D-27). Delivers SC#3 (backup dumps + 24h-miss alert) and SC#4 (budget-inherited agent_job + ask_user auto-reject) — with the agent_job dependency graph kept `internal/swarm`-free (D-24).**

## Performance

- **Duration:** ~70 min
- **Completed:** 2026-06-04
- **Tasks:** 2/2
- **Files:** 13 created, 0 modified

## Accomplishments

- **Task 1 — reminder + agent_job handlers (`8c30b05a`):**
  - `handler.go` (101 LOC): the cron-free `handlers` package contract — `Handler`/`HandlerMeta`/`Job`/`AgentDeps`; `childRegistry` drops `swarm_spawn` via the promoted `tools.Without` (NO `internal/swarm` import, D-24); `newAgentWorker` mirrors `swarm.runChild`'s `NewLlmAgent` construction VERBATIM except for the FLAT ephemeral `agent_job:<runID>` session (amendment #23 — never `runner.Turn`).
  - `reminder.go` (50 LOC): `ReminderHandler` returns the verbatim payload text; empty/malformed payloads still complete (D-21 never silent-fails).
  - `agentjob.go` (187 LOC): `AgentJobHandler.Run` builds `agent.NewBudget(BudgetOptions{MaxSteps:&stepBudget})` from the row (D-24/SC#4), drains the LlmAgent, and on `Actions.AwaitingInput` runs the auto-reject inject-and-continue (synthesized assistant `ask_user` turn + `RoleTool` answer keyed to `ai.ToolCallID` with the `auto-rejected` marker, D-25); a `maxAutoRejects` bound + the wall-clock `MaxDuration` guarantee it never blocks (<30s). `ask_user` STAYS in the registry.
  - Tests prove **SC#4**: `TestAgentJobBudgetInherit` (step_budget=10 caps LLM calls near max_steps+2 — NOT a fresh 25), `TestAgentJobBudgetDefaultWhenRowUnset` (step_budget=0 → default 25 allows >14 calls), `TestAskUserAutoReject` (AwaitingInput → marker in the summary, completes <30s, never blocks), `TestChildRegistryDropsSwarmKeepsAskUser`.

- **Task 2 — backup handler + composite Notifier + dispatch seam (`82a2f920`):**
  - `backup.go` (206 LOC): `BackupHandler` (postgres|neo4j) — LookPath-gated `exec.CommandContext(ctx, "docker", "exec", "aura-postgres", "pg_dump", -Fc, -f, dest, "aura")` / `"aura-neo4j", "neo4j-admin", "database", "dump", ..., "--to-path", dest`; the argv is constant operator-config, NEVER model output (T-10-16), and the socket is NEVER mounted (T-10-15). Dumps land in `AURA_BACKUP_DIR` (default `~/.aura/backups/`, `~`-expanded) with a 14d/7d rolling `sweepRetention`; `MissedBackupAlert` fires the SC#3 24h-miss `slog` line only past the window.
  - `notify.go` (171 LOC): `Notifier` + `compositeNotifier` resolving whatsapp→`send_message` / email→`send_email` via a cron-local `SelfSendResolver` (keeps cron free of an `internal/agent/tools` import); on MCP failure → stdout fallback + a non-nil error (notification-undelivered → D-22 bound-retry); unknown/unset routes fail-safe to stdout.
  - `dispatch.go` (178 LOC): `Dispatch` satisfies the 10-03 `Dispatcher` seam — routes by `TaskKind` via a kind→`Handler` map (no switch, Slice 0.9), `CompleteRun`s idempotently (`completed_with_hash`=run id, SC#2), notifies on success AND failure (D-21), and rides `RequiresImmediateAlert` through the Notifier for a Destructive task even in quiet hours while deferring non-destructive notifications and still firing in-window reminders (D-23/D-27).
  - Tests: `notify_test.go` (route resolution, MCP-failure stdout fallback + undelivered signal, unmounted/unknown route degrade, env default route), `dispatch_test.go` (routing + completion + failure-notify + unknown-kind-fails-loud + destructive-immediate-alert + quiet-hours-defer vs in-window-reminder-fires + idempotent-completer-non-fatal), `dispatch_integration_test.go` (db_integration: real `CompleteRun` writes the summary + redelivery idempotency — live against Postgres). `backup_test.go` (argv shape + no-payload-injection + LookPath gate + 24h alert + retention sweep + `~`/env dir; live docker-exec test Manual-Only / `t.Fatal`-under-CI).

## Task Commits

1. **Task 1: reminder + agent_job handlers (LlmAgent spawn + ask_user auto-reject)** — `8c30b05a` (feat)
2. **Task 2: backup handler + composite Notifier + dispatch seam** — `82a2f920` (feat)

## Verification

- `go vet ./...` + `go build ./...` → clean (whole module).
- `go vet -tags db_integration ./internal/cron/` → clean.
- `go test ./internal/cron/ ./internal/cron/handlers/` → ok (unit, Git Bash).
- `go test -race ./internal/cron/ ./internal/cron/handlers/` → **ok 1.02s each** (WSL native race) — agent_job auto-reject goroutine + dispatch are race-clean AND goleak-clean (both packages have a `goleak.VerifyTestMain`).
- **`go test -tags db_integration -run TestDispatch ./internal/cron/` → ok LIVE** against Postgres on 127.0.0.1:5432 (DSNs derived from `.env` POSTGRES_PASSWORD: aura_app / aura_migrate). `TestDispatchWritesSummaryToRun` (0.15s) + `TestDispatchCompletionIsIdempotent` (0.08s) — real execution, not a sub-second skip.
- **`go test -tags db_integration -race -run TestDispatch ./internal/cron/` → ok 1.015s** (WSL native race).
- `golangci-lint run ./internal/cron/...` → **0 issues**; `golangci-lint run --build-tags db_integration ./internal/cron/...` → **0 issues**.
- SC#4 verbose: `TestAgentJobBudgetInherit` / `TestAgentJobBudgetDefaultWhenRowUnset` / `TestAskUserAutoReject` (logs `agent_job.ask_user.auto_rejected`) all PASS.
- Acceptance greps: `agent.NewLlmAgent` in handler.go ✓; `exec.CommandContext` in backup.go ✓; `send_message` in notify.go ✓; `HandlerMeta` in dispatch.go ✓.
- **D-24 cycle/dep invariant:** `go list -deps ./internal/cron/handlers | grep internal/swarm` → EMPTY; `go list -deps ./internal/cron | grep internal/agent/tools` → EMPTY (cron stays tools-free — no tools→cron→tools cycle).
- File sizes (all ≤600 LOC): backup.go 206, agentjob.go 187, dispatch.go 178, notify.go 171, handler.go 101, reminder.go 50.

## Deviations from Plan

### Rule 3 (auto-fix blocking issue) — broke an import cycle the plan's stated layout would otherwise hit

**1. cron-local consumer-declared interfaces instead of cron importing handlers/tools**
- **Found during:** Task 2 — `internal/cron/dispatch.go` and `notify.go` (in package `cron`) needed `internal/cron/handlers` (for the `Handler` impls) and `internal/agent/tools` (for the MCP registry), but `internal/agent/tools/task.go` already imports `internal/cron` (10-04's documented deviation #2). cron→handlers→tools→cron and cron→tools→cron are both cycles.
- **Resolution:** Applied the SAME consumer-declared-interface pattern 10-04 used for `taskStore`. `dispatch.go` declares cron-local `Handler`/`Job`/`HandlerMeta` + a `RunCompleter` seam; `notify.go` declares `SelfSendResolver`/`SelfSendTool`. The real `handlers.{Reminder,AgentJob,Backup}Handler` and a `*tools.Registry`→`SelfSendResolver` adapter wire in at the composition root (`cmd/aura`, 10-06), which imports both. This keeps package `cron` free of `internal/agent/tools` (verified) AND keeps the plan's stated file layout (the `send_message`/`HandlerMeta` literals live in `cron/notify.go`/`cron/dispatch.go` exactly as the `<artifacts_produced>` `contains` checks require). The dispatcher + Notifier are now unit-testable against fakes with zero LLM/tool/DB wiring — a strict improvement.

### Within plan latitude

**2. `RunCompleter` seam over `*Store`**
- The plan's dispatch writes "the summary via `CompleteRun`". `DispatchDeps.Store` is the `RunCompleter` interface (`*Store` satisfies it) so the routing/notify/alert logic is unit-tested with a fake completer, and the real `Store.CompleteRun` is exercised at the db_integration tier. No behavior change — the live idempotency path is proven against Postgres.

**3. Live taskStore wiring NOT done here (scoped to 10-06)**
- The env-notes handoff mentions a thin adapter over `cron.Store` satisfying `tools.task.go`'s `taskStore`. That registration is a composition-root concern (the plan's `files_modified` lists none of `task.go`/`main.go`), so it lands in 10-06 alongside the `Handler`/`SelfSendResolver` adapters — keeping this plan's file ownership clean (no edits to 10-04's `task.go` or `cmd/aura/main.go`).

No other deviations — Rules 1/2/4 did not fire; no auth gates.

## Threat Flags

None beyond the plan's `<threat_model>` register — the new surface is exactly:
- **T-10-15** (docker socket exposure): fixed-argv `docker exec`, LookPath-gated, socket NEVER referenced — asserted by `TestBackupDumpArgv*Fixed` + the no-`docker.sock` scan.
- **T-10-16** (argv injection): the dump argv is constant operator-config; the Job payload is NEVER interpolated into the command — asserted by `TestBackupArgvCarriesNoPayload`.
- **T-10-17** (notification exfiltration): delivery only to the per-task route's configured recipient (per-task `notify` field / `AURA_SCHEDULER_NOTIFY_RECIPIENT`); the MCP allowlists already scope `send_message`/`send_email` (D-19/D-20).
- **T-10-18** (destructive agent_job): scoring gating at schedule time (10-04) + `RequiresImmediateAlert` rides the Notifier at dispatch (`TestDispatchDestructiveRidesImmediateAlert`); agent_job runs minus `swarm_spawn`.
- **T-10-19** (unbounded agent_job): budget-from-row hard cap (SC#4) + ask_user auto-reject completes <30s (`TestAgentJobBudgetInherit` + `TestAskUserAutoReject`).
- **T-10-20** (silent-failing cron): notify-on-failure (`TestDispatchFailureCompletesFailedAndNotifies`) + audit summary always written + the SC#3 24h missed-backup alert (`TestMissedBackupAlertFiresOnlyPast24h`).
- **T-10-SC** (package installs): none — no new packages.

## Known Stubs

None that block the plan's goal. Two intentional future-wiring seams land in 10-06 (the composition root), documented above as deviations #1/#3:
- The `Handler` map (`KindReminder/KindAgentJob/KindBackupPostgres/KindBackupNeo4j` → the real `handlers.*Handler` adapters) and the `SelfSendResolver` adapter over the mounted `*tools.Registry` are wired at boot. They are fully exercised here by fakes (`fakeHandler`/`fakeSelfSend`) and the live db_integration dispatch, so every routing/completion/delivery/risk-gate path is proven today.
- The live `taskStore` injection for `internal/agent/tools/task.go` is a 10-06 composition concern (this plan does not own `task.go`/`main.go`).

The backup live docker-exec path is Manual-Only (`AURA_BACKUP_LIVE=1` + stack up) by design (RESEARCH Open Q2 container-name stability); its argv/gate/retention/alert logic is unit-proven and `t.Fatal`s under `$CI` when unset (no skip-as-green).

## Self-Check: PASSED

- FOUND: internal/cron/handlers/handler.go
- FOUND: internal/cron/handlers/reminder.go
- FOUND: internal/cron/handlers/reminder_test.go
- FOUND: internal/cron/handlers/agentjob.go
- FOUND: internal/cron/handlers/agentjob_test.go
- FOUND: internal/cron/handlers/backup.go
- FOUND: internal/cron/handlers/backup_test.go
- FOUND: internal/cron/handlers/main_test.go
- FOUND: internal/cron/notify.go
- FOUND: internal/cron/notify_test.go
- FOUND: internal/cron/dispatch.go
- FOUND: internal/cron/dispatch_test.go
- FOUND: internal/cron/dispatch_integration_test.go
- FOUND commit 8c30b05a (Task 1)
- FOUND commit 82a2f920 (Task 2)
- STATE.md / ROADMAP.md NOT modified (orchestrator owns those writes); 10-03/10-04 files (scheduler.go, store.go, task.go, cmd/aura/main.go) untouched; go.mod/go.sum untouched
