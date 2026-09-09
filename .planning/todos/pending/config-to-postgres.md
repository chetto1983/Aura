---
id: config-to-postgres
created: 2026-09-09
source: operator, during phase 02 plan 02-07
severity: maintainability
resolves_phase:
---

# Move configuration into Postgres and shrink `.env` to a bootstrap floor

The operator's instinct, stated while resolving an env-var naming argument: *"io sposterei
tutto su postgres e cancellerei la .env"*. The direction is right and half-built already —
this note records what is measured, so whoever picks it up does not rediscover it.

## What already exists

`internal/settings` + the `aura.settings` table + the cockpit settings surface. Several
`.env.example` entries already carry the comment *"Owned by Postgres (aura.settings), set
from the cockpit. Left EMPTY on purpose: the persisted row wins over this file at boot"* —
so the precedence is decided and implemented, not hypothetical.

## What is measured (2026-09-09)

- `internal/settings/settings.go`'s allowlist holds **22 keys**.
- `cmd/aura/serve_settings.go` actually applies **10** of them as boot overrides.
- The PRD's env catalogue lists roughly **60** variables.

So the Postgres layer covers a fraction of the surface today, and the gap between "allowed"
and "applied" (22 vs 10) is itself a defect worth closing first — an allowlisted key nobody
applies is a setting the cockpit can write and the process ignores.

## Why `.env` cannot be deleted outright

Three values cannot live in the store they unlock, by construction:

- `POSTGRES_PASSWORD` / `AURA_DB_URL` — without the DSN there is no database to read the
  settings from.
- `AURA_AUTHULA_SECRET` — the KEK that decrypts every stored secret (`internal/identitykey`,
  `internal/mcpoauth`). A key that decrypts the store cannot itself be a row in that store
  encrypted with itself.
- `AURA_ARCADEDB_TENANT_SECRET` — the HMAC root each tenant's ArcadeDB credential is derived
  from; same argument.

The honest end state is therefore **not** "no `.env`" but *"`.env` holds three or four
bootstrap values, everything else lives in `aura.settings` and changes from the cockpit
without a restart"*. Say that plainly when this is planned, or the phase will be written
against a goal it cannot reach and will be judged to have failed.

## Why it was not done inline

Raised mid-flight during phase 02 (plan 02-07, 7 of 10 plans landed). It is a milestone-level
change touching config load, the settings allowlist, the cockpit, CI workflows and every
smoke script — not something to open with a phase half-executed.

## The argument that produced it

`.env.example` was renaming `OPENROUTER_API_KEY` to `AURA_OPENROUTER_API_KEY`. Measured blast
radius for that one variable: **12 non-test Go sites** (including `internal/llm/config.go`,
the settings allowlist and three cockpit settings surfaces), ~10 test files, 2 CI workflows
and ~10 scripts — plus CLAUDE.md §Env vars, which names `OPENROUTER_API_KEY` as a deliberate
upstream-canonical exception. That blast radius is the same one this todo would have to
absorb deliberately rather than by accident, and it is the reason a naming decision that
looks cosmetic is not.
