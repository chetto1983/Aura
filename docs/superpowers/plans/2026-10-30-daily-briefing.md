# Daily briefing — Implementation Plan

**Status (2026-10-30):** spec agreed with the operator the same day. Nothing implemented yet.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task.

**Goal:**
- A scheduled job writes the operator's daily briefing from calendar, board and unread mail,
  read live, in the operator's timezone.
- The board snapshot puts what is due first, so no overdue card hides behind the 32-card cap.
- A dumb baseline, rendered from the same run's tool results with no model, is what the model
  briefing must beat on the lab VM.

**Architecture:**
- **Scheduler.** A cron task with no `tz` takes the identity's profile zone, then
  `AURA_TIMEZONE`, then `Europe/Rome` as the last resort. The `task` tool resolves the zone
  before it calls the pure `cron.ParseSchedule`.
- **Job clock.** `AgentDeps` gains the profile lookup and the deployment zone. `newAgentWorker`
  sets `Location` the way `runner.turnLocation` does.
- **Board.** `renderBoard` lists the cards due within 24 hours, overdue first, before the
  columns. Each card is shown once, and these count toward the cap.
- **Skill.** `internal/skills/embed/briefing-aura/SKILL.md`, under 100 lines of body.
- **Baseline.** `scripts/briefing_baseline.py` renders the dumb briefing from a run's recorded
  tool results (spec, open question 1).

**Tech Stack:** Go (`internal/agent/tools`, `internal/cron/handlers`, `cmd/aura`), the embedded
skills, Python 3 for the measurement script.

**Spec:** `docs/superpowers/specs/2026-10-09-daily-briefing-design.md`.

## Global Constraints

- **No migration, no new env var, no dependency.** `AURA_TIMEZONE` already exists
  (`config.Timezone`).
- **Files stay ≤ 600 LOC.** `internal/agent/tools/task.go` is at 561, so the zone resolution goes
  in `task_timezone.go`.
- **A test changes only for a stated reason.**
- **The acceptance is the lab VM's five working days,** which need the operator. The local
  close names it as owed.

## Review Focus

- **One zone rule** for the interactive turn, the job's clock and the schedule's fallback:
  identity, then deployment. No second parser: `tools.LocationOrUTC`.
- **The board's due section never drops a card.** The "more not shown" count stays exact, and no
  card appears twice.
- **The skill forbids writes,** and the acceptance counts any.

## Decisions taken while writing this plan

- **"Due within 24 hours" rather than "due today".** The tool has no zone, and overdue already
  means "before now" in the cockpit (`web/src/board/boardModel.ts`). The briefing converts with
  its clock hint.
- **The due section appears only on the whole-board `list`,** not when one column is asked for.
  A column listing is a request for that column.
- **The deployment zone reaches the task tool as a string.** `config.Timezone` is read once at
  boot, as the runner's `location` is.

## File structure

| File | Change |
|---|---|
| `internal/agent/tools/task_timezone.go` | New: the empty-`tz` resolution. |
| `internal/agent/tools/task.go` | Two seam fields; `resolveSchedule` takes the resolved zone; the `tz` description. |
| `internal/agent/tools/task_timezone_test.go` | New: the fallback order. |
| `cmd/aura/serve_adapters.go` | Wires the profile store and the deployment zone into the tool. |
| `internal/cron/handlers/handler.go`, `agentjob.go` | `AgentDeps` gains the lookup; `newAgentWorker` gets the location. |
| `internal/cron/handlers/*_test.go` | The job agent's location is the identity's. |
| `cmd/aura/serve_dispatch.go` | Wires the profile store and the zone into `AgentDeps`. |
| `internal/agent/tools/board_actions.go`, `board_test.go` | The due section. |
| `internal/skills/embed/briefing-aura/SKILL.md`, `internal/skills/builtin_names_test.go` | The skill and its name. |
| `scripts/briefing_baseline.py`, `scripts/briefing_baseline_test.py` | The baseline renderer and its unit tests. |

## Tasks

### Task 1 — A cron task with no tz runs in the identity's zone
- [x] Red, unit: an identity in `America/New_York` schedules a cron with no `tz`, and the stored
  `TZ` is New York. With no profile zone, `AURA_TIMEZONE` applies. With neither, `Europe/Rome`.
  An explicit `tz` always wins.
- [x] Green: `task_timezone.go`; the seam; the wiring; the tool description names the order.
  db_integration: `TestACronWithoutTZFiresOnTheIdentitysWallClock`, red without the fallback
  (Rome, 02:30 in New York), green with it (07:30).
- [x] Commit `fix(scheduler): a cron task without tz runs in the identity's zone`.

### Task 2 — The job's clock is the identity's
- [x] Red, unit: the clock in a job's model request carries New York's offset for an identity in
  New York, and the deployment's (Tokyo) with no profile zone. Red without `Location` on both.
- [x] Green: `AgentDeps.Timezones`/`Location`, `clockLocation`, `newAgentWorker`, the wiring in
  `serve_dispatch.go` and its test.
- [x] Commit `fix(scheduler): a scheduled agent reads the clock in the identity's zone`.

### Task 3 — The board shows what is due first
- [x] Red, unit: 40 cards, one overdue at position 40. The whole-board list shows it in a "due"
  section, the "more not shown" count is exact, and no card is listed twice. A column listing
  has no due section.
- [x] Green: `renderBoard` and `renderDue`; the tool's description names the due section.
- [x] Commit `feat(board): the snapshot lists what is due first`.

### Task 4 — The briefing-aura skill
- [ ] The skill per spec §Shape, with the forbidden list and the 1,500-character cap.
- [ ] `builtin_names_test.go` and the builtin-validity tests pass.
- [ ] Commit `feat(skills): briefing-aura writes the daily briefing from live sources`.

### Task 5 — The baseline renderer
- [ ] Red: `briefing_baseline_test.py` with recorded tool results: events in time order, cards due
  on or before the day by due date, the unread count and the five newest senders.
- [ ] Green: `scripts/briefing_baseline.py`.
- [ ] Commit `feat(scripts): render the briefing baseline from a run's tool results`.

### Task 6 — Close
- [ ] vet, build, race, lint, `bash scripts/coverage_docker.sh`, and the mutation spot-check on
  `task_timezone.go` and the due section.
- [ ] `docs/superpowers/verification/2026-10-30-daily-briefing-local.md`.
- [ ] Lab VM, the spec's acceptance 1–4, over five working days, in its own verification file.
- [ ] Merge to `master`.
