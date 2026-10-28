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

**What this does not show:**
- Anything on the live stack or the lab VM.
- What a real appliance's `local` owns. The retirement still lets 22 tables go with `local`
  (PRD §15); this slice does not decide which of them should move.
