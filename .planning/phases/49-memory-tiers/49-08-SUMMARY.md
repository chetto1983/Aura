---
phase: 49-memory-tiers
plan: 08
subsystem: memory
tags: [mcp, arcadedb, active-context, oauth, negative-filter, tdd]

requires:
  - phase: 49-03
    provides: "Stable one-tool mixed fact/conversation recall with typed evidence and honest path metadata"
provides:
  - "Per-request host-only active conversation carrier over the shipped MCP HeaderFunc seam"
  - "Strict bounded server decode with OAuth-first identity and actor-turn revalidation"
  - "Pre-ranking active-conversation negative filters across semantic and browse recall"
affects: [49-13, MEM-02, TOOL-05, memory-recall]

actuals:
  tokens: 11312
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Derive mutable request metadata from context inside HeaderFunc, never from reusable session state"
    - "Validate transport metadata under authenticated identity before converting it into negative-only query scope"
    - "Apply exclusion both in ArcadeDB ranking SQL and defensive hydration filtering"

key-files:
  created:
    - internal/agent/mcptools/bridge_recall_context.go
    - internal/arcadedb/memory_recall_exclusion.go
  modified:
    - internal/agent/tools/result.go
    - internal/agent/tools/result_test.go
    - internal/agent/mcptools/bridge_actor.go
    - internal/agent/mcptools/bridge_actor_test.go
    - internal/agent/mcptools/mount.go
    - cmd/arcadedb-mcp/tool_memory_recall.go
    - cmd/arcadedb-mcp/tool_memory_recall_test.go
    - internal/arcadedb/memory_recall.go
    - internal/arcadedb/memory_recall_browse.go

key-decisions:
  - "The active source pair is the host tool-call session/conversation ID plus the existing per-turn request ID; the server requires that turn ID to equal the separately carried actor run ID."
  - "The carrier is canonical unpadded base64url JSON, capped at 8 sources, 256 runes per ID, 1536 decoded bytes, and 2048 encoded bytes."
  - "OAuth subject resolution precedes header decoding, and every requested conversation must be found under that identity before it becomes an exclusion."
  - "The bridge and MCP server keep independent wire codecs, matching the existing actor-header boundary between separate loopback binaries rather than importing agent code into the server."

patterns-established:
  - "A reused identity-scoped MCP session carries fresh actor and recall headers from each request context without storing either on the session."
  - "Host exclusions can remove conversation candidates but cannot select identity, add evidence, or enter MemoryRecallInput/_meta."

requirements-completed: [MEM-02, TOOL-05]

coverage:
  - id: D1
    description: "Two calls over one reused session carry only their own host-derived active conversation/turn pair while preserving actor headers."
    requirement: MEM-02
    verification:
      - kind: unit
        ref: "internal/agent/tools/result_test.go#TestSessionIDFromContext and internal/agent/mcptools/bridge_actor_test.go#TestRecallContextHeaders"
        status: pass
      - kind: integration
        ref: "internal/agent/mcptools/bridge_actor_test.go#TestRecallContextHeadersProductionMount"
        status: pass
    human_judgment: false
  - id: D2
    description: "The server rejects malformed, over-cap, ambiguous, actor-mismatched, and foreign active sources before ordinary recall, then passes validated conversations only as negative filters."
    requirement: MEM-02
    verification:
      - kind: unit
        ref: "cmd/arcadedb-mcp/tool_memory_recall_test.go#TestMemoryRecallActiveSourceHeader"
        status: pass
      - kind: integration
        ref: "cmd/arcadedb-mcp/tool_memory_recall_test.go#TestMemoryRecallSuppressesActiveConversation"
        status: pass
    human_judgment: false
  - id: D3
    description: "memory_recall remains the single model-facing retrieval operation and active-source state is absent from input JSON and MCP _meta."
    requirement: TOOL-05
    verification:
      - kind: unit
        ref: "cmd/arcadedb-mcp/tool_memory_recall_test.go#TestMemoryRecallActiveSourceIsNotModelInput"
        status: pass
      - kind: unit
        ref: "cmd/arcadedb-mcp/tool_memory_recall_test.go#TestMemoryRecallModeContract"
        status: pass
    human_judgment: false

duration: 29min
completed: 2026-08-31
status: complete
---

# Phase 49 Plan 08: Host-Derived Active Recall Exclusions Summary

**Fresh per-call active context now crosses Aura's existing MCP header seam and suppresses only authenticated current-conversation evidence from unified recall.**

## Performance

- **Duration:** 29 min
- **Started:** 2026-08-31T23:16:51Z
- **Completed:** 2026-08-31T23:45:24Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Added a read-only session accessor and bounded canonical `(conversation_id, turn_id)` carrier derived from each tool call, with fresh values on reused MCP sessions and deterministic composition with existing actor headers.
- Wired the production identity-scoped mount to attach both header families per request while bare/non-identity contexts emit no recall metadata.
- Added strict server-side decode, OAuth-first handling, actor-turn matching, authenticated conversation ownership lookup, and fail-closed malformed/foreign behavior.
- Extended unified recall with negative-only active-conversation scope in native ranking SQL, hydration defense, recent browsing, and explicit open/scroll abstention without changing the model-facing schema.

## Task Commits

1. **Task 1 RED: Active carrier and reused-session contracts** — `f53bee04a` (`test`)
2. **Task 1 GREEN: Per-request canonical carrier and actor composition** — `bb7362a68` (`feat`)
3. **Task 2 RED: Production mount, decode, ownership, and suppression contracts** — `e6725150d` (`test`)
4. **Task 2 GREEN: Authenticated negative filters across unified recall** — `263848a5d` (`feat`)

## Files Created/Modified

- `internal/agent/tools/result.go`, `result_test.go` — Read-only current conversation/session accessor and its bare/present contract.
- `internal/agent/mcptools/bridge_recall_context.go` — Bounded canonical active-source codec and per-request HeaderFunc.
- `internal/agent/mcptools/bridge_actor.go`, `bridge_actor_test.go`, `mount.go` — Deterministic actor/recall composition and production identity-mount wiring.
- `cmd/arcadedb-mcp/tool_memory_recall.go`, `tool_memory_recall_test.go` — Strict host-header decode, actor matching, tenant ownership validation, model-input isolation, and exclusion route tests.
- `internal/arcadedb/memory_recall.go`, `memory_recall_browse.go`, `memory_recall_exclusion.go` — Bounded internal negative scope across semantic ranking, hydration, recent, open, and scroll modes.

## Decisions Made

- The existing per-turn request ID is the carrier's `turn_id`. Matching it to `X-Aura-Actor-Run-Id` gives the server a second host-derived value to revalidate before consulting storage.
- Unknown or foreign conversation IDs fail closed after one ownership lookup and before the recall query. Valid IDs are sorted and forwarded only as `excluded_conversation_ids`.
- Negative filtering happens before rank fusion and again during hydration. The second check is deliberate defense against stale/noncompliant backend responses; neither layer can add a candidate or alter OAuth identity.
- No environment variable, schema field, sibling retrieval tool, MCP `_meta` field, or new dependency was introduced.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical correctness] Extended the internal recall engine with negative-only scope**
- **Found during:** Task 2 GREEN
- **Issue:** The plan's file list ended at the MCP handler, but passing validated exclusions into `RecallMemory` as pre-ranking negative filters was impossible without an internal request field and storage-layer SQL/hydration support. Filtering only the final MCP output would still let active candidates consume the bounded rank set and violate D-06.
- **Fix:** Added `ExcludeConversationIDs` plus a focused `memory_recall_exclusion.go`, injected parameterized `NOT IN` clauses into semantic/recent ranking and hydration, defensively removed excluded rows, and made open/scroll abstain after their normal input validation.
- **Files modified:** `internal/arcadedb/memory_recall.go`, `internal/arcadedb/memory_recall_browse.go`, `internal/arcadedb/memory_recall_exclusion.go`
- **Verification:** Named suppression test, complete `internal/arcadedb` regression, repository vet/build, and WSL-native race all pass.
- **Committed in:** `263848a5d`

**2. [Rule 3 - Blocking state close-out] Synchronized out-of-order activity metadata**
- **Found during:** Sequential plan metadata close-out
- **Issue:** `state.update-progress` correctly refused to advance the current plan because Plans 49-04/05 remain incomplete, while the other state handlers updated the canonical completed count and session but left the prose progress and last-activity fields on Plan 49-03.
- **Fix:** Preserved `current_plan: 4`, synchronized canonical/prose last activity to Plan 49-08, and reconciled the milestone progress display to the handler-counted 54/62 completed plans.
- **Files modified:** `.planning/STATE.md`
- **Verification:** STATE keeps Plan 4 as the next sequential slot, reports 54 completed plans, stops at Plan 49-08, and its canonical/prose activity fields agree.
- **Committed in:** final plan metadata commit

---

**Total deviations:** 2 auto-fixed (1 Rule 2 correctness completion, 1 Rule 3 state close-out correction).
**Impact on plan:** The internal files are the minimum layer required to honor the plan's explicit exclusion-only contract, and the state correction preserves the real next incomplete plan. Neither adds model surface, authority, protocol, dependency, or storage schema.

## Issues Encountered

- Windows-native `go test -race` cannot run without cgo. The project-authoritative WSL Go 1.26.6 toolchain ran every touched package under `-race` successfully.
- The normal pre-commit hook rejected two intermediate commits for modernize findings (`maps.Copy`, `slices.Contains`). Both were corrected and recommitted with hooks enabled; no hook was bypassed.

## TDD Gate Compliance

- **Task 1 RED:** `f53bee04a` compiled and failed only because the session accessor and composed carrier intentionally emitted no current context.
- **Task 1 GREEN:** `bb7362a68` passed fresh reused-session values, canonical decode, actor preservation, bounds, package regression, and WSL race; the automated tracer feedback gate repeated the full verification from committed state.
- **Task 2 RED:** `e6725150d` failed on missing production recall-header composition and the not-yet-implemented server suppression path.
- **Task 2 GREEN:** `263848a5d` passed malformed/over-cap/ambiguous/foreign/actor-mismatch cases, authenticated historical-only output, SQL negative-scope assertions, full package regression, and WSL race.
- **REFACTOR:** No behavior-neutral commit was needed; the focused exclusion helper was required during GREEN to keep responsibilities and file sizes bounded.

## Verification Evidence

- Named Task 1 and Task 2 inventories discovered all required tests; no target reported `no tests to run`.
- `go vet ./...` and `go build ./...` pass.
- Windows unit regression passes for `internal/agent/tools`, `internal/agent/mcptools`, `cmd/arcadedb-mcp`, and `internal/arcadedb`.
- WSL Go 1.26.6 `go test -race` passes for all four touched packages.
- The strict schema/_meta negative test proves model arguments cannot assert an exclusion; the only consumer is `memory_recall`.

## Known Stubs

None.

## User Setup Required

None - no new environment variable, dependency, service, migration, or manual configuration is required.

## Next Phase Readiness

- Plan 49-13 can seed an active and historical conversation and exercise the carrier through the published OAuth-authenticated route against live ArcadeDB.
- The stable mixed recall surface from Plan 49-03 remains additive and byte-compatible for callers that send no host active-source header.

## Self-Check: PASSED

- All eleven declared implementation/test files and this SUMMARY exist.
- Task commits `f53bee04a`, `bb7362a68`, `e6725150d`, and `263848a5d` exist in RED → GREEN order with no tracked-file deletion.
- Coverage metadata classifies all three deliverables as automatically covered by passing evidence.
- No skipped test, unrun verification, model-visible active-source field, or known stub was introduced.

---
*Phase: 49-memory-tiers*
*Completed: 2026-08-31*
