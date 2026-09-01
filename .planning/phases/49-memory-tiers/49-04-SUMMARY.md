---
phase: 49-memory-tiers
plan: 04
subsystem: memory
tags: [arcadedb, reasoning-memory, mcp, retention, redaction, tdd]

requires:
  - phase: 49-01
    provides: "Measured PRD Amendment #201 before reasoning-tier implementation"
  - phase: 49-03
    provides: "Unified memory_recall surface with a reserved explicit reasoning mode"
  - phase: 49-08
    provides: "OAuth-first active-context handling without model-supplied authority"
provides:
  - "Shared identity-scoped ReasoningTrace/ReasoningStep/ReasoningToolCall schema with five audit edges"
  - "Explicit-only reasoning search and trace traversal through memory_recall"
  - "Exact 30-day successful and 7-day failed/cancelled terminal retention"
affects: [49-09, 49-11, 49-12, MEM-03, CTX-05, memory-recall]

actuals:
  tokens: 19084
  tasks: 3
  commits: 7

tech-stack:
  added: []
  patterns:
    - "Reasoning is a separate graph/query domain reached only by explicit MCP dispatch"
    - "Persist complete reasoning graphs in one ArcadeDB transaction after boundary validation"
    - "Terminal retention is a maximum class cap that source or existing expiry may only shorten"

key-files:
  created:
    - internal/arcadedb/memory_reasoning.go
    - internal/arcadedb/memory_reasoning_validate.go
    - internal/arcadedb/memory_reasoning_test.go
    - cmd/arcadedb-mcp/tool_memory_reasoning_test.go
  modified:
    - internal/arcadedb/memory.go
    - cmd/arcadedb-mcp/tool_memory_recall.go
    - docs/arcadedb-mcp-live-tools.json
    - internal/config/config_retention.go
    - internal/config/config_retention_test.go

key-decisions:
  - "Intercept mode=reasoning in the unified MCP handler; semantic/recent/open/scroll continue through the existing RecallMemory path and cannot query reasoning storage."
  - "Persist only provider-visible summaries, SHA-256 argument digests, redacted bounded observations, allowlisted artifact references, and validated entity references."
  - "Apply exact status retention before every reasoning upsert; an existing or authoritative source expiry may shorten but never extend the class cap."

patterns-established:
  - "ReasoningTrace writes are atomic: trace, initiating turn, ordered steps, tool calls, INVOKED, and TOUCHED edges commit together."
  - "Reasoning reads repeat the identity predicate for trace, step, and tool hydration and discard foreign rows defensively."

requirements-completed: [MEM-03, CTX-05]

coverage:
  - id: D1
    description: "EnsureMemorySchema installs the complete reasoning graph fragment after ordinary memory, and Amendment #201 remains the ancestor of every protected path touched so far."
    requirement: MEM-03
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_reasoning_test.go#TestEnsureMemorySchemaRegistersReasoningSchema and TestReasoningSchemaStatements"
        status: pass
      - kind: other
        ref: "git diff-tree/merge-base Amendment #201 six-path intermediate ancestry gate"
        status: pass
    human_judgment: false
  - id: D2
    description: "Only explicit owner reasoning recall can search or traverse bounded provider-visible traces; ordinary recall modes issue zero reasoning queries."
    requirement: CTX-05
    verification:
      - kind: unit
        ref: "cmd/arcadedb-mcp/tool_memory_reasoning_test.go#TestReasoningRecallExplicitOnly and TestReasoningRecallIdentity"
        status: pass
      - kind: unit
        ref: "internal/arcadedb/memory_reasoning_test.go#TestReasoningToolMetadataBounded and TestReasoningRecallIdentity"
        status: pass
    human_judgment: false
  - id: D3
    description: "Successful reasoning expires after exactly 30 days, failed/cancelled reasoning after exactly 7, with source deletion and shorter existing caps authoritative."
    requirement: MEM-03
    verification:
      - kind: unit
        ref: "internal/config/config_retention_test.go#TestReasoningRetentionPolicy"
        status: pass
      - kind: unit
        ref: "internal/arcadedb/memory_reasoning_test.go#TestReasoningTerminalExpiry"
        status: pass
    human_judgment: false

duration: 33min
completed: 2026-09-01
status: complete
---

# Phase 49 Plan 04: Explicit Reasoning Graph and Retention Summary

**Provider-visible reasoning now persists as an identity-scoped ArcadeDB graph, is reachable only through explicit `memory_recall` reasoning mode, and expires under exact status-aware caps.**

## Performance

- **Duration:** 33 min
- **Started:** 2026-08-31T23:58:49Z
- **Completed:** 2026-09-01T00:31:30Z
- **Tasks:** 3
- **Files modified:** 9

## Accomplishments

- Registered `ReasoningTrace`, `ReasoningStep`, and `ReasoningToolCall` plus `INITIATED_BY`, `HAS_STEP`, `NEXT`, `INVOKED`, and `TOUCHED` through the one shared memory initializer, with replay-safe identity/source/status/expiry/search indexes.
- Added transactional trace persistence, bounded hybrid/lexical summary search, ordered exact traversal, and additive MCP output behind `mode=reasoning`; every ordinary recall mode retains its existing non-reasoning path.
- Froze successful traces at 30 days and failed/cancelled traces at 7 days from terminal status, rejecting invalid/widened policy while allowing only earlier source or existing expiry.

## Task Commits

1. **Task 1 RED: Reasoning schema and shared-initializer contract** — `57d18e96c` (`test`)
2. **Task 1 GREEN: Identity-scoped reasoning schema registration** — `7e5615843` (`feat`)
3. **Task 2 RED: Explicit-only recall and bounded metadata contract** — `ecfbf817d` (`test`)
4. **Task 2 GREEN: Transactional reasoning storage and explicit MCP dispatch** — `829f53ba9` (`feat`)
5. **Task 3 RED: Exact status-aware retention contract** — `4a10d52ce` (`test`)
6. **Task 3 GREEN: 30-day/7-day terminal retention** — `37fe5bfb6` (`feat`)

## Files Created/Modified

- `internal/arcadedb/memory_reasoning.go` — Domain schema, typed graph model, transactional writer, bounded search/traversal, and terminal expiry policy.
- `internal/arcadedb/memory_reasoning_validate.go` — Provider-visible evidence allowlist, credential redaction, digest/blob/reference validation, and storage parameter mapping.
- `internal/arcadedb/memory.go` — Registers the reasoning fragment after the ordinary conversation schema.
- `internal/arcadedb/memory_reasoning_test.go` — Schema, identity, storage-boundary, redaction, and fake-clock retention contracts.
- `cmd/arcadedb-mcp/tool_memory_recall.go` — Additive `trace_id` selector, explicit reasoning branch, bounded public result types, and telemetry.
- `cmd/arcadedb-mcp/tool_memory_reasoning_test.go` — Ordinary-mode zero-query spies and explicit owner traversal tests.
- `docs/arcadedb-mcp-live-tools.json` — Regenerated canonical MCP schema for explicit reasoning recall.
- `internal/config/config_retention.go`, `config_retention_test.go` — Typed exact reasoning TTL classes and validation.

## Decisions Made

- The MCP handler branches before ordinary recall only for explicit `mode=reasoning`. This makes the isolation boundary visible in one place and leaves semantic, recent, open, scroll, compaction, and proactive context code unchanged.
- Similarity search returns bounded trace summaries; `trace_id` performs the progressively disclosed full step/tool traversal. Both repeat the authenticated identity predicate at every hydration layer.
- The graph writer redacts at its persistence chokepoint even when an upstream ledger already sanitized the observation. Raw arguments have no field; only lowercase SHA-256 digests cross the boundary.
- `SetTerminalExpiry` is monotone: status selects a maximum, and any already-shorter or source-authoritative cap remains shorter. Reads cannot refresh it because all reasoning read statements are `SELECT` only.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Project constraint] Split reasoning validation from the storage/search implementation**
- **Found during:** Task 2 GREEN
- **Issue:** Schema, writer, search/traversal, and the required security boundary would exceed the repository's 600-line production ceiling in the single planned file.
- **Fix:** Kept the graph/storage contract in `memory_reasoning.go` (534 lines) and moved validation, redaction, and parameter mapping into `memory_reasoning_validate.go` (203 lines).
- **Files modified:** `internal/arcadedb/memory_reasoning.go`, `internal/arcadedb/memory_reasoning_validate.go`
- **Verification:** Normal file-size, vet, lint, unit, and race gates pass with hooks enabled.
- **Committed in:** `829f53ba9`

**2. [Rule 3 - Blocking generated contract] Regenerated the canonical MCP manifest**
- **Found during:** Task 2 full package regression
- **Issue:** `TestToolManifestMatchesServer` correctly rejected the stale golden after `trace_id` and the bounded reasoning result shape became public.
- **Fix:** Regenerated `docs/arcadedb-mcp-live-tools.json` from the in-memory server and reran the exact manifest test.
- **Files modified:** `docs/arcadedb-mcp-live-tools.json`
- **Verification:** `TestToolManifestMatchesServer` and the complete `cmd/arcadedb-mcp` package pass.
- **Committed in:** `829f53ba9`

**3. [Rule 3 - Blocking state close-out] Synchronized sequential activity and progress metadata**
- **Found during:** Plan metadata close-out
- **Issue:** `state.advance-plan` correctly advanced to Plan 5 and 55 completed plans, while `state.update-progress` declined the unscoped phase and left prose activity/progress on out-of-order Plan 49-08.
- **Fix:** Preserved the handler-written plan pointer and canonical count, then synchronized canonical/prose activity, 55/62 progress, session continuity, and the next action to Plan 49-05.
- **Files modified:** `.planning/STATE.md`
- **Verification:** STATE reports current plan 5, 55 completed plans, stopped at Plan 49-04, and matching Plan 49-04 activity fields.
- **Committed in:** final plan metadata commit

---

**Total deviations:** 3 auto-fixed (1 Rule 2 project constraint, 2 Rule 3 contract/state synchronizations).
**Impact on plan:** Both changes enforce the planned security/public-contract behavior without adding a dependency, model-facing tool, automatic reasoning path, or storage authority.

## Issues Encountered

- Context7 was unavailable, so implementation used the official ArcadeDB SQL type/property/index/edge, transaction, traversal, and vector-search documentation before writing database code.
- The first Task 2 RED commit attempt was rejected by the normal modernize lint hook (`omitzero`, `reflect.TypeFor`). The findings were corrected and the commit was retried with hooks enabled; no hook was bypassed.
- Windows-native race is not authoritative on this host. The final race gates ran under WSL Go 1.26.6 with CGO enabled, matching project policy.

## TDD Gate Compliance

- **Task 1 RED:** `57d18e96c` failed only because the shared initializer emitted no reasoning schema statements.
- **Task 1 GREEN:** `7e5615843` passed the exact inventory, replay/order, amendment isolation/ancestry, package regression, and WSL race; the automated tracer feedback gate repeated the full verification from committed state.
- **Task 2 RED:** `ecfbf817d` failed only on the intentional store/search/traversal stubs and reserved public mode while ordinary-mode zero-query assertions already held.
- **Task 2 GREEN:** `829f53ba9` passed owner/foreign/missing identity, redaction, digest/blob/reference bounds, public schema, package regression, and WSL race.
- **Task 3 RED:** `4a10d52ce` failed only on zero default TTLs and the intentional terminal-expiry stub.
- **Task 3 GREEN:** `37fe5bfb6` passed exact fake-clock durations, invalid values, no-widening, source precedence, no-refresh reads, package regression, and WSL race.
- **REFACTOR:** No behavior-neutral commit was needed; the production file split was required during Task 2 GREEN to satisfy the hard line ceiling.

## Verification Evidence

- Amendment #201 (`f231f15b5`) changes only `prd.md`; every protected path touched in Phase 49 descends from it, while future Plan 49 paths remain permitted to be untouched.
- All named schema, isolation, identity, metadata, and retention inventories discover tests and pass; no target reported `no tests to run`.
- `go vet ./...`, `go build ./...`, and complete unit regressions for `internal/config`, `internal/arcadedb`, and `cmd/arcadedb-mcp` pass.
- WSL Go 1.26.6 `go test -race` passes for all three touched packages.
- The canonical MCP manifest matches the server; no tracked-file deletion, skipped test, unrun plan verification, migration, environment variable, or runtime dependency was introduced.

## Known Stubs

None.

## User Setup Required

None - no new dependency, environment variable, migration, service, or manual configuration is required.

## Next Phase Readiness

- Plan 49-12 can build provider-authorized production traces into the initialized writer and explicit recall surface without changing the public contract.
- Plan 49-09 can own the retention worker, operator catalogue wiring, and live deletion/expiry races over the exact policy frozen here.
- Plan 49-11 can require the final non-empty six-path Amendment #201 ancestry and running-Aura reasoning-isolation evidence.

## Self-Check: PASSED

- All nine implementation/test/contract files and this SUMMARY exist.
- Task commits `57d18e96c`, `7e5615843`, `ecfbf817d`, `829f53ba9`, `4a10d52ce`, and `37fe5bfb6` exist in RED → GREEN order.
- Coverage classification reports 3/3 deliverables automatically covered by passing evidence.
- No tracked-file deletion, known stub, skipped test, or unrun plan verification remains.

---
*Phase: 49-memory-tiers*
*Completed: 2026-09-01*
