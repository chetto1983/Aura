---
phase: 49-memory-tiers
plan: 05
subsystem: memory
tags: [runner, memory-capture, durability-barrier, backpressure, provenance, tdd]

requires:
  - phase: 49-09
    provides: "Production-owned identity-scoped memory lifecycle and explicit reasoning isolation boundaries"
provides:
  - "Closed typed direct-evidence producers for explicit memory facts and durable workspace artifacts"
  - "Bounded serial accepted-capture queue with monotonic sequence, backpressure, retry, and joined close"
  - "Runner terminal and stop barriers that fail honestly until the accepted global watermark is durable"
affects: [49-10, 49-14, AUTO-03, CTX-05, memory-capture]

actuals:
  tokens: 12660
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "ToolResult.Meta carries typed model-invisible evidence; result prose is never parsed into memory"
    - "One lazy runner-owned worker serializes bounded accepted captures and exits whenever drained"
    - "Terminal success snapshots and flushes the global accepted watermark"

key-files:
  created:
    - internal/runner/runner_memory_capture.go
    - internal/runner/runner_memory_capture_test.go
    - internal/agent/mcptools/bridge_memory_capture.go
    - internal/agent/mcptools/bridge_memory_capture_test.go
  modified:
    - internal/agent/mcptools/bridge_call.go
    - internal/agent/tools/result.go
    - internal/agent/tools/write_file.go
    - internal/agent/tools/patch.go
    - internal/agent/tools/fs_box_fake_router_test.go
    - internal/runner/runner.go
    - internal/runner/runner_deps.go
    - internal/runner/runner_persist.go
    - internal/runner/runner_resume.go

key-decisions:
  - "Only exact structured successes from memory__memory_upsert_fact, write_file, and patch can produce AcceptedCapture; shell/read/document/prose/reasoning and malformed metadata are structurally ineligible."
  - "Durable filesystem evidence is limited to /workspace identity plus operation; file contents and unrestricted result previews never enter the capture."
  - "The queue applies one capture at a time, makes a terminal sink failure sticky, and uses a lazy worker so an idle or drained runner retains no goroutine."
  - "Final answers flush the runner-global accepted watermark, including captures accepted before a pause; a tracker-local watermark is insufficient across resume."

patterns-established:
  - "Direct provenance is host-bound: authenticated identity, request UUID, parent/worker role, conversation, tool call, source references, confidence, and timestamp are validated before acceptance."
  - "Accepted capture failures are fail-honest, unlike fail-soft conversation projection."

requirements-completed: [AUTO-03, CTX-05]

coverage:
  - id: D1
    description: "Only canonical memory-fact successes and real durable workspace writes emit typed direct-evidence captures; excluded, failed, temporary, secret, prose, and reasoning sources emit none."
    requirement: CTX-05
    verification:
      - kind: unit
        ref: "internal/runner/runner_memory_capture_test.go#TestMemoryUpsertAcceptedCapture, TestDurableArtifactAcceptedCapture, TestAcceptedCaptureProducerRejectsExcludedSources"
        status: pass
      - kind: unit
        ref: "internal/agent/mcptools/bridge_memory_capture_test.go#TestMemoryUpsertAcceptedCapture and internal/agent/tools/fs_box_fake_router_test.go#TestDurableArtifactAcceptedCapture"
        status: pass
      - kind: other
        ref: "WSL go test -race ./internal/runner ./internal/agent/tools ./internal/agent/mcptools -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: "Accepted captures preserve order through bounded backpressure and must become durable through the global watermark before terminal success or normal stop."
    requirement: AUTO-03
    verification:
      - kind: unit
        ref: "internal/runner/runner_memory_capture_test.go#TestMemoryCaptureQueueOrder, TestMemoryCaptureTerminalBarrier, TestMemoryCaptureRetryDiscard, TestMemoryCaptureStop"
        status: pass
      - kind: other
        ref: "go vet ./... && go build ./..."
        status: pass
      - kind: other
        ref: "WSL go test -race ./internal/runner ./internal/agent/tools ./internal/agent/mcptools ./internal/arcadedb -count=1"
        status: pass
    human_judgment: false

duration: 35min
completed: 2026-09-01
status: complete
---

# Phase 49 Plan 05: Direct-Evidence Capture Queue Summary

**Canonical fact and workspace evidence now enters one bounded serial queue, and the runner cannot publish terminal success before the accepted global watermark is durable.**

## Performance

- **Duration:** 35 min
- **Started:** 2026-09-01T03:33:12Z
- **Completed:** 2026-09-01T04:09:11Z
- **Tasks:** 2
- **Files modified:** 13

## Accomplishments

- Added typed, model-invisible evidence for the exact `memory__memory_upsert_fact`, `write_file`, and `patch` success paths, retaining host actor, source, artifact, confidence, and time provenance without parsing result prose.
- Added a closed classifier that rejects failed calls, non-allowlisted tools, temporary artifacts, missing identity, malformed actor provenance, configured/known credential shapes, assistant prose, and reasoning.
- Added a bounded runner-owned queue with monotonic acceptance, capacity backpressure, one serial lazy worker, bounded retries, sticky failure, `FlushThrough`, and leak-free repeated `Close`.
- Wired tool-result acceptance into the existing persistence seam and made final answers plus `Runner.Stop` flush through the accepted global watermark before reporting success.
- Re-ran existing ArcadeDB duplicate-provenance and parent/worker supersession tests to confirm the accepted capture contract does not create a second memory authority or weaken graph authority.

## Task Commits

Each TDD task was committed in RED then GREEN order:

1. **Task 1 RED: Direct fact/artifact producer contracts** — `e8061e6bf` (`test`)
2. **Task 1 GREEN: Typed direct-evidence emitters and closed classifier** — `141826ed5` (`feat`)
3. **Task 2 RED: Ordered queue, barrier, failure, discard, and stop contracts** — `f1b5e1778` (`test`)
4. **Task 2 GREEN: Bounded serial durability queue and runner lifecycle wiring** — `24660a75e` (`feat`)
5. **Task 2 Rule 1 fix: Global watermark across pause/resume** — `253da0336` (`fix`)

## Files Created/Modified

- `internal/runner/runner_memory_capture.go` — AcceptedCapture contract, closed classifier, idempotency, bounded serial queue, retries, barriers, and runner adapters.
- `internal/runner/runner_memory_capture_test.go` — Direct-source positives/negatives, ordering, backpressure, runner terminal race, sink failure, retry discard, resumed watermark, stop, and goleak coverage.
- `internal/agent/mcptools/bridge_memory_capture.go` — Exact structured `memory__memory_upsert_fact` success projection with host-derived actor provenance.
- `internal/agent/mcptools/bridge_call.go` — Lifts the typed accepted fact onto `ToolResult.Meta` after a successful MCP call.
- `internal/agent/tools/result.go`, `write_file.go`, `patch.go` — Typed durable-artifact metadata for real `/workspace` writes only.
- `internal/runner/runner.go`, `runner_deps.go` — Optional sink composition and bounded terminal-flush configuration.
- `internal/runner/runner_persist.go`, `runner_resume.go` — Event-time acceptance plus final-answer and normal-stop global-watermark drains.

## Decisions Made

- `ToolResult.Meta` is the only evidence carrier. It is structured, not model-visible, and already reaches `ToolInvocation`; parsing previews or final assistant narration would violate D-15/CTX-05.
- The allowlist is exact, not suffix-based. A different MCP server advertising a similarly named tool cannot become a fact source.
- Filesystem capture stores only canonical `/workspace` path and operation. Content, diffs, shell output, sidecars, and arbitrary metadata remain outside durable capture.
- A capture failure is terminal for that ordered queue. Later captures are not applied around an earlier failed sequence, and every relevant barrier returns the durable error.
- The worker starts lazily and exits at an empty queue. This preserves one serial writer without a process-lifetime idle goroutine.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical typed seam] Added exact MCP structured-result projection**
- **Found during:** Task 1 RED/GREEN
- **Issue:** The runner receives typed `ToolInvocation.Meta`, but the existing MCP bridge discarded `structuredContent` for non-view tools; parsing `ResultPreview` would have laundered server prose into evidence.
- **Fix:** Added a narrow exact-name adapter that admits only a non-refused structured `memory__memory_upsert_fact` result and copies only the canonical fact fields plus host actor/source references.
- **Files modified:** `internal/agent/mcptools/bridge_call.go`, `bridge_memory_capture.go`, `bridge_memory_capture_test.go`, `internal/agent/tools/result.go`
- **Verification:** Bridge production-result test, runner producer test, full package regression, and WSL race pass.
- **Committed in:** `141826ed5`

**2. [Rule 2/3 - Required lifecycle ownership] Extended constructor and normal stop seams**
- **Found during:** Task 2 GREEN
- **Issue:** A queue held only in tests would not be runner-owned, and terminal-only draining would leave a normal conversation stop unable to prove its accepted watermark.
- **Fix:** Added the optional `MemoryCaptureSink`/queue constructor dependency, a bounded flush timeout, and global-watermark draining in `Runner.Stop` without closing the runner-wide queue.
- **Files modified:** `internal/runner/runner_deps.go`, `runner.go`, `runner_resume.go`
- **Verification:** `TestMemoryCaptureStop/runner_stop_drains_watermark`, goleak, full runner regression, and WSL race pass.
- **Committed in:** `24660a75e`

**3. [Rule 1 - Bug] Flushed the global watermark after resume**
- **Found during:** Final task audit
- **Issue:** Final completion initially flushed only the current tracker watermark. A capture accepted before a pause could still be pending when a resumed turn produced no new capture and returned success.
- **Fix:** Snapshot the runner queue's global accepted sequence at terminal completion and flush through it; added an explicit pre-pause/resume regression.
- **Files modified:** `internal/runner/runner_persist.go`, `runner_memory_capture_test.go`
- **Verification:** `TestMemoryCaptureTerminalBarrier/resume_drains_pre-pause_global_watermark`, full unit, vet/build, and WSL race pass.
- **Committed in:** `253da0336`

**4. [Rule 3 - Blocking state close-out] Restored the real next incomplete plan**
- **Found during:** Plan metadata close-out
- **Issue:** `state.advance-plan` moved the pointer from Plan 5 to already-completed Plan 6, while `state.update-progress` correctly declined the unscoped phase and left Plan 09 activity plus 58/62 prose progress stale.
- **Fix:** Preserved the handler-written 59/62 canonical count, metric, decisions, state head, and session metadata while setting both plan pointers to the next incomplete Plan 10 and synchronizing activity/progress to Plan 49-05.
- **Files modified:** `.planning/STATE.md`
- **Verification:** STATE reports current Plan 10, 59 completed milestone plans, Plan 49-05 activity/session metadata, and ROADMAP reports 11/14 Phase 49 plans.
- **Committed in:** final plan metadata commit

---

**Total deviations:** 4 auto-fixed (2 missing critical/blocking lifecycle seams, 1 terminal durability bug, 1 tracking correction).
**Impact on plan:** All changes are required to enforce the planned typed evidence and durability guarantee. No package, environment variable, schema, endpoint, model-facing field, or second memory authority was added.

## Issues Encountered

- The first Task 1 RED commit attempt was rejected by the normal revive hook for missing exported-constant comments. The comments were added and the commit retried with hooks enabled.
- The initial secret fixture used a noncanonical fake prefix that the repository's single credential-pattern table intentionally does not classify. It was corrected to a real covered OpenRouter shape; configured-secret exact detection is also enforced.
- The shared checkout retained the pre-existing `.planning/state.json` modification and untracked `.planning/milestone.lock`. Every commit inspected the cached index and used explicit allowlists, so neither entered Plan 49-05 history.

## TDD Gate Compliance

- **Task 1 RED:** `e8061e6bf` compiled and failed only because the explicit-fact classifier, durable filesystem metadata, and MCP structured-result adapter were intentional stubs.
- **Task 1 GREEN:** `141826ed5` passed all exact producer positives, closed-source negatives, full touched-package regressions, repository vet/build, and WSL race; the tracer feedback gate repeated the committed-state verification.
- **Task 2 RED:** `f1b5e1778` compiled and failed only on the intentional queue/wiring stubs; retry-discard was structurally green.
- **Task 2 GREEN:** `24660a75e` passed ordering, capacity backpressure, terminal race, bounded failure, retry-discard, repeated close, normal stop, goleak, full runner regression, and WSL race.
- **Rule 1 regression:** `253da0336` preserves the RED → GREEN sequence and adds the resumed global-watermark case without rewriting shared history.
- **REFACTOR:** No separate behavior-neutral commit was required; all touched production files remain below 600 lines.

## Verification Evidence

- Named producer and queue inventories discovered every required test; no target reported `no tests to run`.
- `go vet ./...`, `go build ./...`, and full `internal/runner`, `internal/agent/tools`, and `internal/agent/mcptools` unit suites pass from committed HEAD.
- WSL Go 1.26.6 `go test -race ./internal/runner ./internal/agent/tools ./internal/agent/mcptools ./internal/arcadedb -count=1` passes with zero races.
- Existing `TestUpsertFactReplaysAndAttachesSourcesWithoutDuplicateEdges`, `TestMaySupersedeRefusesWorkerAllowsParent`, `TestUpsertFactRefusesWorkerSupersedeWithoutTouchingTheFact`, and `TestUpsertFactAllowsParentSupersede` pass.
- No tracked file deletion, skipped test, unrun task verification, migration, environment variable, dependency, endpoint, schema field, or public tool was introduced.

## Known Stubs

| Stub | File | Line | Reason |
|------|------|------|--------|
| Production boot does not yet inject `MemoryCaptureSink` | `internal/runner/runner_deps.go` | 46 | Intentional phase boundary: Plan 49-10 owns graph application semantics and Plan 49-14 owns production composition plus the live mid-task durability proof. The producer, queue, and barrier contracts are complete and tested here. |

This intentional boundary is recorded in `.planning/WINDOWS.md` so Phase 49 cannot ship while the production sink remains unwired.

## User Setup Required

None - no package, environment variable, service, migration, or manual configuration is required.

## Next Phase Readiness

- Plan 49-10 can implement identity-scoped duplicate/provenance/contradiction application behind `MemoryCaptureSink` without changing producer or queue contracts.
- Plan 49-14 can inject the sink at the existing constructor seam and prove a real shell/file task cannot complete before the graph write becomes durable.
- Production boot wiring remains intentionally open and is recorded in the broken-windows ledger; no other blocker remains.

## Self-Check: PASSED

- All thirteen implementation/test files and this SUMMARY exist.
- Commits `e8061e6bf`, `141826ed5`, `f1b5e1778`, `24660a75e`, and `253da0336` exist in RED → GREEN order with no tracked-file deletions.
- Coverage classification reports 2/2 scoped deliverables automatically covered by passing evidence.
- No TODO/FIXME/placeholder, skipped test, unrun task verification, or unrecorded stub remains in the Plan 49-05 diff.

---
*Phase: 49-memory-tiers*
*Completed: 2026-09-01*
