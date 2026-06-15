---
phase: 04-hitl-identity-conversations
plan: 02
subsystem: identity
tags: [identity, capability-grants, store-pattern, sqlc, pgx, sqlstate, cli, hitl]

# Dependency graph
requires:
  - phase: 04-hitl-identity-conversations
    plan: 01
    provides: migrations 0004 (identities + capability_grants + seed local/'*') applied + idempotent; sqlc surface (CreateIdentity/GetIdentityByName/GetIdentityByID/ListIdentities/DeleteIdentity + GrantCapability/RevokeCapability/HasCapability/ListCapabilities); db.WithTx
  - phase: 01-infra-db-knowledge
    provides: db.Open pgxpool + EnsureRoles + role separation (aura_app/aura_migrate)
provides:
  - "internal/identity.Store — the CANONICAL per-domain Store pattern (Store{pool,q} via sqlc.New(pool)) that 04-03/04-04/04-05 copy verbatim"
  - "HasCapability(ctx, identityID, cap) wildcard-or-exact (true on '*' OR exact match; error only on DB failure)"
  - "GrantCapability/RevokeCapability idempotent (ON CONFLICT DO NOTHING + defensive 23505 swallow via errors.As; absent-revoke is a no-op)"
  - "ErrWildcardManaged / ErrInvalidCapability / ErrIdentityNotFound sentinel errors (callers classify without string-matching)"
  - "capability name grammar ^[a-z][a-z0-9._-]{0,63}$ validated PRE-DB (T-04-05 mitigation); '*' grant/revoke rejected PRE-DB (T-04-06)"
  - "aura identity {list|get|grant|revoke} hand-rolled switch CLI wired into cmd/aura/main.go"
affects: [04-03-askuser, 04-04-conversations, 04-05-runner, swarm-phase-9, skills-phase-11, memory-phase-15]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Canonical Store: type Store struct{pool *pgxpool.Pool; q *sqlc.Queries}; func New(pool) *Store { return &Store{pool, sqlc.New(pool)} } — non-tx reads use s.q; no interface declared in the domain package (D-A2-02, consumer declares it)"
    - "SQLSTATE classification via var pgErr *pgconn.PgError; errors.As(err,&pgErr) + switch pgErr.Code (23505 unique) — NEVER message-match (RESEARCH Pitfall 2)"
    - "domain projection (Identity{ID,Name,Kind} string fields) converts pgtype.UUID at the package boundary via uuid.UUID(r.ID.Bytes).String() (RESEARCH Pitfall 5)"
    - "pre-DB validation gate (validateGrantInput) runs wildcard + name-grammar + uuid-parse BEFORE any round-trip — the threat-model mitigation surface"
    - "hand-rolled switch CLI mirroring runDB (config.Load -> db.Open -> Store.New -> switch args[0]); tabwriter list/get; os.Exit(1) on error — NO cobra"

key-files:
  created:
    - internal/identity/store.go
    - internal/identity/store_test.go
    - internal/identity/store_unit_test.go
    - internal/identity/main_test.go
    - cmd/aura/identity.go
    - scripts/run_identity_integration.sh
    - scripts/smoke_identity_cli.sh
  modified:
    - cmd/aura/main.go

key-decisions:
  - "cobra -> hand-rolled switch (RESEARCH OPEN QUESTION 1 / Assumption A2): CONTEXT D-A3-05 says 'cobra command group' but go.mod has no spf13/cobra and the codebase uses nested switch dispatchers (runDB); CLAUDE.md mandates following existing patterns; SPEC never requires cobra. HOW deviation, no SPEC change, no PRD amendment."
  - "goleak TestMain lives in main_test.go (db_integration-tagged) — the leak surface is the live pgx pool, so goleak guards the integration tier (mirrors internal/db)"
  - "unit tier (store_unit_test.go, no build tag) covers the pure validation gate (validateGrantInput, isUniqueViolation, parseUUID) so coverage holds without a DB; DB paths covered under db_integration"
  - "DB-error-wrap branches covered by a canceled-context integration test (mirrors db_test.go TestPing_QueryErrorOnCanceledContext) so every '%w' wrap is exercised"
  - "the 23505-swallow return-nil in GrantCapability is the only uncovered line (ON CONFLICT DO NOTHING never raises 23505) — defensive belt-and-suspenders, isUniqueViolation itself is unit-tested"

patterns-established:
  - "THE CANONICAL STORE PATTERN (D-A4-01 derisk goal achieved). 04-03 askuser.Store, 04-04 conversations.Store, 04-05 runner consumers copy this exact shape: Store{pool,q}, New(pool), s.q reads, db.WithTx for atomic writes, errors.As+pgErr.Code classification, sentinel errors, pgtype boundary conversion, no domain-package interface."
  - "test discipline: split unit tier (pure logic, no tag) + db_integration tier (real Postgres, goleak, envOrSkip t.Fatal-under-CI no-skip-as-green) — combined coverage measured across the full tag matrix"
  - "CLI: hand-rolled switch dispatcher per-domain (runIdentity) mirroring runDB; tabwriter output; destructive/system-managed ops surface the Store sentinel to stderr non-zero"

metrics:
  tasks: 2
  duration: ~35min
  completed: 2026-05-30
  coverage: "98.0% combined (unit + db_integration), floor 85%"
  files-created: 7
  files-modified: 1
---

# Phase 4 Plan 02: Identity Slice (1.7) Summary

**One-liner:** The canonical per-domain `Store{pool,q}` pattern proven on the lowest-risk domain — `internal/identity.Store` with wildcard-or-exact `HasCapability`, idempotent grant/revoke, pre-DB `'*'`/name-grammar rejection, and FK-cascade delete, plus a hand-rolled `aura identity` switch CLI — establishing the exact template 04-03/04/05 copy verbatim.

## What Was Built

### Task 1 — `internal/identity.Store` (commit `9c4890a7`)
The canonical Store over the 04-01 sqlc surface:
- **`Store{pool *pgxpool.Pool, q *sqlc.Queries}`** built via `New(pool) = &Store{pool, sqlc.New(pool)}`. Non-tx reads use `s.q`. No interface in the domain package — the consumer (the Runner, 04-05) declares the narrow interface (D-A2-02).
- **`HasCapability(ctx, identityID, cap) (bool, error)`** — wildcard-or-exact via the generated `HasCapability` query (`capability = '*' OR capability = $2`); error only on real DB failure. Parses the identity UUID string into `pgtype.UUID` at the boundary.
- **`GrantCapability` / `RevokeCapability`** — idempotent. Grant uses the `ON CONFLICT DO NOTHING` query and additionally swallows a SQLSTATE `23505` (classified via `errors.As(&pgErr)` + `pgErr.Code`, never message-match — RESEARCH Pitfall 2). Revoking an absent capability affects zero rows and returns no error.
- **`'*'` rejection + name validation BEFORE any DB call** — `validateGrantInput` rejects the system-managed wildcard (`ErrWildcardManaged`) and any name failing `^[a-z][a-z0-9._-]{0,63}$` (`ErrInvalidCapability`) prior to a round-trip (threat-model T-04-05/T-04-06). The regex is compiled once at package init.
- **`ListIdentities` / `GetIdentityByName` / `DeleteIdentity`** — `GetIdentityByName` maps `pgx.ErrNoRows` to a wrapped `ErrIdentityNotFound`; `DeleteIdentity` cascades `capability_grants` away via the FK `ON DELETE CASCADE`.
- **Domain projection** `Identity{ID,Name,Kind}` converts `pgtype.UUID` → canonical string at the package boundary (RESEARCH Pitfall 5).

Tests: `store_unit_test.go` (unit tier, no tag) covers the pure validation gate; `store_test.go` + `main_test.go` (`db_integration`, goleak) cover seed assertion, wildcard, isolated exact-match, grant/revoke idempotency, `'*'` rejection, name rejection, FK cascade, `ListIdentities` seed presence, not-found, and every DB-error-wrap branch via a canceled context.

### Task 2 — `aura identity` CLI (commit `502c50a5`)
`cmd/aura/identity.go` — `runIdentity(args)` mirroring `runDB`: `config.Load()` → `db.Open(ctx,&cfg.DB)` → `identity.New(pool)` → `switch args[0]` over `list|get <name>|grant <name> <cap>|revoke <name> <cap>`. `tabwriter` for `list`/`get`; `os.Exit(1)` on error; grant/revoke of `'*'` surface the Store's `ErrWildcardManaged` to stderr with a non-zero exit. Wired `case "identity"` into `cmd/aura/main.go` + the usage line + header doc.

## Verification Evidence

| Gate | Result |
|------|--------|
| `go build ./...` / `go vet ./...` | exit 0 |
| unit tier `go test ./internal/identity/` (+ `-race`) | green |
| db_integration `-race` (WSL, live Postgres) | 9 integration tests RAN green (60-70ms each — real round-trips, not skips) |
| combined coverage (unit + db_integration) | **98.0%** (floor 85%) |
| `golangci-lint run ./internal/identity/ ./cmd/aura/` | **0 issues** |
| live CLI smoke (WSL) | list shows `local`; get works; grant/revoke idempotent; `grant/revoke local '*'` rejected non-zero with the system-managed message — `ALL_SMOKE_PASS` |
| file sizes | store.go 185, store_test.go 378, identity.go 136 — all ≤ 600 LOC |
| cobra dep check | `grep cobra go.mod` → empty (no new dependency) |

The `db_integration` tier was run against the live `aura-postgres` container via `scripts/run_identity_integration.sh` (derives `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`, `set +H` for the `!`-bearing password). No-skip-as-green honored: `envOrSkip` `t.Fatal`s under `$CI`.

## The Canonical Store Pattern (for 04-03 / 04-04 / 04-05 to copy)

This plan's primary purpose (D-A4-01 derisk) is to lock the shape every later Phase-4 (and future Scheduler/Skills/Memory) DB slice copies:

1. **Struct + constructor:** `type Store struct{ pool *pgxpool.Pool; q *sqlc.Queries }`; `func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }`.
2. **Reads via `s.q`**; **atomic multi-statement writes wrap `db.WithTx`** (identity has none — single-statement only; 04-04 `AppendTurn` is the first real consumer).
3. **SQLSTATE classification only:** `var pgErr *pgconn.PgError; errors.As(err, &pgErr)` then `switch pgErr.Code` (`23505` unique, `23503` FK, `42501` privilege). Never string-match the message.
4. **Sentinel errors** at the package top (`ErrWildcardManaged`, `ErrIdentityNotFound`, …) so callers `errors.Is` without coupling to wording.
5. **pgtype boundary conversion:** convert `pgtype.UUID`/`pgtype.Text`/`pgtype.Numeric` to plain Go types in a small `fromRow` helper at the package edge.
6. **No interface in the domain package** — the consumer declares the narrow interface (D-A2-02, "accept interfaces, return structs").
7. **Validation BEFORE the DB** for any operator-controlled string (threat-boundary input).
8. **Test discipline:** unit tier (pure logic, no tag) + `db_integration` tier (real Postgres, `goleak.VerifyTestMain` in a tagged `main_test.go`, `envOrSkip` that `t.Fatal`s under `$CI`). Cover DB-error-wrap branches with a canceled-context test. Measure coverage across the full tag matrix; floor 85%.
9. **CLI:** a per-domain `run<X>(args []string)` hand-rolled `switch` mirroring `runDB` — NOT cobra.

## Deviations from Plan

### Documented HOW Deviation (planned, in the Task 2 brief)

**1. [Rule N/A — planned] cobra → hand-rolled switch CLI**
- **Source:** RESEARCH OPEN QUESTION 1 / Assumption A2; the deviation was pre-authorized in the Task 2 action block.
- **Issue:** CONTEXT D-A3-05 says "`aura chat` becomes a cobra command group", but `go.mod` has no `spf13/cobra` and the codebase uses nested `switch` dispatchers (`runDB`).
- **Resolution:** Implemented `runIdentity` as a `switch` subcommand tree mirroring `runDB`. CLAUDE.md mandates following existing patterns; SPEC never requires cobra. This is a HOW deviation from CONTEXT wording, not a SPEC requirement change — no PRD amendment needed beyond this note. No new dependency introduced (verified: `grep cobra go.mod` empty).

### Auto-fixed Issues

**None of Rule 1/3.** No bugs or blocking issues encountered.

**2. [Rule 2 — coverage hardening] DB-error-wrap + isUniqueViolation coverage**
- **Found during:** Task 1 coverage measurement (initial combined coverage 78.0%, below the 85% floor).
- **Issue:** the `%w` error-wrap branches (DB-failure paths) and `isUniqueViolation` (the defensive 23505 classifier — unreachable on the happy path because `ON CONFLICT DO NOTHING` never raises 23505) were uncovered.
- **Fix:** added a unit test for `isUniqueViolation` (synthetic `*pgconn.PgError` for 23505/23503/42501/wrapped/nil — proves type-classification not message-match) and a `db_integration` `TestStoreMethods_DBErrorWrapping` that drives every method against a canceled context. Combined coverage → **98.0%**.
- **Files modified:** internal/identity/store_unit_test.go, internal/identity/store_test.go
- **Commit:** `9c4890a7` (same task commit)

## Authentication Gates

None — the DB stack (`aura-postgres`) was already up; no credentials beyond the `.env` `POSTGRES_PASSWORD` were needed.

## Known Stubs

None. The identity slice is fully wired end-to-end (Store → CLI → live DB), verified by the CLI smoke against the real seeded `local` identity.

## Threat Flags

None — no new security surface beyond the planned `<threat_model>` (T-04-04/05/06/07). The identity/capability surface is CLI/infra-only (no LLM-facing grant tool registered, T-04-04); name validation + parameterized sqlc queries mitigate injection (T-04-05); `'*'` rejected pre-DB (T-04-06); SQLSTATE classification via `errors.As` not message-match (T-04-07).

## Self-Check: PASSED

- Created files verified present: internal/identity/{store.go, store_test.go, store_unit_test.go, main_test.go}, cmd/aura/identity.go.
- Commits verified in git log: `9c4890a7` (Task 1), `502c50a5` (Task 2).
