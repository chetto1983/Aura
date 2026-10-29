# Telegram pushes reach the chats the task names — design

Date: 2026-10-28
Status: approved by the operator on 2026-10-28, R6 included. D10 of
`docs/superpowers/specs/2026-10-27-telegram-account-links-design.md`, split into its own slice.
Plan: `docs/superpowers/plans/2026-10-28-telegram-push-recipients.md`.

## 1. Why

One person can link several Telegram accounts, for example two phones with two numbers. The
operator asked (2026-10-28) that the scheduler deliver to one of them, the other, or both, chosen
when the task is scheduled, and asked for "both" from the start.

Pushes bound to a conversation already reach that conversation's chat (D9, `18b0988c`). Two pushes
are bound to the identity instead, and both resolve their recipient with
`GetTelegramAccountByIdentity`, a `:one` query with no `ORDER BY`:
- the result of a task with `notify=telegram` (`DeliverToIdentity`);
- the scheduled-task approval prompt (`DeliverApproval`), whatever the task's route.

**Measured on 2026-10-28** with a scratch db_integration test (not committed) on PostgreSQL 18, on
`be99204`, with two accounts linked through the real onboarding:

| Step | `Deliver` and `DeliverApproval` reached |
|---|---|
| after linking the first, then the second | the first |
| after the first chat sends an update (`TouchLastSeen`) | the second |
| after the second chat sends an update | the first |

With no `ORDER BY`, PostgreSQL promises no order. On this database and plan, the query returned
the row first in physical order, and each inbound update wrote a new row version with
`last_seen_at` that came back last. So the push went to the account used **least** recently: the
phone in your hand is the one that does not ring. This matches #196, where the
notifications went to an obsolete account that had blocked the bot.

**Where the choice can live** (`git grep`, 2026-10-28):
- `scheduler_tasks` holds `notify_route` (`none`, `stdout`, `telegram`, `whatsapp`, `email`) and no
  recipient. `pending_notifications` holds the route and no recipient either.
- The `task` tool already refuses to schedule when `notify` is missing, with a choice directive
  the model turns into `ask_user(kind=choice)`. It also resolves a destination before it persists
  (`Destinations`, `task_destination_test.go`) and refuses a route that cannot deliver.
  `internal/agent/tools/task.go` is at 537 lines.
- The failed-push retry is the sweep over `pending_notifications`: `status` is `pending`,
  `failed` or `delivered`; a `failed` row is retried with backoff while `attempts` is under the cap.
- telebot v4 (`v4.0.0-beta.10`, `errors.go`) maps API errors to sentinels, among them
  `ErrBlockedByUser`, `ErrChatNotFound`, `ErrNotStartedByUser` and `ErrUserIsDeactivated`.
- The cockpit's scheduler board does not show the route.

## 2. Goals, non-goals

Goals:
1. A task names the Telegram accounts its pushes go to: one, another, or all of them.
2. Each named account gets the push. A failure on one does not hold or repeat the others.
3. A push that cannot succeed (the bot was blocked, the chat is gone) is not retried; the
   conversation the task came from says so.
4. A task that names no account goes to the account seen most recently, not to the one seen least.

Non-goals:
- Showing or editing the recipients in the cockpit's scheduler board.
- Other channels: WhatsApp and email have one destination per identity.
- The relink guard of the previous slice (§6).

## 3. Decisions

| # | Decision | Why |
|---|---|---|
| R1 | `scheduler_tasks.telegram_recipients bigint[]`, NULL when the task names none; when set it is not empty and holds each id once. "All" stores every account linked at schedule time. | One column covers one, another and all. A snapshot is explicit: an account linked later is not added behind the operator's back. |
| R2 | The `task` tool takes an optional `telegram_recipients` (array of Telegram user ids), and only with `notify=telegram`. With two or more linked accounts and none named, it refuses with a choice directive that lists each account (name, `@username`, id) and "all". With one linked account it stores that one. With none linked it refuses, as a route that cannot deliver is refused today. An id not linked to the identity is refused; a repeated id is stored once. `aura task schedule` applies the same rule through `--telegram-recipients` (ids separated by commas, or `all`); it cannot ask, so with two or more links and none named it refuses and lists them (operator, 2026-10-28, from the review of #198). | The same guard as a missing `notify`, at the same moment. So a task the agent or the CLI schedules after this slice with `notify=telegram` names its recipients; the cockpit's scheduler board takes none (R6). |
| R3 | A task's Telegram pushes, the result and the approval prompt, go to its recipients. A recipient no longer linked is skipped: a terminal result, named in the record, with no retry row; it is never replaced by another account. | Unlink means stop (previous slice, D6). |
| R4 | Each recipient is sent to on its own. A failed recipient gets its own `pending_notifications` row, with the new column `telegram_user_id`, and the sweep retries that recipient only; a push deferred to the end of quiet hours is queued as one row per recipient too, so a Telegram row always names at most one account. The push counts as handled when every recipient is delivered, queued, skipped (R3) or recorded as permanent (R5). Delivery stays at least once: a crash between a send and its record can send it again, as today. | Retrying the whole push would resend to the phones that already got it, at every attempt. |
| R5 | `ErrBlockedByUser`, `ErrChatNotFound`, `ErrNotStartedByUser` and `ErrUserIsDeactivated` are permanent: no retry row; the origin conversation records "not delivered to <account>: <reason>". | No retry can succeed. The fix is the operator's: unlink, or unblock the bot. |
| R6 | With no recipients named, `GetTelegramAccountByIdentity` orders by `COALESCE(last_seen_at, added_at) DESC, added_at DESC, telegram_user_id DESC`, the order the password reset and the recovery already use. After R2, that is a task scheduled before this slice; a task switched to Telegram in the cockpit's scheduler board, which takes no recipients (non-goal); and the approval prompt of a task whose route is not Telegram. | Goal 4. It also fixes #196 for every task scheduled before this slice. |
| R7 | The migration takes the next free number at landing (0145 today). | CLAUDE.md. |

R6 answers "existing tasks with two accounts" (approved 2026-10-28). #197 refused such a push
as ambiguous instead. Reconciled on 2026-10-28, when #197's commits were merged after this slice:
R6 kept, the refusal dropped, its incident record and regression cases kept under R6.

## 4. Design

**Migration.** `ALTER TABLE aura.scheduler_tasks ADD COLUMN telegram_recipients bigint[]` with
`CHECK (telegram_recipients IS NULL OR cardinality(telegram_recipients) > 0)`;
`ALTER TABLE aura.pending_notifications ADD COLUMN telegram_user_id bigint`, and its status CHECK
gains `undeliverable`: a swept row whose push turns permanent (the account was unlinked during
quiet hours, the bot was blocked between attempts) ends there instead of spending the attempt
bound; the sweep and the one-shot purge count only `pending` and `failed` under the bound as live,
so neither reads it. Down deletes the undeliverable rows, which nothing will send, and drops the
rest. Neither table has row security (0087, cross-identity daemon tables). The status was added
while writing Task 4 (2026-10-28), before 0145 left the branch.

**cron.** `Task.TelegramRecipients []int64` and `PendingNotification.TelegramUserID int64`; a NULL
column reads as 0, which means none (`pgtype.Int8` at the boundary). `ChannelDeliverer` gains
`DeliverToTelegramAccount(ctx, identityID string, telegramUserID int64, text string) (bool, error)`
with the tri-state of every channel push. `deliverToOrigin` calls it once per recipient, each id
once, when the route is `telegram` and the task names recipients; `deliverSweptRow` calls it for
the one account a row names. An error is permanent when it answers `Permanent() bool` true through
`errors.As`, so cron imports neither `channels` nor `telegram`. A permanent error queues no retry
row, on these paths and on the identity-wide one (R5). The approval sweep calls
`DeliverApprovalToTelegramAccount` per recipient the same way, and keeps today's rule: it pushes
the prompt once, when it mints the pause. A recipient that fails is logged and not retried, and the
prompt stays in the cockpit's approvals; `approval_reminded_at` is stamped as today.

**channels.** The capability `TelegramAccountDeliverer` (both methods). The `Registry` methods of
the same names use the started channel that implements it; none started is `(false, nil)`, which
is "not mine", and the caller falls back to its route as today.

**telegram.** `DeliverToTelegramAccount` reads `LinkedAccounts(identity)`, sends to the account
when the identity still links it (with the existing chunking and pacing), and returns a permanent
error naming the account when it does not (R3), never another account. A send error is permanent
when `errors.Is` matches one of the four sentinels (R5). `DeliverApprovalToTelegramAccount` does
the same with the approval prompt.

**Amended 2026-10-28, Task 3.** The approved text had one call per push that took the recipient
list and returned a `RecipientResult` per account. One account per call replaces it: cron does not
import `channels`, so a result struct would have needed an adapter in `cmd/aura`, while primitives
let `*channels.Registry` satisfy the cron seam directly as it does today; the sweep sends a row's
one account anyway, so the live path and the sweep use the same call. The cost is one
`LinkedAccounts` read per recipient instead of one per push; an identity links a handful.

**Tool.** A new file, `internal/agent/tools/task_telegram_recipients.go`: the field, the
directive, and the validation against the identity's linked accounts through a narrow seam over
`telegram.Store.LinkedAccounts`. `task list` names each task's recipients.

## 5. Acceptance (machine-checkable)

1. **db_integration, migration.** Up and down; the CHECK refuses an empty array.
2. **db_integration, telegram store.** With two accounts and no recipients, the identity push goes
   to the account that sent the last update (red today: the probe of §1, committed as a test).
3. **telegram unit.** `DeliverToTelegramAccount` and `DeliverApprovalToTelegramAccount` send to the
   named account, in any order of `LinkedAccounts`, and never ask the identity-wide lookup; an
   unlinked account (or 0) is a permanent error naming it, with no send; the four sentinels are
   permanent, a network error and a rate limit are not; a channel with no bot or no store is
   `(false, nil)`.
4. **cron unit.** A two-recipient push sends to each once, a repeated id once; with one failure it
   inserts one failed row for that account and none for the other; the sweep sends that row to
   that account only, and ends a row whose push is permanent as undeliverable; a permanent failure
   and a skipped account insert no row and the record names them; a deferred push inserts one row
   per recipient; with no channel running the route chain queues one row per recipient; a failed
   approval recipient inserts no row and the reminder is stamped.
5. **tool unit.** `notify=telegram` with two links and no recipients refuses with both accounts
   and "all" in the directive and persists nothing; with recipients it persists them, a repeated
   id once; an unlinked id is refused; one link stores that account; no link is refused. The
   CLI, unit: the same cases through `--telegram-recipients`, plus `all`, an id that is no number,
   and two links with none named refused with the accounts and the flag; db_integration: its
   INSERT stores the recipients.
6. **db_integration E2E** (`internal/channels/telegram` and `internal/cron`): a task with both
   accounts as recipients reaches both chats through the links in Postgres, the channel, the
   registry and the cron dispatcher, and so does its approval prompt from the registry on; after
   one is unlinked the push reaches the other and the record names the skipped one. The rows round
   trip, and an undeliverable row leaves the sweep.
7. **Lab VM**, recorded in `docs/superpowers/verification/`: with two phones, a reminder for both
   rings both; after unlinking one it rings the other and the conversation names the skipped one.
8. **Gates.** Coverage gate and package policy; mutation ≥ 70% on the new telegram and tool files.

## 6. Risks

- **The relink guard** (previous slice, §6) was approved and landed as D11 in #198: the consume
  refuses to link a chat whose conversation is another identity's.
- **Two writers of the recipient rule** (#197 refused ambiguity, R6 orders): reconciled on
  2026-10-28 in favour of R6.
- **A task the cockpit sets to Telegram names no recipients** and goes to the account
  seen most recently (R6), not to a list the operator chose.
- **A recipient list snapshot goes stale.** An account linked after the task was scheduled is not
  in it. The tool's `list` names the recipients, so the operator can see it.

## 7. What the measurements do not show

- The heap order is what PostgreSQL 18 did on this database, with a sequential or index scan and
  two rows. It is not a documented order, which is the point: the code must not rely on it.
- How the model phrases the choice to the operator; the directive is tested, the dialogue is not.
- Telegram's error strings on the live API; the sentinels are telebot's.
