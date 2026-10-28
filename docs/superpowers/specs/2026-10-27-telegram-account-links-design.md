# Telegram account links: owner isolation, and a list the owner can prune — design

Date: 2026-10-27
Status: approved by the operator on 2026-10-28, with D3 and D9 confirmed explicitly. Plan:
`docs/superpowers/plans/2026-10-27-telegram-account-links.md`.

## 1. Why

Issue #196 and PR #197 found three Telegram accounts linked to one identity on an appliance.
Scheduled notifications went to an obsolete one, which had blocked the bot. #197 refuses an
ambiguous recipient and asks the operator to reconcile the links. Aura gives the operator no way
to do that: no query, route or control deletes a row of `aura.telegram_accounts` (`git grep` on
`2e41ead`). The reconciliation is SQL today. The operator asked for an API that lists the linked
accounts and deletes them (2026-10-27).

The operator then noted that the table has no row-level security. Measured on 2026-10-27, on a
disposable PostgreSQL 18 with the 118 migrations of `2e41ead` (up to 0143), inside a transaction
rolled back:
- `relrowsecurity` is false and the table has no policy. `aura_app`, the runtime role, is neither
  superuser nor `BYPASSRLS`.
- As `aura_app`, with `app.current_identity` set to identity A, a `SELECT` returned identity B's
  link. A `DELETE` of B's link by its primary key removed it.

Migration 0087 left the table out on purpose. It is in the "authentication, pre-identity" family:
`GetAccountByTelegramID` is the inbound lookup that produces the identity of a Telegram turn, and a
fail-closed policy would deny the lookup that establishes the identity. That reason holds for
reads with no identity. It does not hold for the list and the unlink: they would be the first
reads and writes on this table driven by a signed-in user, and a `WHERE` clause would be their
only isolation.

**The writers.** Three paths write the table, all on the runtime pool with no identity bound:
- `ConsumeOnboarding` inserts the link;
- `TouchLastSeen` updates `last_seen_at` on every inbound update;
- `retireLegacyLocalIdentity` (`cmd/aura/serve_auth.go`) moves the legacy `local` identity's rows
  to the operator, then deletes `local`. It re-keys `conversations`, `assets`, `telegram_accounts`
  and `identity_auth_links` with plain `UPDATE`s.

**The retirement already loses rows.** Measured on the same database, as `aura_app` with no
identity:
- the retirement's `UPDATE aura.conversations … WHERE identity_id = <local>` moved 0 rows,
  because `conversations` has had fail-closed RLS since 0089;
- the same `UPDATE` on `telegram_accounts` moved its row, because that table has no RLS;
- the foreign keys of `conversations`, `assets` and `telegram_accounts` to `aura.identities` are
  all `ON DELETE CASCADE`;
- as `aura_app` with no identity, deleting an identity that owned a conversation removed the
  conversation too, although `aura_app` could not see it: the cascade passes row security.

So deleting `local` takes the conversations the `UPDATE` left behind. `assets` has the same RLS
since 0090; its `UPDATE` was not run with a row. With RLS on `telegram_accounts` and nothing else
changed, the Telegram links would go the same way. The retirement's existing test asserts only
that `local` is gone, which is why this was not caught. The whole path is inferred from these
measurements: each step was measured on its own, and Task 2 of the plan runs the function with
real rows as a red test.

**The readers with no identity.** These must keep working:
- the inbound lookup (`bot_dispatch_auth.go`, `bot_dispatch_turn.go`, `bot_dispatch_asset.go`,
  `profile_onboarding.go`);
- scheduler delivery (`GetAccountByIdentity`, and its #197 form);
- the setup server's `CountAccounts`;
- the password reset's `lookupPasswordResetTelegramUserID`;
- the recovery's `LookupRecoveryByEmail`.

`Store.ListAccounts`, over `ListTelegramAccounts`, has no production caller. Its own test is the
only reader.

**A push bound to a conversation misses the second chat.** The operator asked what happens when
one person links two Telegram accounts, for example two phones with two numbers. One account on
two phones is a single row, and Telegram delivers to every device. Two accounts are two rows.
Each account's chat is its own conversation (`convID`, a hash of the chat id), and inbound
messages work on both. Scheduled work does not:
- `notify=telegram` and the approval prompts resolve the recipient with
  `GetTelegramAccountByIdentity`. That is a `:one` query with no `ORDER BY`, so one row of the
  database's choosing receives every push. This is #196.
- Results bound to the conversation that created the task, which are what `notify=stdout` does
  from a Telegram chat, go through `DeliverConversation`. It resolves the account the same way,
  then checks that the origin conversation is that account's chat. If it is not, it reports
  "not mine". The dispatcher then falls through to stdout, so the result is written into the
  conversation's history and the process log, and the phone gets nothing.

Measured on 2026-10-28 on `8b57628`, in the db_integration test
`TestConversationPushReachesItsOwnChat`, with two accounts linked through the real onboarding: the
push bound to the first chat reached it. The push bound to the second chat returned
`(false, nil)` and sent nothing. Which chat loses depends on the row the database returns. #197
would refuse this case as ambiguous, but it is not ambiguous: the conversation names the chat.

## 2. Goals, non-goals

**Goals**
1. A session bound to an identity reads and writes only that identity's links, and the database
   enforces it.
2. Every path that runs with no identity today still resolves: the inbound lookup, scheduler
   delivery, the setup count, the password reset and the recovery.
3. In Settings → Telegram, the owner lists the Telegram accounts linked to their identity and
   unlinks one.
4. Retiring the legacy `local` identity moves its conversations, assets, Telegram links and auth
   links to the operator and loses none of them.
5. A push bound to a Telegram conversation reaches that conversation's chat, whatever else the
   identity links.

**Non-goals**
- Choosing a primary account, or replacing an old link at enrollment.
- An admin view across identities.
- The ambiguity refusal itself, which is #197.
- Making the read with no identity stricter (§6).
- Deleting the conversation of an unlinked Telegram account. Its turns stay with the identity.
- How the password reset and the recovery pick among several accounts. They pick the one seen
  last, and #197 deliberately does not. That difference is left to #197's review.

## 3. Decisions

| # | Decision | Why |
|---|---|---|
| D1 | `aura.telegram_accounts` gets the shape 0089 gave `aura.shared_links`, with three policies: owner isolation for every command; a `SELECT`-only widening for a session with no `app.current_identity`; a restrictive floor `TO aura_app`, where a read reaches a row that is mine or a session with no identity, and a write reaches only a row that is mine. | Goals 1 and 2. 0087's reason covers reads with no identity, and the widening keeps exactly those. `shared_links` already accepts the same trade-off. The floor holds whatever permissive policy a later migration adds. |
| D2 | A write runs under the identity that owns the row. `ConsumeOnboarding` binds the identity of the pending token inside its transaction, right after the consume returns it. That is the `identity.CreateUserTx` precedent: `db.SetTxIdentity` mid-transaction. `TouchLastSeen` takes the account's identity and runs under `db.WithIdentityTx`. | With no identity bound, the `INSERT` fails its `WITH CHECK`, and the `UPDATE` matches no row and reports no error. The PostgreSQL manual (CREATE POLICY) says such rows are "silently suppressed; no error is reported". |
| D3 | The retirement re-keys its RLS tables through one function: `aura.retire_legacy_local_rows(target uuid)`, `SECURITY DEFINER`, owned by `aura_migrate`, with `search_path` pinned. The function finds the source itself: the identity named `local` of kind `system`. It refuses a target that is not of kind `user`. It moves `conversations`, `assets` (owner and the deleted-key scrub) and `telegram_accounts`. The rest of the retirement stays in Go. | Goal 4. Under owner isolation a row cannot change owner in one statement: `USING` needs the old owner, `WITH CHECK` the new one. 0089 is the precedent for a definer function with a pinned search path. Fixing the source in the function keeps it from becoming a primitive that moves any tenant's rows. |
| D4 | `GET /api/settings/telegram/accounts` answers 200 with `[{telegramUserId, username, firstName, addedAt, lastSeenAt}]` for the caller's identity, oldest link first. `DELETE /api/settings/telegram/accounts/{telegramUserId}` answers: 204 when it unlinked; 404 when the caller has no such link, another identity's link included; 400 when the id is not an integer. Both run under `db.WithIdentityTx(requester)` and also filter on `identity_id`. Both are mounted behind `governance.write`, like the three sibling Telegram routes. | Goal 3. The `WHERE` is the application filter, and RLS is the backstop for a forgotten one (`db/tx.go`). A 404 for another identity's link does not tell the caller the link exists. Telegram says a user id "has at most 52 significant bits", so a JSON number holds it (Bot API, `User.id`). |
| D5 | Settings → Telegram lists the linked accounts, with an Unlink action behind a confirmation. It lives in a new `TelegramLinkedAccounts.tsx` (the panel is 351 lines). | A route no client calls is dark code (the 2026-10-26 scan). |
| D6 | Unlinking revokes inbound access. The next message from that Telegram account gets the activation-required reply (`bot_dispatch_auth.go`). | That is what an unlink means. The gate already fails closed on a missing row. |
| D7 | `Store.ListAccounts` and `ListTelegramAccounts` are removed. The list reads a new `ListTelegramAccountsByIdentity`. | No production caller (§1). |
| D8 | The migration takes the next free number when it lands (0144 when this was written). | CLAUDE.md: `ls internal/db/migrations/ \| tail -1` at landing. |
| D9 | A push bound to a conversation picks, among the identity's linked accounts (`LinkedAccounts`), the one whose chat is that conversation. No match is "not mine", as today. Identity-wide pushes (`notify=telegram`, approvals) keep `GetAccountByIdentity`. | Goal 5, measured in §1. The conversation names the chat, so there is nothing to choose. `convID` is a one-way hash, so the accounts are compared one by one; an identity links a handful. Approved by the operator on 2026-10-28. |

## 4. Design

### 4.1 Migration

```sql
ALTER TABLE aura.telegram_accounts ENABLE ROW LEVEL SECURITY;

CREATE POLICY telegram_accounts_owner_isolation ON aura.telegram_accounts
    FOR ALL
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY telegram_accounts_pre_identity_read ON aura.telegram_accounts
    FOR SELECT
    USING (NULLIF(current_setting('app.current_identity', true), '') IS NULL);
CREATE POLICY telegram_accounts_owner_or_pre_identity ON aura.telegram_accounts
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (
        identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
        OR NULLIF(current_setting('app.current_identity', true), '') IS NULL
    )
    WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
```

Plus `aura.retire_legacy_local_rows(target uuid)` (D3), `GRANT EXECUTE … TO aura_app`, and
`COMMENT ON` for each policy and the function. The down migration drops the function and the
policies and disables RLS. `aura_app` is neither owner nor `BYPASSRLS`, so `ENABLE` applies to it.
Its owner `aura_migrate` keeps bypassing RLS. The manual: "Table owners normally bypass row
security as well". The identity cascade keeps reaching the links: §1 measured an
`ON DELETE CASCADE` removing a row that RLS hid from the deleting session. The manual says only
that referential-integrity checks bypass row security, so acceptance 4 measures the cascade on
`telegram_accounts` itself.

### 4.2 Store (`internal/channels/telegram`)

- `ConsumeOnboarding`: the transaction binds `pending.IdentityID` before `InsertTelegramAccount`.
- `TouchLastSeen(ctx, identityID string, telegramUserID int64)`: under `WithIdentityTx`. The
  auth gate keeps the account its lookup returns and passes `account.IdentityID`. It discards it
  today.
- `LinkedAccounts(ctx, identityID) ([]Account, error)` over
  `ListTelegramAccountsByIdentity :many`, ordered by `added_at, telegram_user_id`.
- `Unlink(ctx, identityID string, telegramUserID int64) (bool, error)` over
  `DeleteTelegramAccountForIdentity :execrows`, with `WHERE telegram_user_id = $1 AND
  identity_id = $2`.
- The no-identity reads (`GetAccountByTelegramID`, `GetAccountByIdentity`, `CountAccounts`) do
  not change.
- D9, in `deliver.go`: the account resolver also offers `LinkedAccounts`. `DeliverConversation`
  takes the linked account whose `convID` is the origin conversation. `Deliver` and
  `DeliverApproval` keep `GetAccountByIdentity`.

### 4.3 Retirement (`cmd/aura/serve_auth.go`)

The `conversations`, `assets` and `telegram_accounts` statements become one
`SELECT aura.retire_legacy_local_rows($1)` in the same transaction. The `identity_auth_links`,
`scheduler_tasks` and `pending_notifications` statements stay as they are: those tables have no
RLS.

### 4.4 HTTP (`internal/agui`)

A new file, `settings_api_telegram_accounts.go`, holds:
- the seam `TelegramAccountLinks` (`LinkedAccounts`, `UnlinkAccount`), with agui-local types;
- `SetTelegramAccountLinks`;
- the two handlers. The identity comes from `principalIdentityID(r)`, never from the path or the
  body. With no principal the answer is 401; with the seam unset, 503.

`cmd/aura` adapts `*telegram.Store` onto the seam, as `setupStoreAdapter` does, and mounts both
routes behind `governanceWriteCapability`.

### 4.5 Cockpit (`web/src/settings`)

- `settingsApi.ts` gets `fetchTelegramAccounts` and `unlinkTelegramAccount`.
- `TelegramLinkedAccounts.tsx` shows a TanStack Query read with:
  - empty, loading and error states;
  - each account's name, `@username`, id, link date and last seen;
  - Unlink behind a confirmation, which refetches the list.
- It mounts in `TelegramSettingsPanel`. The i18n keys go in it and en.

## 5. Acceptance (machine-checkable)

1. **db_integration, migration.** Up and down. After up, RLS is on and the three policies exist,
   one of them restrictive. As `aura_app`:
   - with identity A bound, only A's rows are visible;
   - a `DELETE` of B's link affects 0 rows, and the link is still there;
   - an `INSERT` for B is refused;
   - with no identity bound, both rows are visible, an `INSERT` is refused, and `UPDATE` and
     `DELETE` affect 0 rows.
2. **db_integration, telegram store.**
   - `ConsumeOnboarding` links the account to the token's identity.
   - `TouchLastSeen` changes `last_seen_at`, read back.
   - With no identity bound, `GetAccountByTelegramID` and `GetAccountByIdentity` still resolve.
   - `LinkedAccounts(A)` returns only A's links.
   - `Unlink(A, <B's account>)` is false, and B's link remains. `Unlink(A, <A's account>)` is true.
   - D9: with two accounts linked to one identity, a push bound to either chat's conversation
     reaches that chat (`TestConversationPushReachesItsOwnChat`, red on `8b57628`). A unit test
     covers the order the database returns the accounts in, and a conversation no linked account
     owns.
3. **db_integration, retirement** (`cmd/aura`). `local` owns a conversation, an asset, a Telegram
   link and an auth link. After the retirement, all four belong to the target, and `local` is
   gone. The test is red before D3.
4. **db_integration, identity delete.** With RLS on, deleting an identity as `aura_app` removes
   its links. `identity_cascade_integration_test.go`, which checks the catalog only, stays green.
5. **db_integration, no-identity readers.** The password reset lookup and `LookupRecoveryByEmail`
   still find the account. The breakglass suite still passes.
6. **agui unit.**
   - GET returns the seam's rows.
   - DELETE answers 204, 404 or 400.
   - With no principal, 401. With the seam unset, 503.
   - The identity handed to the seam is the principal's, whatever the path carries.
7. **cmd/aura.** Both routes refuse an unauthenticated request.
8. **Vitest.**
   - The list renders, and so does the empty state.
   - Unlink after the confirmation sends the DELETE with that id and refreshes the list.
   - A failed unlink shows the error and keeps the row.
9. **Two-identity cross-deny E2E** (`cmd/aura`, the CI job "MUSR two-identity cross-deny E2E"),
   with two identities signed in through Authula and the real mounted routes:
   - A lists only A's links;
   - A's DELETE of B's link is 404, and B's link remains;
   - B unlinks its own link.

   A Playwright spec with stubbed routes checks the panel in a real browser: the list renders,
   and Unlink sends the DELETE with the right id. Playwright has no database client, so the
   isolation is proven by the E2E above, not by the browser spec.
10. **Lab VM, recorded** in `docs/superpowers/verification/`.
    - Link two real Telegram accounts, list them, and unlink one.
    - The unlinked account gets the activation-required reply.
    - A scheduled reminder reaches the remaining account.
11. **Gates.** The coverage gate and the package policy are green. Mutation is at least 70% on
    `internal/channels/telegram/store.go` and on the new agui file.

## 6. Risks

- **Inbound Telegram stops.** A policy mistake that hides the row from the inbound lookup refuses
  every Telegram turn. Acceptance 2 and 5 cover each reader that has no identity.
- **Silent writes.** An `UPDATE` or `DELETE` with the wrong identity reports success on 0 rows.
  Every write has a test that reads the row back.
- **The definer function is a privilege boundary.** It moves rows only away from the system
  identity `local`, and only to a `user` identity. Its tests include a refused target.
- **Reads with no identity stay wide.** Code on the runtime pool with no identity still reads
  every link: as today, and as with `shared_links`. A stricter design, with a definer function for
  the inbound lookup and strict RLS everywhere else, would also change the scheduler, the setup
  count, the reset and the recovery. It is not proposed here.
- **Relinking an unlinked Telegram account to another identity.** Its conversation id is derived
  from the chat id (`convID`), so the conversation stays owned by the first identity. This was not
  measured, and it exists today without this change.
- **#197 touches the same store and query file.** Whichever lands second merges and regenerates
  sqlc. #197's test expects the conversation route to refuse two links as ambiguous. Under D9
  that route no longer asks `GetAccountByIdentity`, so the expectation becomes "delivered to that
  chat".

## 7. What the measurements do not show

- The retirement end to end with real rows. Task 2 of the plan measures it with a red test.
- `assets` under the retirement: its RLS state was measured, not its `UPDATE` with a row.
- How many appliances still hold a legacy `local` identity with conversations.
