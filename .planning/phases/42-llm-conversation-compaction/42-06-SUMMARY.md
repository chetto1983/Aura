---
phase: 42-llm-conversation-compaction
plan: 06
subsystem: memory
tags: [go, postgres, durable-memory, privacy, consent, regional-isolation]
requires:
  - phase: 42-05
    provides: semantic-first compaction and immutable canonical/rebase contracts
provides:
  - typed idempotent durable-memory candidates separate from working summaries
  - disabled-by-default class-specific promotion and retrieval policy
  - transactional consent, deletion, expiry, forget-me, and supersession propagation
  - independent privacy/security review evidence and residual-risk gate
affects: [42-07, compaction-rollout, memory-retrieval, identity-deletion]
tech-stack:
  added: []
  patterns: [digest-only evidence minimization, hard-filter-before-ranking, transactional lifecycle propagation]
key-files:
  created: [internal/memory/compaction_candidates.go, internal/memory/compaction_policy.go, internal/db/migrations/0038_compaction_memory.up.sql, docs/security/compaction-memory-review.md]
  modified: [internal/memory/compaction_candidates_test.go]
key-decisions:
  - "Continuation summary prose has no durable-memory column; candidate evidence is validated ephemerally and persisted only as a digest."
  - "Promotion and retrieval require an approved independent review plus explicit per-class policy; default behavior is disabled."
  - "Tenant, identity, capability, purpose, region, and sensitivity filters run before relevance and recency ranking."
patterns-established:
  - "Privacy lifecycle mutations revoke candidates and promoted memories in one PostgreSQL transaction."
  - "Immutable minimized source links preserve provenance and deletion reachability without copying source prose."
requirements-completed: [IC-10, IC-13, IC-14]
coverage:
  - id: D1
    description: Typed minimized candidates deduplicate restore/rebuild emission and reject secret or excessive evidence
    requirement: IC-10
    verification:
      - kind: integration
        ref: "go test -race -tags=db_integration ./internal/memory -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: Promotion, retrieval, and lifecycle propagation fail closed across every privacy boundary
    requirement: IC-10
    verification:
      - kind: integration
        ref: "internal/memory/compaction_candidates_test.go#TestRetrievalGateDeniesCrossBoundaryBeforeRelevance"
        status: pass
      - kind: integration
        ref: "internal/memory/compaction_candidates_test.go#TestConsentWithdrawalDeletionForgetAndExpiryPropagate"
        status: pass
    human_judgment: false
  - id: D3
    description: Separate privacy/security disposition documents abuse controls, exact evidence, residual risks, and re-review triggers
    requirement: IC-14
    verification:
      - kind: manual_procedural
        ref: "docs/security/compaction-memory-review.md reviewer gate"
        status: pass
    human_judgment: true
    rationale: "Independent operational approval for each future memory class and deployment region remains a human governance decision."
duration: 18min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 06: Durable-Memory Privacy Lifecycle Summary

**Digest-minimized typed memory candidates with disabled-by-default promotion, hard authorization-before-ranking, transactional privacy propagation, and a separate security disposition**

## Performance

- **Duration:** 18 min
- **Started:** 2026-07-13T15:08:00+02:00
- **Completed:** 2026-07-13T15:25:56+02:00
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Added rollback-compatible migration 0038 and a typed candidate store whose uniqueness survives restore/rebuild, whose immutable sources contain no prose, and whose evidence persists only as SHA-256.
- Added explicit review-plus-class promotion and retrieval gates with tenant, identity, capability, purpose, region, sensitivity, expiry, supersession, and revocation checks before ranking.
- Added transactional propagation for source deletion, consent withdrawal, expiry, forget-me, and supersession across candidates and promoted memories.
- Completed a separate privacy/security review covering threat boundaries, abuse cases, exact tests, residual risk, and mandatory re-review triggers.

## Task Commits

1. **Task 1: Persist typed candidates with additive privacy lifecycle** - `54ae3bc0b`
2. **Task 2: Enforce retrieval, promotion, deletion, consent, and regional gates** - `63830851c`

TDD RED was observed before each implementation. The first RED lacked candidate/store symbols; the second lacked policy/principal/promotion/retrieval/lifecycle symbols. Normal hooks require compiling commits, so each task landed as one atomic GREEN feature commit.

## Files Created/Modified

- `internal/memory/compaction_candidates.go` - Typed minimized candidate validation and idempotent persistence.
- `internal/memory/compaction_policy.go` - Promotion/retrieval hard gates and transactional lifecycle propagation.
- `internal/memory/compaction_candidates_test.go` - Live PostgreSQL privacy, migration, idempotency, and lifecycle proofs.
- `internal/db/migrations/0038_compaction_memory.up.sql` - Candidate, immutable source, promoted-memory, uniqueness, lifecycle, and index schema.
- `internal/db/migrations/0038_compaction_memory.down.sql` - Rollback of only migration 0038 objects.
- `docs/security/compaction-memory-review.md` - Independent disabled-by-default deployment disposition.

## Decisions Made

- Durable memory stores typed metadata and evidence digests, never continuation-summary prose or copied transcript evidence.
- Promotion and retrieval are separate policy decisions and remain disabled until the review bit and exact class allowlist are both present.
- Region is enforced as a retrieval policy label here; infrastructure residency remains an explicit residual-risk gate before regional enablement.

## Deviations from Plan

None - implementation remained within the specified memory, migration, test, and security-review surfaces.

## Issues Encountered

- Gate retry: the first WSL race command's regex was misquoted into shell pipelines; the malformed run was rejected and the full package suite reran.
- Gate retry: a subsequent WSL invocation returned exit 0 with no test output in 0.5 seconds; it was treated as false green and rerun via `wsl.exe -e` with an explicit completion marker.
- Gate retry: both commit tool calls timed out while normal hooks were starting. Task 1's original PID remained active and was monitored to completion without duplication; Task 2's first process had exited without committing, so one normal hook-enabled retry was run.
- Repository hooks took about 149 seconds; no hook was bypassed. Unrelated graph dirt remained unstaged.

## User Setup Required

None.

## Next Phase Readiness

- Plan 42-07 can expose operator surfaces without conflating working checkpoints and durable memory.
- Production promotion remains disabled; every future memory class, region, replica, or export requires the documented reviewer gate.

## Self-Check: PASSED

- Task commits `54ae3bc0b` and `63830851c` exist.
- Fresh WSL/CGO `go test -race -tags=db_integration ./internal/memory -count=1` passed in 1.018s with an explicit completion marker.
- `go vet ./...`, `go build ./...`, normal pre-commit hooks, lint (0 issues), and file-size gates passed.
- Unrelated `.planning/graphs/` changes remain unstaged.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
