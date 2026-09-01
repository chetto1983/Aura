---
phase: 49-memory-tiers
plan: 12
subsystem: memory
tags: [reasoning-memory, arcadedb, runner, redaction, retry, tdd]

requires:
  - phase: 49-01
    provides: "Measured PRD Amendment #201 as the prd.md-only reasoning governance ancestor"
  - phase: 49-04
    provides: "Explicit-only ReasoningTrace/ReasoningStep/ReasoningToolCall graph schema and storage boundary"
provides:
  - "Provider-authorized reasoning trace builder connected to the production event persistence seam"
  - "Post-commit source-turn binding with fail-soft bounded graph delivery"
  - "Allowlisted, redacted, retry-correct tool evidence and graph-validated TOUCHED entities"
affects: [49-09, 49-11, MEM-03, MEM-06, CTX-05]

actuals:
  tokens: 8357
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Observe graph reasoning only through the existing provider-display authorization gate"
    - "Commit PostgreSQL source truth first, then attempt bounded fail-soft derived graph delivery"
    - "Resolve structured entity candidates inside the ArcadeDB transaction before TOUCHED edges"

key-files:
  created:
    - internal/runner/runner_reasoning_graph.go
    - internal/runner/runner_reasoning_graph_test.go
  modified:
    - internal/runner/runner.go
    - internal/runner/runner_persist.go
    - internal/runner/runner_reasoning_persist.go
    - internal/arcadedb/memory_reasoning.go
    - internal/arcadedb/memory_reasoning_test.go

key-decisions:
  - "Use the host request UUID as the trace ID and the committed PostgreSQL conversation/turn sequence as the authoritative source reference."
  - "Allow only memory_batch, memory_forget, memory_recall, memory_upsert_fact, send_file, and task tool events; persist raw arguments only as a SHA-256 digest after repository redaction."
  - "Persist observations only for send_file, derive entity candidates only from allowlisted memory-tool argument fields, and keep only entities confirmed to exist inside the identity-scoped graph transaction."
  - "Reset reasoning text, steps, tool calls, entity candidates, call deduplication, and timestamps together on DiscardStreamed."

patterns-established:
  - "Reasoning steps are independent of streaming chunks: deltas concatenate until a structured tool boundary, and post-tool reasoning starts the next ordered step."
  - "Graph failures occur only after the authoritative answer commits and cannot rewrite or invalidate that answer."

requirements-completed: [MEM-03, MEM-06, CTX-05]

coverage:
  - id: D1
    description: "Authorized provider-visible reasoning becomes one bounded trace linked to the exact committed source turn, while failed appends, hidden reasoning, final prose, and graph outages cannot create or invalidate source truth."
    requirement: MEM-03
    verification:
      - kind: unit
        ref: "internal/runner/runner_reasoning_graph_test.go#TestReasoningGraphTracer"
        status: pass
      - kind: other
        ref: "Amendment #201 prd.md-only six-path ancestry gate"
        status: pass
      - kind: other
        ref: "go test -race ./internal/runner ./internal/arcadedb -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: "The final accepted attempt alone contributes stable ordered steps, bounded redacted tool metadata, direct source references, and TOUCHED edges to graph-existing structured entities."
    requirement: CTX-05
    verification:
      - kind: unit
        ref: "internal/runner/runner_reasoning_graph_test.go#TestReasoningGraphToolMetadata, TestReasoningGraphRetryDiscard, TestReasoningGraphTouchedEntities"
        status: pass
      - kind: unit
        ref: "internal/arcadedb/memory_reasoning_test.go#TestReasoningGraphTouchedEntities"
        status: pass
      - kind: other
        ref: "go vet ./... && go build ./..."
        status: pass
    human_judgment: false

duration: 31min
completed: 2026-09-01
status: complete
---

# Phase 49 Plan 12: Provider-Authorized Reasoning Producer Summary

**Provider-visible reasoning now becomes a bounded, retry-correct graph trace only after its exact PostgreSQL answer turn commits, with allowlisted tool evidence and validated entity audit edges.**

## Performance

- **Duration:** 31 min
- **Started:** 2026-09-01T01:39:15Z
- **Completed:** 2026-09-01T02:10:57Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Connected the existing provider-visible reasoning authorization seam to a separate bounded trace builder; hidden reasoning, final answers, and synthetic summaries have no producer.
- Finalized a trace only after `AppendAssistantTurnWithCacheMetric` succeeded, bound it to the exact committed turn sequence, and kept the bounded graph call fail-soft after source durability.
- Added stable tool-boundary step segmentation, a six-tool allowlist, redacted argument digests, safe bounded observations, artifact references, and whole-builder retry reset.
- Resolved structured memory-tool entity candidates inside the same ArcadeDB transaction and persisted/linked only graph-existing entities.

## Task Commits

Each TDD task was committed in RED then GREEN order:

1. **Task 1 RED: Provider-visible source-turn tracer** - `26ae96499` (`test`)
2. **Task 1 GREEN: Authorized post-commit trace producer** - `05fdc9eb6` (`feat`)
3. **Task 2 RED: Tool metadata, retry, and entity contracts** - `173419db6` (`test`)
4. **Task 2 GREEN: Bounded tool evidence and validated TOUCHED edges** - `43cebe886` (`feat`)

## Files Created/Modified

- `internal/runner/runner_reasoning_graph.go` - Authorized builder, stable segmentation, tool allowlist/redaction, source commit, retry reset, and bounded sink delivery.
- `internal/runner/runner_reasoning_graph_test.go` - Production-event ordering, authorization, graph failure, metadata, retry, and entity-reference contracts.
- `internal/runner/runner.go` - Holds the narrow reasoning graph sink dependency for downstream boot composition.
- `internal/runner/runner_persist.go` - Observes structured tool events and offers finalized traces only after source-answer commit.
- `internal/runner/runner_reasoning_persist.go` - Shares the exact display-authorization gate with graph reasoning observation.
- `internal/arcadedb/memory_reasoning.go` - Resolves candidate entity names inside the transaction before storing references or creating TOUCHED edges.
- `internal/arcadedb/memory_reasoning_test.go` - Proves missing/untrusted entity candidates cannot enter persisted tool metadata or edges.

## Decisions Made

- The per-turn host request UUID is the deterministic trace ID. The source reference is `postgres://aura/conversations/{conversation}/turns/{committed-seq}` and is assigned only after the source store confirms its committed sequence.
- Tool nodes are a closed allowlist. `send_file` is the only tool whose bounded result preview may become an observation; memory results, shell output, sidecars, blobs, errors, and arbitrary metadata are never copied.
- `memory_upsert_fact` subject/object and `memory_forget` entity fields are structured candidates, not authority. ArcadeDB confirms existence in the current identity database before the references or TOUCHED edges are written.
- Plan 49-09 remains the sole owner of production boot injection and lifecycle ownership for `ReasoningGraphSink`; Plan 49-12 freezes the producer contract it will inject.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical security proof] Extended the existing authorization seam and storage-boundary test files**
- **Found during:** Tasks 1 and 2
- **Issue:** The literal file list omitted `runner_reasoning_persist.go`, where the existing provider-display authorization gate lives, and `memory_reasoning_test.go`, the only layer that can prove TOUCHED candidates are graph-existing rather than merely syntactically valid.
- **Fix:** Called the graph observer from the same authorization gate and added an ArcadeDB transaction test that filters a missing candidate before tool upsert and edge creation.
- **Files modified:** `internal/runner/runner_reasoning_persist.go`, `internal/arcadedb/memory_reasoning_test.go`
- **Verification:** Hidden-provider tests, graph entity-resolution tests, repository vet/build, package unit, and WSL race gates all pass.
- **Committed in:** `26ae96499`, `173419db6`, `43cebe886`

**2. [Rule 3 - Blocking state close-out] Restored the real next incomplete plan after out-of-order execution**
- **Found during:** Plan metadata close-out
- **Issue:** `state.advance-plan` moved the sequential pointer from Plan 49-05 to 49-06 even though 49-05 still has no SUMMARY; Plan 49-12 executed out of order after 49-13.
- **Fix:** Preserved the handler-written 57/62 completion count, metric, decisions, state head, and session metadata while restoring both canonical and prose current-plan pointers to 49-05 and synchronizing last activity/progress to Plan 49-12 at 57/62.
- **Files modified:** `.planning/STATE.md`
- **Verification:** STATE reports current plan 5, 57 completed milestone plans, Plan 49-12 activity/session metadata, and next action Plan 49-05; ROADMAP reports 9/14 Phase 49 plans.
- **Committed in:** final plan metadata commit

---

**Total deviations:** 2 auto-fixed (1 Rule 2 security/correctness completion, 1 Rule 3 tracking correction).
**Impact on plan:** The code additions enforce the plan's named authorization and TOUCHED trust boundaries; the tracking correction preserves the real sequential queue. Neither changes the public schema, adds a dependency, or widens model-visible behavior.

## Issues Encountered

- Context7 was unavailable on this host. Before database changes, the executor used ArcadeDB's official HTTP/JSON transaction documentation to confirm same-session query/command/commit behavior and parameterized SQL.
- The complete WSL repository build took several minutes on the mounted Windows filesystem, but completed normally; all unit and race gates passed.
- The shared checkout retained the pre-existing `.planning/state.json` modification and untracked `.planning/milestone.lock`. Every code and metadata commit used cached-index inspection plus explicit allowlists, so neither entered Plan 49-12 history.

## TDD Gate Compliance

- **Task 1 RED:** `26ae96499` compiled and failed only because the authorized event stream produced no graph offer after the source commit.
- **Task 1 GREEN:** `05fdc9eb6` passed exact source ordering, failed-append zero-write, graph fail-soft, hidden/synthetic negative, ancestry, package, and WSL race gates; the automated tracer feedback gate repeated the complete verification from committed state.
- **Task 2 RED:** `173419db6` failed on absent allowlisted tool nodes, absent retry-final tool/entity state, and unresolved graph entity candidates.
- **Task 2 GREEN:** `43cebe886` passed stable segmentation, deterministic tool order, digest/redaction/blob/cap negatives, structured-entity fencing, whole-builder retry reset, package regression, and WSL race.
- **REFACTOR:** No behavior-neutral commit was needed; all touched production and test files remain below 600 lines.

## Verification Evidence

- Amendment #201 (`f231f15b5`) changes only `prd.md`; every protected path already touched in Phase 49 has an earliest Phase 49 commit descended from it, and future untouched paths remain permitted.
- All four named runner producer tests and the ArcadeDB entity-resolution test are discovered and pass; no target reports `no tests to run`.
- `go vet ./...`, `go build ./...`, and complete `internal/runner` plus `internal/arcadedb` unit suites pass from committed HEAD.
- WSL Go 1.26.6 `go test -race ./internal/runner ./internal/arcadedb -count=1` passes with zero races.
- No package, environment variable, migration, endpoint, auth path, skipped test, unrun verification, or tracked-file deletion was introduced.

## Known Stubs

| Stub | File | Line | Reason |
|------|------|------|--------|
| `ReasoningGraphSink` has no production boot injection yet | `internal/runner/runner.go` | 68 | Intentional phase boundary: dependent Plan 49-09 owns the one boot sink, retention worker, and close lifecycle. The runner producer and sink contract are complete and fully tested here. |

## User Setup Required

None - no new dependency, environment variable, service, migration, or manual configuration is required.

## Next Phase Readiness

- Plan 49-09 can inject one identity-resolving sink at boot and add retention/deletion lifecycle ownership without changing the producer contract.
- Plan 49-11 can use the stable trace/run/conversation/tool/source IDs for its authenticated running-Aura reasoning/TOUCHED and exclusion evidence.
- No blocker remains for dependent implementation; the intentional boot-composition stub is tracked in `.planning/WINDOWS.md` until Plan 49-09 closes it.

## Self-Check: PASSED

- All declared implementation, test, ledger, and summary artifacts exist.
- Task commits `26ae96499`, `05fdc9eb6`, `173419db6`, and `43cebe886` exist in RED -> GREEN order.
- Coverage classification reports 2/2 deliverables automatically covered by passing evidence.
- No tracked-file deletion, skipped test, or unrun plan verification remains; the intentional Plan 49-09 boot-composition stub is recorded in both this summary and `.planning/WINDOWS.md`.

---
*Phase: 49-memory-tiers*
*Completed: 2026-09-01*
