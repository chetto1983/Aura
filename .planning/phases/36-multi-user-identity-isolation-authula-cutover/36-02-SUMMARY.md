---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 02
subsystem: database
tags: [postgres, migrations, identity-isolation, rls, saga, garage, audit, config, rollout-flag]

# Dependency graph
requires:
  - phase: 36-01
    provides: "migration 0026 local_admin_caps — the ledger floor this plan sequences above (0027-0031)"
provides:
  - "Migration 0027: aura.paused_states.identity_id column, backfilled from parent conversation, indexed (OQ2)"
  - "Migration 0028: aura.provisioning_saga resumable saga journal, mutable pending->done/failed, no identity FK (D-14/D-27)"
  - "Migration 0029: aura.identities soft-delete columns deactivated_at + purge_after (D-27 grace window)"
  - "Migration 0030: aura.identity_object_store per-identity Garage key store, secret_key_enc bytea encrypted-at-rest (D-08/OQ4)"
  - "Migration 0031: audit identity indexes on mcp_audit + skill_audit for the D-28 admin per-user reads"
  - "config.Config.MUSRIsolation bool — the D-13 documents-retrieval scoped-vs-unscoped path selector (default OFF)"
  - "REQUIREMENTS.md RBAC-03 amendment note (OQ1/A4): RLS-for-isolation IN, RLS-for-roles OUT"
affects: [36-04 RLS/owner-scoping, 36-05 documents scoping, 36-06 Garage key persistence, 36-08 provisioning saga, 36-10 audit UI, 36-12 rollout flip]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Additive forward-only migration pairs (up + matching down), sequenced above the shipped floor without renumbering"
    - "First-class owner column over a per-read conversation_id subquery (OQ2) for a clean *ForIdentity / RLS predicate"
    - "Mutable saga journal (0023 grant shape) vs append-only audit ledger (0010/0021 trigger shape) — chosen by lifecycle"
    - "Identity-scoped secret stored as encrypted-at-rest bytea (never plaintext), key derived from an existing app secret"
    - "Security rollout flag as a dedicated config field, excluded from the internal/settings model-knob overlay"

key-files:
  created:
    - "internal/db/migrations/0027_paused_states_identity.{up,down}.sql"
    - "internal/db/migrations/0028_saga_journal.{up,down}.sql"
    - "internal/db/migrations/0029_identity_soft_delete.{up,down}.sql"
    - "internal/db/migrations/0030_identity_object_store.{up,down}.sql"
    - "internal/db/migrations/0031_audit_identity_indexes.{up,down}.sql"
  modified:
    - "internal/config/config.go (MUSRIsolation field + envutil.BoolDefault load)"
    - "internal/config/config_knobs.go (AURA_MUSR_ISOLATION KnobSpec registry entry)"
    - "internal/config/config_test.go (TestMUSRIsolationDefaultOff + clearPostgresEnv key)"
    - ".planning/REQUIREMENTS.md (RBAC-03 OQ1/A4 amendment note)"

key-decisions:
  - "paused_states.identity_id is a stored, backfilled, NULLABLE column (OQ2) — write-path population deferred to plan 05"
  - "provisioning_saga.identity_id has NO FK — a de-provision saga must survive the identity deletion it executes (mirrors 0014)"
  - "identity_object_store secret_key_enc wrapping key derives from AURA_AUTHULA_SECRET (existing 32-byte hex app secret); AEAD impl is plan 06"
  - "0031 indexes only mcp_audit + skill_audit — identity_audit already has new_identity_id idx; tool_invocations is conversation-keyed (no identity column)"
  - "AURA_MUSR_ISOLATION defaults OFF — load-bearing so plan 12's deploy-flag-off step is safe; NOT routed through OverlayEnv (T-36-02-T)"
  - "No RLS ENABLE/policy this plan — deferred to plan 04 with its safe WithIdentityTx read-path (RESEARCH Pitfall 1)"

patterns-established:
  - "Additive migration above the ledger floor with a matching down for every up"
  - "D-13 reversible-rollout flag: dedicated config field, default-off, catalogued in the knob registry, excluded from the mutable overlay"

requirements-completed: []  # MUSR-01/03/06 are phase-spanning; this foundation-only plan advances but does NOT close them (see Requirements Advanced below). Mirrors the 36-01 precedent that kept MUSR-06 open.

coverage:
  - id: D1
    description: "Migrations 0027-0031 apply and reverse cleanly on the live Postgres stack (paused_states.identity_id backfill, provisioning_saga, soft-delete, identity_object_store, audit indexes)"
    requirement: "MUSR-01"
    verification:
      - kind: integration
        ref: "go test -tags db_integration -run TestMigrate ./internal/db/ (shippedMigrationCount auto-counts the 5 new up files)"
        status: unknown
    human_judgment: false
    rationale: "Windows host has no live stack (no CGO/db_integration); the apply/reverse round-trip must run in WSL/CI per CLAUDE.md 'Where to run what'. Not fabricated green (no-skip-as-green)."
  - id: D2
    description: "config.Config.MUSRIsolation reads AURA_MUSR_ISOLATION via envutil.BoolDefault, defaults false, honors override"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/config/config_test.go#TestMUSRIsolationDefaultOff"
        status: pass
    human_judgment: false
  - id: D3
    description: "The isolation flag is NOT routed through the internal/settings OverlayEnv AllowedKeys (T-36-02-T)"
    verification:
      - kind: other
        ref: "grep -c MUSR internal/settings/settings.go => 0 matches"
        status: pass
    human_judgment: false
  - id: D4
    description: "REQUIREMENTS.md RBAC-03 amendment note records D-07/OQ1/A4 (RLS-for-isolation IN, RLS-for-roles OUT)"
    verification:
      - kind: manual_procedural
        ref: ".planning/REQUIREMENTS.md RBAC-03 bullet — 36-02 note (OQ1/A4)"
        status: pass
    human_judgment: false

# Metrics
duration: ~25min
completed: 2026-07-05
status: complete
---

# Phase 36 Plan 02: Additive Identity-Isolation Schema Foundation + AURA_MUSR_ISOLATION Rollout Flag Summary

**Five additive Postgres migrations (0027-0031: owner column + resumable saga journal + soft-delete + per-identity Garage-key store + audit identity indexes) plus the default-off `AURA_MUSR_ISOLATION` documents-retrieval path selector — the schema foundation plans 04/05/06/08/10/12 build on.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-05T18:09Z (approx)
- **Completed:** 2026-07-05T18:34Z
- **Tasks:** 2
- **Files modified:** 14 (10 migration files created, 4 modified)

## Accomplishments
- **Migrations 0027-0030 (Task 1):** paused_states gains a first-class backfilled `identity_id` (OQ2); a mutable `aura.provisioning_saga` resumable journal (pending->done/failed, no identity FK so a de-provision saga survives its own teardown); `aura.identities` soft-delete columns; and `aura.identity_object_store` holding the per-identity Garage secret as encrypted-at-rest `bytea`.
- **Migration 0031 + config flag + RBAC-03 note (Task 2):** `(actor_identity_id, created_at DESC)` on mcp_audit and `(identity_id, created_at DESC)` on skill_audit for the D-28 admin reads; `config.Config.MUSRIsolation` (default OFF) — the D-13 documents-retrieval scoped-vs-unscoped path selector plan 05 consumes and plan 12 flips; RBAC-03 amendment citing OQ1/A4.
- **Threat mitigations held:** the flag is a dedicated config field excluded from the `internal/settings` OverlayEnv overlay (T-36-02-T, verified 0 matches); the object-store secret is ciphertext `bytea` not plaintext (T-36-02-I); default-OFF prevents pre-backfill enforcement (T-36-02-T2); the saga journal enables forward-recovery (T-36-02-R); zero new packages (T-36-02-SC).
- **No RLS enabled** — deferred to plan 04 with its safe `WithIdentityTx` read-path (enabling it here would fail-close pooled non-tx reads, RESEARCH Pitfall 1).

## Task Commits

Each task was committed atomically:

1. **Task 1: Owner + saga + soft-delete + object-store migrations (0027-0030)** - `79ba36a4` (feat)
2. **Task 2: Audit identity indexes (0031) + AURA_MUSR_ISOLATION flag + RBAC-03 amendment** - `10ee788f` (feat)

**Plan metadata:** _(final docs commit — see completion)_

## Files Created/Modified
- `internal/db/migrations/0027_paused_states_identity.{up,down}.sql` - identity_id column + conversation backfill + index
- `internal/db/migrations/0028_saga_journal.{up,down}.sql` - aura.provisioning_saga resumable journal (mutable status)
- `internal/db/migrations/0029_identity_soft_delete.{up,down}.sql` - identities deactivated_at + purge_after
- `internal/db/migrations/0030_identity_object_store.{up,down}.sql` - per-identity Garage key, secret_key_enc bytea
- `internal/db/migrations/0031_audit_identity_indexes.{up,down}.sql` - mcp_audit + skill_audit identity indexes
- `internal/config/config.go` - MUSRIsolation bool field + envutil.BoolDefault("AURA_MUSR_ISOLATION", false)
- `internal/config/config_knobs.go` - AURA_MUSR_ISOLATION KnobSpec (KindBool, default false)
- `internal/config/config_test.go` - TestMUSRIsolationDefaultOff + clearPostgresEnv entry
- `.planning/REQUIREMENTS.md` - RBAC-03 amendment note (OQ1/A4)

## Decisions Made
- **OQ2 (paused_states):** stored `identity_id` column over a per-read subquery — a clean predicate for the plan-04 `*ForIdentity` / RLS owner filter. NULLABLE; plan 05 wires the write path.
- **Saga FK omission:** `provisioning_saga.identity_id` carries NO FK (mirrors `pending_notifications.identity_id`, 0014) so a de-provision saga survives the deletion of the identity it is executing — the whole point of a forward-recovery journal.
- **Object-store encryption:** documented the wrapping key derives from the existing `AURA_AUTHULA_SECRET` (32-byte hex, already used for key derivation) at the same trust boundary as `.env`; the AEAD encrypt/decrypt is left to the plan-06 persistence layer (this plan is pure additive schema).
- **0031 scope:** indexed only the two audit ledgers that carry an identity-principal column but lack an index on it (mcp_audit→actor_identity_id, skill_audit→identity_id). `identity_audit` already has `new_identity_id`; `tool_invocations` is conversation-keyed (no identity column). Documented both exclusions in the migration.
- **Requirements not closed:** MUSR-01/03/06 are phase-spanning and only partially advanced by this schema foundation — not marked complete (see below).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] NOT NULL constraints on provisioning_saga columns**
- **Found during:** Task 1 (0028 saga journal)
- **Issue:** The plan's inline DDL sketch (`saga_id uuid, identity_id uuid, kind text ..., step text, status text ...`) did not spell out nullability; a saga row with a NULL saga_id/kind/step/status is meaningless and would defeat forward-recovery.
- **Fix:** Added `NOT NULL` to saga_id, identity_id, kind, step, status (updated_at keeps `DEFAULT now()`), matching the codebase table convention (0004/0020/0023).
- **Files modified:** internal/db/migrations/0028_saga_journal.up.sql
- **Verification:** go build + go vet + untagged `go test ./internal/db/` green; CHECK constraints present (`status IN ('pending','done','failed')` verified by grep).
- **Committed in:** `79ba36a4` (Task 1 commit)

**2. [Clarification, not a deviation] skill_audit index uses its actual identity column name**
- The plan's shape "`(actor_identity_id, created_at DESC)`" is generic; skill_audit's identity-principal column is named `identity_id` (mcp_audit's is `actor_identity_id`). Each 0031 index uses the table's real column. No behavior change — documented for the verifier.

---

**Total deviations:** 1 auto-fixed (1 missing-critical hardening) + 1 documented clarification.
**Impact on plan:** The NOT NULL hardening is a correctness requirement within the declared file scope; no scope creep. All other work matches the plan exactly.

## Issues Encountered

- **`.env.example` could not be modified (environment permission block).** The workspace permission settings hard-deny access to `.env*` paths across the Read, Grep, and Bash tools, so the file listed in the plan's `files_modified` frontmatter is inaccessible. **No information is lost:** `AURA_MUSR_ISOLATION` is fully catalogued in `internal/config/config_knobs.go` — the registry whose own doc-comment names it the source "for .env.example / doc generation" — plus the `config.Config` struct field comment. A follow-up with `.env*` access should add the one-line `AURA_MUSR_ISOLATION=false` documentation entry to `.env.example`.
- **Live migration round-trip not run on this host (no-skip-as-green).** This Windows host has no live Postgres/CGO stack, so the `db_integration` apply/reverse round-trip (`TestMigrate*`) was NOT executed and is honestly reported `status: unknown` in the coverage block. Local validation that DID run green: `go build ./...`, `go vet ./internal/db/ ./internal/config/`, untagged `go test ./internal/db/` and `go test ./internal/config/` (the latter includes the property tests that now sample the new bool knob). The migration round-trip must run in WSL/CI per CLAUDE.md.

## Requirements Advanced (not closed)

This plan lays the **additive schema foundation** for `MUSR-01` (identity isolation), `MUSR-03`, and `MUSR-06`, but does NOT satisfy them — the enforcement they require ships later in the phase:
- **MUSR-01 (storage-enforced isolation):** RLS ENABLE + owner-scoping is plan 04; flag-gated documents scoping is plan 05.
- **MUSR-06:** phase-spanning (Authula default cutover + capability-per-route + no-token-in-URL E2E) closes at 36-12, exactly as the 36-01 SUMMARY noted.

Marking them complete now would be a false green. They remain `[ ]`; `requirements mark-complete` was intentionally NOT run.

## User Setup Required

None for this plan's runtime. One deferred doc task: add `AURA_MUSR_ISOLATION=false` to `.env.example` from an environment with `.env*` write access (blocked here by permission settings; the knob is already in the config_knobs registry).

## Next Phase Readiness
- **Ready:** plans 04 (RLS/owner-scoping — schema owner columns + soft-delete in place), 05 (flag-gated documents scoping — `MUSRIsolation` selector available), 06 (Garage key persistence — `identity_object_store` table + documented KEK source), 08 (provisioning saga — `provisioning_saga` journal), 10 (audit UI — identity indexes).
- **Blocker/verification owed:** run the `db_integration` migration apply/reverse round-trip on the live stack (WSL/CI) to flip D1 from `unknown` to `pass` before phase close.

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-05*

## Self-Check: PASSED

- Files verified present: 0027/0028/0029/0030/0031 `*.up.sql` + this SUMMARY.md — all FOUND.
- Commits verified in `git log`: `79ba36a4` (Task 1), `10ee788f` (Task 2) — both FOUND.
- Post-edit gate green (Windows host): `go build ./...`, `go vet ./internal/db/ ./internal/config/`, untagged `go test ./internal/db/ ./internal/config/`.
- Outstanding for phase close (honest, not fabricated): the `db_integration` migration apply/reverse round-trip must run in WSL/CI (D1 status `unknown`).
