---
phase: 49-memory-tiers
plan: 06
subsystem: memory
tags: [arcadedb, transactions, idempotency, optimistic-retry, tdd]

requires:
  - phase: 49-01
    provides: "Amendment #201 memory governance, identity isolation, and final-state batch contract"
provides:
  - "Typed four-operation final-state memory batch compiler over isolated graph snapshots"
  - "Identity-bound durable idempotency receipts committed atomically with graph mutations"
  - "Whole-decision conflict and ambiguous-outcome retry with no partial visibility"
affects: [49-11, HARN-05, memory-mutations]

actuals:
  tokens: 17065
  tasks: 2
  commits: 6

tech-stack:
  added: []
  patterns:
    - "Load committed state, clone, apply ordered operations, validate final state, persist one diff, store receipt, commit once"
    - "Retry the complete decision from newly committed state after optimistic conflict or ambiguous commit outcome"

key-files:
  created:
    - internal/arcadedb/memory_batch.go
    - internal/arcadedb/memory_batch_state.go
    - internal/arcadedb/memory_batch_store.go
    - internal/arcadedb/memory_batch_test.go
    - internal/arcadedb/memory_batch_operations_test.go
    - internal/arcadedb/memory_batch_live_test.go
  modified:
    - internal/arcadedb/memory.go
    - internal/arcadedb/merge.go
    - internal/arcadedb/forget.go

key-decisions:
  - "Kept authenticated identity and actor outside MemoryBatchRequest; the host passes MemoryBatchActor separately so model input cannot spoof either field."
  - "Stored the canonical request hash and bounded result in MemoryBatchReceipt inside the same ArcadeDB transaction as the graph diff."
  - "Performed no embedding or telemetry side effect inside the retry loop; new or re-pointed facts stay lexically reachable and use the existing backfill path."
  - "Serialized same-identity batches in-process and retained ArcadeDB conflict detection/retry for cross-process contention."

patterns-established:
  - "Memory batch retries restart begin -> receipt lookup -> state load -> compile -> final validation -> persist; a suffix is never retried."
  - "Destructive batch variants require the parent actor; workers may only add or enrich facts."

requirements-completed: [HARN-05]

coverage:
  - id: D1
    description: "Ordered heterogeneous operations validate only the final working state and become visible in one commit."
    requirement: HARN-05
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_batch_test.go#TestMemoryBatch_FinalStateTracer"
        status: pass
      - kind: integration
        ref: "internal/arcadedb/memory_batch_live_test.go#TestMemoryBatchLive_AtomicRollbackAndReplay"
        status: pass
    human_judgment: false
  - id: D2
    description: "Malformed, missing, ambiguous, unauthorized, and invalid-final requests preserve the deterministic first error and leave live state unchanged."
    requirement: HARN-05
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_batch_test.go#TestMemoryBatch_RollbackFirstError and internal/arcadedb/memory_batch_operations_test.go"
        status: pass
      - kind: integration
        ref: "internal/arcadedb/memory_batch_live_test.go#TestMemoryBatchLive_AtomicRollbackAndReplay"
        status: pass
    human_judgment: false
  - id: D3
    description: "Conflict and ambiguous-outcome retries converge from fresh committed state without partial visibility or duplicate effects."
    requirement: HARN-05
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_batch_test.go#TestMemoryBatch_ConflictRetry, TestMemoryBatch_IdempotentReplay, and TestMemoryBatch_NoPartialObserver"
        status: pass
      - kind: integration
        ref: "internal/arcadedb/memory_batch_live_test.go#TestMemoryBatchLive_ConcurrentBatchesConverge under WSL -race"
        status: pass
    human_judgment: false

duration: 56min
completed: 2026-08-31
status: complete
---

# Phase 49 Plan 06: Final-State Atomic Memory Batches Summary

**Four typed memory mutations now compile against an isolated ArcadeDB snapshot, commit with an identity-bound replay receipt, and restart the whole decision after conflicts or ambiguous outcomes.**

## Performance

- **Duration:** 56 min
- **Started:** 2026-08-31T19:35:29Z
- **Completed:** 2026-08-31T20:31:36Z
- **Tasks:** 2
- **Files modified:** 9

## Accomplishments

- Added `upsert_fact`, `supersede_fact`, `merge_entities`, and `forget` as a typed internal batch union with operation-count, payload, authority, and idempotency validation before transaction I/O.
- Added a pure final-state compiler that resolves targets against a cloned snapshot, tolerates correction-shaped intermediate absence, validates the final graph once, and persists only the resulting diff.
- Added a durable `MemoryBatchReceipt` vertex whose identity, key, canonical request hash, and final result commit in the same transaction as the mutation.
- Added bounded whole-decision retry for ArcadeDB 409/503 conflicts and lost commit responses; retry reloads committed state and never resumes an operation suffix.
- Proved rollback, replay, observer isolation, all four operation variants, and eight-writer convergence with daemon-free race tests and disposable live ArcadeDB 26.8.1 databases.

## Task Commits

1. **Task 1 RED: Final-state, rollback, and replay tracer** — `570513bd6` (`test`)
2. **Task 1 GREEN: Atomic compiler, snapshot engine, persistence adapter, and receipt schema** — `96ec1fc23` (`feat`)
3. **Task 2 RED: Conflict, ambiguous-outcome, late-failure, identity, and observer contracts** — `c1c587178` (`test`)
4. **Task 2 GREEN: Whole-decision retry and live ArcadeDB proof** — `40bd9a237` (`feat`)
5. **Task 2 coverage: Exact supersede, ambiguity, merge collision, and malformed union contracts** — `a5d8df3c1` (`test`)

## Files Created/Modified

- `internal/arcadedb/memory_batch.go` — public internal request/result/error contract, canonical compiler, host authority, identity lock, receipt key, and retry orchestration.
- `internal/arcadedb/memory_batch_state.go` — isolated working graph, four operation implementations, target resolution, merge deduplication, and final-state validation.
- `internal/arcadedb/memory_batch_store.go` — ArcadeDB session adapter, state/receipt loading, minimal graph diff persistence, and native DATETIME decoding.
- `internal/arcadedb/memory_batch_test.go` — final-state tracer, rollback, replay, conflict, ambiguous outcome, late failure, cross-identity, and no-partial-observer tests.
- `internal/arcadedb/memory_batch_operations_test.go` — exact/ambiguous supersede, merge collision/provenance, and malformed tagged-union tests.
- `internal/arcadedb/memory_batch_live_test.go` — disposable real-ArcadeDB rollback/replay and concurrent convergence proof.
- `internal/arcadedb/memory.go` — aggregates the replay-safe receipt schema during memory bootstrap.
- `internal/arcadedb/merge.go` — shares canonical merge-name validation with the batch compiler.
- `internal/arcadedb/forget.go` — shares canonical filter normalization and matching semantics with the working-state compiler.

## Decisions Made

- `MemoryBatchRequest` contains no identity or actor fields. `ApplyMemoryBatch` accepts a separate host-derived `MemoryBatchActor`; facts must match its role, and only `parent` may supersede, merge, or forget.
- Idempotency is durable, not process-local. The receipt key hashes authenticated identity plus idempotency key, while the stored request hash detects same-key/different-request misuse.
- A lost commit response is treated as ambiguous and retried. If the first commit landed, the next transaction reads its receipt and returns the committed result as replayed; if it did not, the full decision is recomputed.
- Embeddings are never called inside the retrying transaction. Existing unchanged vectors stay intact; merge-repointed and new facts remain lexical and are eligible for the existing embedding backfill.
- Plan 49-11 remains the owner of `cmd/arcadedb-mcp/tool_memory_batch.go`, public JSON schema, host-header wiring, and model-facing risk classification, exactly as its dependency on 49-06 specifies.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Live database bug] Accepted ArcadeDB's documented native DATETIME rendering**
- **Found during:** Task 2 live rollback/replay test
- **Issue:** ArcadeDB 26.8.1 returned stored `DATETIME` values as `yyyy-MM-dd HH:mm:ss`; the first decoder accepted only RFC3339 and aborted a second batch while loading committed state.
- **Fix:** Kept RFC3339 support and added the documented native format, restoring the UTC zone Aura writes and citing the official ArcadeDB date-format documentation in the code.
- **Files modified:** `internal/arcadedb/memory_batch_store.go`, `internal/arcadedb/memory_batch_test.go`
- **Verification:** Native-format unit regression plus both disposable live tests under WSL-native `-race`.
- **Committed in:** `40bd9a237`

**2. [Rule 2 - Required maintainability and coverage] Split the engine and froze all four variants**
- **Found during:** Task 1 implementation and Task 2 close-out review
- **Issue:** Keeping compiler, working state, and HTTP transaction persistence in one planned file would exceed the hard 600-LOC project cap; the initial named tracer also left merge and supersede paths dark.
- **Fix:** Split state and store concerns into dedicated files under 600 lines and added exact/ambiguous supersede, merge collision/provenance, and malformed-union tests.
- **Files modified:** `internal/arcadedb/memory_batch_state.go`, `internal/arcadedb/memory_batch_store.go`, `internal/arcadedb/memory_batch_operations_test.go`
- **Verification:** Pre-commit file-size/lint/vet hooks, full package unit/race suite, and operation-specific tests all pass.
- **Committed in:** `96ec1fc23`, `a5d8df3c1`

---

**Total deviations:** 2 auto-fixed (1 live database bug, 1 required maintainability/coverage completion).
**Impact on plan:** Both fixes enforce the planned correctness and project quality contracts. The public MCP adapter remains correctly deferred to Plan 49-11.

## Issues Encountered

- The first Task 1 RED commit attempt was rejected by the normal pre-commit lint hook for modernize/exported-comment/unused-field findings in the compiling stub. The stub was corrected and committed with hooks enabled; no hook was bypassed.
- Live ArcadeDB exposed its native DATETIME response format, described above. The failed live transaction rolled back, and the same disposable tests passed after the decoder correction.
- `state.update-progress` declined the out-of-order Plan 49-06 close because the current-position slot correctly remains on the next incomplete Plan 49-03. The derived milestone count and activity fields were reconciled to the on-disk 51/62 summary count without advancing the plan pointer.

## TDD Gate Compliance

- **Task 1 RED:** `570513bd6` compiled but the three named tracers failed only on the intentional not-implemented batch engine.
- **Task 1 GREEN:** `96ec1fc23` passed final-state replacement, deterministic rollback, durable replay, repository vet/build, package regression, and WSL-native race; the tracer feedback gate passed again after commit.
- **Task 2 RED:** `c1c587178` failed on the intended commit-conflict and lost-response cases while late rollback, identity fencing, and observer isolation already held.
- **Task 2 GREEN:** `40bd9a237` passed whole-decision retry, ambiguous-outcome reconciliation, live rollback/replay, and eight-writer live convergence under `-race`.
- **Additional coverage:** `a5d8df3c1` freezes every operation variant without changing production behavior.

## Verification Evidence

- Named inventory: all seven required `TestMemoryBatch_(FinalStateTracer|RollbackFirstError|IdempotentReplay|ConflictRetry|LateRollback|CrossIdentity|NoPartialObserver)` tests were discovered and passed.
- Repository: `go vet ./...` and `go build ./...` passed from the final code commits.
- Package regression: `go test ./internal/arcadedb -count=1` passed.
- Race: WSL Go 1.26.6 `go test -race ./internal/arcadedb -count=1` passed.
- Live database: `go test -tags arcadedb_integration -race ./internal/arcadedb -run TestMemoryBatchLive_ -count=1 -v` passed against disposable databases on the healthy ArcadeDB 26.8.1 service.
- No named test reported "no tests to run"; no new test contains a skip.

## Known Stubs

None.

## User Setup Required

None - no new package, environment variable, service, or manual configuration is required.

## Next Phase Readiness

- Plan 49-11 can publish the bounded identity-free `memory_batch` MCP schema and call this engine exactly once per handler invocation.
- The live test file is intentionally ready for Plan 49-11 to extend with published-route and evaluator evidence.
- No blocker remains for dependent plans.

## Self-Check: PASSED

- All nine implementation/test files and this SUMMARY exist.
- Task commits `570513bd6`, `96ec1fc23`, `c1c587178`, `40bd9a237`, and `a5d8df3c1` exist.
- No tracked file deletion, known stub, skipped test, or unrun verification remains.

---
*Phase: 49-memory-tiers*
*Completed: 2026-08-31*
