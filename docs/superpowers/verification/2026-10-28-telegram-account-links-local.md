# Telegram account links: owner isolation, list and unlink — local verification

Date: 2026-10-28. Branch `ccr-648b75d9-4hbqnw`, from `8b57628`. Spec:
`docs/superpowers/specs/2026-10-27-telegram-account-links-design.md`. Plan:
`docs/superpowers/plans/2026-10-27-telegram-account-links.md`.

All database runs are on a disposable PostgreSQL 18 (`aura_it`), as `aura_app` unless named.

## Task 2 — row security on the links, and a retirement that keeps `local`'s rows

**Red, before the migration, on `28e0c95`:** `TestLocalIdentityRetirementKeepsLocalsRows`
(`cmd/aura`, db_integration). `local` owns a conversation, a live asset with an ingestion job and
its event, a deleted asset whose key names `local`, a Telegram link and an auth link, all seeded
under `local`'s identity. After `retireLegacyLocalIdentity`:

```
conversation belongs to "" after the retirement, want the target 52b1f071-…
asset belongs to "" after the retirement, want the target 52b1f071-…
ingestion job belongs to "" after the retirement, want the target 52b1f071-…
ingestion event belongs to "" after the retirement, want the target 52b1f071-…
deleted asset key = "", want local's id replaced by the target's
--- FAIL: TestLocalIdentityRetirementKeepsLocalsRows (0.12s)
```

The Telegram link and the auth link moved: neither table had row security yet.

**The composite foreign keys**, probed as the superuser in a rolled-back transaction: a lone
`UPDATE aura.assets SET identity_id = …` fails with `violates foreign key constraint
"ingestion_jobs_asset_identity_fkey"`. One statement whose `WITH` clauses move the asset, its job
and the job's event moves 1, 1 and 1 rows. So `aura.retire_legacy_local_rows` moves the three
together.

**Green, with 0144:**
- `TestMigrate0144IsolatesTelegramLinks` (`internal/db`), on a fresh database stepped to 143:
  - before up: no row security, no policy, no function;
  - after up: row security on; the three policies, with the floor `RESTRICTIVE` to `aura_app`;
    the function is `SECURITY DEFINER`, owned by `aura_migrate`, with
    `search_path=pg_catalog`; `aura_app` may run it and `PUBLIC` may not;
  - bound to A: only A's link is visible; a `DELETE` of B's link is 0 rows and B's link
    remains; A's own `UPDATE` is 1 row; an `INSERT` for B is SQLSTATE 42501;
  - no identity: both links are visible; `UPDATE` and `DELETE` are 0 rows; an `INSERT` is 42501;
  - the function refuses a service identity and an unknown one with 22023, moving nothing. Onto
    A it moves `local`'s link and conversation, and leaves B's. With `local` deleted it returns
    and moves nothing;
  - deleting B as `aura_app` with no identity removes B's link, which that session cannot change;
  - after down, nothing remains; up again restores it all.
- `TestLocalIdentityRetirementKeepsLocalsRows`: every row belongs to the target, and the deleted
  asset's key names the target.
- `TestPasswordResetFindsTheTelegramLinkWithNoIdentity`: the reset lookup and
  `LookupRecoveryByEmail` find the link with no identity bound.
- db_integration under `-race`: `internal/channels/telegram` (D9's test skipped, red until
  Task 3), `internal/breakglass`, `internal/db` (172.7 s) and `cmd/aura` (110.7 s). The one
  failure was the migration-head pin in `db_unit_test.go`, which names 144 now.
- Hand mutations of the up migration, each failing `TestMigrate0144IsolatesTelegramLinks`, 6 of 6:
  the widening `FOR ALL` instead of `FOR SELECT`; no `REVOKE … FROM PUBLIC`; no `kind = 'user'`
  check; no `SECURITY DEFINER`; the links not moved; conversations moved with no source filter.

## Task 3 — the owner's list and unlink, and D9

- `TestLinkedAccountsAndUnlinkStayWithTheirOwner` (db_integration): A's two links come back in
  the order they were linked, not by id; `local` lists none; `Unlink(A, B's)` is false and B's
  link resolves; `Unlink(A, A's)` is true and the link is gone.
- D9, red on `b43b8c0`: `TestDeliverConversation` failed "each chat's conversation reaches that
  chat" with `(false, <nil>)` for 4242 listed first, and "a failing list" with `(false, <nil>)`
  instead of the list's error.
- D9, green: the unit cases (both orders, a cockpit conversation, no link, a failing list, no
  conversation) and `TestConversationPushReachesItsOwnChat`, which runs now and reaches both
  chats. `internal/channels/...` under `-race`, unit and db_integration.

## Task 4 — HTTP

- agui unit: GET answers the seam's rows in order, a 52-bit id exact, `lastSeenAt` null when never
  seen, `[]` with no link, 502 on a failing store; DELETE answers 204 with the principal's identity
  handed to the seam, 404 for a link the caller does not have, 400 for `abc`, `0`, `-5` and
  `9223372036854775808` without reaching the store, 502 on a failing store; both answer 401 with
  no principal and 503 with the seam unset.
- `cmd/aura`: both routes answer 401 with no session and 403 without `governance.write`, and
  reach AG-UI with a capable session; `TestServeWebuiMountsEveryAGUIRoute` finds both mounted.
- `TestTwoIdentityTelegramLinksCrossDeny` (the five MUSR tags; locally on Postgres only, since
  it needs nothing else): A lists only its link; A's DELETE of B's link is 404 and B's link
  remains; B's own DELETE is 204; B then lists none and A still lists its own.
- `internal/agui` and `cmd/aura` under `-race -p 1`, unit (46.4 s, 71.1 s) and db_integration
  (102.4 s, 116.8 s).

## Task 5 — cockpit

- Vitest on Node 24.22.1 (CI's `AURA_CI_NODE_VERSION`), `TelegramLinkedAccounts.test.tsx`: both
  rows in order with name, @username, the 52-bit id and "not seen yet"; the empty state; Unlink
  after the confirmation sends `DELETE …/4503599627370495` and the list is read twice and keeps
  one row; Cancel sends nothing; a 502 says "Couldn't unlink the account: telegram accounts
  unavailable" and keeps the row. `src/settings`: 331 of 331 in 42 files.
- `npm run lint`, `lint:contract`, `mutation:contract`, `format:check`, `typecheck`: clean.
- Playwright (`chrome`) against `aura serve` built from `7105e80`, with the dist rebuilt on Node 24,
  on a disposable database `aura_pw` migrated to 0144: `telegram-linked-accounts.spec.ts` passes in
  3.0 s. The spec stubs the two account routes only; the DELETE carried an `Idempotency-Key`.
- On the same serve, with no session, both real routes answer 401.
- That serve's boot retired `local` on `aura_pw` through `aura.retire_legacy_local_rows` with no
  error logged; `local` owned no content rows there, so this shows the call works on the live boot
  path, not that rows move (Task 2 shows that).

## Task 6 — mutation and coverage

- `go-mutesting` in a detached worktree of `9bc7033`:
  - `internal/channels/telegram/store.go` with `GOFLAGS=-tags=db_integration` on `aura_it`:
    51.4% (18 of 35) first, then 90.9% (30 of 33) once
    `TestStoreReportsWhatItCannotDo` drove the error paths and two redundant not-found branches
    were removed. The 3 survivors are equivalent: an invalid identity on `InsertPending` still
    fails at the NOT NULL column; `SetTxIdentity` fails only on a database fault;
    `LinkedAccounts`' non-UUID early return matches what the planner folds the query to.
  - `internal/agui/settings_api_telegram_accounts.go` with `GOFLAGS=-run=TelegramAccount` (its
    own three tests; the whole agui suite cost minutes per mutant): 83.9% (26 of 31), then 90.3%
    (28 of 31) with the id-1 case and the setter in the test. Survivors: `ParseInt` with 63 bits,
    equivalent for Telegram ids of at most 52 bits, and the two `slog.WarnContext` calls.
- `bash scripts/coverage_docker.sh` on a disposable Postgres (`aura-postgres-cov`, `aura_cov`,
  119 migrations): owned internal 49851/56922 = 87.6%; the package-local policy passes.

**The relink, measured with a scratch test that was not committed:** after A unlinks a chat and B
links it, B's `Get` of the chat's conversation is "conversation not found", its `Create` is 23505
(which `runner.EnsureConversation` takes for a lost race and reports as success), and its
`AppendTurn` is "conversation not found". A's turn is still there and B never sees it. So every
Telegram turn of B on that chat fails at its first append. This was measured on the store calls a
turn makes, not on a full runner turn.

**What this does not show:**
- Anything on the lab VM. Acceptance 10 (two real Telegram accounts, an unlink, the activation
  reply, a reminder reaching the remaining account) is the operator's run and has its own file.
- Real Telegram traffic: every send here is a recorded fake.
- What a real appliance's `local` owns. The retirement still lets 22 tables go with `local`
  (PRD §15); this slice does not decide which of them should move.
