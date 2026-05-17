# Phase07D Plan - User/Operational Memory Typed Tiers

Status: closed with local and live verification on 2026-05-17. Existing code
satisfies the core Phase07D writer/recall contract through package tests, a
disposable mixed-tier runtime tool golden, a live `cmd/probe_chat` chat-level
golden, and full repo build/vet/test gates.

## Phase Goal

Make `user_memory` and `operational` first-class typed retrieval tiers. The
agent should recall approved user facts and validated Aura lessons through
task-level tools with stable handles, layer labels, active-only filtering,
freshness/degraded annotations, and no leakage from pending proposals, raw tool
failures, archive noise, or wiki auto-apply paths.

## Scope

- Map current code evidence for `CollectionUserMemory` and
  `CollectionOperational`.
- Verify approved writers:
  `WriteApprovedUserFact` and `WriteApprovedLesson`.
- Verify task-level recall tools:
  `recall_user_memory` and `recall_operational`.
- Verify app registration and tool manifest metadata for both recall tools.
- Verify broad `search_memory` does not silently absorb these tiers into
  `scope=all`.
- Record deferred projection and write-governance questions without expanding
  this slice.

## Non-Goals

- Do not create new memory tables unless a verifier proves the shared compact
  store violates typed-tier behavior.
- Do not index raw `tool_attempts`, `run_events`, `audit_events`, chat logs, or
  tool outputs as operational memory.
- Do not add Mem0, Letta, Zep, Neo4j, or another memory backend.
- Do not move user-memory write-governance repairs out of Phase 9 unless the
  verifier finds a Phase07D retrieval blocker.
- Do not implement Phase07E source span offsets or Phase07F wiki frontmatter
  promotion.

## Deep Module RFC

| Option | Shape | Decision | Reason |
| --- | --- | --- | --- |
| A. Keep user/operational tiers as comments on compact rows | Only retain enum constants and broad search behavior | Rejected | Shallow interface: callers still need to know compact-store details and cannot rely on task-level recall semantics. |
| B. Add separate tables for user and operational memory now | New stores, migrations, projection rows, and recall tools | Deferred | Deeper long-term shape, but current code already has compact-backed writers; no measured need for extra storage yet. |
| C. Use compact-backed typed adapters plus task-level recall tools | Approved writers write typed rows; recall tools query only their kind and expose handles/freshness | Chosen | Deep enough for Phase07D: small caller interface, strong locality in writer/recall modules, low migration risk. |
| D. Use a hosted memory platform | Replace writers/recall with Mem0/Letta-style backend | Rejected | Violates local-first source-of-truth and adds a second memory authority. Use docs as pattern sources only. |

Chosen module shape:

```text
approved proposal
  -> typed writer
  -> compact_memory_documents(kind=user_memory|operational, status=active)
  -> recall_user_memory or recall_operational
  -> layer-labelled hit with stable handle and freshness/degraded metadata
```

## Dependencies

- Phase07A excludes raw tool output and loop scaffolding from default compact
  memory recall.
- Phase07B defines typed collection descriptors, stable handles, score
  components, SourceID filtering, and follow-up handles.
- Phase07C provides shared compact projection freshness and
  `degraded_read=true` annotations.
- Phase-N writes validated operational lessons after promotion.
- Phase-O writes approved user facts after user-memory triage.
- Phase-Q adds user-memory write guards; Phase07D does not broaden that policy.

## Implementation / Closure Slices

| Slice | Goal | Source Files | Verification | Status |
| --- | --- | --- | --- | --- |
| P07D-01 | Confirm registry entries and back-compat kind constants for both tiers | `internal/storage/memoryindex/collections.go`, `store.go` | `go test ./internal/storage/memoryindex -run "Test(CollectionConstants\|KindBackCompat\|ScaffoldedCollections)" -count=1` | existing code passed in combined command on 2026-05-17 |
| P07D-02 | Confirm writers create active typed rows with stable handles and no wrong-kind writes | `internal/learning/writer.go`, `internal/learning/user_memory_writer.go`, `internal/api/summaries.go` | `go test ./internal/learning -run "Test(WriteApprovedLesson\|WriteApprovedUserFact)" -count=1` | existing code passed in combined command on 2026-05-17 |
| P07D-03 | Confirm recall tools return only active entries, support filters, emit handles, and keep pending proposals hidden | `internal/agent/tools/registry/recall_user_memory.go`, `recall_operational.go` | `go test ./internal/agent/tools/registry -run "Test(RecallUserMemoryTool\|RecallOperationalTool\|ExamplesParameterEval)" -count=1` | existing code passed in combined command on 2026-05-17 |
| P07D-04 | Confirm runtime registration, freshness wiring, and mixed-tier no-leak behavior are acceptable for closure | `cmd/aura/app_wire.go`, `cmd/aura/app_wire_test.go`, `tool_definitions.go`, existing registry scans | `go test ./cmd/aura -run TestRegisterMemoryRecallToolsWiresTypedTiersAndFreshness -count=1` | closed |
| P07D-06 | Confirm live chat can use typed recall without broad `search_memory` leakage | `cmd/probe_chat/phase07d.go`, `cmd/probe_chat/cases.go` | `go run ./cmd/probe_chat -case phase07d-mixed-tier-recall ...` in Compose | closed |
| P07D-05 | Decide whether user-memory approval must be actor-aware in dashboard approval path | `internal/api/summaries.go`, Phase09 guards | verifier decision; likely Phase09 follow-up unless retrieval is blocked | open |

## PRD Coverage Matrix

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Define memory layer IDs and citation handles for user and operational tiers | Scope, P07D-01, Deep Module RFC | `benchmark.md` rows 1-2 | `source.md` rows for PRD, ADR-026, `collections.go` | covered by existing code; self-audited |
| Split recall behavior by task intent | Scope, P07D-03 | `benchmark.md` rows 5-7 | `source.md` rows for PRD, recall tools, Letta docs | covered by existing code; self-audited |
| User facts do not appear in wiki unless intentionally promoted | Non-goals, P07D-02 | `benchmark.md` row 3 and Phase09 refs | Phase-O/Phase-Q docs, `summaries.go`, `user_memory_writer.go` | covered at writer/approval level; live verifier pending |
| Tool failures do not appear in wiki or operational memory without validation | Non-goals, P07D-02 | `benchmark.md` row 4 | Phase-N docs, `writer.go`, Mem0 ingestion controls | covered at writer level; live verifier pending |
| Retrieval hits keep layer labels and handles | Phase Goal, P07D-03 | `benchmark.md` rows 5-6 | recall tool tests and source audit | covered by existing tests |
| Freshness/degraded warnings return with retrieval hits | Dependencies, P07D-03 | `benchmark.md` row 7 | Phase07C source and recall tool code | covered by package tests; broader per-tier projection deferred |
| Broad memory soup is avoided | Non-goals, P07D-03 | `benchmark.md` rows 8 and mixed-tier golden rows | `memory_search.go` scope enum; task-level recall docs; `cmd/aura/app_wire_test.go`; `cmd/probe_chat/phase07d.go` | covered by deterministic and live tests |
| Golden RAG evals for user facts and operational lessons | Implementation gate | `benchmark.md` disposable direct tool rows plus live/chat row | PRD Phase 7 gate | passed for Phase07D typed tiers |

## Open Decisions

1. Should Phase07D closure accept `compact_memory_documents` as the backing
   store for both tiers, or require separate canonical tables now?
   - Recommended: accept compact-backed typed tiers for Phase07D. Defer
     separate stores until retention/decay or concurrency metrics prove need.
2. Should `projection_state` split user and operational memory into
   per-tier projection IDs now?
   - Recommended: defer. Current FTS/Qdrant mirror is shared by compact docs;
     splitting freshness without separate projection work would be decorative.
3. Should dashboard approval write user memory through
   `WriteApprovedUserFactAs` with actor context?
   - Recommended: record as Phase09 hardening unless the verifier classifies it
     as a Phase07D blocker. Phase07D owns retrieval tier shape, not all write
     authority plumbing.

## First Bounded Implementation Slice

The first bounded slice is closed:

```text
P07D-closure-verifier
  Goal: prove current code closes Phase07D or identify the smallest repair.
  Affected files if repair is needed:
    - internal/storage/memoryindex/collections.go
    - internal/learning/writer.go
    - internal/learning/user_memory_writer.go
    - internal/api/summaries.go
    - internal/agent/tools/registry/recall_user_memory.go
    - internal/agent/tools/registry/recall_operational.go
    - cmd/aura/app_wire.go
  Baseline command:
    go test ./internal/storage/memoryindex ./internal/learning ./internal/agent/tools/registry -run "Test(CollectionConstants|KindBackCompat|ScaffoldedCollections|WriteApprovedLesson|WriteApprovedUserFact|RecallUserMemoryTool|RecallOperationalTool|ExamplesParameterEval)" -count=1
  Mixed-tier direct tool golden:
    go test ./cmd/aura -run TestRegisterMemoryRecallToolsWiresTypedTiersAndFreshness -count=1
  Live/chat golden:
    go run ./cmd/probe_chat -case phase07d-mixed-tier-recall ... in Compose
  Non-goals:
    - no schema split
    - no broad search_memory scope change
    - no live user data mutation
```

## Rollback / Deviation Rule

If verifier evidence shows a user fact, pending proposal, raw tool failure, or
archive row can surface through the wrong tier, stop and repair the smallest
writer/recall/filter module before calling Phase07D closed.
