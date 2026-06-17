---
phase: 25-chat-approval-center
plan: 06
subsystem: database
tags: [postgres, sqlc, golang-migrate, conversations, branch-tree, kv-cache, recursive-cte]

# Dependency graph
requires:
  - phase: 25-01
    provides: conversation REST adapter + reasoning-on flip (the chat lane this branch backend feeds)
  - phase: 1.8
    provides: conversations.Store + LoadHistory byte-identity contract + the L1/L2/L2.5 microcompact ladder
  - phase: 6
    provides: the CAP-04 messages[0] cache-invariant CI gate (scripts/cache_invariant_audit.sh)
provides:
  - CHAT-05 requirement recorded in ROADMAP + REQUIREMENTS (PRD-first)
  - Migration 0017 — additive parent_seq/branch_id pointers on aura.conversation_turns, defaulting existing rows into one canonical linear branch
  - sqlc queries CanonicalBranchLeafSeq + ListTurnsByBranchPath (recursive leaf->root walk) + SetTurnBranchPointers
  - conversations.Store.LoadBranchHistory (path-aware analog of LoadHistory) + CanonicalBranchLeaf + SetBranchPointers
  - conversations.Store.LoadManagedHistoryForBranch (path-aware managed history over the same microcompact ladder)
affects: [25-07, chat-branch-ui, agent-run-rerun-wiring]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Recursive-CTE leaf->root path walk returning root->leaf rows (column list mirrors ListTurnsBySeq so the single turnFromRow projection serves both loaders)"
    - "Additive tx-safe branch pointers with a canonical-branch default backfill keeping non-branched LoadHistory byte-identical"
    - "Branch path walk preserves the protected head (messages[0]/messages[1]) by construction — only body turns differ per branch (CAP-04)"

key-files:
  created:
    - internal/db/migrations/0017_conversation_turn_branches.up.sql
    - internal/db/migrations/0017_conversation_turn_branches.down.sql
    - internal/db/migrate_0017_integration_test.go
    - internal/conversations/store_branch.go
    - internal/conversations/store_branch_test.go
    - internal/conversations/store_branch_unit_test.go
    - internal/conversations/context_branch_test.go
  modified:
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md
    - internal/db/queries/conversation_turns.sql
    - internal/db/sqlc/conversation_turns.sql.go
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/conversations/context.go
    - internal/conversations/store_helpers.go
    - internal/conversations/store_fakedbtx_test.go

key-decisions:
  - "Option A path-walk shape: branch_id (all-zero canonical sentinel) + parent_seq pointers supporting N siblings (RESEARCH OQ3 full tree); no index in 0017 (single-operator pending set is tiny — RESEARCH A4 — and a plain ALTER stays tx-safe, Pitfall 5)"
  - "LoadManagedHistory (the default Runner entry point) stays UNCHANGED (loads full seq history) so non-branched conversations are byte-identical to the pre-0017 linear case; the path walk is the explicit-leaf LoadManagedHistoryForBranch. Plan 25-07 wires the append path to set parent_seq for new branches."
  - "Foundation provides the branch-write seam (SetBranchPointers) + path-walk read; the edit/re-run agent wiring is plan 25-07's scope."

patterns-established:
  - "Pattern: a sqlc recursive-CTE that mirrors an existing query's column list lets one domain projection (turnFromRow) serve both the linear and path-walk loaders, preserving byte-identity"
  - "Pattern: a canonical default-backfill (parent = seq-1, branch = all-zero sentinel) folds a pre-migration table into the new topology without changing any existing read"

requirements-completed: [CHAT-05]

# Metrics
duration: ~75min
completed: 2026-06-17
---

# Phase 25 Plan 06: D-09 Conversation Branch-Tree Backend Foundation Summary

**Migration 0017 adds additive parent_seq/branch_id pointers to aura.conversation_turns (defaulting existing rows into one canonical branch) plus a recursive-CTE leaf->root path walk (LoadBranchHistory / LoadManagedHistoryForBranch) that reconstructs a selected branch deterministically while keeping messages[0] byte-identical — the CAP-04 cache-invariant gate stays green.**

## Performance

- **Duration:** ~75 min
- **Started:** 2026-06-17 (sequential executor on master)
- **Completed:** 2026-06-17
- **Tasks:** 3 (1 docs + 2 TDD)
- **Files modified/created:** 16

## Accomplishments
- Recorded **CHAT-05** (conversation branch trees) in REQUIREMENTS.md + ROADMAP.md BEFORE any branch code (PRD-first); v1 total 50→51, CHAT 4→5, Phase-25 roll-up 7→8.
- Shipped **migration 0017** — tx-safe additive `branch_id`/`parent_seq` columns with a canonical default-backfill (`parent_seq = seq-1`, root NULL, `branch_id` = all-zero sentinel) so a pre-0017 conversation's `LoadHistory` is byte-identical. Up/down/up round-trip verified against the live DB.
- Added the sqlc path-walk surface: `CanonicalBranchLeafSeq`, `ListTurnsByBranchPath` (recursive leaf→root, returned root→leaf, column list mirrors `ListTurnsBySeq`), `SetTurnBranchPointers`.
- Implemented `store_branch.go` (LoadBranchHistory + CanonicalBranchLeaf + SetBranchPointers, reusing the unchanged turnFromRow/turnToMessage/repairToolMessagePairs) and `context.go` `LoadManagedHistoryForBranch` (feeds the selected path into the SAME L1/L2/L2.5 ladder via a shared `managedFromTurns` tail).
- **[BLOCKING] `scripts/cache_invariant_audit.sh` is GREEN** (22 identical messages[0] hashes; messages[1] + skill manifest also stable) — branching does not poison the OpenRouter prompt cache (T-25-21).

## Task Commits

1. **Task 1: PRD-first CHAT-05 amendment** — `7f908652` (docs)
2. **Task 2: Migration 0017 + sqlc path-walk queries + round-trip/backfill test** — `ffe511dc` (feat)
3. **Task 3: Path-aware branch walk + context extension + cache-invariant-safe loaders + tests** — `14c2c4ad` (feat)

_Task 2 also folded in the forced turnFromRow adaptation (see Deviations). Task 3 covered both the integration behavior tests and the unit error-path tests that lifted coverage over the floor._

## Files Created/Modified
- `internal/db/migrations/0017_conversation_turn_branches.up.sql` — additive `branch_id`/`parent_seq` + canonical default-backfill (tx-safe, no CONCURRENTLY)
- `internal/db/migrations/0017_conversation_turn_branches.down.sql` — DROP the two columns (IF EXISTS)
- `internal/db/queries/conversation_turns.sql` — `CanonicalBranchLeafSeq`, `ListTurnsByBranchPath` (recursive CTE), `SetTurnBranchPointers`
- `internal/db/sqlc/{conversation_turns.sql.go,models.go,querier.go}` — regenerated (model gained branch columns; `ListTurnsBySeq` now returns a query-specific row struct)
- `internal/db/migrate_0017_integration_test.go` — up/down/up round-trip + canonical default-backfill assertion (db_integration)
- `internal/conversations/store_branch.go` — branch path walk + write seam (139 LOC)
- `internal/conversations/context.go` — `LoadManagedHistoryForBranch` + shared `managedFromTurns` (LoadManagedHistory unchanged)
- `internal/conversations/store_helpers.go` — `turnFromRow` accepts the new `ListTurnsBySeqRow` type
- `internal/conversations/{store_branch_test.go,context_branch_test.go,store_branch_unit_test.go}` — integration + unit tests
- `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md` — CHAT-05 amendment

## Decisions Made
- **Branch topology:** `branch_id` (all-zero canonical sentinel) + `parent_seq` pointers supporting N siblings (RESEARCH OQ3 full tree). No index in 0017 — a single-operator pending branch set is tiny (RESEARCH A4) and an index would force a CONCURRENTLY split (Pitfall 5); the plain `ALTER ADD COLUMN DEFAULT` + row-local `UPDATE` backfill stays tx-safe (T-25-23).
- **Default path unchanged:** `LoadManagedHistory` (the Runner's entry point) still loads the full seq history, so non-branched conversations remain byte-identical to the pre-0017 linear case (the must-have regression guard). The path walk is the explicit-leaf `LoadManagedHistoryForBranch`. New-branch parent-chaining of the append path is plan 25-07's job (edit/re-run wiring); this plan ships the foundation (path-walk read + `SetBranchPointers` write seam).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] sqlc regen forced a turnFromRow type change**
- **Found during:** Task 2 (regenerating the sqlc client)
- **Issue:** Adding `branch_id`/`parent_seq` to the table model made sqlc emit a query-specific `ListTurnsBySeqRow` struct for `ListTurnsBySeq` (it no longer returns the table model `AuraConversationTurns`). `turnFromRow(sqlc.AuraConversationTurns)` then failed to compile in `loadTurns` (the pre-commit `vet` hook caught it).
- **Fix:** Changed `turnFromRow` to accept `sqlc.ListTurnsBySeqRow` (the type `loadTurns` now receives); added `branchPathRowAsSeqRow` to adapt the field-identical `ListTurnsByBranchPathRow` onto the same single projection. Projection logic is unchanged, so `LoadHistory` stays byte-identical (verified by the existing `TestLoadHistory_ByteIdenticalAfterRestart` + the new non-branched regression test).
- **Files modified:** internal/conversations/store_helpers.go, internal/conversations/store_fakedbtx_test.go (stale comment)
- **Verification:** `go build ./...` + `go vet ./...` clean; conversations integration tier (race+goleak) green; cache-invariant audit green
- **Committed in:** ffe511dc (Task 2 commit)

**2. [Rule 3 - Blocking] "extend migrate_test.go" — file does not exist**
- **Found during:** Task 2 (the plan said "extend migrate_test.go")
- **Issue:** There is no `internal/db/migrate_test.go`; migration round-trips live in `db_test.go` (`TestMigrate_Phase4_AppliesAndSeeds`) and `migrate_steps_integration_test.go` (`TestMigrateSteps_DownUpReversible`).
- **Fix:** Created a new `internal/db/migrate_0017_integration_test.go` (`TestMigrate0017_BranchPointersBackfillAndRoundTrip`) that does the up/down/up round-trip + the canonical default-backfill assertion against a throwaway DB, matching the existing migration-test pattern. It is matched by the plan's `-run TestMigrate` verify command.
- **Files modified:** internal/db/migrate_0017_integration_test.go (new)
- **Verification:** `go test -tags db_integration ./internal/db/ -run TestMigrate0017 -race` PASS (0.74s, real DB)
- **Committed in:** ffe511dc (Task 2 commit)

**3. [Rule 1 - Bug] sqlc "column reference ambiguous" in the recursive CTE**
- **Found during:** Task 2 (`sqlc generate`)
- **Issue:** sqlc's analyzer rejected unqualified column references in the recursive CTE's anchor member + final SELECT (`conversation_id is ambiguous`).
- **Fix:** Aliased the anchor member table (`ct`) and qualified the final SELECT with the `path` CTE alias.
- **Files modified:** internal/db/queries/conversation_turns.sql
- **Verification:** `sqlc generate` exit 0; the query runs correctly against the live DB (branch tests green)
- **Committed in:** ffe511dc (Task 2 commit)

**4. [Rule 2 - Missing critical coverage] added branch unit tests to clear the 85% floor**
- **Found during:** Task 3 (coverage measurement)
- **Issue:** The integration tests alone left the conversations package at 84.7% (just under the CLAUDE.md 85% floor) — the new code's error/parse/sidecar paths were uncovered.
- **Fix:** Added `store_branch_unit_test.go` (14 no-DB tests via the existing DBTX fake) covering the projection, error-classification, sidecar-rehydration, and the row adapter.
- **Files modified:** internal/conversations/store_branch_unit_test.go (new)
- **Verification:** conversations coverage 86.3% (full db_integration matrix); `store_branch.go` funcs all 100%
- **Committed in:** 14c2c4ad (Task 3 commit)

---

**Total deviations:** 4 auto-fixed (3 blocking, 1 missing-critical-coverage)
**Impact on plan:** All necessary for correctness and the coverage floor. No scope creep — the branch topology, default path, and gate behavior match the plan's must-haves exactly. Migration number used is **0017** (verified: latest shipped was 0016).

## Issues Encountered
- The `.env` loader emits harmless `export: '=' not a valid identifier` warnings for values containing `=`; those vars are not needed for the DB tests and `POSTGRES_PASSWORD` loaded correctly (verified by the passing live integration tier).

## Verification Evidence
- `go build ./...` clean; `go vet ./...` clean (both default + db_integration tags on conversations).
- `go test -tags db_integration ./internal/db/ -run TestMigrate -race` — PASS (3.88s, real DB; round-trip + backfill).
- `go test -tags db_integration ./internal/conversations/ -run TestBranch -race` — 6/6 PASS (deterministic walk, non-branched==linear for raw + managed, protected-head byte-identity across two sibling branches, tool pairing, empty-leaf).
- Full conversations integration tier (race + goleak) — PASS (6.7s).
- **[BLOCKING] `bash scripts/cache_invariant_audit.sh` — exit 0** (22 identical messages[0] hashes `0daddf93…`; messages[1] + skill manifest stable).
- Coverage: conversations **86.3%** (≥85% floor); `store_branch.go` all functions 100%.

## Mutation Testing
- Go mutation spot-check on `store_branch.go` (≥70% target, AC for Task 3) is deferred to the WSL `go-mutesting` run during phase validation (25-VALIDATION.md Manual-Only table), per the established phase-validation procedure. The new path loader has 100% statement coverage and explicit byte-identity + protected-head + error-path assertions, which is the input that drives a healthy mutation score.

## Next Phase Readiness
- The D-09 backend foundation is in place: plan **25-07** can build branch list/select + re-run + the assistant-ui BranchPicker on top of `LoadManagedHistoryForBranch` (path read) + `SetBranchPointers` (branch write seam) + `CanonicalBranchLeaf` (default selection).
- 25-07 must wire the append path to set `parent_seq`/`branch_id` for new branches (the current linear `AppendTurn` leaves them at the post-0017 defaults: canonical branch, NULL parent), and run the WSL mutation spot-check on `store_branch.go`.

## Self-Check: PASSED
- All 8 created files verified present on disk.
- All 3 task commits verified in git log (7f908652, ffe511dc, 14c2c4ad).

---
*Phase: 25-chat-approval-center*
*Completed: 2026-06-17*
