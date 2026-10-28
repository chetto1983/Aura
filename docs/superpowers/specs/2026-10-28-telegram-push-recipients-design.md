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

The recipient is the account whose row comes first in the heap. Every inbound update rewrites the
row with `last_seen_at`, which moves it to the end. So the push goes to the account used **least**
recently: the phone in your hand is the one that does not ring. This matches #196, where the
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
| R1 | `scheduler_tasks.telegram_recipients bigint[]`, NULL when the task names none; when set it is not empty. "All" stores every account linked at schedule time. | One column covers one, another and all. A snapshot is explicit: an account linked later is not added behind the operator's back. |
| R2 | The `task` tool takes an optional `telegram_recipients` (array of Telegram user ids). With `notify=telegram` and two or more linked accounts and none named, it refuses with a choice directive that lists each account (name, `@username`, id) and "all". With one linked account it stores that one. An id not linked to the identity is refused. | The same guard as a missing `notify`, at the same moment. |
| R3 | A task's Telegram pushes, the result and the approval prompt, go to its recipients. A recipient no longer linked is skipped and named in the record; it is never replaced by another account. | Unlink means stop (previous slice, D6). |
| R4 | Each recipient is sent to on its own. A failed recipient gets its own `pending_notifications` row, with the new column `telegram_user_id`, and the sweep retries that recipient only. The push counts as handled when every recipient is delivered, queued, or recorded as permanent (R5). | Retrying the whole push would resend to the phones that already got it, at every attempt. |
| R5 | `ErrBlockedByUser`, `ErrChatNotFound`, `ErrNotStartedByUser` and `ErrUserIsDeactivated` are permanent: no retry row; the origin conversation records "not delivered to <account>: <reason>". | No retry can succeed. The fix is the operator's: unlink, or unblock the bot. |
| R6 | With no recipients named, `GetTelegramAccountByIdentity` orders by `COALESCE(last_seen_at, added_at) DESC, added_at DESC, telegram_user_id DESC`, the order the password reset and the recovery already use. | Goal 4. It also fixes #196 for every task scheduled before this slice. |
| R7 | The migration takes the next free number at landing (0145 today). | CLAUDE.md. |

R6 answers "existing tasks with two accounts" (approved 2026-10-28). #197 refuses such a push
as ambiguous instead; with R6 it is no longer ambiguous. Whichever lands second reconciles.

## 4. Design

**Migration.** `ALTER TABLE aura.scheduler_tasks ADD COLUMN telegram_recipients bigint[]` with
`CHECK (telegram_recipients IS NULL OR cardinality(telegram_recipients) > 0)`;
`ALTER TABLE aura.pending_notifications ADD COLUMN telegram_user_id bigint`. Down drops both.
Neither table has row security (0087, cross-identity daemon tables).

**cron.** `Task.TelegramRecipients []int64` and `PendingNotification.TelegramUserID int64` (0 for
none). `ChannelDeliverer` gains
`DeliverToTelegramRecipients(ctx, identityID string, recipients []int64, text string) ([]RecipientResult, error)`;
`deliverToOrigin` and `deliverSweptRow` call it when the route is `telegram` and the task, or the
row, names recipients. `RecipientResult` carries the account, whether it was delivered, the error,
and whether the error is permanent. The approval sweep passes the task's recipients the same way.

**channels.** `Registry.DeliverToTelegramRecipients` finds the started channel that implements it;
no channel is `(nil, nil)`, which is "not mine".

**telegram.** `DeliverToRecipients` reads `LinkedAccounts(identity)` once, sends to each named
account still linked (with the existing chunking and pacing), and returns one result per named
account. `isPermanent(err)` is `errors.Is` against the four sentinels. `DeliverApproval` with
recipients sends the prompt to each.

**Tool.** A new file, `internal/agent/tools/task_telegram_recipients.go`: the field, the
directive, and the validation against the identity's linked accounts through a narrow seam over
`telegram.Store.LinkedAccounts`. `task list` names each task's recipients.

## 5. Acceptance (machine-checkable)

1. **db_integration, migration.** Up and down; the CHECK refuses an empty array.
2. **db_integration, telegram store.** With two accounts and no recipients, the identity push goes
   to the account that sent the last update (red today: the probe of §1, committed as a test).
3. **telegram unit.** `DeliverToRecipients` sends to each named linked account, in any order of
   `LinkedAccounts`; skips an unlinked one and reports it; reports a mid-list failure for that
   account only; classifies the four sentinels as permanent and a network error as not.
4. **cron unit.** A two-recipient push with one failure inserts one failed row for that account
   and none for the other; the sweep sends that row to that account only; a permanent failure
   inserts no row and the record names it.
5. **tool unit.** `notify=telegram` with two links and no recipients refuses with both accounts
   and "all" in the directive and persists nothing; with recipients it persists them; an unlinked
   id is refused; one link stores that account.
6. **db_integration E2E** (`internal/channels/telegram` and `internal/cron`): a task with both
   accounts as recipients reaches both chats, and so does its approval prompt.
7. **Lab VM**, recorded in `docs/superpowers/verification/`: with two phones, a reminder for both
   rings both; after unlinking one it rings the other and the conversation names the skipped one.
8. **Gates.** Coverage gate and package policy; mutation ≥ 70% on the new telegram and tool files.

## 6. Risks

- **The relink guard is still missing** (previous slice, §6): relinking an unlinked chat to
  another identity breaks that identity's Telegram turns. It is independent of this slice and
  waits for the operator's decision.
- **Two writers of the recipient rule.** #197 refuses ambiguity; R6 orders. Whichever lands second
  reconciles the test that expects the other behaviour.
- **A recipient list snapshot goes stale.** An account linked after the task was scheduled is not
  in it. The tool's `list` names the recipients, so the operator can see it.

## 7. What the measurements do not show

- The heap order is what PostgreSQL 18 did on this database, with a sequential or index scan and
  two rows. It is not a documented order, which is the point: the code must not rely on it.
- How the model phrases the choice to the operator; the directive is tested, the dialogue is not.
- Telegram's error strings on the live API; the sentinels are telebot's.
