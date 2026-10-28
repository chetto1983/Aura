# Telegram account links: owner isolation, and a list the owner can prune — Implementation Plan

**Status (2026-10-28):** in progress. The operator approved the spec, D3 and D9 included.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task.

**Goal:**
- The database isolates each identity's Telegram links.
- The owner lists and unlinks their linked accounts from Settings → Telegram.
- Retiring the legacy `local` identity keeps its rows.

**Architecture:**
- **Migration.** RLS on `aura.telegram_accounts` with the 0089 `shared_links` shape, plus the
  definer function the retirement re-keys through.
- **Store.** The writers bind the owning identity. A list and an unlink run under the caller's
  identity.
- **HTTP.** Two self-scoped routes under `/api/settings/telegram/accounts`.
- **Cockpit.** A linked-accounts list in the Telegram settings panel.

**Tech Stack:** PostgreSQL 18, golang-migrate, sqlc 1.31.1, Go (`internal/db`,
`internal/channels/telegram`, `internal/agui`, `cmd/aura`), React + TanStack Query
(`web/src/settings`).

**Spec:** `docs/superpowers/specs/2026-10-27-telegram-account-links-design.md`.

## Global Constraints

- **Migration number.** `ls internal/db/migrations/ | tail -1` at landing, never this plan's 0144.
- **Branch.** Work starts after #195 merges. The branch `ccr-648b75d9-4hbqnw` restarts from
  `master`.
- **Files stay ≤ 600 LOC.** `web/src/i18n/resources.settings.ts` is at 548: keep the new keys few.
- **A test changes only for a stated reason.** The cleanup helpers and the breakglass seed change
  because RLS makes a write with no identity refused or a no-op. Each such commit says which.
- **No new env var. No new dependency.**
- **#197 may land first.** It touches `store.go`, `telegram_accounts.sql` and the sqlc output.
  Whichever lands second merges and regenerates. Its test's raw `DELETE` cleanup must then run
  under the row's identity.

## Review Focus

- **Every write names an identity.** The consume, the touch, the unlink and the test helpers.
- **Every reader with no identity is tested under RLS.** The inbound lookup, scheduler delivery,
  the setup count, the password reset and the recovery.
- **The definer function moves nothing but `local`'s rows**, and only to a `user` identity.

## Decisions taken while writing this plan

- **The consume's transaction.** `ConsumeOnboarding` moves from `db.WithTx` to
  `db.WithIdentityTxRaw(ctx, pool, "", fn)`. `fn` calls `db.SetTxIdentity` with the pending row's
  identity after the consume, before the insert. The identity exists only once the consume
  returns it, which is the `identity.CreateUserTx` precedent. No new transaction helper.
  `telegram_setup_pending` has no RLS, so the consume works with the empty identity.
- **The cross-identity proof lives in the MUSR E2E, not in Playwright.** `web/e2e` has no
  database client, and the two-identity harness already signs in two real identities. A new
  file, `cmd/aura/two_identity_telegram_e2e_test.go`, reuses `two_identity_e2e_harness_test.go`
  under its tags.
- **The retirement keeps three statements in Go.** `identity_auth_links`, `scheduler_tasks` and
  `pending_notifications` have no RLS. Only the RLS tables go through the function.

## File structure

| File | Change |
|---|---|
| `internal/db/migrations/NNNN_telegram_accounts_owner_isolation.{up,down}.sql` | New: RLS, three policies, `aura.retire_legacy_local_rows`. |
| `internal/db/migrate_NNNN_integration_test.go` | New: up/down, the policy matrix, the function. |
| `internal/db/queries/telegram_accounts.sql`, `internal/db/sqlc/*` | `+ListTelegramAccountsByIdentity`, `+DeleteTelegramAccountForIdentity`, `−ListTelegramAccounts`. |
| `internal/channels/telegram/store.go` (249) | Consume binds the identity; `TouchLastSeen(identity)`; `LinkedAccounts`, `Unlink`; `−ListAccounts`. |
| `internal/channels/telegram/bot_dispatch_auth.go` (129) | The gate keeps the account and passes its identity to the touch. |
| `internal/channels/telegram/deliver.go` (210) | D9: a conversation-bound push picks its account by chat. |
| `internal/channels/telegram/deliver_conversation_test.go`, `deliver_conversation_integration_test.go` (new) | D9's unit and db_integration cases. |
| `internal/channels/telegram/store_integration_test.go` (299) | Cleanup under the row's identity; new cases, or a new `store_links_integration_test.go` if past 600. |
| `cmd/aura/serve_auth.go` (359) | The retirement calls the function. |
| `cmd/aura/serve_auth_retirement_integration_test.go` (72) | `local` owns four kinds of rows; all four move. |
| `internal/breakglass/breakglass_integration_test.go` (409) | The Telegram seed runs under its identity. |
| `internal/agui/settings_api_telegram_accounts.go` (new), `settings_api.go` | The seam, the two handlers, their registration. |
| `cmd/aura/serve_channels.go` (420), `serve_webui_routes.go` (425), `serve_webui.go` (413) | Adapter, route constants, mounts behind `governance.write`. |
| `cmd/aura/two_identity_telegram_e2e_test.go` (new) | Cross-deny over HTTP. |
| `web/src/settings/settingsApi.ts` (222), `TelegramLinkedAccounts.tsx` (new), `TelegramSettingsPanel.tsx` (351), `web/src/i18n/resources.settings.ts` (548) | Client, list, mount, keys. |
| `web/src/settings/__tests__/TelegramLinkedAccounts.test.tsx`, `web/e2e/telegram-linked-accounts.spec.ts` (new) | Vitest and a stubbed browser spec. |

## Tasks

### Task 0 — Record the measurement (PRD-first)
- [x] The spec and this plan, committed for the operator's review.
- [x] `prd.md`, in the Telegram section: the 2026-10-27 measurements of spec §1, with what they do
  not show (spec §7).
  - RLS is off and A reads and deletes B's link.
  - The retirement leaves conversations behind, and the cascade deletes them.
- [x] Commit `docs(prd): measure the Telegram links' isolation and the legacy retirement`.

### Task 1 — Writes run under the owning identity (D2, acceptance 2)
- [x] Red, db_integration, `internal/channels/telegram`:
  - `TouchLastSeen` moves `last_seen_at`, read back;
  - `ConsumeOnboarding` files the link under the token's identity.

  Both pass before RLS. They are the read-backs that will bite once Task 2 turns RLS on.
- [x] Green:
  - the consume transaction (the decision above);
  - `TouchLastSeen(ctx, identityID, telegramUserID)` under `db.WithIdentityTx`;
  - `telegramSeenMarker` and the auth gate pass `account.IdentityID`;
  - the unit fakes follow the signature.
- [x] The test helpers write under the row's identity: `cleanupAccount` takes the identity, and the
  breakglass seed uses `db.WithIdentityTxRaw`.
- [x] Gates:
  - `go vet ./...`;
  - golangci-lint 2.14.0 with both tags on the touched packages;
  - `-race` on `internal/channels/telegram`, unit and db_integration;
  - `internal/breakglass` db_integration.
- [x] Commit `fix(telegram): write a link under the identity that owns it`.

### Task 2 — RLS on the links, and a retirement that keeps `local`'s rows (D1, D3, D8; acceptance 1, 3, 4, 5)
- [x] Red, `cmd/aura` db_integration. `local` owns a conversation, an asset, a Telegram link and an
  auth link, all seeded under `local`'s identity. After `retireLegacyLocalIdentity`, all four
  belong to the target.
  - Expected red today: the conversation and the asset are gone.
  - Record the output in the verification file.
  - Measured on `28e0c95`: red as expected, and the asset's ingestion job and event went too.
    Their composite foreign keys refuse a lone asset move, so the function moves the three in
    one statement (spec D3, PRD §15).
- [x] Red, `internal/db/migrate_NNNN_integration_test.go`, on the `migrate_0141` pattern
  (`fresh0093Database`, `migrateToVersion`, `MigrateSteps`):
  - before up: RLS is off and the function is absent;
  - after up: RLS is on, the three policies exist, one restrictive, and the function is
    `SECURITY DEFINER` with a pinned `search_path`;
  - the matrix of acceptance 1, as `aura_app` (`SET LOCAL ROLE` inside a rolled-back
    transaction);
  - `DELETE FROM aura.identities` as `aura_app` removes that identity's links (acceptance 4);
  - the function refuses a target that is not of kind `user`, and moves nothing when `local` is
    absent;
  - after down: no policy, RLS off, no function.
- [x] Green:
  - the migration;
  - `retireLegacyLocalIdentity` calls `SELECT aura.retire_legacy_local_rows($1)` in place of the
    conversations, assets and telegram statements.
- [x] Acceptance 5. Find the db_integration tests of `lookupPasswordResetTelegramUserID` and of
  `LookupRecoveryByEmail`, and add one where none exists. Both must pass with RLS on.
- [x] Gates:
  - db_integration `-race` on `internal/db`, `internal/channels/telegram`, `internal/breakglass`
    and `cmd/aura`;
  - `scripts/check-file-size.sh`;
  - sqlc in sync.
- [x] Commit `fix(db): isolate Telegram links per identity, and keep local's rows on retirement`.

### Task 3 — The store lists and unlinks an identity's links (D7, acceptance 2)
- [x] Red, db_integration:
  - `LinkedAccounts(A)` returns only A's links, oldest first;
  - `Unlink(A, <B's>)` is false, and B's row remains;
  - `Unlink(A, <A's>)` is true, and the row is gone.
- [x] Green:
  - the two sqlc queries, and `sqlc generate`;
  - `LinkedAccounts` and `Unlink` under `db.WithIdentityTx`;
  - remove `ListAccounts`, `ListTelegramAccounts` and their test.
- [x] Gates as in Task 1, plus `deadcode`.
- [x] Commit `feat(telegram): list and unlink an identity's Telegram accounts`.
- [ ] D9, red: `TestConversationPushReachesItsOwnChat` (db_integration, measured red on `8b57628`
  for the second chat), and a unit test in `deliver_conversation_test.go`:
  - the matching account is found whatever the order the resolver returns;
  - a conversation no linked account owns is "not mine" and sends nothing;
  - a failing list is an error, not "not mine".
- [ ] D9, green: `accountResolver` gains `LinkedAccounts`, and `deliver` picks the account by
  conversation when one is required. The stub resolver in `deliver_test.go` follows.
- [ ] Commit `fix(telegram): a conversation's push reaches that conversation's chat`.

### Task 4 — HTTP (D4, acceptance 6, 7, 9)
- [ ] Red, agui unit, in `settings_api_telegram_accounts_test.go`:
  - GET 200 with the seam's rows;
  - DELETE 204, 404 and 400;
  - 401 with no principal, and 503 with the seam unset;
  - the identity the seam receives is the principal's.
- [ ] Green:
  - the agui file and its registration;
  - the `cmd/aura` adapter over `*telegram.Store`, the route constants, and the mounts behind
    `governanceWriteCapability`.
- [ ] `cmd/aura`: both routes refuse an unauthenticated request, on the `serve_webui_auth_test.go`
  pattern.
- [ ] The cross-deny E2E (acceptance 9) in `cmd/aura/two_identity_telegram_e2e_test.go`.
  - Run it with `make musr-e2e`.
  - It is gated in CI by the job "MUSR two-identity cross-deny E2E".
- [ ] Gates: `-race` on `internal/agui` and `cmd/aura`, unit and db_integration; lint.
- [ ] Commit `feat(agui): list and unlink the caller's Telegram accounts`.

### Task 5 — Cockpit (D5, acceptance 8, 9)
- [ ] Red, Vitest, in `TelegramLinkedAccounts.test.tsx`:
  - the list and the empty state;
  - Unlink after the confirmation sends the DELETE with that id and refetches;
  - a failed unlink shows the error and keeps the row.
- [ ] Green:
  - `fetchTelegramAccounts` and `unlinkTelegramAccount`;
  - the component, mounted in the panel;
  - the i18n keys in it and en.
- [ ] `web/e2e/telegram-linked-accounts.spec.ts` with regex-stubbed routes, against `aura serve`.
- [ ] Gates: `npm run lint`, `npm run typecheck`, and Vitest on `src/settings` on Node 24.
- [ ] Commits:
  - `feat(web): list and unlink linked Telegram accounts in Settings`;
  - then `build(web): rebuild the embedded cockpit`.

### Task 6 — Close
- [ ] Mutation (`go-mutesting`, in a separate worktree, with raised `--exec-timeout`) on
  `internal/channels/telegram/store.go` and `internal/agui/settings_api_telegram_accounts.go`.
  Each must reach ≥ 70%, and the survivors are named.
- [ ] `bash scripts/coverage_docker.sh`: the aggregate floor and the package policy.
- [ ] Write `docs/superpowers/verification/2026-10-27-telegram-account-links-local.md`, with what
  it does not show.
- [ ] Lab VM (acceptance 10), recorded in its own verification file. The operator links two
  Telegram accounts and unlinks one; the unlinked account gets the activation-required reply, and
  a scheduled reminder reaches the remaining account.
- [ ] PRD: what the slice measured once wired. Then set this plan's status.
