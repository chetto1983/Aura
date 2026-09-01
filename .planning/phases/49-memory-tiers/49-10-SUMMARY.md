---
phase: 49-memory-tiers
plan: 10
subsystem: memory
tags: [arcadedb, accepted-capture, idempotency, temporal-provenance, authority, tdd]

requires:
  - phase: 49-05
    provides: "Typed direct-evidence producers, ordered queue, and terminal durability barrier"
  - phase: 49-06
    provides: "Identity-bound final-state memory batch receipts, authority, and whole-decision retry"
provides:
  - "AcceptedCapture graph sink over the existing atomic memory-batch authority"
  - "Temporal contradiction transitions with principal-only supersession"
  - "Deduplicated structured capture provenance with direct refs, time, confidence, and host actor"
affects: [49-14, AUTO-03, CTX-05, memory-capture]

actuals:
  tokens: 10445
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Translate accepted evidence into the existing identity-scoped final-state batch engine"
    - "Keep the runner capture DTO as an alias of the ArcadeDB sink contract"
    - "Store per-capture audit records inside the existing fact provenance map"

key-files:
  created:
    - internal/arcadedb/memory_capture.go
    - internal/arcadedb/memory_capture_test.go
  modified:
    - internal/arcadedb/memory.go
    - internal/arcadedb/memory_provenance.go
    - internal/arcadedb/memory_batch_state.go
    - internal/runner/runner_memory_capture.go
    - internal/agent/tools/result.go
    - internal/agent/mcptools/bridge_memory_capture.go

key-decisions:
  - "ApplyAcceptedCapture delegates to ApplyMemoryBatch's existing identity lock, durable receipt, principal authorization, one-transaction diff, and full-decision conflict retry."
  - "AcceptedCapture is canonical in arcadedb and re-exported as a runner alias, so Client satisfies MemoryCaptureSink without a conversion model or second authority."
  - "Explicit valid-time and supersession controls travel only through model-invisible structured success evidence; actor identity and role remain host-derived."
  - "Each fact source retains deduplicated capture records while flat MemoryIDs remain populated for existing provenance readers."

patterns-established:
  - "An exact active fact short-circuits supersession during replay, enriching provenance without closing history twice."
  - "Capture validation rejects every non-enum source and malformed direct ref before opening a graph transaction."

requirements-completed: [AUTO-03, CTX-05]

coverage:
  - id: D1
    description: "Accepted captures replay idempotently, enrich direct provenance, and recompute from committed graph state after conflicts."
    requirement: AUTO-03
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_capture_test.go#TestAcceptedCapture_Tracer, TestAcceptedCapture_Idempotent, TestAcceptedCapture_Retry"
        status: pass
      - kind: other
        ref: "go vet ./... && go build ./..."
        status: pass
      - kind: other
        ref: "WSL go test -race ./internal/arcadedb ./internal/runner ./internal/agent/tools ./internal/agent/mcptools -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: "Contradictions retain historical intervals, workers cannot supersede principal state, and targeted principal corrections close exactly one fact."
    requirement: AUTO-03
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_capture_test.go#TestAcceptedCapture_Contradiction, TestAcceptedCapture_WorkerAuthority, TestAcceptedCapture_PrincipalAuthority"
        status: pass
      - kind: unit
        ref: "internal/arcadedb/fact_authority_test.go#TestMaySupersedeRefusesWorkerAllowsParent"
        status: pass
    human_judgment: false
  - id: D3
    description: "Only allowlisted structured fact/artifact evidence reaches storage, with one wire-round-trippable provenance record per accepted capture and no reasoning/generated source."
    requirement: CTX-05
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_capture_test.go#TestAcceptedCapture_SourceDefense, TestAcceptedCapture_ProvenanceEnrichment"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_memory_capture_test.go#TestMemoryUpsertAcceptedCapture, TestDurableArtifactAcceptedCapture"
        status: pass
    human_judgment: false

duration: 28min
completed: 2026-09-01
status: complete
---

# Phase 49 Plan 10: Authority-Aware Accepted Capture Sink Summary

**Accepted direct evidence now commits through Aura's existing identity-bound atomic memory engine, preserving temporal history, principal authority, and structured provenance under replay and conflict.**

## Performance

- **Duration:** 28 min
- **Started:** 2026-09-01T04:24:54Z
- **Completed:** 2026-09-01T04:52:31Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Added `ApplyAcceptedCapture` as a thin capture-to-batch translation over the shipped final-state transaction engine, inheriting identity locks, atomic receipts, rollback, and fresh-state conflict retry.
- Preserved contradiction history and explicit authority: workers may add evidence but cannot close principal state, while a principal may close one targeted interval and open its replacement atomically.
- Persisted one structured provenance record per accepted capture, including the idempotency key, source enum, direct refs, conversation/tool IDs, observation time, confidence, run, and writer role.
- Moved the capture contract to ArcadeDB with runner aliases and carried valid-time/supersession fields through the existing model-invisible successful-tool metadata path.
- Rejected reasoning, summaries, assistant prose, generated text, and raw tool results before any transaction begins.

## Task Commits

Each TDD task was committed in RED then GREEN order:

1. **Task 1 RED: Accepted capture sink contracts** - `d07a46868` (`test`)
2. **Task 1 GREEN: Atomic accepted-capture application** - `5dde3437e` (`feat`)
3. **Task 2 RED: Temporal and structured provenance contracts** - `848fad689` (`test`)
4. **Task 2 GREEN: Structured capture provenance** - `e16d9811f` (`feat`)

## Files Created/Modified

- `internal/arcadedb/memory_capture.go` - Canonical capture contract, sink validation, capture-to-batch translation, and structured provenance codec/helpers.
- `internal/arcadedb/memory_capture_test.go` - Replay, conflict, source defense, temporal contradiction, worker/principal authority, and provenance round-trip contracts.
- `internal/arcadedb/memory.go` - Adds capture audit records to the existing `FactSource` provenance shape while remaining below 600 lines.
- `internal/arcadedb/memory_provenance.go` - Merges, serializes, decodes, and compares capture provenance without duplicate records.
- `internal/arcadedb/memory_batch_state.go`, `memory_batch_test.go` - Deep-clone nested provenance across isolated working states and fake retry snapshots.
- `internal/runner/runner_memory_capture.go`, `runner_memory_capture_test.go` - Alias the canonical graph contract and retain explicit valid-time/supersession data in stable capture identity.
- `internal/agent/tools/result.go` - Extends typed accepted-fact metadata with explicit temporal and correction fields.
- `internal/agent/mcptools/bridge_memory_capture.go`, `bridge_memory_capture_test.go` - Projects those fields only from a non-refused structured `memory_upsert_fact` success while keeping actor data host-derived.

## Decisions Made

- Reused `ApplyMemoryBatch` instead of building a second capture transaction layer. Its receipt and request hash make exact replay a durable no-op; its isolated working state and commit retry already meet the sink's convergence requirements.
- Kept explicit correction semantics. The sink never guesses that a different object is a contradiction: only a structured `supersedes`/target key from the accepted success can close prior state.
- Stored capture audit records inside the existing `sources` map on each fact rather than introducing a new vertex, edge type, table, or authority. Existing `MemoryIDs` readers continue to see the direct refs and capture key.
- An exact replacement fact already present from the successful foreground memory call is treated as provenance enrichment before supersession resolution, so capture replay cannot close history twice.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical contract seam] Made AcceptedCapture a direct ArcadeDB sink contract**
- **Found during:** Task 1 GREEN
- **Issue:** `runner.AcceptedCapture` could not be accepted directly by an ArcadeDB client method without importing `runner` back into `arcadedb` and creating an import cycle; a conversion DTO would violate the unchanged structured-evidence link and duplicate the contract.
- **Fix:** Defined the canonical DTO/source enum in `internal/arcadedb/memory_capture.go` and re-exported exact aliases from the runner, making `*arcadedb.Client` satisfy `runner.MemoryCaptureSink` directly.
- **Files modified:** `internal/arcadedb/memory_capture.go`, `internal/runner/runner_memory_capture.go`
- **Verification:** Full runner/ArcadeDB unit and WSL race suites pass; no adapter or duplicate DTO exists.
- **Committed in:** `5dde3437e`

**2. [Rule 2 - Missing critical temporal input] Preserved explicit correction controls through typed success evidence**
- **Found during:** Task 1 GREEN
- **Issue:** Plan 49-05's capture envelope retained fact content but not `valid_from`, `valid_to`, `supersedes`, or `supersedes_fact_key`; without them the sink would have to guess contradictions or discard temporal intent.
- **Fix:** Extended only the model-invisible `AcceptedFactEvidence` and capture hash with the structured successful call's explicit temporal/correction fields. Actor run/role still come from host context.
- **Files modified:** `internal/agent/tools/result.go`, `internal/agent/mcptools/bridge_memory_capture.go`, `internal/runner/runner_memory_capture.go`
- **Verification:** Bridge and runner producer contracts plus principal/worker sink tests pass.
- **Committed in:** `5dde3437e`

**3. [Rule 2 - AGENTS.md file ceiling] Split capture provenance out of memory.go**
- **Found during:** Task 2 RED commit hook
- **Issue:** Adding the planned capture provenance type inline grew `memory.go` from 598 to 611 lines, and the normal hook correctly rejected it.
- **Fix:** Kept only the additive `FactSource.Captures` field in `memory.go` and placed the capture-specific type/codec in `memory_capture.go`; final sizes are 591 and 417 lines.
- **Files modified:** `internal/arcadedb/memory.go`, `internal/arcadedb/memory_capture.go`
- **Verification:** The file-size hook, vet, lint, unit, and WSL race gates pass with hooks enabled.
- **Committed in:** `848fad689`, `e16d9811f`

---

**Total deviations:** 3 auto-fixed (3 Rule 2 correctness/project-constraint completions).
**Impact on plan:** The changes close required contract gaps without adding a storage authority, lock, retry loop, dependency, model-facing field, endpoint, or environment variable.

## Issues Encountered

- Context7 and its CLI fallback were unavailable. Official ArcadeDB HTTP transaction, `UPDATE ... UPSERT`, and `CREATE EDGE ... IF NOT EXISTS` documentation was read before implementation.
- The Task 1 RED assertion initially counted a shared conversation reference globally across two distinct sources. It was corrected before GREEN to require one occurrence per provenance source while capture/tool refs remain globally unique.
- The shared checkout retained the pre-existing `.planning/state.json` modification and untracked `.planning/milestone.lock`. The tracking handler refreshed only `state.json`'s `updated_at` on top of its existing `pending -> in_progress` phase change; per the shared-tree owner, the file remains uncommitted rather than being reset to HEAD and destroying that foreign status change. Every commit inspected the cached index and used explicit `--only` allowlists.

## TDD Gate Compliance

- **Task 1 RED:** `d07a46868` compiled and failed only on the intentional `ApplyAcceptedCapture` not-implemented sentinel.
- **Task 1 GREEN:** `5dde3437e` passed tracer, idempotency, conflict retry, source defense, producer wiring, package regression, and WSL race; the automated tracer feedback gate repeated the committed-state verification.
- **Task 2 RED:** `848fad689` failed only because per-capture structured provenance was not yet stored; temporal close/open and authority behavior were already green from the tracer slice.
- **Task 2 GREEN:** `e16d9811f` passed historical-interval, worker/principal, target-key, deduplication, JSON round-trip, repository vet/build, full package, and WSL race gates.
- **REFACTOR:** No behavior-neutral commit was needed; the mandatory file split landed with the RED scaffold and GREEN implementation while hooks remained enabled.

## Verification Evidence

- Both named test inventories discover four tests each; no target reports `no tests to run`.
- All eight `TestAcceptedCapture_*` contracts pass from committed HEAD.
- Real `ToolInvocationEnd` producer shapes, ordered queue, terminal barrier, retry/discard, normal stop, and package goleak tests pass.
- `go vet ./...`, `go build ./...`, and full `internal/arcadedb`, `internal/runner`, `internal/agent/tools`, and `internal/agent/mcptools` regressions pass.
- WSL Go 1.26.6 `go test -race` passes for all four touched packages with zero races.
- Production queue construction and fresh-image live proof remain intentionally owned by dependent Plan 49-14; this plan provides the direct sink it will inject.

## Known Stubs

None. Plan 49-14's pre-existing production-composition boundary remains open and tracked separately; Plan 49-10 introduced no stub or fallback path.

## User Setup Required

None - no dependency, environment variable, service, migration, endpoint, or manual configuration is required.

## Next Phase Readiness

- Plan 49-14 can inject `*arcadedb.Client` directly as the one process-lifetime `MemoryCaptureSink`, with no adapter or conversion model.
- The live plan can assert immediate recall of the stored nested capture provenance while reusing the already-proven queue/barrier semantics.
- No blocker remains for dependent implementation.

## Self-Check: PASSED

- All declared key files exist.
- Commits `d07a46868`, `5dde3437e`, `848fad689`, and `e16d9811f` exist in RED -> GREEN order with no tracked-file deletions.
- Coverage classification reports 3/3 deliverables automatically covered by passing evidence.
- No TODO/FIXME/placeholder, skipped test, unrun task verification, or plan-owned untracked artifact remains.

---
*Phase: 49-memory-tiers*
*Completed: 2026-09-01*
