---
phase: 49-memory-tiers
plan: 03
subsystem: memory
tags: [arcadedb, memory-recall, reciprocal-rank-fusion, mcp, opentelemetry, cursor, tdd]

requires:
  - phase: 49-02
    provides: "Identity-scoped Conversation/ConversationTurn projection and hybrid search"
  - phase: 49-07
    provides: "Production conversation schema registration and post-commit reconciliation lifecycle"
provides:
  - "One memory_recall call returning typed fact and historical-conversation evidence"
  - "ArcadeDB-native nested RRF with separate evidence-tier and executed-backend metadata"
  - "Bounded recent/open/scroll modes with strict identity-bound canonical cursors"
affects: [49-04, 49-08, 49-13, MEM-02, TOOL-05]

actuals:
  tokens: 21007
  tasks: 2
  commits: 6

tech-stack:
  added: []
  patterns:
    - "Independently rank fact and conversation tiers, then fuse them inside ArcadeDB with rank-only RRF"
    - "Report evidence contribution independently from the graph/hybrid/lexical backend that executed"
    - "Treat base64url cursor state as unsigned, bounded, and untrusted on every use"

key-files:
  created:
    - internal/arcadedb/memory_recall.go
    - internal/arcadedb/memory_recall_browse.go
    - internal/arcadedb/memory_recall_test.go
    - internal/arcadedb/memory_recall_live_test.go
    - cmd/arcadedb-mcp/tool_memory_recall.go
    - cmd/arcadedb-mcp/tool_memory_recall_test.go
    - cmd/arcadedb-mcp/memory_recall_live_integration_test.go
  modified:
    - cmd/arcadedb-mcp/tool_memory.go
    - cmd/arcadedb-mcp/tool_memory_retrieval_test.go
    - docs/arcadedb-mcp-live-tools.json
    - go.mod

key-decisions:
  - "Nested ArcadeDB vector.fuse calls independently rank dense+lexical fact and conversation sources before a final RRF; no Go fusion helper or fixed tier quota exists."
  - "retrieval.effective_path derives only from returned evidence, while retrieval.path derives only from the backend branch that executed; the same values populate OTel."
  - "Open/scroll cursors use versioned canonical JSON in unpadded base64url and carry only identity, conversation, stable anchor, direction, and clamped page size."
  - "Reasoning is advertised only as the reserved explicit mode and abstains until Plan 49-04 owns its graph contract."

patterns-established:
  - "Mixed recall hydrates by engine rank, deduplicates identical fact/conversation prose, and expands conversation hits into bounded chronological windows."
  - "Cursor decode applies encoded/decoded size ceilings, rejects unknown fields and RID-shaped identifiers, then revalidates every request constraint before query execution."

requirements-completed: [MEM-02, TOOL-05]

coverage:
  - id: D1
    description: "One OAuth-authenticated memory_recall invocation returns fact and historical conversation evidence with mixed contribution and hybrid backend metadata."
    requirement: MEM-02
    verification:
      - kind: unit
        ref: "cmd/arcadedb-mcp/tool_memory_recall_test.go#TestMemoryRecallMixedTierTracer"
        status: pass
      - kind: integration
        ref: "cmd/arcadedb-mcp/memory_recall_live_integration_test.go#TestAgentMemoryMCPLiveMixedTierRecall under WSL -race"
        status: pass
    human_judgment: false
  - id: D2
    description: "The response and OTel agree on hybrid/graph/lexical backend paths independently from facts/conversations/mixed evidence contribution."
    requirement: TOOL-05
    verification:
      - kind: unit
        ref: "cmd/arcadedb-mcp/tool_memory_recall_test.go#TestMemoryRecallBackendPath"
        status: pass
      - kind: integration
        ref: "internal/arcadedb/memory_recall_live_test.go#TestMemoryRecallLive_VectorFuseMixedTier"
        status: pass
    human_judgment: false
  - id: D3
    description: "Recent/open/scroll stay bounded and reject malformed, oversized, foreign, wrong-direction, stale-anchor, over-cap, and RID-bearing cursor state before database access."
    requirement: MEM-02
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_recall_test.go#TestMemoryRecallWindow and TestRecallCursorFailsClosedBeforeQuery"
        status: pass
      - kind: integration
        ref: "internal/arcadedb/memory_recall_live_test.go#TestMemoryRecallLive_BrowseCursor under WSL -race"
        status: pass
    human_judgment: false
  - id: D4
    description: "memory_recall remains the sole model-facing memory retrieval and advertises exactly semantic, recent, open, scroll, and reserved reasoning modes."
    requirement: TOOL-05
    verification:
      - kind: unit
        ref: "cmd/arcadedb-mcp/tool_memory_recall_test.go#TestMemoryRecallModeContract"
        status: pass
      - kind: unit
        ref: "internal/agent/mcptools/bridge_memory_surface_test.go#TestMemorySurfacePolicy_AliasKeepsIsolationAndHiddenSurface"
        status: pass
    human_judgment: false

duration: 51 min
completed: 2026-08-31
status: complete
---

# Phase 49 Plan 03: Unified Mixed-Tier Memory Recall Summary

**One OAuth-scoped `memory_recall` now fuses facts and historical conversations inside ArcadeDB, returns bounded typed evidence, and supports identity-revalidated browse cursors.**

## Performance

- **Duration:** 51 min
- **Started:** 2026-08-31T22:12:17Z
- **Completed:** 2026-08-31T23:03:38Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Replaced fact-only recall with independent fact/conversation ranking and a final nested ArcadeDB `vector.fuse` RRF, retaining engine order without a Go fusion layer or fixed quota.
- Added typed fact and bounded conversation-window evidence, cross-tier prose deduplication, explicit abstention, legacy `facts`, stable rank/provenance, and separate `effective_path` versus backend `path` metadata.
- Added `recent`, `open`, and `scroll` modes plus strict versioned base64url cursors whose identity, conversation, anchor, direction, page size, unknown fields, and RID-shaped values are revalidated before access.
- Emitted bounded `memory.recall.*` span attributes from the same result metadata returned to the caller, including tier candidates/counts, mode, abstention reason, and backend latency.
- Proved the nested fusion SQL, cursor paging, and published OAuth-authenticated MCP call against live ArcadeDB 26.8.1 and the 768-dimensional EmbeddingGemma sidecar under WSL `-race`.

## Task Commits

1. **Task 1 RED: Mixed evidence, native fusion, backend paths, and abstention contracts** — `dee0cad4d` (`test`)
2. **Task 1 GREEN: Unified mixed-tier recall and route telemetry** — `a1cd80805` (`feat`)
3. **Task 2 RED: Mode, window, and untrusted cursor contracts** — `1d714e415` (`test`)
4. **Task 2 GREEN: Bounded recent/open/scroll cursors and exact mode schema** — `ceb67eb1f` (`feat`)
5. **Published-path evidence: OAuth-authenticated live mixed recall** — `99e68753a` (`test`)

## Files Created/Modified

- `internal/arcadedb/memory_recall.go` — Unified semantic/entity recall, nested native fusion, hydration, typed evidence, dedupe, abstention, and route counts.
- `internal/arcadedb/memory_recall_browse.go` — Recent/open/scroll dispatch, bounded window reads, canonical cursor codec, and fail-closed replay validation.
- `internal/arcadedb/memory_recall_test.go` — Native fusion/order, path, abstention, mode, page cap, and adversarial cursor tests.
- `internal/arcadedb/memory_recall_live_test.go` — Disposable-engine mixed RRF and cursor traversal tests.
- `cmd/arcadedb-mcp/tool_memory_recall.go` — Additive public schema, typed response conversion, mode enum, and OTel attributes.
- `cmd/arcadedb-mcp/tool_memory_recall_test.go` — Handler tracer, response/OTel equality, and advertised-mode contract.
- `cmd/arcadedb-mcp/memory_recall_live_integration_test.go` — Real OAuth/SDK published-call proof spanning both tiers.
- `cmd/arcadedb-mcp/tool_memory.go` — Removed the extracted legacy recall handler while preserving fact mutation/search helpers.
- `cmd/arcadedb-mcp/tool_memory_retrieval_test.go` — Updated the legacy semantic fixture to the native ranked-source/hydration flow.
- `docs/arcadedb-mcp-live-tools.json` — Regenerated the exact additive input/output contract.
- `go.mod` — Promoted the MCP SDK's existing `jsonschema-go` dependency to direct use for the exact mode enum.

## Decisions Made

- The two tier rankings are themselves native dense+lexical `vector.fuse` sources, and their final combination is another native RRF. The live test demonstrated this nested shape on the deployed engine.
- Tier contribution and backend execution remain orthogonal: mixed evidence can come from the hybrid backend, a fact-only entity lookup reports graph, and failed embeddings report lexical without rewriting contribution.
- Cursor state is deliberately unsigned. It grants no authority: server-side OAuth identity and explicit request fields must match the decoded values before any ArcadeDB query.
- Browse logic lives in a focused sibling file because adding it to the 504-line semantic implementation would violate the hard 600-line production ceiling.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Project constraint] Split browse/cursor logic and added live proof files**
- **Found during:** Tasks 1 and 2 GREEN
- **Issue:** The planned single `memory_recall.go` would exceed 600 lines once strict cursor/browse behavior landed, while the action required engine-backed and published live proof not represented in the file list.
- **Fix:** Kept semantic fusion in `memory_recall.go`, moved browse/cursor logic to `memory_recall_browse.go`, and added focused internal plus MCP live integration files.
- **Files modified:** `internal/arcadedb/memory_recall_browse.go`, `internal/arcadedb/memory_recall_live_test.go`, `cmd/arcadedb-mcp/memory_recall_live_integration_test.go`
- **Verification:** Production files are 504/304 lines; all live tests pass against disposable tenants under WSL `-race`.
- **Committed in:** `a1cd80805`, `ceb67eb1f`, `99e68753a`

**2. [Rule 3 - Blocking verification] Synchronized the generated MCP contract and legacy recall fixture**
- **Found during:** Task 1 full package regression
- **Issue:** The generated live-tools golden no longer matched the additive schema, and the pre-existing fact-only semantic test returned a direct fact row where the new engine contract first returns ranked RIDs.
- **Fix:** Regenerated the canonical manifest from the running in-memory server and changed the legacy fixture to model ranking followed by fact/turn hydration without weakening its path assertions.
- **Files modified:** `docs/arcadedb-mcp-live-tools.json`, `cmd/arcadedb-mcp/tool_memory_retrieval_test.go`
- **Verification:** `TestToolManifestMatchesServer` and the complete `cmd/arcadedb-mcp` package pass.
- **Committed in:** `a1cd80805`, `ceb67eb1f`

**3. [Rule 3 - Blocking state close-out] Corrected stale last-activity metadata**
- **Found during:** Sequential tracking update
- **Issue:** The state handlers advanced the plan and milestone count but retained Plan 49-07 as `last_activity_desc` and the prose last activity after completing the sequential Plan 49-03 slot.
- **Fix:** Updated both canonical frontmatter and prose activity fields to Plan 49-03 while preserving the handler-written plan pointer, metrics, decisions, session timestamp, and state head.
- **Files modified:** `.planning/STATE.md`
- **Verification:** STATE reports current plan 4, completed plans 53, stopped at Plan 49-03, and matching Plan 49-03 activity fields.
- **Committed in:** final plan metadata commit

---

**Total deviations:** 3 auto-fixed (1 project-constraint/live-proof completion, 2 blocking close-out/contract synchronizations).
**Impact on plan:** Both changes enforce the planned behavior and Aura's quality gates; no sibling retrieval tool, environment variable, migration, or new runtime dependency was introduced.

## Issues Encountered

- Task 2's `read_first` named the nonexistent `cmd/arcadedb-mcp/tool_document_retrieval.go`; the tracked phase analog is `internal/arcadedb/document_retrieval.go`, which was read and followed.
- The first two Task 1 RED commit attempts were rejected by the normal revive hook until every exported enum value had an explicit comment. The comments were added and all commits ran with hooks enabled.
- The shared checkout retained the pre-existing `.planning/state.json` modification and untracked `.planning/milestone.lock`; every commit used cached-index inspection and explicit `--only` allowlists, so neither entered Plan 49-03 history.

## TDD Gate Compliance

- **Task 1 RED:** `dee0cad4d` compiled and failed only on the intentional `RecallMemory` not-implemented sentinel across mixed evidence, path, native fusion, and abstention cases.
- **Task 1 GREEN:** `a1cd80805` passed named unit suites, vet/build, package regression, WSL race, and a disposable real-engine nested-RRF probe; the tracer feedback gate repeated the complete automated verification from committed state.
- **Task 2 RED:** `1d714e415` failed on unsupported browse/reserved modes, absent cursor validation, and a missing advertised mode enum.
- **Task 2 GREEN:** `ceb67eb1f` passed omitted-mode compatibility, exact enum, bounded recent/open/scroll, every adversarial cursor case, package regression, WSL race, and a live two-page cursor traversal.
- **Additional live coverage:** `99e68753a` proves the final OAuth-authenticated MCP surface returns both evidence tiers in one call.
- **REFACTOR:** No behavior-neutral commit was needed; the required file split landed during GREEN to satisfy the repository ceiling.

## Verification Evidence

- Named Task 1 and Task 2 inventories found all required tests; no target reported `no tests to run`.
- `go vet ./...`, `go build ./...`, and full `internal/arcadedb` plus `cmd/arcadedb-mcp` package tests pass.
- WSL Go 1.26.6 unit suites pass under `-race` for both touched packages.
- WSL tagged live suite under `-race`: `TestMemoryRecallLive_VectorFuseMixedTier`, `TestMemoryRecallLive_BrowseCursor`, and `TestAgentMemoryMCPLiveMixedTierRecall` all pass against disposable ArcadeDB 26.8.1 tenants.
- `TestMemorySurfacePolicy_AliasKeepsIsolationAndHiddenSurface` proves `memory_recall` remains the sole model-facing retrieval.
- `python scripts/agent_memory_eval.py --tier deterministic` reports `DETERMINISTIC_PASS`; final Phase 49 evidence remains owned by later live plans.
- No tracked deletion, skipped test, unrun verification, migration, environment variable, or sibling memory retrieval tool was introduced.

## Known Stubs

| Stub | File | Line | Reason |
|------|------|------|--------|
| Reserved `reasoning` mode returns `reasoning_not_available` | `internal/arcadedb/memory_recall.go` | 186 | Intentional Plan 49-03 contract; Plan 49-04 owns the explicit reasoning graph and retrieval implementation. |

## User Setup Required

None - no new package, service, environment variable, or manual configuration is required.

## Next Phase Readiness

- Plan 49-04 can connect its isolated reasoning graph to the already-reserved explicit mode without changing ordinary semantic or browse paths.
- Plans 49-08/49-13 can add host-derived active-context exclusions and final evaluator evidence on the stable one-tool contract.
- No blocker remains for dependent plans.

## Self-Check: PASSED

- All seven declared implementation/test artifacts and this SUMMARY exist.
- Task commits `dee0cad4d`, `a1cd80805`, `1d714e415`, `ceb67eb1f`, and `99e68753a` exist.
- Coverage classification reports 4/4 deliverables auto-covered with passing evidence.
- No tracked-file deletion, skipped test, or unrun verification remains; the one intentional reserved-mode stub is recorded above and in `.planning/WINDOWS.md` for Plan 49-04.

---
*Phase: 49-memory-tiers*
*Completed: 2026-08-31*
