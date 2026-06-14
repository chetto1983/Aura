---
phase: 17-packaging-distribution
plan: 01
subsystem: docs
tags: [prd, spec, packaging, gvisor, docker]
requires: []
provides:
  - PRD amendment #63 for Phase 17 box-model settlement
  - SPEC Req 11b optional gVisor appliance tier
  - Audit-jail revert and sbx deferral documentation
affects: [phase-17-packaging, docker-aura, compose-topology, installer]
tech-stack:
  added: []
  patterns:
    - PRD-first amendment gate
    - Transparent container isolation via optional gVisor override
key-files:
  created:
    - .planning/phases/17-packaging-distribution/17-01-SUMMARY.md
  modified:
    - prd.md
    - .planning/phases/17-packaging-distribution/17-SPEC.md
key-decisions:
  - Amendment #63 is the Phase 17 pre-code PRD-first gate.
  - gVisor is optional transparent host isolation, not capability stripping.
  - The ec7fe2f6 audit jail is explicitly reverted; Docker Sandboxes is deferred.
requirements-completed: [OPS-01]
metrics:
  duration: ~6 min
  tasks: 2
  files-modified: 2
  completed: 2026-06-14
---

# Phase 17 Plan 01: Packaging Box-Model Amendment Summary

PRD amendment #63 and SPEC Req 11b settle Phase 17 packaging isolation before code.

## Performance

- Started: 2026-06-14T12:17:18Z
- Completed: 2026-06-14T12:22:55Z
- Duration: ~6 min
- Tasks completed: 2
- Files modified: 2

## Accomplishments

- Added PRD amendment #63 documenting the optional `compose.gvisor.yaml` / `runtime: runsc` appliance tier, the `ec7fe2f6` audit-jail revert, Docker Sandboxes (`sbx`) deferral, and the future docker-in-box-without-host-socket direction.
- Updated the Phase 17 SPEC with Req 11b, corrected Background wording, replaced the stale in-scope hardening line, and added the dated box-model settlement amendment note.
- Verified the change remained docs-only and did not add Dockerfile or compose code blocks to `prd.md`.

## Task Commits

| Task | Commit | Summary |
| --- | --- | --- |
| 1 | c60c33c5 | Added packaging box-model PRD amendment #63. |
| 2 | 0fd03cd7 | Recorded the packaging box-model settlement in the Phase 17 SPEC. |

## Files

- Created: `.planning/phases/17-packaging-distribution/17-01-SUMMARY.md`
- Modified: `prd.md`
- Modified: `.planning/phases/17-packaging-distribution/17-SPEC.md`

## Decisions Made

- Phase 17 code execution remains gated by the PRD-first amendment record.
- gVisor is an optional transparent host-isolation tier; Aura keeps full parity inside the container.
- The audit jail from `ec7fe2f6` is reverted as incompatible with the locked SPEC box model.
- Docker Sandboxes (`sbx`) remains evaluated-and-deferred for this phase.

## Deviations

None.

## Issues Encountered

None.

## User Setup Required

None.

## Next Phase Readiness

Wave 2 can begin with plans `17-02`, `17-03`, `17-04`, and `17-05`.

## Verification Evidence

- `rg "compose\.gvisor\.yaml|ec7fe2f6|gVisor|runsc" prd.md .planning/phases/17-packaging-distribution/17-SPEC.md` found the required decision terms.
- `FROM_COUNT=0`, so no Dockerfile block was added to `prd.md`.
- `AC_CHECKBOX_COUNT=16`, so SPEC acceptance checklist structure stayed stable.
- `git diff --name-only HEAD~2..HEAD` showed only `prd.md` and `.planning/phases/17-packaging-distribution/17-SPEC.md`.

## Self-Check: PASSED
