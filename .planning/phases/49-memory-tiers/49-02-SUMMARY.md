---
phase: 49-memory-tiers
plan: 02
subsystem: memory
tags: [postgresql, arcadedb, conversation-projection, reconciliation, hybrid-search, tdd]

requires:
  - phase: 49-01
    provides: "Amendment #201 PostgreSQL-authority and derived-memory contract"
provides:
  - "PostgreSQL-authoritative eligible conversation-turn replay with bounded sidecar hydration"
  - "Identity-scoped ArcadeDB Conversation/ConversationTurn projection and hybrid search"
  - "Ordered fail-soft projection queue with retry, reconciliation, edit, and deletion convergence"
affects: [49-03, 49-07, 49-08, 49-13, 49-14]

actuals:
  tokens: 15327
  tasks: 2
  commits: 6

tech-stack:
  added: []
  patterns:
    - "PostgreSQL authority with idempotent, source-keyed ArcadeDB projection"
    - "Cursor advances only after a complete derived page succeeds"
    - "Full replay plus graph pruning repairs crash windows without an outbox"

key-files:
  created:
    - internal/conversations/store_projection.go
    - internal/conversations/store_projection_test.go
    - internal/arcadedb/memory_conversation.go
    - internal/arcadedb/memory_conversation_test.go
    - internal/arcadedb/memory_conversation_live_test.go
    - internal/runner/runner_memory_projection.go
    - internal/runner/runner_memory_projection_test.go
  modified:
    - internal/runner/runner_delete_reconcile.go

key-decisions:
  - "Kept PostgreSQL as the only conversation authority: the graph stores stable source refs, hashes, and derived high-water metadata only."
  - "Used a bounded in-process ordered queue plus full source replay/pruning; no outbox or independent retention store was added."
  - "Kept conversationSchemaStatements domain-owned for Plan 49-07 to aggregate after the Wave-2 memory.go owner lands."
  - "Used regular unique HAS_TURN/NEXT_TURN edges because live ArcadeDB 26.8.1 rejects IF NOT EXISTS after LIGHTWEIGHT UNIQUE."

patterns-established:
  - "Projection eligibility is structural: only non-empty user/final-assistant content with no tool payload crosses the PostgreSQL-to-graph boundary."
  - "Deletion is idempotent and source-authoritative: soft tombstones delete directly, while full replay prunes hard-deleted conversations."

requirements-completed: [MEM-01]

coverage:
  - id: D1
    description: "Eligible PostgreSQL turns replay in stable source order without reasoning, tool, system, or compaction content."
    requirement: MEM-01
    verification:
      - kind: unit
        ref: "internal/conversations/store_projection_test.go#TestProjectionTurnEligibility"
        status: pass
      - kind: integration
        ref: "internal/runner/runner_memory_projection_test.go#TestConversationProjectionTracer"
        status: pass
    human_judgment: false
  - id: D2
    description: "Conversation and turn vertices are idempotent, identity-scoped, directly sourced, and hybrid-searchable."
    requirement: MEM-01
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_conversation_test.go#TestConversationProjectionIsIdempotentAndIdentityScoped"
        status: pass
      - kind: unit
        ref: "internal/arcadedb/memory_conversation_test.go#TestConversationProjectionSearchFailsClosedAcrossIdentity"
        status: pass
      - kind: integration
        ref: "internal/arcadedb/memory_conversation_live_test.go#TestConversationProjectionLive_RestartGapAndReplay"
        status: pass
    human_judgment: false
  - id: D3
    description: "Ordered retry, restart reconciliation, deterministic edits, and repeated conversation/identity deletes converge from PostgreSQL authority."
    requirement: MEM-01
    verification:
      - kind: unit
        ref: "internal/runner/runner_memory_projection_test.go#TestConversationProjectionReconcileRepairsRestartGapAndEdit"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_memory_projection_test.go#TestConversationProjectionQueueRetriesAndFlushesInOrder"
        status: pass
      - kind: integration
        ref: "internal/arcadedb/memory_conversation_live_test.go#TestConversationProjectionLive_DeleteConvergesAndIsIdentityScoped"
        status: pass
    human_judgment: false

duration: 1h 24m
completed: 2026-08-31
status: complete
---

# Phase 49 Plan 02: PostgreSQL-Authoritative Conversation Projection Summary

**Eligible conversation turns now flow from PostgreSQL into an identity-isolated, hybrid-searchable ArcadeDB projection that repairs lag, edits, and deletions without becoming authoritative.**

## Performance

- **Duration:** 1h 24m
- **Started:** 2026-08-31T18:00:31Z
- **Completed:** 2026-08-31T19:25:26Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- Added an RLS-scoped, cursor-paged PostgreSQL feed that selects only user/final-assistant natural language, rehydrates sidecars under an 8 MiB bound, redacts at the projection boundary, and emits stable source references plus SHA-256 content hashes.
- Added replay-safe Conversation/ConversationTurn schema fragments, idempotent upserts, unique ordered relations, lexical/vector hybrid search, defensive identity filtering, and direct PostgreSQL provenance.
- Added a bounded ordered worker with retry, fail-soft offers, cursor-safe flush/close, full restart replay, soft tombstones, hard-delete pruning, edit replacement, and reserved-delete reconciliation integration.

## Task Commits

1. **Task 1 RED: Projection eligibility, schema, identity, and tracer contracts** — `f6512b2a6`, `54c658892` (`test`)
2. **Task 1 GREEN: PostgreSQL feed through ArcadeDB search** — `1b6cac6b3` (`feat`)
3. **Task 2 RED: Restart/edit/delete and queue lifecycle contracts** — `80a141ac6` (`test`)
4. **Task 2 GREEN: Ordered reconciliation and deletion convergence** — `285cb65d0` (`feat`)

`b14f03382` is not counted as a Plan 49-02 commit. It is the concurrent AG-UI session's commit, but it also captured the then-unstaged formatting-safe `runner_memory_projection_test.go` RED additions after `80a141ac6`; the shared-tree chronology is documented below.

## Files Created/Modified

- `internal/conversations/store_projection.go` — stable eligible-turn pages and soft-deletion tombstones from PostgreSQL authority.
- `internal/conversations/store_projection_test.go` — role/tool/blank-content eligibility boundary.
- `internal/arcadedb/memory_conversation.go` — graph schema, idempotent upsert/search, stale-vector clearing, and conversation/identity deletion.
- `internal/arcadedb/memory_conversation_test.go` — replay safety, provenance, identity mismatch, and fail-closed search contracts.
- `internal/arcadedb/memory_conversation_live_test.go` — real ArcadeDB restart/replay, edit, repeated-delete, and foreign-identity lifecycle.
- `internal/runner/runner_memory_projection.go` — ordered queue, retry, flush, close, full replay, tombstones, and prune lifecycle.
- `internal/runner/runner_memory_projection_test.go` — real HTTP tracer plus restart/edit/order/delete convergence tests under package goleak.
- `internal/runner/runner_delete_reconcile.go` — optional projection cleanup joined to durable reserved-delete recovery.

## Decisions Made

- PostgreSQL content and deletion state always win. ArcadeDB keeps no independent retention clock and can be rebuilt from a zero cursor.
- Queue saturation is fail-soft for the completed turn: `OfferConversation` returns immediately, while boot/periodic reconciliation repairs anything missed.
- The cursor updates only after every projection in a page succeeds, so failure replays the page idempotently instead of skipping a partial tail.
- A no-embedder edit removes any previous embedding before continuing lexically; otherwise old semantic content could survive after the source changed.
- Plan 49-07 remains the sole owner that aggregates `conversationSchemaStatements` into `EnsureMemorySchema`, avoiding Wave-2 overlap with `memory.go`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected replay-safe edge DDL against live ArcadeDB 26.8.1**
- **Found during:** Task 2 RED live test
- **Issue:** `CREATE EDGE TYPE ... LIGHTWEIGHT UNIQUE IF NOT EXISTS` passed unit string checks but the deployed parser rejected `IF` after `UNIQUE`.
- **Fix:** Used regular edge types plus replay-safe unique `(@out,@in)` indexes; `CREATE EDGE ... IF NOT EXISTS` remains the idempotent write primitive.
- **Files modified:** `internal/arcadedb/memory_conversation.go`
- **Verification:** All three `TestConversationProjectionLive_*` tests create the schema in fresh disposable databases and pass under `-race`.
- **Committed in:** `80a141ac6`

**2. [Rule 3 - Shared-worktree blocker] Preserved concurrent ownership after a pre-staged-index contamination**
- **Found during:** Task 2 RED commit
- **Issue:** Another session had already staged `cmd/aura/serve.go`, `cmd/aura/serve_bootstrap_resources.go`, and `cmd/aura/serve_bootstrap_resources_test.go`. The hook completed before the interrupt arrived, so `80a141ac6` contains those three paths in addition to the Plan 49-02 RED files.
- **Fix:** A non-rewriting corrective sequence was started, but before it could commit the concurrent owner landed `b14f03382` on top of `80a141ac6`, intentionally committing the full AG-UI change and also capturing the then-unstaged runner RED test. Reverting or amending either shared commit at that point would have partially destroyed a valid concurrent feature. Execution continued from `b14f03382`; every later commit used cached-index inspection, explicit unstaging, and `git commit --only` allowlists.
- **Files affected:** the three `cmd/aura` paths above and `internal/runner/runner_memory_projection_test.go`
- **Verification:** `b14f03382` owns the complete concurrent AG-UI change; Plan 49-02 production GREEN `285cb65d0` contains exactly its five declared files; all combined verification passed from the resulting HEAD.
- **Committed in:** contamination `80a141ac6`; ownership transfer `b14f03382`

---

**Total deviations:** 2 auto-fixed (1 live database bug, 1 shared-worktree commit blocker).
**Impact on plan:** Runtime scope and MEM-01 behavior are complete. History transparently records one contaminated RED commit; no concurrent feature was reverted or rewritten.

## Issues Encountered

- Two GREEN commit attempts met another session's active `golangci-lint` lock. Both stopped before commit; the third normal hook run passed. No hook was bypassed.
- The shared index, not the working-tree allowlist, caused the Task 2 RED contamination. Subsequent commits inspected both `git diff --cached --name-only` and `git status --short`, unstaged foreign paths, and used `git commit --only`.

## TDD Gate Compliance

- **Task 1 RED:** `f6512b2a6` and `54c658892` failed on the intended eligibility/schema/projector/search stubs.
- **Task 1 GREEN:** `1b6cac6b3` passed the named source-to-projector-to-real-ArcadeDB-client-to-search tracer, then the required post-commit tracer rerun.
- **Task 2 RED:** `80a141ac6` failed on the intended unimplemented reconciliation/deletion boundaries after the live DDL bug was fixed; the runner RED test body was captured by concurrent commit `b14f03382` as described above.
- **Task 2 GREEN:** `285cb65d0` passed restart/edit/order/delete convergence, the live lifecycle suite, full package regression, and WSL-native race.
- **REFACTOR:** No separate behavior-neutral commit was needed; all touched production files remain below 600 lines and lint is clean.

## Verification Evidence

- Amendment gate: `f231f15b5f08d0b7434b31c533b5e8021e383e14` is an ancestor and changes only `prd.md`.
- Named tracer: `TestProjectionTurnEligibility`, `TestConversationSchemaStatements`, and `TestConversationProjectionTracer` all pass.
- Live lifecycle: all three `TestConversationProjectionLive_*` cases pass under `-race` against disposable ArcadeDB 26.8.1 databases.
- Repository checks: `go vet ./...`, `go build ./...`, package unit regression, and WSL-native package race all pass.
- No named test reported "no tests to run"; no integration test skipped in the final tagged command.

## Known Stubs

None.

## User Setup Required

None - no new environment variables, services, packages, or migrations were added.

## Next Phase Readiness

- Plan 49-03 can consume projected conversation evidence for unified recall behavior.
- Plan 49-07 must aggregate `conversationSchemaStatements` into `EnsureMemorySchema` and wire the projector at the composition root after Wave-2 ownership clears.
- Later phase verification should run the full published `memory_recall` E2E; this plan proves the storage/projector slice, not final mixed-tier retrieval quality.

## Self-Check: PASSED

- All eight declared implementation/test files and this SUMMARY exist.
- Task commits `f6512b2a6`, `54c658892`, `1b6cac6b3`, `80a141ac6`, and `285cb65d0` exist; concurrent ownership commit `b14f03382` also exists.
- No tracked file deletion or stub marker is present in the plan-owned files.
- Coverage classification reports 3/3 deliverables auto-covered with passing evidence.

---
*Phase: 49-memory-tiers*
*Completed: 2026-08-31*
