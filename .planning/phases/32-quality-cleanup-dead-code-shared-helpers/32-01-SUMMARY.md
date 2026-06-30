---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 01
subsystem: testing
tags: [dead-code, deadcode, assets, lifecycle, qual-02, triage]

# Dependency graph
requires:
  - phase: 31-stabilization
    provides: quality-audit findings (QA-C-09 → QUAL-02) flagging assets.Status constants
provides:
  - "Operator-signed-off keep/kill decision (D-04) on assets.Status{Created,Embedding,Canceled}"
  - "Deferred-lifecycle doc annotation on the internal/assets Status const block (all 12 constants retained)"
affects: [33+ asset-upload-pipeline wiring, future deadcode/audit runs]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deferred-lifecycle annotation: document designed-but-unwired exported symbols in-place so deadcode/audit runs treat them as known-deferred, not dead"

key-files:
  created:
    - .planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-01-SUMMARY.md
  modified:
    - internal/assets/types.go

key-decisions:
  - "D-04: keep-annotate — keep all 12 assets.Status constants, annotate the deferred lifecycle, no deletion, no wiring (operator sign-off 2026-06-30)"

patterns-established:
  - "Deferred-lifecycle annotation pattern for unwired-but-designed exported constants"

requirements-completed: [QUAL-02]

coverage:
  - id: D1
    description: "internal/assets/types.go documents the deferred multimodal-asset lifecycle; all 12 Status constants remain exported (keep-annotate branch of D-04)"
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "go build ./internal/assets/ && go vet ./internal/assets/ && go test ./internal/assets/ (WSL, exit 0)"
        status: pass
      - kind: other
        ref: "rg -n 'deferred|lifecycle' internal/assets/types.go (annotation present); 12 'Status*  Status = \"...\"' const lines retained"
        status: pass
    human_judgment: false

# Metrics
duration: ~12min
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 01: assets.Status QUAL-02 Triage Summary

**Operator decision D-04 = keep-annotate: all 12 `assets.Status` constants retained and annotated as a deferred multimodal-asset lifecycle, disambiguated from the wired `documents.JobStatus`; zero wiring added, build/vet/test green.**

## Performance

- **Duration:** ~12 min (continuation agent; Task 1 was decision-only with no prior commits)
- **Completed:** 2026-06-30T05:29:06Z
- **Tasks:** 1 of 1 executed in this continuation (Task 2; Task 1 was the resolved operator checkpoint)
- **Files modified:** 1 (`internal/assets/types.go`)

## Decision

**D-04: keep-annotate** (operator sign-off, 2026-06-30).

Keep ALL 12 `assets.Status` constants. Do NOT delete any constant. Do NOT wire the
state machine (D-02 guardrail: wiring = new feature behavior → out of scope).

## D-04 Evidence (gathered in Task 1)

- **`deadcode -test`** (WSL, 59 pkgs, exit 0): flagged 4 dead *functions*, NONE in
  `internal/assets`. Caveat — `deadcode` models **function reachability only, not `const`
  declarations**, so it is non-informative for these constants. The authoritative check for
  unused constants is repo-wide `rg`.
- **Repo-wide `rg 'StatusCreated|StatusEmbedding|StatusCanceled' --glob '!**/dist/**'`**
  (including `_test.go` + build-tagged files): **ZERO consumers** — the only hits in package
  `assets` are the declaration sites at `internal/assets/types.go:14,20,25` (now shifted to
  :26,:32,:37 after the annotation). Disambiguation of the other `rg` hits:
  - `internal/onboarding/*` → `onboarding.StatusCanceled` is a **different package's** constant
    and IS used (session.go:61, :150, :292; tests).
  - `internal/agui/*` → stdlib `http.StatusCreated`, unrelated.
  - `docs/audit/*`, `docs/superpowers/*` → documentation references only.
- **Assumption A1** — `rg '"created"|"embedding"|"canceled"'` (minus `_test.go`) + eyeball of
  `store.go`: no consumer reads the assets-`Status` literal values. `store.go` emits status only
  via symbolic constants — `StatusPresigned` (Create, store.go:41) and the `MarkUploaded`/
  `SetStatus`/`SetResult` paths take a caller-supplied `Status`. **A1 CONFIRMED.**

## Disambiguation (confirmed this session)

`internal/documents.JobStatus` is a separately-typed, **wired** document-ingest lifecycle
(`JobEmbedding = "embedding"`, `JobCanceled = "canceled"`, …) actively used in
`documents/worker.go`, `store.go`, `indexer.go`. It shares the string values
`"embedding"`/`"canceled"` with `assets.Status` but is a different type in a different package.
The annotation calls this out explicitly to prevent the confusion the operator flagged.

## Outcome

- All 12 `assets.Status` constants retained and annotated as a deferred lifecycle
  (`docs/superpowers/plans/2026-06-18-industrial-multimodal-asset-pipeline.md`).
- No status-machine wiring added (D-02 guardrail held).
- No siblings touched (D-03 no sweep).
- `go build ./internal/assets/ && go vet ./internal/assets/ && go test ./internal/assets/`
  exit 0 (WSL).

## Task Commits

1. **Task 2: annotate deferred assets.Status lifecycle** - see commit below (docs)

## Files Created/Modified

- `internal/assets/types.go` - Added a deferred-lifecycle doc comment above the `Status`
  `const (` block: documents the 12-state designed lifecycle, that production emits only
  `StatusPresigned`/`StatusUploaded`, that the remaining states are intentionally retained for
  the unbuilt asset-upload pipeline, and that `assets.Status` is DISTINCT from
  `documents.JobStatus`. All 12 constants retained.

## Decisions Made

- **D-04 keep-annotate** (operator). Chosen over delete-3-named to preserve designed work,
  avoid delete-and-re-add churn when phases 33+ wire the pipeline, and avoid leaving the
  lifecycle half-present (D-03 forbids sweeping the ~7 sibling constants).

## Deviations from Plan

None - plan executed exactly as written (keep-annotate branch of Task 2).

## Issues Encountered

None.

## Follow-up (out of scope for Phase 32)

- **Document delete + document update is a required NET-NEW feature that is currently MISSING**
  — there is no user-facing handler; only test-cleanup SQL exists. This must be specced as a
  dedicated phase after Phase 32. The asset-pipeline lifecycle was kept partly because that
  future feature may build on it.
- The ~7 other unused `assets.Status` siblings (accepted/processing/searchable/complete/failed/
  refused/deleted) remain intentionally untouched (D-03 no sweep) under the same deferred-lifecycle
  rationale, now documented inline.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- QUAL-02 (assets.Status) resolved by an evidence-backed, operator-signed-off decision rather
  than a blind deletion. types.go is build-green.
- Remaining Phase 32 plans (32-02 … 32-10) unaffected by this change.

## Self-Check: PASSED

- `internal/assets/types.go` — FOUND (annotation present, all 12 constants retained)
- `.planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-01-SUMMARY.md` — FOUND
- Task commit `9d655409` — FOUND in git log

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
