---
phase: 45-harness-correctness
plan: 01
subsystem: docs
tags: [prd-amendment, roadmap, claude-md, redact, harness-correctness]

# Dependency graph
requires: []
provides:
  - "ROADMAP.md §Phase 45 no longer promises a new ReplayPolicy value; RoundOrdinal is named as the discriminator"
  - "ROADMAP.md §Phase 46 dependency on Phase 45 narrowed to risk-override/hide-list work only"
  - "prd.md Amendment #121 recording both falsified-claim measurements and their limits"
  - "CLAUDE.md's arcadedb_integration tier claim corrected to the measured truth"
  - "internal/toolinvocations/redact.go comments no longer claim the 2 KiB ledger cap mirrors AURA_CONTEXT_PREVIEW_CAP_BYTES"
affects: [45-02, 45-03, 46]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PRD-first amendment: measure against a named commit, record what the measurement does not prove, before code lands"

key-files:
  created: []
  modified:
    - .planning/ROADMAP.md
    - prd.md
    - CLAUDE.md
    - internal/toolinvocations/redact.go

key-decisions:
  - "D-08 (BLOCKING) satisfied: both amendment commits (ROADMAP §45 + prd.md, then ROADMAP §46) landed before any code commit in Phase 45."
  - "prd.md amendment number computed via grep at write time (highest found was 120, not the planning-time estimate of 119), yielding Amendment #121 — confirms the plan's own 'if the grep disagrees, the grep wins' instruction."
  - "CLAUDE.md correction kept the real remaining concern (arcadedb_integration runs but feeds no coverage) and only deleted the falsified four-count claim; AURA_COVERAGE_TAGS default was NOT changed."
  - "redact.go's ResultPreviewCapBytes value (2 * 1024) was NOT changed — only the two comments claiming it mirrors AURA_CONTEXT_PREVIEW_CAP_BYTES were corrected."

patterns-established:
  - "PRD amendment heading convention: `## §<area> — <headline> (Amendment #NN, <date>)` followed by a `>` blockquote body naming what was measured, what changes, and what the measurement does not prove."

requirements-completed: [HARN-01, HARN-02]

# Metrics
duration: ~20min
completed: 2026-08-13
---

# Phase 45 Plan 01: BLOCKING amendments and fix-on-touch doc corrections Summary

**Withdrew ROADMAP's false `ReplayPolicy`-vocabulary claim (D-08), narrowed Phase 46's dependency line, and corrected two stale doc claims (CLAUDE.md's `arcadedb_integration` tier, `redact.go`'s ledger-cap comment) — three commits, no `.go` behavior change.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-08-13T13:17Z (approx, per STATE.md)
- **Completed:** 2026-08-13T13:26Z
- **Tasks:** 3
- **Files modified:** 4 (`.planning/ROADMAP.md`, `prd.md`, `CLAUDE.md`, `internal/toolinvocations/redact.go`)

## Accomplishments

- ROADMAP.md §Phase 45's `**Rationale**` no longer claims the phase introduces a new `ReplayPolicy` value (`ReplayReissueExecutes`); it now names `RoundOrdinal` in the child operation key as the discriminator and cites `internal/agent/model_round.go`, per commit `09f91a865`.
- prd.md gained Amendment #121, recording both measurements (the ROADMAP §45 claim and the ROADMAP §46 claim) and stating explicitly what neither measurement proves.
- ROADMAP.md §Phase 46's `**Depends on**` line no longer waits on a `ReplayPolicy` vocabulary; it cites `applyMCPOperationMetadata` (`internal/agent/mcptools/bridge.go:230-240`) and narrows the dependency to the risk-override/hide-list work.
- CLAUDE.md's `arcadedb_integration` claim ("not CI, not the Makefile, not the coverage scripts, not even `go vet`-compiled") is corrected: it runs in CI job `arcadedb-integration-test` via `make agent-memory-eval`, with `-race` and a coverage profile, and is `go test`-compiled — but that profile does not feed the `db_integration`-only 85% floor.
- `internal/toolinvocations/redact.go`'s two comments (lines 23 and 33-35 as measured at planning time) no longer claim the durable 2 KiB `ResultPreviewCapBytes` "mirrors" or "keeps the ceiling of" `AURA_CONTEXT_PREVIEW_CAP_BYTES`; they now name the real 30000-byte default and explain why the ledger cap is deliberately ~15x tighter.

## Task Commits

Each task was committed atomically:

1. **Task 1: Amend ROADMAP §45 and record the measurement in prd.md** - `8f4a466f4` (docs)
2. **Task 2: Amend ROADMAP §46's dependency line** - `5416b8d3e` (docs)
3. **Task 3: Fix-on-touch — stale arcadedb_integration claim and stale redact.go comment** - `3a984a153` (docs)

**Plan metadata:** committed at end of execution (this file + STATE/ROADMAP tracking bookkeeping owned by the orchestrator; only this SUMMARY is committed by this agent in worktree mode).

## Files Created/Modified

- `.planning/ROADMAP.md` - §Phase 45 Rationale corrected (drops the `ReplayPolicy` vocabulary claim, names `RoundOrdinal`); §Phase 46 Depends-on line narrowed to risk-override/hide-list scope
- `prd.md` - new `## §Harness correctness — the ROADMAP ReplayPolicy vocabulary claim is withdrawn (Amendment #121, 2026-08-13)` section
- `CLAUDE.md` - `arcadedb_integration` paragraph in §COVERAGE GATE TAG SET corrected to the measured truth (still notes it doesn't feed the floor)
- `internal/toolinvocations/redact.go` - two comments corrected (package doc comment at the caps-discipline paragraph, and the `ResultPreviewCapBytes` const comment); no value or logic changed

## Decisions Made

- prd.md amendment number: computed via `grep -o "[Aa]mendment #[0-9]*" prd.md | grep -o "[0-9]*" | sort -n | tail -1` at write time, which returned **120** (Amendment #120, 2026-08-12, `prd.md:4557`) — not the planning-time estimate of 119 (`prd.md:6270`, which is actually Amendment #119). Per the plan's own instruction ("if the grep disagrees, the grep wins"), the new amendment is **#121**.
- `applyMCPOperationMetadata` (`internal/agent/mcptools/bridge.go:230-240`) was re-verified before writing the ROADMAP §46 narrowing (per the task's own STOP-and-report instruction if it no longer held): confirmed unchanged — for every `Mutating` tool it assigns `OperationScope` (`:232`), `OperationNormalizer` (`:233`), `ReplayPolicy` (`:234`) as three unconditional lines within the `Mutating` branch (no per-tool/per-value branching among the three assignments themselves; the only gate is whether the tool is `Mutating` at all, which decides whether it gets operation metadata, not which value it gets).
- CLAUDE.md correction preserved the real remaining concern (the tier's coverage profile is produced but never aggregated into the 85% floor) rather than deleting the whole paragraph — the falsified part was specifically the "NOTHING runs it" / "not CI / not Makefile / not coverage scripts / not go-vet-compiled" claim.
- `redact.go`'s `ResultPreviewCapBytes` numeric value was deliberately left at `2 * 1024` per the plan's explicit prohibition — only the comment's false equivalence claim was corrected.

## Deviations from Plan

None - plan executed exactly as written. All acceptance criteria in the plan were verified via the exact grep/sed commands specified before each commit.

## Issues Encountered

None. `go vet ./internal/toolinvocations/` and `go build ./...` both passed clean after the Task 3 comment-only edits.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Both D-08 BLOCKING amendments are on disk and committed before any Phase 45 code commit — plan 45-02 (the `RoundOrdinal` tracer) is unblocked.
- `applyMCPOperationMetadata`'s confirmed line numbers (`bridge.go:230-240`, assignments at `:232-234`) are recorded here for Phase 46 planning to cite directly rather than re-deriving.
- The `redact.go` comment fix landed on top of the working tree's in-progress redact rewrite (branch `fix/unify-credential-redaction`) with no conflict — the file's current shape (77 LOC, `RedactForLedger`/`capUTF8`) was read fresh before editing, per CLAUDE.md's READ BEFORE EDIT rule.

---
*Phase: 45-harness-correctness*
*Completed: 2026-08-13*
