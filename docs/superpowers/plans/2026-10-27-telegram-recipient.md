# Telegram recipient implementation plan

**Goal:** prevent identity-routed notifications from choosing an arbitrary linked account.

**Architecture:** bounded sqlc `:many` lookup plus an explicit Store ambiguity error;
reuse the delivery methods' existing resolver-error handling.

**Tech Stack:** Go 1.27.3, sqlc 1.31.1, pgx v5, PostgreSQL 18.

**Spec:** `docs/superpowers/specs/2026-10-27-telegram-recipient-design.md`.

## Global Constraints

- No deployment, schema migration, automatic mapping edits or real Telegram sends.
- Synthetic fixtures only; use a disposable local database.
- Preserve not-found semantics and inbound account authentication.
- Keep touched handwritten files below 600 lines; regenerate sqlc.
- Open a PR for review; do not merge as part of this request.

## Review Focus

1. Ambiguity cannot become `pgx.ErrNoRows` and trigger a channel fallback.
2. Text and approval sends both stop before contacting Telegram.
3. Two rows suffice to detect any number of matches, without choosing by timestamps.
4. Existing mappings are left for the operator to reconcile deliberately.

## Decisions taken while writing this plan

Use sqlc's `:many` support instead of a new database abstraction. Do not add a unique
constraint: existing duplicates cannot safely be reconciled by a migration.

## File structure

- `internal/db/queries/telegram_accounts.sql` and regenerated sqlc output.
- `internal/channels/telegram/store.go`: cardinality check and sentinel.
- New unit and database integration regression files in that package.
- PRD measurement and a dated verification ledger.

### Task 1: record the measured failure

- [x] Confirm the unchecked lookup on current upstream and open issue #196.
- [x] Record the production repair's evidence and limits in the spec and PRD.
- [ ] Commit the documentation decision before implementation.

### Task 2: reproduce and fix the resolver

- [ ] Write a PostgreSQL regression for zero, one, two and three accounts.
- [ ] Run it on the base implementation and observe failure for multiple matches.
- [ ] Change the query to return at most two rows and regenerate with sqlc 1.31.1.
- [ ] Add the cardinality check, error classification and daemon-free unit tests.
- [ ] Run the regression with the fix, including text/conversation/approval delivery.

### Task 3: validate and publish

- [ ] Run package tests and race/leak checks, vet and build.
- [ ] Attempt the repository's disposable coverage gate; record exact limitations.
- [ ] Review the diff and record verification evidence.
- [ ] Commit, push to the contributor fork and open a linked PR.
- [ ] Check CI on the published commit and stop the temporary local test VM.
