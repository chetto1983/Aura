---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 20
subsystem: database
tags: [postgres, row-level-security, rls, migrations, golang-migrate, pgx, defense-in-depth, identity-isolation]

# Dependency graph
requires:
  - phase: 37F-07
    provides: internal/share/store.go with owner-scoped methods already routed through db.WithIdentityTxRaw, plus migration 0040 (aura.shared_links / aura.share_audit)
  - phase: 36 (multi-user-identity-isolation-authula-cutover)
    provides: migration 0032_owner_rls — the fail-closed-on-mismatch / permissive-on-unset RLS pattern this plan mirrors exactly
provides:
  - Migration 0041 — ENABLE ROW LEVEL SECURITY + shared_links_owner_isolation policy on aura.shared_links
  - internal/share/store_rls_integration_test.go — live DB proof the backstop actually bites (cross-identity denial, owner self-visibility, public/internal resolvers unaffected)
  - prd.md + docs/adr/0039 truth-up recording the 0041 migration floor and closing the RLS-backstop parity gap
affects: [any future phase reading/writing aura.shared_links, future RLS/identity-isolation hardening work, the remaining 37F plans (10-13, 17-19) that close the phase]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "RLS owner-isolation policy retrofit: mirror an established policy (0032_owner_rls) verbatim in shape when adding RLS to a table created after the pattern was established, rather than inventing new semantics"

key-files:
  created:
    - internal/db/migrations/0041_shared_links_rls.up.sql
    - internal/db/migrations/0041_shared_links_rls.down.sql
    - internal/share/store_rls_integration_test.go
  modified:
    - prd.md
    - docs/adr/0039-conversation-sharing-vs-identity-isolation.md

key-decisions:
  - "Mirrored 0032_owner_rls exactly (ENABLE not FORCE, fail-closed-on-mismatch, permissive-on-unset) instead of inventing new RLS semantics for shared_links"
  - "aura.share_audit deliberately excluded from RLS, consistent with the skill_audit (0010) / mcp_audit append-only audit-table precedent"
  - "Verified the migration's up-down-up round trip manually against a throwaway database (aura_migrate0041_drill) via a temporary, uncommitted Go test, rather than adding a persisted internal/db/migrate_0041_integration_test.go — honors this run's explicit file-scope lock to the plan's 5 declared files"
  - "Left internal/share/store.go's now-stale doc comment (predicting this exact future migration) untouched for the same scope-lock reason; documented as a safe follow-up rather than silently expanding this run's touched-file set"

requirements-completed: [WEBSHARE-02]

coverage:
  - id: D1
    description: "Migration 0041 (up + down) enables RLS on aura.shared_links with an owner-isolation policy mirroring 0032_owner_rls; down cleanly reverses (policy dropped, RLS disabled); share_audit stays RLS-free"
    requirement: "WEBSHARE-02"
    verification:
      - kind: other
        ref: "manual up->down->up round trip against a throwaway DB (aura_migrate0041_drill) via `go test -tags db_integration -race -run TestDrill0041RoundTrip ./internal/db` (temporary file, deleted after use — never committed)"
        status: pass
    human_judgment: true
    rationale: "The round-trip proof ran against a temporary, uncommitted test file deleted after verification (per this run's explicit file-scope lock), not a persisted regression test — a future edit to 0041 will not be auto-re-checked for reversibility by CI. A human should periodically re-run the manual drill (commands documented in this SUMMARY) or a future plan should add a persisted migrate_0041_integration_test.go mirroring the 0040 precedent."
  - id: D2
    description: "RLS backstop proven live at the DB layer: cross-identity denial with zero owner_identity_id predicates in the query, owner self-visibility (not USING(false)), and the public/internal plain-pool resolvers unaffected"
    requirement: "WEBSHARE-02"
    verification:
      - kind: integration
        ref: "internal/share/store_rls_integration_test.go#TestShareStoreRLSDeniesCrossIdentityRead"
        status: pass
      - kind: integration
        ref: "internal/share/store_rls_integration_test.go#TestShareStoreRLSOwnerStillSeesOwnRow"
        status: pass
      - kind: integration
        ref: "internal/share/store_rls_integration_test.go#TestShareStoreRLSPublicLaneUnaffected"
        status: pass
    human_judgment: false
  - id: D3
    description: "prd.md and docs/adr/0039 amended to record migration 0041 as shipped, closing the parity gap in ADR-0039's RLS-backstop Context claim"
    requirement: "WEBSHARE-02"
    verification: []
    human_judgment: true
    rationale: "Documentation accuracy and wording are not automatable — a human should confirm the PRD migration-numbering note and the ADR addendum read correctly, stay factual, and do not restate the seven public-tier mitigations, per the plan's own instruction."

# Metrics
duration: 38min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 20: RLS Backstop for aura.shared_links Summary

**Migration 0041 gives `aura.shared_links` its own owner-isolation RLS policy, mirroring `0032_owner_rls` exactly, and proves the backstop bites at the database layer with zero `owner_identity_id` predicates in the proving query.**

## Performance

- **Duration:** ~38 min
- **Started:** 2026-07-17T19:58:00Z (approx)
- **Completed:** 2026-07-17T20:36:34Z
- **Tasks:** 3 completed
- **Files modified:** 5 (3 created, 2 modified)

## Accomplishments

- `aura.shared_links` now has `ENABLE ROW LEVEL SECURITY` plus a `shared_links_owner_isolation` policy (migration `0041_shared_links_rls`), closing the defense-in-depth gap 37F-07 flagged: every other identity-scoped table (`conversations`, `paused_states`, `conversation_turns`) has carried an RLS backstop since `0032_owner_rls`, but `shared_links` (migration `0040`) shipped without one.
- Three new integration tests in `internal/share/store_rls_integration_test.go` prove the property live against Postgres: a foreign identity's deliberately unfiltered `SELECT id FROM aura.shared_links WHERE id = $1` (no owner predicate anywhere in the file — proven by grep) returns zero rows; the true owner's identical query returns exactly one row (the mandatory self-lockout guard, ruling out a vacuous `USING (false)` policy); `ResolveByToken` and `ResolveLiveByID` — both plain-pool reads with no principal set — still resolve live public/internal links unchanged.
- `prd.md` and `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` now correctly state the on-disk migration floor is `0041` and that the ADR's RLS-backstop claim is true for all four identity-scoped tables, not three.
- Migration round-trip (up → down → up) verified against a throwaway database (`aura_migrate0041_drill`), never the live `aura` database.

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0041 (up + down)** - `d158bc97` (feat)
2. **Task 2: Integration test proving the backstop actually bites** - `68c43a06` (test)
3. **Task 3: Documentation truth-up (PRD + ADR-0039)** - `029c6189` (docs)

**Plan metadata:** committed alongside this SUMMARY (see final commit below)

## Files Created/Modified

- `internal/db/migrations/0041_shared_links_rls.up.sql` - ENABLE ROW LEVEL SECURITY + shared_links_owner_isolation policy on aura.shared_links, mirroring 0032_owner_rls's USING clause shape exactly, keyed on owner_identity_id
- `internal/db/migrations/0041_shared_links_rls.down.sql` - DROP POLICY + DISABLE ROW LEVEL SECURITY, full reversal
- `internal/share/store_rls_integration_test.go` - three live-DB tests: cross-identity denial, owner self-visibility, public/internal resolver non-regression
- `prd.md` - §Persistence migration-numbering fonte-di-verità updated to record the floor as 0041
- `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` - short factual addendum recording the RLS backstop is now true for shared_links too

## Decisions Made

- **Mirrored `0032_owner_rls` exactly** rather than designing new RLS semantics: `ENABLE` (not `FORCE`) row-level security, because `aura_migrate` owns the table and bypasses RLS by default while `aura_app` is a non-owner/non-superuser/non-BYPASSRLS role; the `USING` clause is byte-identical in shape to `0032`'s, just keyed on `shared_links.owner_identity_id` instead of `identity_id`. This keeps the codebase's one RLS idiom, not two.
- **`aura.share_audit` stays RLS-free**, consistent with the `skill_audit` (0010) / `mcp_audit` append-only audit-table precedent — an audit ledger's append-only property is enforced by grants (`aura_app` has `SELECT`+`INSERT` only), and a self-filtering audit table would stop being useful for cross-identity forensics.
- **No `internal/db/migrate_0041_integration_test.go`** was added, even though migration `0040` has a `migrate_0040_integration_test.go` precedent for exactly this round-trip proof. This run operated under an explicit file-scope lock to the plan's 5 declared files (a separate branch and an unrelated leaked commit exist on master that are the human's to reconcile). The round trip was instead proven manually via a temporary, uncommitted Go test against a throwaway database, then deleted before any commit — see "Known Follow-ups" below.
- **`internal/share/store.go`'s doc comment (lines 16-25) was left untouched** for the same scope-lock reason, even though it is now stale: it says "as of migration 0040, aura.shared_links carries NO RLS policy" and "today it is inert for this table," both now false. The comment itself already anticipated this exact migration ("A future migration adding RLS to aura.shared_links would make the backstop live with zero Go-side changes") — that prediction is now correct, but the comment's own wording needs a follow-up touch.

## Deviations from Plan

None - plan executed exactly as written. All 3 tasks completed; the touched file set matches the plan's `files_modified` frontmatter exactly (verified via `git diff --name-only` against all 5 declared paths — no more, no less).

## Known Follow-ups (not deviations — deliberately deferred by explicit scope lock)

These are safe, low-risk, low-priority items identified during execution but intentionally not actioned, because this run was explicitly constrained to touch only the plan's 5 declared files (a separate `fix/ci-red-37f-drift` branch and an unrelated leaked `ci.yml` commit exist on master; the human owns that reconciliation, and the instruction was to stay tightly scoped rather than expand the touched-file set):

1. **`internal/share/store.go` lines 16-25** — the doc comment predicting a future RLS migration is now stale (it says the backstop "is inert for this table" and gives instructions for "fixing" the assumption; both are now out of date). A future touch of this file should update the comment to state the backstop is live as of migration 0041.
2. **`internal/db/migrate_0041_integration_test.go`** does not exist, unlike its `migrate_0040_integration_test.go` sibling. The round-trip property (up→down→up against a throwaway DB) was proven manually during this execution (see Deviations above) but is not re-checked by any future CI run. A future plan could add this file to close the gap permanently.

## Issues Encountered

None. The `state.advance-plan` SDK call revealed that STATE.md's simple "Plan: N of 20" counter for phase 37F had already drifted significantly out of sync with reality before this session touched anything (it read "Plan: 3 of 20" despite 12 of 37F's 20 plan files already having a SUMMARY.md, because 37F plans have landed via multiple sessions/mechanisms that don't all call this counter). This is a pre-existing characteristic of how this large, multi-phase project's STATE.md evolves (rich per-plan narrative history blocks carry the real detail; the simple counter is decorative), not something this session broke. Rather than freehand-editing ambiguous shared prose I don't have full provenance for, this session added an accurate, appropriately detailed `#### 37F-20 — ...` narrative block to STATE.md's Current Position history (matching the established convention used by the adjacent 37E-02..37E-05 blocks) and left the pre-existing stale summary line as-is.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The defense-in-depth gap 37F-07 flagged is closed: `aura.shared_links` now has parity with `conversations`/`paused_states`/`conversation_turns` on the RLS backstop.
- 37F plans 10, 11, 12, 13, 17, 18, 19 remain pending for full phase closure — independent of this gap closure, since 37F-20 depended only on 37F-07 (already shipped) and was inserted mid-phase after explicit operator approval.
- No blockers for downstream work. The two "Known Follow-ups" above are safe to pick up in any future touch of `internal/share/store.go` or `internal/db/`.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: internal/db/migrations/0041_shared_links_rls.up.sql
- FOUND: internal/db/migrations/0041_shared_links_rls.down.sql
- FOUND: internal/share/store_rls_integration_test.go
- FOUND commit: d158bc97 (feat(37F-20): migration 0041)
- FOUND commit: 68c43a06 (test(37F-20): RLS integration tests)
- FOUND commit: 029c6189 (docs(37F-20): PRD + ADR-0039)
- Touched-file set matches plan `files_modified` frontmatter exactly (5/5, verified via `git diff --name-only`)
- All plan acceptance criteria re-verified passing (migration slot, RLS test run, grep proof, owner-visibility, resolver non-regression, throwaway round trip, build/vet/test, file-size gate)
