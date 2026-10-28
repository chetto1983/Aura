# Telegram pushes reach the chats the task names — Implementation Plan

**Status (2026-10-28):** in progress. The operator approved the spec, R6 included.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task.

**Goal:**
- A scheduled task names the Telegram accounts its pushes go to: one, another, or all.
- Each named account gets the push, and a failure is retried for that account only.
- A task that names none goes to the account seen most recently.

**Architecture:**
- **Migration.** `scheduler_tasks.telegram_recipients bigint[]` and
  `pending_notifications.telegram_user_id bigint`.
- **cron.** The task and the retry row carry the recipients; `ChannelDeliverer` gains a push to
  one Telegram account, called once per recipient (spec §4, amended at Task 3).
- **telegram.** `DeliverToTelegramAccount` over `LinkedAccounts`; permanent errors classified by
  the telebot sentinels and answered through `Permanent() bool`.
- **Tool.** The `task` tool asks for the recipients when two or more accounts are linked.

**Tech Stack:** PostgreSQL 18, golang-migrate, sqlc 1.31.1, Go (`internal/cron`,
`internal/channels`, `internal/channels/telegram`, `internal/agent/tools`, `cmd/aura`), telebot v4.

**Spec:** `docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md`.

## Global Constraints

- **Migration number.** `ls internal/db/migrations/ | tail -1` at landing.
- **Files stay ≤ 600 LOC.** `internal/agent/tools/task.go` is at 537: the tool's new code goes in
  `task_telegram_recipients.go`.
- **No new env var. No new dependency.**
- **#197.** Whichever lands second reconciles its test with R6.
- **A test changes only for a stated reason.**

## Review Focus

- **No retry resends to a recipient that already got the push**, on the live path or the sweep.
  Delivery stays at least once: a crash between a send and its record can repeat it, as today.
- **An unlinked recipient is skipped, never replaced.**
- **The permanent errors are exactly the four sentinels**; anything else is retried.

## Decisions taken while writing this plan

- **Recipients are a snapshot of ids**, not a mode: "all" stores the accounts linked at schedule
  time (spec R1).
- **A Telegram pending row names at most one account.** A deferred push is queued per recipient,
  so the sweep never fans out (review of #198, 2026-10-28).
- **The identity-wide default changes in the query** (`GetTelegramAccountByIdentity ORDER BY`), so
  every existing task benefits without a data migration.

## File structure

| File | Change |
|---|---|
| `internal/db/migrations/NNNN_telegram_push_recipients.{up,down}.sql` | New: the two columns and the CHECK. |
| `internal/db/migrate_NNNN_integration_test.go` | New: up/down and the CHECK. |
| `internal/db/queries/{scheduler_tasks,pending_notifications,telegram_accounts}.sql`, `internal/db/sqlc/*` | The columns in insert/select; the `ORDER BY`. |
| `internal/cron/store*.go`, `deliver.go`, `deliver_approval.go` | The fields, the recipients-aware origin push, the per-recipient retry rows. |
| `internal/cron/deliver_recipients.go` (new) | The fan-out over the recipients and its result handling. |
| `internal/channels/deliver.go`, `registry.go` | `TelegramAccountDeliverer`; the `Registry` methods. |
| `internal/channels/telegram/deliver_account.go` (new) | `DeliverToTelegramAccount`, `DeliverApprovalToTelegramAccount`, `classified`. |
| `internal/agent/tools/task_telegram_recipients.go` (new), `task.go` | The field, the directive, the validation, `list`. |
| `cmd/aura/serve_dispatch.go`, the tool wiring | The linked-accounts seam for the tool. |

## Tasks

### Task 0 — Record the measurement (PRD-first)
- [x] `prd.md`, Telegram section: the heap-order measurement of spec §1, with what it does not
  show (spec §7).
- [x] Commit `docs(prd): measure which linked Telegram account an identity push reaches`.

### Task 1 — The default goes to the account seen last (R6, acceptance 2)
- [x] Red, db_integration: the probe of spec §1 as a test; after the first chat's update, the
  identity push goes to the first chat.
- [x] Green: the `ORDER BY` in `GetTelegramAccountByIdentity`; `sqlc generate`.
- [x] Commit `fix(telegram): an identity push goes to the account seen last`.

### Task 2 — The columns (R1, R4, R7; acceptance 1)
- [x] Red: the migration test (up/down, the CHECK refuses `'{}'`).
- [x] Green: the migration (0145); the cron store reads and writes `TelegramRecipients` and
  `TelegramUserID`; their round trips in the cron db_integration tests.
- [x] Commit `feat(db): a scheduled task names its Telegram recipients`.

### Task 3 — The telegram push to one named account (R3, R5; acceptance 3)
- [x] Red, unit: acceptance 3 (`deliver_account_test.go`), and the `Registry` methods.
- [x] Green: `deliver_account.go`; the send and approval paths of `deliver.go` shared with it; the
  `TelegramAccountDeliverer` capability and its `Registry` methods. Spec §4 amended: one account
  per call instead of a list with a result per account.
- [x] Commit `feat(telegram): push to one named account and say when no retry can help`.

### Task 4 — cron uses it, and retries per recipient (R4, R5; acceptance 4, 6)
- [x] Red, unit: acceptance 4 (`deliver_recipients_test.go`). Red, db_integration: acceptance 6
  (`internal/channels/telegram/deliver_recipients_integration_test.go`, red without the recipient
  branch: the push reached one chat).
- [x] Green: `deliverToOrigin`, `deliverSweptRow` and the approval sweep; the record lines. A
  deferred push queues one row per recipient; the approval sweep keeps pushing once, at mint,
  logs a failed recipient and stamps the reminder (spec §4). A permanent error queues no row on
  every path, and a swept row that turns permanent ends `undeliverable` (0145 amended).
- [x] Commit `feat(cron): a task's Telegram pushes reach its recipients, one retry each`.

### Task 5 — The tool asks (R2; acceptance 5)
- [ ] Red, unit: acceptance 5.
- [ ] Green: `task_telegram_recipients.go`, the seam and its wiring; `list` names the recipients.
- [ ] Commit `feat(tools): ask which Telegram accounts a task reaches`.

### Task 6 — Close
- [ ] Mutation ≥ 70% on `deliver_account.go`, `deliver_recipients.go` and
  `task_telegram_recipients.go`, survivors named.
- [ ] `bash scripts/coverage_docker.sh`.
- [ ] `docs/superpowers/verification/2026-10-28-telegram-push-recipients-local.md`, with what it
  does not show.
- [ ] Lab VM (acceptance 7), in its own verification file.
- [ ] PRD and this plan's status.
