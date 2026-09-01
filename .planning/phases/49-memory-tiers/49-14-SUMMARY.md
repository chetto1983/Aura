---
phase: 49-memory-tiers
plan: 14
subsystem: memory
tags: [arcadedb, memory-capture, runner, durability-barrier, provenance, goleak, tdd]

requires:
  - phase: 49-05
    provides: "Typed direct-evidence producers, ordered queue, and terminal durability barrier"
  - phase: 49-10
    provides: "Authority-aware ApplyAcceptedCapture sink over the atomic memory-batch engine"
provides:
  - "One production-owned tenant capture queue injected unchanged into Runner and joined at shutdown"
  - "Host-bound user-turn, tool-call, conversation, and artifact provenance on every accepted capture"
  - "Live real-event proof that accepted captures are recallable before terminal completion"
affects: [AUTO-03, CTX-05, memory-capture, phase-49-verification]

actuals:
  tokens: 9466
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Build the process-lifetime queue at the composition root and inject the exact pointer into Runner"
    - "Resolve each capture through the existing identity-scoped TenantClients and ApplyAcceptedCapture path"
    - "Use the host request id as the unspoofable user-turn provenance reference"

key-files:
  created:
    - cmd/aura/chat_boot_memory_capture_test.go
    - internal/runner/runner_memory_capture_live_test.go
  modified:
    - cmd/aura/chat_boot.go
    - cmd/aura/chat_boot_memory.go
    - internal/runner/runner_deps.go
    - internal/runner/runner_stop_leak_test.go
    - internal/runner/runner_memory_capture.go
    - internal/runner/runner_memory_capture_test.go
    - internal/arcadedb/memory_capture.go
    - internal/arcadedb/memory_capture_test.go
    - internal/arcadedb/memory_batch_store.go

key-decisions:
  - "Chat boot constructs the one MemoryCaptureQueue; Runner receives that prebuilt pointer and does not mint a second queue from a sink/config pair."
  - "The capture sink is a narrow tenant resolver adapter that selects the identity-scoped *arcadedb.Client and calls ApplyAcceptedCapture directly."
  - "Every capture requires user_turn:<host request id> in addition to direct conversation/tool/artifact refs; model-supplied source ids cannot substitute for it."
  - "Memory-batch DATETIME parameters use the established second-precision RFC3339 form so newly created capture facts are immediately visible to as-of recall."

patterns-established:
  - "Process ownership: boot constructs, Runner consumes, chatEnv closes once with a bound."
  - "Terminal evidence: the live sink reports durable only after ApplyAcceptedCapture returns, and terminal output remains blocked until all accepted sequences release."

requirements-completed: [AUTO-03, CTX-05]

coverage:
  - id: D1
    description: "Configured production boot creates exactly one capture queue/sink, injects the same queue into Runner, and absent memory creates no fallback."
    requirement: AUTO-03
    verification:
      - kind: unit
        ref: "cmd/aura/chat_boot_memory_capture_test.go#TestMemoryCaptureBoot and internal/runner/runner_stop_leak_test.go#TestMemoryCaptureBoot"
        status: pass
      - kind: other
        ref: "WSL go test -race ./internal/runner ./cmd/aura -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: "Normal stop and process close drain accepted work; exhausted or stuck persistence is bounded, sticky, and cannot report success."
    requirement: AUTO-03
    verification:
      - kind: unit
        ref: "internal/runner/runner_stop_leak_test.go#TestMemoryCaptureClose and TestMemoryCaptureSinkFailure"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_memory_capture_test.go#TestMemoryCaptureTerminalBarrier and TestMemoryCaptureStop"
        status: pass
    human_judgment: false
  - id: D3
    description: "Real MCP memory-upsert and write_file events persist typed direct provenance, exclude generated/reasoning sentinels, and become recallable before terminal success."
    requirement: CTX-05
    verification:
      - kind: integration
        ref: "internal/runner/runner_memory_capture_live_test.go#TestMemoryCaptureLive_ExplicitUserEvent, DurableArtifactEvent, TerminalBarrier under WSL -race"
        status: pass
      - kind: other
        ref: "go vet ./... && go build ./..."
        status: pass
    human_judgment: false

duration: 47min
completed: 2026-09-01
status: complete
---

# Phase 49 Plan 14: Production Capture Composition and Live Durability Summary

**One tenant-scoped production capture queue now joins shutdown and proves real fact/artifact events are graph-durable, principal-safe, and recallable before terminal success.**

## Performance

- **Duration:** 47 min
- **Started:** 2026-09-01T05:06:31Z
- **Completed:** 2026-09-01T05:53:07Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Composed one process-lifetime `MemoryCaptureQueue` at chat boot over a tenant-resolving `ApplyAcceptedCapture` sink, injected that exact queue into `Runner`, and joined it with a bounded process close.
- Proved normal drain, exhausted/stuck sink failure, repeated close, absent-memory behavior, and goleak/race cleanliness without adding configuration, schema, or a fallback producer.
- Drove actual agent execution through a real in-memory MCP bridge and the production `write_file` implementation into real `ToolInvocationEnd` events, Runner persistence, the ordered queue, and disposable identity-scoped ArcadeDB databases.
- Held terminal output behind two accepted sequences while querying each committed capture immediately, then proved exact `user_turn`, `conversation`, `tool_call`, and `artifact` provenance and complete absence of preview/final/summary/reasoning sentinels.
- Corrected the batch DATETIME representation exposed by the live artifact case so newly created capture facts are visible to immediate as-of recall.

## Task Commits

Each TDD task was committed in RED then GREEN order:

1. **Task 1 RED: Production ownership, bounded close, failure, and goleak contracts** - `85b47dedf` (`test`)
2. **Task 1 GREEN: One boot-owned tenant capture queue and joined lifecycle** - `cf03d7108` (`feat`)
3. **Task 2 RED: Real MCP/write_file event and terminal durability proof** - `afa97160a` (`test`)
4. **Task 2 RED hardening: Host-bound user-turn provenance** - `cd3176ad5` (`test`)
5. **Task 2 GREEN: Immediate recall precision and principal-safe refs** - `2c7ba011c` (`feat`)

## Files Created/Modified

- `cmd/aura/chat_boot_memory.go` - Tenant capture sink and one-queue construction helper.
- `cmd/aura/chat_boot.go` - Queue ownership, dependency injection, and bounded shutdown join.
- `cmd/aura/chat_boot_memory_capture_test.go` - Exact construction/injection/absent-memory composition contract.
- `internal/runner/runner_deps.go` - Prebuilt queue dependency replacing sink/config-based construction.
- `internal/runner/runner_stop_leak_test.go` - Bounded failure, repeated close, pointer identity, and goleak contracts.
- `internal/runner/runner_memory_capture.go` - Host-derived `user_turn` source reference on every production event.
- `internal/runner/runner_memory_capture_test.go` - Daemon-free producer assertions for the new trusted ref.
- `internal/runner/runner_memory_capture_live_test.go` - Real MCP/write-file events, disposable live graph, immediate recall, terminal ordering, and sentinel negatives.
- `internal/arcadedb/memory_capture.go` - Graph-boundary enforcement of the exact host user-turn ref.
- `internal/arcadedb/memory_capture_test.go` - Principal-safe provenance and recall-compatible time regression.
- `internal/arcadedb/memory_batch_store.go` - Recall-compatible ArcadeDB DATETIME parameter precision.

## Decisions Made

- Queue construction belongs at chat boot, not in `runner.New`. This makes one process owner visible, lets shutdown close admission exactly once, and prevents tests or future composition from accidentally minting a second queue from the same sink.
- The tenant adapter remains narrow: identity chooses a server-enforced tenant client, then the existing `ApplyAcceptedCapture` authority performs the write. No conversion DTO, second transaction engine, or fallback identity exists.
- The request UUID is the user-turn reference because it is minted and stamped by the host before agent execution. Memory IDs remain optional model evidence; they cannot replace the required host-derived reference.
- Capture `observed_at` retains nanosecond evidence time, while FACT validity timestamps use the established second-precision RFC3339 form already used by ordinary fact writes and recall queries.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Live database bug] Made new batch-created facts immediately recallable**
- **Found during:** Task 2 RED live durable-artifact case
- **Issue:** `ApplyAcceptedCapture` committed the artifact FACT and provenance, but `FactsAbout` and lexical `SearchFacts` both returned empty immediately. The raw row showed a nanosecond RFC3339 input stored in ArcadeDB's millisecond `DATETIME`, while the established recall path uses second-precision values.
- **Fix:** Aligned `nullableMemoryBatchTime` with the existing `UpsertFact`/as-of format and retained exact nanoseconds separately in capture provenance.
- **Files modified:** `internal/arcadedb/memory_batch_store.go`, `internal/arcadedb/memory_capture_test.go`
- **Verification:** The durable-artifact and two-sequence live cases now recall the typed capture immediately under WSL `-race`; daemon-free precision regression passes.
- **Committed in:** `2c7ba011c`

**2. [Rule 2 - Missing critical trust evidence] Added a host-bound user-turn reference**
- **Found during:** Task 2 provenance audit
- **Issue:** Stored captures had host conversation/tool ids and actor role/run fields, but no required source ref explicitly tied to the host-minted turn. Model-supplied memory ids could not satisfy the plan's principal-safe user-turn proof.
- **Fix:** Added `user_turn:<request-id>` in the runner producer and required the exact actor-run match at the ArcadeDB boundary for every source kind.
- **Files modified:** `internal/runner/runner_memory_capture.go`, `internal/runner/runner_memory_capture_test.go`, `internal/arcadedb/memory_capture.go`, `internal/arcadedb/memory_capture_test.go`
- **Verification:** Both live event classes assert the exact host run/reference and fail without it; existing replay, contradiction, and authority tests pass.
- **Committed in:** `cd3176ad5`, `2c7ba011c`

**3. [Rule 2 - Project evidence constraints] Added omitted daemon-free and composition test files**
- **Found during:** Tasks 1 and 2 RED
- **Issue:** The literal plan file list named production boot files but no available test file with headroom, and the live-only timestamp fix needed a daemon-free pure regression under the repository coverage policy.
- **Fix:** Added a focused boot test file and extended existing capture unit tests while keeping every touched file below 600 lines.
- **Files modified:** `cmd/aura/chat_boot_memory_capture_test.go`, `internal/runner/runner_memory_capture_test.go`, `internal/arcadedb/memory_capture_test.go`
- **Verification:** Normal file-size, vet, lint, unit, and WSL race gates pass with hooks enabled.
- **Committed in:** `85b47dedf`, `cf03d7108`, `2c7ba011c`

**4. [Rule 3 - Blocking state close-out] Restored the real next incomplete plan after out-of-order execution**
- **Found during:** Plan metadata close-out
- **Issue:** `state.advance-plan` moved the sequential pointer from incomplete Plan 49-11 to Plan 49-12 even though this execution completed out-of-order Plan 49-14; the activity/prose progress fields also remained on Plan 49-10.
- **Fix:** Preserved the handler-written 61/62 count, metric, decisions, state head, and session metadata while restoring both plan pointers to 49-11 and synchronizing activity/progress to Plan 49-14.
- **Files modified:** `.planning/STATE.md`
- **Verification:** STATE reports current Plan 11, 61 completed milestone plans, Plan 49-14 activity/session metadata, and ROADMAP reports 13/14 Phase 49 plans.
- **Committed in:** final tracking metadata commit

---

**Total deviations:** 4 auto-fixed (1 Rule 1 live database bug, 2 Rule 2 correctness/project-evidence completions, 1 Rule 3 tracking correction).
**Impact on plan:** All changes are required to prove or enforce the locked AUTO-03/CTX-05 contract. No package, migration, schema field, endpoint, auth path, environment variable, model-facing argument, or second memory authority was added.

## Issues Encountered

- The project `.env` does not set `ARCADEDB_URL`; the strict live fixture uses the deployed loopback default `http://127.0.0.1:2480`, then fails hard if the server/version, admin password, tenant secret, provisioning, or identity-scoped client is unavailable.
- The MCP bridge's process-global always-loaded slot budget was exhausted by the third case. The test wraps only `Spec().Deferred` while delegating `Execute` to the exact mounted production bridge tool, so every positive still comes from a real MCP call and real `ToolInvocationEnd`, not a synthetic producer.
- WSL full-repository builds were quiet for several minutes on the mounted Windows filesystem but completed successfully.
- The shared checkout retained the pre-existing `.planning/state.json` modification and untracked `.planning/milestone.lock`. Every commit inspected the cached index and used explicit `--only` allowlists, so neither entered Plan 49-14 history.

## TDD Gate Compliance

- **Task 1 RED:** `85b47dedf` compiled and failed only on the absent prebuilt-queue dependency and missing boot construction/ownership; close/failure/goleak mechanics already passed.
- **Task 1 GREEN:** `cf03d7108` passed exact construction count, pointer identity, absent-memory nil behavior, bounded shutdown, package regression, and WSL race. The automated tracer feedback gate reran the complete committed-state verification before expansion.
- **Task 2 RED:** `afa97160a` drove real events and failed because a committed artifact capture was invisible to immediate recall; raw graph evidence isolated DATETIME precision.
- **Task 2 RED hardening:** `cd3176ad5` failed both source classes on the missing host `user_turn` ref after the timestamp repair was present locally.
- **Task 2 GREEN:** `2c7ba011c` passed all three non-skipping live cases, exact provenance, sentinel negatives, repository vet/build, full package regression, and WSL race.
- **REFACTOR:** No behavior-neutral commit was needed; the implementation remains focused and every touched file is below 600 lines.

## Validation Evidence

- Task 1 named inventory discovers queue order/barrier/stop plus `MemoryCaptureBoot`, `MemoryCaptureClose`, and `MemoryCaptureSinkFailure`; all pass.
- Task 2 tagged inventory discovers exactly `TestMemoryCaptureLive_ExplicitUserEvent`, `DurableArtifactEvent`, and `TerminalBarrier`; all execute against disposable tenant databases on live ArcadeDB under WSL `-race`.
- The terminal case executes two real tool calls, observes each `ApplyAcceptedCapture` commit through immediate typed recall, and proves the final event remains unavailable until both accepted sequences release.
- `go vet ./...`, `go build ./...`, full `internal/runner`, `internal/arcadedb`, and `cmd/aura` unit suites pass.
- WSL Go 1.26.6 race gates pass for `internal/runner`, `internal/arcadedb`, and `cmd/aura`.
- All five commits ran normal pre-commit hooks; no hook was bypassed and no tracked file was deleted.

## Known Stubs

None.

## User Setup Required

None - the live proof uses the already deployed ArcadeDB stack and existing tenant credentials; no new configuration is introduced.

## Next Phase Readiness

- Plan 49-11 can consume completed AUTO-03/CTX-05 live evidence while closing the remaining Phase 49 published-surface/evaluator gate.
- The Phase 49 verifier can rely on one production queue owner, bounded truthful shutdown, exact principal-safe capture provenance, and real terminal-before-durable negatives.
- No Plan 49-14 blocker remains.

---
*Phase: 49-memory-tiers*
*Completed: 2026-09-01*

## Self-Check: PASSED
