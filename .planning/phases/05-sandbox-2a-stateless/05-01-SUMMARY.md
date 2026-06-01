---
phase: 05-sandbox-2a-stateless
plan: 01
subsystem: infra
tags: [sandbox, gvisor, runsc, seccomp, docker, prd-amendment, doc-only]

# Dependency graph
requires:
  - phase: 04-conversation-persistence
    provides: conversation_id surface (forward-stable for 2b session_id)
provides:
  - "prd.md §Slice 2 D12 re-decided to gVisor-primary x86 (amendment #36)"
  - "prd.md Slice 2a acceptance #4 = sidecar down AND auto-start fails -> clear error (D-09)"
  - "prd.md docker.go scope = docker-CLI-gated socket-free one-shot auto-start, HTTP-only path preserved"
  - "prd.md §Slice 2 + ROADMAP Phase 5 goal = D-20 build-time curated package bake (amendment #37)"
  - ".planning/DECISIONS.md D12 row re-decided gVisor-primary; §5 follow-up corrected"
  - ".planning/ROADMAP.md Phase 5 SC#5 gVisor default-on x86 note"
affects: [05-02-sidecar-artifacts, 05-03-go-runner, 05-04-gate3-evidence]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PRD-amendment gate as a Wave-1 doc-only plan that every code wave depends_on (structural ordering of the truth-source ahead of code)"

key-files:
  created:
    - .planning/phases/05-sandbox-2a-stateless/05-01-SUMMARY.md
  modified:
    - prd.md
    - .planning/DECISIONS.md
    - .planning/ROADMAP.md

key-decisions:
  - "gVisor runsc is the PRIMARY x86 sandbox boundary (D-05/06/07), not a >5%-only escalation seam; hardened-container+seccomp+userns-remap is the portable floor / arm64 fallback; microVM stays REJECTED (KVM-less infra)"
  - "D-09: DockerRunner does ONE best-effort docker-CLI-gated auto-start on connect failure, NEVER mounts the docker socket; execution path stays HTTP-only"
  - "D-20: curated hash-pinned requirements.txt baked at IMAGE-BUILD time (batteries-included user code); runtime stays net-none + read_only + stateless with no runtime pip; sidecar.py server stays stdlib-only; on-demand pip remains a 2b/Phase-8 capability"

patterns-established:
  - "Amendment numbering: D12 re-decision = #36 (supersedes #32), D-20 bake = #37"

requirements-completed: [CAP-01]

# Metrics
duration: ~12min
completed: 2026-06-01
---

# Phase 5 Plan 01: PRD-Amendment Gate Summary

**Three truth-source amendments (gVisor-primary D12 #36, D-09 docker-CLI-gated socket-free auto-start, D-20 #37 build-time curated package bake) landed across prd.md + DECISIONS.md + ROADMAP.md, doc-only, gating every Phase-5 code wave.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-06-01T18:05:41Z (phase execution marker)
- **Completed:** 2026-06-01
- **Tasks:** 3 (Task 1 pre-existing from a prior run; Tasks 2 + 3 landed this run)
- **Files modified:** 3 (prd.md, .planning/DECISIONS.md, .planning/ROADMAP.md)

## Accomplishments
- D12 re-decided to gVisor-primary x86 consistently across prd.md, DECISIONS.md, and ROADMAP.md; microVM rejection intact in all three (Task 1, pre-existing commit `93e8c5a`)
- Slice 2a acceptance #4 rewritten to "sidecar down AND auto-start fails -> clear error"; docker.go scope note records the docker-CLI-gated, socket-free one-shot auto-start with the HTTP-only execution path preserved (Task 2)
- prd.md §Slice 2 + ROADMAP Phase 5 goal record the D-20 build-time curated package bake; the sidecar.py-server-stdlib-only property and the locked 2b on-demand-pip `pypi.org` boundary are both preserved (Task 3)

## Task Commits

Each task was committed atomically:

1. **Task 1: Amend D12 to gVisor-primary (D-05/06/07)** - `93e8c5a` (docs) — *pre-existing from a prior run; verified in place, not re-committed*
2. **Task 2: Amend Slice 2a acceptance #4 for the D-09 auto-start lifecycle** - `d924466` (docs)
3. **Task 3: Amend "stdlib only, no pip" for the D-20 build-time curated package bake** - `5d8c46e` (docs)

**Plan metadata:** see final docs commit (SUMMARY + STATE + ROADMAP progress)

## Files Created/Modified
- `prd.md` - §Slice 2 D12 paragraph (gVisor-primary, Task 1), Slice 2a acceptance #4 + docker.go scope note (D-09, Task 2), §Slice 2 goal D-20 build-time bake note (Task 3)
- `.planning/DECISIONS.md` - D12 row + §5 follow-up re-decided to gVisor-primary (Task 1)
- `.planning/ROADMAP.md` - Phase 5 SC#5 gVisor-default-on-x86 note (Task 1); Phase 5 goal D-20 build-time bake (Task 3)

## Decisions Made
- Task 1 was already committed (`93e8c5a`) by a prior execution attempt; its acceptance criteria all verified true in place, so it was NOT re-committed (idempotent resume).
- prd.md and DECISIONS.md are written in Italian (the document's native language). For Task 2, the English literal phrase "sidecar down AND auto-start fails -> clear error" required by the acceptance gate was added parenthetically alongside the Italian prose so the automated verify passes without breaking the document's language convention.
- Left the `sandbox/sidecar.py` file-target note "Niente deps Python extra" (line ~1264) UNCHANGED per the plan's D-20a distinction: the server itself is stdlib-only; only the user-code runtime gains baked libs.
- Left the locked 2b `pip install requests SUCCEEDS` integration-test line UNCHANGED per Task 3 acceptance — it is the explicit 2a/2b boundary.

## Deviations from Plan

None - plan executed exactly as written. (Task 1 was found already committed from a prior run and verified rather than re-applied; this is normal continuation behavior, not a deviation.)

## Issues Encountered
- The Task 2 verify gate (`grep -q "auto-start fails"`) and the Task 1/3 greps assume English literals, but the truth-source prose is Italian. Resolved by embedding the required English phrases parenthetically next to the Italian text so both the gate and the document convention are satisfied.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- All three PRD amendments are in the truth-source; no Phase-5 code wave (05-02/03/04) can now contradict locked content. They all `depends_on` this Wave-1 plan, so the ordering is structurally enforced.
- The three documents are mutually consistent on gVisor-primary; microVM rejection intact; the no-runtime-pip + net-none + read-only floor and the 2b on-demand-pip boundary are both preserved.
- Tracked obligation carried forward (from Task 1 / amendment #36): QEMU-arm64 syscall emulation in CI can diverge from a real arm64 kernel's seccomp behavior — real-DGX confirmation remains a pre-production arm64 obligation.

## Self-Check: PASSED

- `.planning/phases/05-sandbox-2a-stateless/05-01-SUMMARY.md` — present (this file)
- Commit `93e8c5a` (Task 1) — present in git log
- Commit `d924466` (Task 2) — present in git log
- Commit `5d8c46e` (Task 3) — present in git log
- prd.md / DECISIONS.md / ROADMAP.md all contain "gVisor"; prd.md contains "auto-start fails" + "MUST NEVER mount"; prd.md + ROADMAP contain the build-time bake + "pypi.org"; no code file touched

---
*Phase: 05-sandbox-2a-stateless*
*Completed: 2026-06-01*
