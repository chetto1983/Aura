---
phase: 49-memory-tiers
plan: 09
subsystem: memory
tags: [arcadedb, reasoning-memory, retention, deletion, isolation, tdd]

requires:
  - phase: 49-04
    provides: "Identity-scoped explicit reasoning graph and exact terminal retention classes"
  - phase: 49-12
    provides: "Provider-authorized post-commit reasoning producer and narrow sink contract"
provides:
  - "One production tenant reasoning store shared by trace persistence, bounded expiry, and immediate deletion"
  - "Identity-scoped atomic trace-subgraph cleanup with expiry/explicit race convergence"
  - "Graph-resident live proof that automatic context paths remain reasoning-free"
affects: [49-11, MEM-03, MEM-06, CTX-05, reasoning-lifecycle]

actuals:
  tokens: 13468
  tasks: 3
  commits: 8

tech-stack:
  added: []
  patterns:
    - "One tenant-aware reasoning store is shared across producer, retention, and deletion seams"
    - "The existing DeleteReconciler owns bounded reasoning expiry and joins it at shutdown"
    - "Source deletion is fail-honest and precedes PostgreSQL removal"

key-files:
  created:
    - cmd/aura/chat_boot_memory.go
    - internal/arcadedb/memory_reasoning_lifecycle.go
    - internal/arcadedb/memory_reasoning_lifecycle_test.go
    - internal/arcadedb/memory_reasoning_live_test.go
    - internal/runner/runner_reasoning_delete_test.go
    - internal/runner/runner_reasoning_retention_test.go
  modified:
    - cmd/aura/chat_boot.go
    - internal/runner/runner_delete.go
    - internal/runner/runner_delete_reconcile.go
    - internal/config/config_retention.go
    - internal/conversations/history_reasoning_free_test.go
    - .env.example

key-decisions:
  - "Reasoning retention overrides may shorten the 30-day successful and 7-day failed/cancelled maximum classes, but validation rejects widening either class."
  - "Conversation deletion removes its reasoning subgraph before PostgreSQL; a graph failure preserves the authoritative source so the operator can retry safely."
  - "Expiry and explicit deletion retry the complete identity-scoped transaction after ArcadeDB 409/503 conflicts and treat an already-absent root as success."
  - "Reasoning composition assigns only dedicated sink/deletion dependencies and cannot replace the digest/preload MemoryContextProvider."

patterns-established:
  - "Privacy lifecycle: authoritative source remains until derived reasoning deletion succeeds."
  - "Capacity evidence is measured independently from policy: storage growth does not set retention duration."

requirements-completed: [MEM-03, MEM-06, CTX-05]

coverage:
  - id: D1
    description: "Production boot owns one validated reasoning sink and one bounded status-aware expiry lifecycle with joined shutdown."
    requirement: MEM-03
    verification:
      - kind: unit
        ref: "internal/runner/runner_reasoning_retention_test.go#TestReasoningRetentionWorker and TestReasoningRetentionClose"
        status: pass
      - kind: unit
        ref: "cmd/aura/chat_boot_test.go#TestReasoningRetentionBoot"
        status: pass
      - kind: other
        ref: "WSL go test -race ./internal/runner ./internal/arcadedb ./cmd/aura -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: "Conversation, source, operator, identity, repeated, and concurrent expiry deletion remove complete identity-scoped trace subgraphs."
    requirement: MEM-03
    verification:
      - kind: integration
        ref: "internal/arcadedb/memory_reasoning_live_test.go#TestReasoningGraphLive_DeletionPrecedence and TestReasoningGraphLive_ExpiryDeleteRace"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_reasoning_delete_test.go#TestReasoningDeletionPrecedence"
        status: pass
    human_judgment: false
  - id: D3
    description: "Graph-resident reasoning is available only to explicit owner reads and remains absent from ordinary recall, history, compaction, and preload composition."
    requirement: CTX-05
    verification:
      - kind: integration
        ref: "internal/arcadedb/memory_reasoning_live_test.go#TestReasoningGraphLive_ExplicitIsolation and TestReasoningGraphLive_FailedCancelledRetention"
        status: pass
      - kind: unit
        ref: "internal/conversations/history_reasoning_free_test.go#TestHistoryReasoningFree_GraphResidentFixture and cmd/aura/chat_boot_test.go#TestChatBootReasoningIsolation"
        status: pass
    human_judgment: false

duration: 1h 1m
completed: 2026-09-01
status: complete
---

# Phase 49 Plan 09: Reasoning Lifecycle and Isolation Summary

**Provider-visible reasoning now has one production-owned tenant lifecycle: status-bounded expiry, immediate source-first deletion, and explicit-only live retrieval.**

## Performance

- **Duration:** 1h 1m
- **Started:** 2026-09-01T02:21:58Z
- **Completed:** 2026-09-01T03:22:41Z
- **Tasks:** 3
- **Files modified:** 19

## Accomplishments

- Composed one `tenantReasoningMemory` at chat boot and shared it across the Plan 49-12 producer sink, the existing boot-owned `DeleteReconciler`, and immediate deletion; shutdown cancels and joins the single worker.
- Added validated operator overrides whose defaults remain exactly 30 days for successful traces and 7 days for failed/cancelled traces, with wider/non-positive policies rejected before boot.
- Added identity-scoped ArcadeDB transactions that select bounded expiry pages, remove five edge families plus step/tool/root vertices, retry full decisions after live 409/503 conflicts, and converge on repeated/competing deletion.
- Proved on real ArcadeDB 26.8.1 that ordinary semantic/recent/open/scroll recall makes zero reasoning reads, explicit owner recall succeeds, foreign recall fails closed, and failed/cancelled traces remain absent from automatic context throughout their seven-day lifetime.

## Task Commits

Each TDD task was committed in RED then GREEN order:

1. **Task 1 RED: Retention composition, cutoff, page, and close contracts** - `b9155aa7c` (`test`)
2. **Task 1 GREEN: One boot-owned bounded reasoning lifecycle** - `6e60a846f` (`feat`)
3. **Task 2 RED: Immediate deletion and live race contracts** - `e95eb9e5e` (`test`)
4. **Task 2 GREEN: Source-first complete graph deletion** - `29b0e15ca` (`feat`)
5. **Task 3 RED: Graph-resident explicit-isolation contracts** - `9acdaabe9` (`test`)
6. **Task 3 GREEN: Isolation-preserving production composition** - `1b0edb3ea` (`feat`)
7. **Plan evidence: Live trace growth measurement** - `cfd761d29` (`test`)
8. **Rule 2 proof hardening: Fail-closed live dependencies** - `7cd444bf7` (`test`)

## Files Created/Modified

- `cmd/aura/chat_boot_memory.go` - Tenant-aware store, validated retention policy, and isolation-preserving boot helper.
- `cmd/aura/chat_boot.go` - Injects the one store into the runner and the one existing reconciliation lifecycle.
- `cmd/aura/chat_memory_projection.go` - Shares the existing tenant-client construction helper instead of duplicating credential/provisioning logic.
- `internal/arcadedb/memory_reasoning_lifecycle.go` - Bounded expiry, source/conversation/trace/identity deletion, full subgraph cleanup, and conflict retry.
- `internal/arcadedb/memory_reasoning_live_test.go` - Live deletion precedence, race, explicit isolation, failed/cancelled retention, and growth evidence.
- `internal/runner/runner_delete.go` - Fail-honest reasoning deletion before authoritative conversation removal.
- `internal/runner/runner_delete_reconcile.go` - One immediate/periodic bounded expiry pass over the authoritative identity roster.
- `internal/config/config_retention.go`, `config_knobs.go`, `config_retention_test.go` - Typed reasoning override loading, registry entries, defaults, and no-widen validation.
- `internal/conversations/history_reasoning_free_test.go` - Non-empty graph fixture kept out of history and compaction transcript inputs.
- `.env.example` - Documents `AURA_RETENTION_REASONING_SUCCESS_DAYS=30` and `AURA_RETENTION_REASONING_FAILED_DAYS=7` as policy defaults.

## Decisions Made

- Retention configuration is a maximum-class policy, not a capacity knob. Operators may shorten either class; successful cannot exceed 30 days, failed/cancelled cannot exceed 7 days, and the shorter class cannot outlive the successful class.
- The reasoning graph is deleted before its authoritative conversation. If ArcadeDB is unavailable, deletion returns an error and leaves PostgreSQL intact, making a retry safe instead of creating an orphaned privacy copy.
- Identity deletion remains structurally strongest: production drops the per-identity tenant database, while the client also exposes an identity-scoped complete reasoning purge for lifecycle tests and bounded callers.
- Automatic context cannot accidentally receive the reasoning store: `tenantReasoningMemory` does not satisfy `MemoryContextProvider`, and boot wiring leaves the existing fact digest/preload provider byte-for-byte in place.

## Live Growth Evidence

A disposable live ArcadeDB database stored one representative successful trace with two steps, two tool calls, a source-turn edge, and touched-entity edges. The measured delta was:

- **Serialized graph records:** 2,427 bytes
- **Vertices:** 5
- **Edges:** 8
- **Scalar index entries:** 8
- **Page-granular database allocation:** 2,494,184 bytes

The database allocation reflects empty-page/file granularity on a fresh disposable database, not per-trace steady-state cost. None of these measurements sets or validates the fixed 30-day/7-day policy; they are capacity observations only.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Project constraint] Split lifecycle code and shared tenant construction**
- **Found during:** Task 1 GREEN
- **Issue:** `memory_reasoning.go` was already 588 lines and `chat_boot.go` 574 lines; adding lifecycle and construction inline would violate the 600-line production ceiling and duplicate tenant credential/client assembly.
- **Fix:** Added `memory_reasoning_lifecycle.go` and `chat_boot_memory.go`, then extracted `newChatTenantClients` for both conversation and reasoning composition.
- **Files modified:** `internal/arcadedb/memory_reasoning_lifecycle.go`, `cmd/aura/chat_boot_memory.go`, `cmd/aura/chat_memory_projection.go`, `cmd/aura/chat_boot.go`
- **Verification:** Normal file-size hooks, vet, lint, build, unit, and WSL race gates pass.
- **Committed in:** `b9155aa7c`, `6e60a846f`

**2. [Rule 1 - Live concurrency bug] Retried complete deletion decisions after ArcadeDB conflict**
- **Found during:** Task 2 RED live `ExpiryDeleteRace`
- **Issue:** Concurrent expiry and explicit deletion raced the same adjacency page; one transaction returned ArcadeDB 503 instead of converging. Direct trace deletion also reported one deletion when the root was already absent.
- **Fix:** Re-select roots inside a bounded full-transaction retry after 409/503 and route exact trace deletion through the same existence query.
- **Files modified:** `internal/arcadedb/memory_reasoning_lifecycle.go`
- **Verification:** `TestReasoningGraphLive_ExpiryDeleteRace` passes repeatedly under WSL `-race`; repeated deletes return zero and remain successful.
- **Committed in:** `29b0e15ca`

**3. [Rule 2 - Missing critical evidence guard] Made live reasoning tests fail closed**
- **Found during:** Summary stub/skip scan
- **Issue:** The new live fixture inherited a shared local helper that skips when `ARCADEDB_URL` is absent, contradicting this plan's explicit no-skip-green contract.
- **Fix:** Require non-empty `ARCADEDB_URL` and `ARCADEDB_PASSWORD` before constructing the disposable client.
- **Files modified:** `internal/arcadedb/memory_reasoning_live_test.go`
- **Verification:** All five `TestReasoningGraphLive_` cases pass with real credentials under WSL `-race`; missing credentials now fail rather than skip.
- **Committed in:** `7cd444bf7`

---

**Total deviations:** 3 auto-fixed (2 Rule 2 correctness/project constraints, 1 Rule 1 live concurrency bug).
**Impact on plan:** The changes enforce the planned lifecycle and evidence contracts without adding a package, migration, schema field, endpoint, auth mechanism, model-facing tool, or second retention authority.

## Issues Encountered

- Context7 and its CLI fallback were unavailable. Official ArcadeDB HTTP transaction, SQL delete, system-schema, and index documentation was read before implementation.
- A combined verification shell sourced the full deployment `.env` for live ArcadeDB and thereby enabled an unrelated managed-MCP fixture, whose loopback SSRF test correctly failed. Live reasoning tests were rerun with their database environment; ordinary unit/race tests were rerun separately in a clean environment and passed.
- The shared checkout retained the pre-existing `.planning/state.json` modification and untracked `.planning/milestone.lock`. Every commit inspected the cached index and used explicit file allowlists, so neither entered Plan 49-09 history.

## TDD Gate Compliance

- **Task 1 RED:** `b9155aa7c` failed on ignored TTL overrides, the expiry not-implemented sentinel, no reconciler call/join, and absent boot store.
- **Task 1 GREEN:** `6e60a846f` passed typed 30d/7d maximum policy, shorter overrides, bounded identity pages, one-owner boot, joined shutdown, full vet/build/unit, and WSL race. The tracer feedback gate repeated the complete committed-state verification before expansion.
- **Task 2 RED:** `e95eb9e5e` failed because conversation deletion skipped reasoning, repeated explicit deletion returned one, and the live expiry race returned ArcadeDB 503.
- **Task 2 GREEN:** `29b0e15ca` passed source/conversation/operator/identity deletion, partial graph cleanup, repeated calls, foreign identity preservation, and the live race under WSL `-race`.
- **Task 3 RED:** `9acdaabe9` failed because the isolation-preserving composition helper did not wire dedicated dependencies; graph-resident history and live storage negatives were already green.
- **Task 3 GREEN:** `1b0edb3ea` passed explicit-only composition, non-empty history/compaction negatives, owner/foreign live recall, failed/cancelled automatic-context exclusion, repository vet/build, package regression, and WSL race.
- **REFACTOR:** No separate behavior-neutral commit was required; mandatory production file splits landed in Task 1 GREEN.

## Verification Evidence

- Named inventories discover every required retention, deletion, isolation, boot, and history test; no target reports `no tests to run`.
- Live WSL Go 1.26.6 `-race`: all five `TestReasoningGraphLive_` cases pass against disposable databases on ArcadeDB 26.8.1.
- Clean WSL gates pass: `go vet ./...`, `go build ./...`, package unit, and package race for runner, ArcadeDB, conversations, config, and `cmd/aura`.
- Normal pre-commit hooks remained enabled on all eight commits; gofmt, file-size, vet, and lint passed.
- No tracked file deletion, skipped reasoning test, unrun plan verification, migration, schema field, runtime dependency, endpoint, or public tool was introduced.

## Known Stubs

None.

## User Setup Required

None - the two retention variables are optional documented overrides; defaults are active without configuration.

## Next Phase Readiness

- Plan 49-11 can consume live explicit-isolation and lifecycle evidence from the production-owned reasoning store.
- The Phase 49 verifier can rely on concrete 30d/7d policy, immediate deletion precedence, and real graph-resident automatic-path negatives.
- No blocker remains for dependent plans.

## Self-Check: PASSED

- All nineteen changed implementation/test/catalog files and this SUMMARY exist.
- All eight Plan 49-09 commits exist in RED -> GREEN order with no tracked-file deletions.
- Coverage classification reports 3/3 deliverables automatically covered by passing evidence.
- No known stub, skipped reasoning test, unrun plan verification, or foreign shared-worktree path remains in the plan diff.

---
*Phase: 49-memory-tiers*
*Completed: 2026-09-01*
