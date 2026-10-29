# Telegram recipient implementation plan

**Status (2026-10-28):** superseded by R6 of the push recipients design; see the note at the top
of its spec. Reconciled when the branch was merged after D10.

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
- [x] Commit the documentation decision before implementation (`3ba538c`).

### Task 2: reproduce and fix the resolver

- [x] Write a PostgreSQL regression for zero, one, two and three accounts.
- [x] Run it on the base implementation and observe failure for multiple matches.
- [x] Change the query to return at most two rows and regenerate with sqlc 1.31.1.
- [x] Add the cardinality check, error classification and daemon-free unit tests.
- [x] Run the regression with the fix, including text/conversation/approval delivery.

### Task 3: validate and publish

- [x] Run package tests and race/leak checks, vet and build.
- [x] Attempt the repository's disposable coverage gate; record exact limitations.
- [x] Review the diff and record verification evidence.

Evidence: `docs/superpowers/verification/2026-10-27-telegram-recipient.md`.
Publication and CI status are tracked on the linked PR. Full-stack acceptance
remains a pre-merge gate; the operational repair is not patch acceptance.
