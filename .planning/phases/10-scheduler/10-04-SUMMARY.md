---
phase: 10-scheduler
plan: 04
subsystem: scheduler
tags: [scheduler, tool, action-router, cli, openai-wire-schema, scoring-gate, risk-tier, doctor]

# Dependency graph
requires:
  - phase: 10-scheduler
    plan: 02
    provides: cron.Store + cron.ParseSchedule/NextRunAt + scheduler_tasks/agent_job_runs (migration 0009)
provides:
  - internal/agent/tools.ActionRouter (D-09, generic action→handler dispatch; Slice 7 reuses it)
  - internal/agent/tools.TaskTool — the ONE non-deferred `task` tool (action enum, OpenAI-wire-safe schema D-10)
  - internal/agent/tools.taskStore consumer-declared seam (live cron store injected at 10-05)
  - cmd/aura `task` CLI — full triad (--cron/--at/--every) + --max-steps + list/cancel/run_now/approve/runs/doctor (D-14/D-17)
affects: [10-05, 10-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Multi-action tool = ONE manifest entry via ActionRouter (D-09): replaces N near-duplicate tool files; OpenAI-wire-safe schema with top-level required=[\"action\"] ONLY, no root oneOf/anyOf/enum (D-10, nanobot regression #3113)."
    - "Consumer-declared store seam (golang-structs-interfaces): the tools package declares taskStore with its own tool-local types (ScheduledTask/CreateTaskInput), so internal/agent/tools never imports internal/cron concretely; the live store adapts at registration (10-05)."
    - "Scoring as the schedule gate (D-27): scoring.ComputeTaskTier → GateRecommended ⇒ status=pending_approval BEFORE the task ever fires; approve is the only transition out (T-10-13)."
    - "CLI persistence via raw parameterized SQL over the aura_app pool for the surface the 10-02 store does not expose (status-aware INSERT, approve/run_now UPDATEs, runs + doctor aggregates) — never string-concatenated (T-10-01)."

key-files:
  created:
    - internal/agent/tools/action.go
    - internal/agent/tools/action_test.go
    - internal/agent/tools/task.go
    - internal/agent/tools/task_test.go
    - cmd/aura/task.go
    - cmd/aura/task_test.go
  modified:
    - cmd/aura/main.go
  deleted: []

key-decisions:
  - "task.go imports internal/cron for the schedule engine (cron.ParseSchedule/NextRunAt). Verified internal/cron does NOT import internal/agent/tools, so there is no cycle (the plan's key_link wants the `cron.` reference). The STORE operations stay behind the consumer-declared taskStore interface so the tool package does not depend on cron.Store's concrete shape — interface segregation per the plan's seam instruction."
  - "The CLI persists via raw parameterized SQL (not cron.Store.CreateTask) because (a) CreateTask hardcodes status='active' and the schedule path must be able to write pending_approval, and (b) approve/run_now/runs/doctor need surface the 10-02 store does not yet expose AND store.go is owned by the parallel 10-03 executor (must not edit). All SQL is parameterized ($1..$N) — T-10-01."
  - "run_now is an UPDATE ... WHERE status='active' (RowsAffected==0 ⇒ refuse): a pending_approval task cannot be run-bypassed, enforcing T-10-13 (approve is the only path out of pending_approval). Verified live."
  - "task tool actions schedule|list|cancel|run_now|approve route through the ActionRouter; the CLI mirrors them plus runs+doctor (CLI is the operator superset, D-14/D-17)."
  - "AlertThreshold on TaskTool defaults to Risky when empty (config owns AURA_RISK_ALERT_THRESHOLD; scoring takes the threshold as an argument, never reads env — the scoring purity contract)."

patterns-established:
  - "ActionRouter (action.go, 58 LOC) is the first D-09 consumer and is kept generic (no cron/scheduler types) so Slice 7 skill_* reuses it verbatim."

requirements-completed: [CAP-06]

# Metrics
duration: ~55min
completed: 2026-06-04
---

# Phase 10 Plan 04: Scheduler Tool Surface + Operator CLI (6b part) Summary

**Built the LLM tool surface and the operator keyboard for the scheduler: the generic `ActionRouter` (D-09, first consumer — one manifest entry fronts a multi-action tool), the ONE non-deferred `task` tool with the `action` enum and an OpenAI-wire-safe schema (`required=["action"]` only, no root oneOf/anyOf/enum — the load-bearing D-10 discipline that keeps DeepSeek from 400ing), scoring-gated scheduling that routes destructive payloads to `pending_approval` before they can fire (D-27/T-10-12), and the `aura task` CLI exposing the full `--cron/--at/--every` triad + `--max-steps` plus `list/cancel/run_now/approve/runs/doctor` (D-14/D-17) — making every grammar path operator-testable without an LLM.**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-06-04
- **Tasks:** 2/2
- **Files:** 6 created, 1 modified

## Accomplishments

- **Task 1 — ActionRouter + task tool (`0f991462`):**
  - `action.go` (58 LOC): `ActionRouter` = `map[string]ActionFunc` + `Dispatch(ctx, action, args)`; an unknown action returns a structured error naming the valid set (never panics); `Actions()` returns the sorted names (stable for the schema enum + error messages); `NewActionRouter` copies the handler map (caller mutation can't change dispatch). Generic by construction — no cron/scheduler types — so Slice 7 reuses it.
  - `task.go` (364 LOC): `TaskTool` (Deferred=FALSE, D-11) with a tight one-line Summary and a turn-stable Description. The schema (`taskParamsSchema`) has top-level `required:["action"]` ONLY; per-action requirements live in field descriptions; NO root `oneOf/anyOf/enum` (the `action` property carries a property-level string enum, which IS wire-safe). `Execute` parses the `action` discriminator and dispatches via the ActionRouter. The schedule action validates the grammar via `cron.ParseSchedule` (gronx.IsValid before persist, T-10-14) + DST-safe `cron.NextRunAt`, computes `scoring.ComputeTaskTier`, and routes Risky/Destructive ⇒ `pending_approval` (T-10-12/D-27). A consumer-declared `taskStore` interface (with tool-local `ScheduledTask`/`CreateTaskInput` types) is the seam the live cron store satisfies at registration (10-05).
  - `task_test.go`: **`TestTaskSchema`** is the load-bearing D-10 gate — it unmarshals `Parameters` and asserts `required==["action"]` exactly + absence of root `oneOf/anyOf/enum`. Plus: cron→active, destructive-payload→pending_approval, invalid-cron-rejected-before-persist (nothing persisted), list renders `[awaiting approval]` + next_run_at, cancel/run_now/approve routing, missing task_id / missing action / unknown action errors, store-error propagation, and `TestRegistryValidatesWithTaskTool` (the Phase-9 ValidatesWithSwarmSpawn precedent). `action_test.go`: dispatch known/unknown/handler-error-propagation/sorted-actions/map-copy.

- **Task 2 — `aura task` CLI (`10538b09`):**
  - `cmd/aura/task.go` (422 LOC): `runTask` switches `schedule|list|cancel|run_now|approve|runs|doctor` (mirrors `runWeb`; hand-parsed, no cobra; `config.LoadDB()` so no `OPENROUTER_API_KEY`). `taskSchedule` parses the full triad `--cron/--at/--every` + `--max-steps` + `--kind/--args/--notify/--tz`, validates via `cron.ParseSchedule` + `cron.NextRunAt`, computes the tier, and inserts active/pending_approval via a parameterized INSERT. `taskList` renders active + pending_approval with `[awaiting approval]` + next_run_at. `taskCancel` uses `cron.Store.CancelTask`. `taskRunNow` is `UPDATE ... WHERE status='active'` (refuses pending). `taskApprove` is the only `pending_approval→active` transition (T-10-13). `taskRuns` lists the agent_job_runs ledger (optional `--task`). `taskDoctor` reports active/pending/due/next-run/in-flight/heartbeat-staleness against PG, exits 0 healthy / 71 infra.
  - `cmd/aura/task_test.go`: unit tests for the pure helpers (triadToSpec cron/at/every, payloadOrEmpty, defaultSchedulerTZ env, nullable* converters, fmtTimePtr).
  - `cmd/aura/main.go`: `case "task": runTask(...)` + the `task` verb in `usage()`.

## Task Commits

1. **Task 1: ActionRouter + non-deferred task tool (OpenAI-wire-safe schema)** — `0f991462` (feat)
2. **Task 2: aura task CLI — full triad + doctor** — `10538b09` (feat)

## Verification

- `go vet ./...` + `go build ./...` → clean.
- `go test ./internal/agent/tools/ ./cmd/aura/` → ok (Git Bash, no -race).
- `go test -race ./internal/agent/tools/ ./cmd/aura/` → ok (WSL native race).
- `golangci-lint run ./internal/agent/tools/... ./cmd/aura/...` → **0 issues.**
- `go test -run TestTaskSchema ./internal/agent/tools/` → PASS (D-10 schema-shape gate).
- **Live operator smoke (Postgres up, aura_app/aura_migrate DSNs derived from .env):**
  - `aura task schedule --at … --kind reminder` → `risk=safe, status=active`, next run printed.
  - `aura task schedule --every 10 --kind agent_job --args '{"goal":"drop the staging db"}'` → `risk=destructive, status=pending_approval` (scoring gate fired).
  - `aura task list` → both rows; the agent_job shows `pending_approval [awaiting approval]`.
  - `aura task doctor` → active=1 pending=1 due=0 next-run + in-flight=0 + `status: OK`.
  - `aura task run_now <pending>` → **refused, exit 64** (T-10-13). `aura task approve <pending>` → now active. `aura task run_now <now-active>` → queued. `aura task cancel <id>` → soft-cancel; post-cancel `list` empty.
  - `aura task bogus` → unknown subcommand, exit 64.
- Acceptance greps: `ActionRouter` in action.go = 7; `"required": ["action"]` in task.go = 1; `cron.` in task.go = 5; `func runTask` in task.go = 1; `case "task"` in main.go = 1.
- File sizes: largest touched is `cmd/aura/task.go` at 422 LOC (all ≤600).
- Forbidden-file check: `git diff fe79b33..HEAD -- go.mod go.sum internal/cron/ STATE.md ROADMAP.md` = empty (none touched). Only the 7 owned files changed.

## Deviations from Plan

### Persistence-path discretion (within plan's stated latitude)

**1. CLI persists scheduled tasks via raw parameterized SQL instead of `cron.Store.CreateTask`**
- **Found during:** Task 2.
- **Issue:** `cron.Store.CreateTask` hardcodes `status='active'`, but the schedule path must be able to write `pending_approval` (D-27); and `approve`/`run_now`/`runs`/`doctor` need store surface the 10-02 store does not expose. `internal/cron/store.go` is owned by the parallel 10-03 executor (must not edit).
- **Resolution:** The CLI opens its own `aura_app` pool (the `dbPing`/`dbStatus` precedent) and runs the status-aware INSERT + the approve/run_now/runs/doctor aggregates as **parameterized** SQL ($1..$N — never concatenated, T-10-01), while using `cron.ParseSchedule`/`cron.NextRunAt` for the grammar (the plan's `ParseSchedule` key_link) and `cron.Store.CancelTask` for cancel. Zero edits to `internal/cron`.

**2. `task.go` (tool) imports `internal/cron` for the schedule engine**
- **Found during:** Task 1. The plan's `<artifacts_produced>` hedged "so the tool package does NOT import internal/cron concretely IF a cycle would form." Verified `internal/cron` does NOT import `internal/agent/tools`, so no cycle forms; the `key_link` explicitly wants the `cron.` reference. The schedule grammar reuses the shipped, DST-proven `cron.ParseSchedule`/`cron.NextRunAt` rather than re-implementing it. The STORE operations stay behind the consumer-declared `taskStore` interface (interface segregation), so the tool is not coupled to `cron.Store`'s concrete method set.

No other deviations — Rules 1-3 did not fire; no auth gates.

## Threat Flags

None. The new surface is exactly the plan's `<threat_model>` register:
- **T-10-11** (OpenAI-wire schema break): `required=["action"]` only, no root oneOf/anyOf/enum; `TestTaskSchema` asserts the shape.
- **T-10-12** (destructive scheduled task): `scoring.ComputeTaskTier → GateRecommended ⇒ pending_approval` before the task fires; verified live (drop-payload agent_job → pending_approval).
- **T-10-13** (approval bypass): `approve` is the only `pending_approval→active` transition; `run_now` refuses a pending task (live-verified exit 64).
- **T-10-14** (unvalidated cron): `cron.ParseSchedule` (gronx.IsValid) runs before any persist; `TestTaskScheduleInvalidCronRejectedBeforePersist` asserts nothing is written on a bad expr.
- **T-10-SC** (package installs): none — no new packages (gronx landed in 10-02).
No new endpoints/auth-paths/trust boundaries beyond the documented ones.

## Known Stubs

None that block the plan's goal. The `taskStore` interface is declared but not yet wired to the live cron store — that injection happens in 10-05 (`buildBaseRegistry` registration), which the plan explicitly scopes out of this plan to keep file ownership clean. The interface is fully exercised by the `fakeTaskStore` in `task_test.go`, and the CLI (the operator path) is fully wired and live-smoked against PG, so every grammar/governance path is verified end-to-end today without the tool registration.

## Self-Check: PASSED

- FOUND: internal/agent/tools/action.go
- FOUND: internal/agent/tools/action_test.go
- FOUND: internal/agent/tools/task.go
- FOUND: internal/agent/tools/task_test.go
- FOUND: cmd/aura/task.go
- FOUND: cmd/aura/task_test.go
- MODIFIED (verified): cmd/aura/main.go (case "task" + usage)
- FOUND commit 0f991462 (Task 1)
- FOUND commit 10538b09 (Task 2)
- go.mod / go.sum / internal/cron / STATE.md / ROADMAP.md NOT modified (verified via git diff)
