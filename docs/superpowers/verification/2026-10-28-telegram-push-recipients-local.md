# Telegram pushes reach the chats the task names — local verification

Date: 2026-10-28. Branch `d10-push-recipients`, on top of `ccr-648b75d9-4hbqnw` (PR #198). Spec:
`docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md`. Plan:
`docs/superpowers/plans/2026-10-28-telegram-push-recipients.md`.

All database runs are on a disposable PostgreSQL 18 (`aura_it`), as `aura_app` unless named, with
`-p 1` across packages: run in parallel, `EnsureRoles` from two packages collides on the role row
(`tuple concurrently updated`), which the coverage gate avoids the same way.

## Task 1 — an identity push goes to the account seen last (R6, acceptance 2)

`TestIdentityPushReachesTheAccountSeenLast` (`internal/channels/telegram`, db_integration) links two
accounts through the real onboarding and checks where `Deliver` and `DeliverApproval` go at three
steps: neither has written (the one linked last), the first writes (the first), the second writes
(the second). Red at each step before the `ORDER BY`, as the probe of spec §1 measured; green after.

## Task 2 — the columns (R1, R4; acceptance 1)

- `TestMigrate0145NamesTelegramRecipients` (`internal/db`), on a fresh database stepped to 144:
  before up, neither column; after up, both, a NULL list and a two-id list accepted, an empty list
  refused by `scheduler_tasks_telegram_recipients_not_empty`, and the status CHECK allows
  `undeliverable`; after down, none of it; up again restores it.
- `TestTelegramRecipientsRoundTrip` (`internal/cron`): a task naming two ids reads them back from
  `CreateTask` and `GetTask`, a task naming none reads back `nil`; a pending row for one account and
  one for none read back from the insert and the sweep; a row marked undeliverable reads
  `undeliverable` with its reason and leaves the sweep.

## Task 3 — the push to one named account (R3, R5; acceptance 3)

`TestDeliverToTelegramAccount` (unit), for the text push and the approval prompt each:
- the named account gets one send, whichever order `LinkedAccounts` lists the links in, and the
  identity-wide lookup is never asked;
- an unlinked id and 0 are a permanent error, "telegram account N is no longer linked", with no
  send;
- a failing list is the list's error, not permanent;
- `ErrBlockedByUser`, `ErrChatNotFound`, `ErrNotStartedByUser` and `ErrUserIsDeactivated` are
  permanent; a wrapped network error and a 429 are not; `errors.Is` still finds each;
- no bot, or a bot and no store, is `(false, nil)` with no send.

`TestRegistryTelegramAccountDelivery`: no channel started is `(false, nil)`; a started channel
with the capability gets both calls, and a channel without it none.

## Task 4 — cron's fan-out and retries (R3, R4, R5; acceptances 4 and 6)

Unit (`internal/cron`):
- `TestTaskPushReachesEachTelegramRecipient`: recipients `1, 2, 1` send to 1 then 2, once each;
  a network failure on 2 queues one failed row for 2 and none for 1, and the record says it was not
  delivered; a permanent failure on 2 and 3 queues nothing and the record names both; quiet hours
  queue one pending row per recipient, a repeated id once; with no channel to push any of them the
  route chain runs and queues one failed row per recipient; a channel that stops part way queues a
  row only for the account it did not reach; a task naming none keeps the identity push, queues one
  row naming no account on a failure that can be retried, and none on a permanent one.
- `TestSweptTelegramRowReachesItsAccount`: a row naming account 1 goes to 1 and is delivered; one
  naming 2 fails and is marked failed with the channel's error; one naming 3, unlinked, ends
  `undeliverable`; a row naming none goes identity-wide; the route notifier is never asked.
- `TestApprovalPromptReachesEachTelegramRecipient`: a Telegram task's minted prompt goes to 1 and
  2, once each, though 1 fails, and the reminder is stamped; the same recipients on a stdout task
  go identity-wide.

db_integration, `TestTaskPushReachesBothLinkedChats` (`internal/channels/telegram`): two accounts
linked through the real onboarding, the real channel (offline bot, recorded sends), the real
`channels.Registry` and `cron.Dispatch`. A reminder naming both reaches both chats; the approval
prompt through the registry reaches both; after one account is unlinked the next run reaches the
other only and the origin conversation reads "telegram account 9000145102 is no longer linked".
Red without the recipient branch in `deliverToOrigin`: the push reached `[9000145102]` only, the
account the identity default picks.

## Task 5 — the tool asks (R2; acceptance 5)

Unit, `TestTaskScheduleNamesItsTelegramRecipients`: two links and none named return
`telegram_recipients_required` with the options `4242`, `4343` and `4242,4343`, labels naming
`@davide` and `Lavoro`, and persist nothing; named ids persist once each in the order given, a
repeat in the middle included, and the result names them; one link stores it; an unlinked id, no
link and a failing lookup are refused and persist nothing; recipients with `notify=stdout` are
refused before any lookup; `notify=stdout` never asks and persists `nil`. `task list` prints
`telegram_recipients=4242,4343`.

db_integration, `TestTaskToolPersistsTheChosenTelegramRecipients` (`cmd/aura`): the live tool,
built by `newTaskTool`, reads two links made through the real onboarding, asks, then persists the
chosen order and lists it; a NULL column lists `nil`.

## Gates

On `28c01092`: `go build ./...`, `go vet ./...` and with `db_integration`, golangci-lint 2.14.0 on
`internal/cron`, `internal/channels`, `internal/db`, `internal/agent/tools` and `cmd/aura` (0
issues), `make deadcode`, `make file-size`, `make tagged-tier-compile`, `sqlc` in sync; `-race`
unit on the touched packages and the whole `cmd/aura` suite (65 s, on `43c3dbd8`); db_integration
under `-race` on `internal/cron/...`, `internal/channels/...` and `internal/db`.

Coverage, `scripts/coverage_docker.sh` on a disposable database (`aura_cov`, 120 migrations):
owned surface 50036/57102 = 87.6% (the account-links slice measured 49851/56922 = 87.6%), package
policy passed.

## Mutation (`go-mutesting`, local, unit tiers, in a separate worktree)

| File | First run | After the fixes | Survivors left |
|---|---|---|---|
| `internal/channels/telegram/deliver_account.go` | 10/14, 71.4% (`43c3dbd8`) | 10/10, 100% (`a4be1e12`) | none |
| `internal/cron/deliver_recipients.go` | 20/25, 80.0% | 23/25, 92.0% (`28c01092`) | `.0`, identical to the source; `.7`, drops a `slog.Warn` |
| `internal/agent/tools/task_telegram_recipients.go` | 41/48, 85.4% | 46/48, 95.8% (`28c01092`) | `.0`, a slice capacity; `.13`, the fallback of a `json.Marshal` that cannot fail |

What the first runs found:
- `deliver_account.go`: the test meant to drive a bot with no store drove the no-bot case twice;
  the edit that should have given it a bot never applied. Two survivors dropped the resolver check.
  Two more were equivalent: `pushSeams` returned both seams nil when the bot was missing, so
  `sender == nil ||` decided nothing. It now returns `ok` (`a4be1e12`).
- `deliver_recipients.go`: a task naming no account could queue its retry under any id, and an
  error answering `Permanent()` false could be taken as permanent (`7abc10d9`).
- `task_telegram_recipients.go`: an empty list instead of `nil` for a task naming no account would
  pass every unit test and fail at the database, whose 0145 CHECK refuses it; a repeated id in the
  middle of the list and the name of an account with neither username nor first name were not
  exercised (`28c01092`).

## What this does not show

- A real Telegram chat: every send is recorded by the test double, so the Bot API's own refusals
  are telebot's sentinels, not observed responses.
- The approval sweep end to end on a database: its loop over the recipients is a unit test; the
  database leg starts at the registry.
- The crash window between a send and its record: delivery stays at least once (R4), untested.
- How the model words the choice to the operator; the directive is tested, the dialogue is not.
- The lab VM run with two phones (acceptance 7), which is the operator's.
