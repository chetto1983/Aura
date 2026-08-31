# Phase 49: Memory tiers - Pattern Map

**Mapped:** 2026-08-31
**Files analyzed:** 38 likely new/modified files
**Analogs found:** 35 / 38

## Scope Notes

- The paths below are planning candidates inferred from `49-CONTEXT.md` and `49-RESEARCH.md`. Wave 0 must freeze the exact public recall modes, batch operation schema, cursor schema, and trusted active-context metadata carrier before implementation.
- PostgreSQL remains authoritative for conversation turns. ArcadeDB conversation data is a rebuildable, asynchronous per-identity projection.
- No migration number is assigned here. If execution proves that a PostgreSQL migration is required, list `internal/conversations/migrations/` and allocate the next free slot at execution time.
- Keep new ArcadeDB domains in split files. `internal/arcadedb/memory.go` is already 582 lines, `cmd/arcadedb-mcp/tool_memory.go` is 480 lines, and `cmd/aura/chat_boot.go` is 559 lines.
- The phase adds no runtime dependency.

## File Classification

| New/Modified File | Role | Data Flow | Closest Tracked Analog | Match Quality |
|---|---|---|---|---|
| `prd.md` | config / contract | transform | `prd.md:1848` | exact |
| `scripts/agent_memory_eval.py` | test utility | batch, request-response | `scripts/agent_memory_eval.py:40` | exact |
| `scripts/agent_memory_eval_test.py` | test | batch | `scripts/agent_memory_eval_test.py:74` | exact |
| `cmd/arcadedb-mcp/memory_live_integration_test.go` | integration test | request-response | `cmd/arcadedb-mcp/memory_live_integration_test.go` | exact |
| `internal/conversations/store_append.go` | service | CRUD | `internal/conversations/store_append.go:31` | exact |
| `internal/conversations/store_projection.go` | service | batch, file-I/O | `internal/conversations/store_search.go:16` | role-match |
| `internal/conversations/store_projection_test.go` | test | batch, file-I/O | `internal/conversations/store_search_test.go` | role-match |
| `internal/runner/interfaces.go` | provider interface | request-response | `internal/runner/interfaces.go:30` | exact |
| `internal/runner/runner_deps.go` | config / provider | request-response | `internal/runner/runner_deps.go:32` | exact |
| `internal/runner/runner_memory_projection.go` | service | event-driven, batch | `internal/runner/runner_delete_reconcile.go:23` | role-match |
| `internal/runner/runner_memory_projection_test.go` | test | event-driven, batch | `internal/runner/runner_stop_leak_test.go:63` | role-match |
| `internal/arcadedb/memory_conversation.go` | service / model | CRUD, transform | `internal/arcadedb/memory_vector.go:111` | role-match |
| `internal/arcadedb/memory_conversation_test.go` | test | CRUD, transform | `internal/arcadedb/memory_vector_test.go:37` | role-match |
| `internal/arcadedb/memory_conversation_live_test.go` | integration test | CRUD, transform | `internal/arcadedb/memory_live_test.go` | role-match |
| `cmd/arcadedb-mcp/tool_memory.go` | route | request-response | `cmd/arcadedb-mcp/tool_memory.go:243` | exact |
| `cmd/arcadedb-mcp/tool_memory_recall.go` | route | request-response | `cmd/arcadedb-mcp/tool_memory.go:243` | role-match |
| `cmd/arcadedb-mcp/tool_memory_retrieval_test.go` | test | request-response | `cmd/arcadedb-mcp/tool_memory_retrieval_test.go:199` | exact |
| `internal/agent/mcptools/bridge_recall_context.go` | middleware | request-response | `internal/agent/mcptools/bridge_actor.go:25` | partial; carrier unresolved |
| `internal/arcadedb/memory_reasoning.go` | service / model | CRUD, graph traversal | `internal/arcadedb/memory.go:81` | role-match |
| `internal/arcadedb/memory_reasoning_test.go` | test | CRUD, graph traversal | `internal/arcadedb/memory_vector_test.go:141` | role-match |
| `internal/arcadedb/memory_reasoning_live_test.go` | integration test | CRUD, graph traversal | `internal/arcadedb/memory_live_test.go` | role-match |
| `internal/runner/runner_reasoning_persist.go` | service | event-driven | `internal/runner/runner_reasoning_persist.go:20` | exact |
| `internal/runner/runner_reasoning_graph.go` | service | event-driven, transform | `internal/runner/runner_reasoning_persist.go:20` | role-match |
| `internal/runner/runner_reasoning_graph_test.go` | test | event-driven, transform | `internal/runner/runner_reasoning_persist_test.go:59` | role-match |
| `internal/toolinvocations/store_reasoning.go` | service | CRUD | `internal/toolinvocations/store.go:23` | role-match |
| `internal/toolinvocations/store_reasoning_test.go` | test | CRUD | `internal/toolinvocations/store_test.go` | role-match |
| `internal/runner/runner_memory_capture.go` | service | event-driven | `internal/agent/llm_agent_dispatch.go:88` | partial; acceptance policy new |
| `internal/runner/runner_memory_capture_test.go` | test | event-driven | `internal/runner/runner_stop_leak_test.go:63` | role-match |
| `internal/arcadedb/memory_batch.go` | service | CRUD, batch | `internal/arcadedb/transaction.go:20` | role-match |
| `internal/arcadedb/memory_batch_test.go` | test | CRUD, batch | `internal/runner/runner_resume_batch_atomic_test.go:12` | role-match |
| `internal/arcadedb/memory_batch_live_test.go` | integration test | CRUD, batch | `internal/arcadedb/memory_live_test.go` | role-match |
| `cmd/arcadedb-mcp/tool_memory_batch.go` | route | request-response | `cmd/arcadedb-mcp/tool_memory.go:125` | role-match |
| `cmd/arcadedb-mcp/tool_memory_batch_test.go` | test | request-response | `cmd/arcadedb-mcp/tool_memory_retrieval_test.go:46` | role-match |
| `internal/arcadedb/memory.go` | service / schema aggregator | CRUD | `internal/arcadedb/memory.go:63` | exact |
| `internal/arcadedb/merge.go` | service | CRUD | `internal/arcadedb/merge.go` | exact |
| `internal/arcadedb/forget.go` | service | CRUD | `internal/arcadedb/forget.go` | exact |
| `cmd/aura/chat_boot_memory.go` | config / provider | event-driven | `cmd/aura/chat_boot.go:468` | role-match |
| `cmd/aura/chat_boot_test.go` | test | event-driven | `cmd/aura/chat_boot_test.go` | exact |

## Pattern Assignments

### Governance and executable acceptance

#### `prd.md` (contract, transform)

**Analog:** `prd.md:1848-1855`

Extend Amendment #91 before reasoning-graph implementation. Preserve its existing type-level boundary: provider-visible reasoning may be rendered through a bounded display projection, but must never become an ordinary `llm.Message`. Replace the current reasoning-graph non-goal with the new, narrower contract: graph residency is allowed only for explicitly authorized reasoning, and graph retrieval remains explicit and excluded from ordinary recall, preload, compaction, summarization, and fact extraction.

#### `scripts/agent_memory_eval.py`, `scripts/agent_memory_eval_test.py`, `cmd/arcadedb-mcp/memory_live_integration_test.go`

**Analogs:** `scripts/agent_memory_eval.py:40-163`, `scripts/agent_memory_eval.py:356-552`, `scripts/agent_memory_eval_test.py:74-120`

Keep the evaluator's manifest-first suite shape and evidence/scoring separation:

```python
# Existing shape: declarative suites/scenarios, then one runner and one scorer.
for suite in manifest["suites"]:
    for scenario in suite["scenarios"]:
        result = run_scenario(scenario)
        evidence.append(score_scenario(scenario, result))
```

Add scenarios against the published `memory_recall` and batch surfaces, not raw internal search commands. Required hard gates should cover conversation/fact/mixed paths, active-context suppression, edit/delete projection, reasoning non-leakage, capture completion durability, final-state-only batch validation, rollback, and idempotent retry. Update exact manifest-name assertions in `scripts/agent_memory_eval_test.py:83-98` with the new suite names.

### Conversation source and asynchronous projection

#### `internal/conversations/store_append.go`

**Analog:** same file, `internal/conversations/store_append.go:31-147`

Preserve sequence allocation inside the append transaction. Add an additive committed reference instead of recomputing the sequence later:

```go
type AppendTurnParams struct {
	ConversationID uuid.UUID
	Role           Role
	Content        string
	// Existing fields remain the write input.
}

// Planning shape: return only after the transaction commits.
type AppendedTurnRef struct {
	ConversationID uuid.UUID
	Seq            int64
	CreatedAt      time.Time
}
```

`AppendTurn` currently allocates `seq` in the transaction at lines 93-105 and returns only `error`. The projection enqueue point must consume the committed reference. Never derive it with `CountTurns()+1`.

#### `internal/conversations/store_projection.go`, `internal/conversations/store_projection_test.go`

**Analog:** `internal/conversations/store_search.go:16-96`, plus sidecar cleanup in `internal/conversations/store_append.go:286-301`

Copy the owner/RLS scoping discipline, but provide a projection-specific paged reader that resolves spilled content instead of copying search's exclusion:

```go
scope := ownerFilter
if scope == "" {
	scope = identityctx.IdentityID(ctx)
}
if err := s.scopedTx(ctx, scope, func(tx pgx.Tx) error {
	// Read ordered turn refs and resolve sidecar-backed content.
	return nil
}); err != nil {
	return nil, err
}
```

Return only projection-eligible fields: stable source reference, role, final content, timestamps, and deletion/edit state. The source reader must exclude reasoning, tool messages, and raw tool results before anything reaches ArcadeDB.

Tests should force inline and sidecar-backed turns through the same reader, assert stable pagination/order, and prove edits and deletes yield projectable changes.

#### `internal/runner/runner_memory_projection.go`, `internal/runner/runner_memory_projection_test.go`

**Analog:** `internal/runner/runner_delete_reconcile.go:23-111`

Use the existing reconciler lifecycle:

```go
type DeleteReconciler struct {
	runner   *Runner
	interval time.Duration
	wg       sync.WaitGroup
	stop     chan struct{}
	once     sync.Once
}

func (r *DeleteReconciler) Start(ctx context.Context) {
	// Run one reconciliation before entering the ticker loop.
}

func (r *DeleteReconciler) Stop(ctx context.Context) error {
	// Close once and wait with the caller's bound.
}
```

Adapt this to an ordered projector keyed by `(identity_id, conversation_id, seq/version)`. After the PostgreSQL commit, enqueue without delaying the turn response; retry idempotently; and run startup/periodic reconciliation to repair missed work. Projection failures are observable and reconcilable, but do not roll back the authoritative PostgreSQL turn.

Use `context.WithTimeout(context.WithoutCancel(ctx), ...)` as in `runner_delete_reconcile.go:81-87` so post-turn projection is not accidentally canceled with the request. Tests should cover duplicate delivery, out-of-order delivery, restart reconciliation, bounded shutdown, and no goroutine leak.

#### `internal/arcadedb/memory_conversation.go` and tests

**Analog:** `internal/arcadedb/memory_vector.go:45-79`, `internal/arcadedb/memory_vector.go:111-237`

Follow the split schema + typed result pattern:

```go
type ConversationHit struct {
	IdentityID     string
	ConversationID string
	Seq            int64
	Role           string
	Excerpt        string
	Score          float64
	Source         ConversationSource
}

type ConversationSource struct {
	ConversationID string
	Seq            int64
	CreatedAt       time.Time
}
```

As in `SearchFactsHybrid`, cap inputs, treat embedding unavailability as a lexical-only fallback, over-fetch candidates before fusion, hydrate authoritative properties by RID, then restore fused rank explicitly. Projection upsert/delete must be identity-scoped and idempotent by stable PostgreSQL source/version keys.

### Unified recall and browsing

#### `cmd/arcadedb-mcp/tool_memory_recall.go`, `cmd/arcadedb-mcp/tool_memory.go`, `cmd/arcadedb-mcp/tool_memory_retrieval_test.go`

**Analog:** `cmd/arcadedb-mcp/tool_memory.go:205-307`, `cmd/arcadedb-mcp/tool_memory_retrieval_test.go:199-265`

Preserve identity-first validation and one public tool:

```go
func (s *server) memoryRecall(ctx context.Context, in MemoryRecallInput) (*MemoryRecallOutput, error) {
	identityID, err := requiredIdentity(ctx)
	if err != nil {
		return nil, err // before selector validation and before DB access
	}
	// Validate the Wave-0-frozen mode/cursor schema, query tiers independently,
	// fuse eligible result lists, and return typed evidence.
}
```

The response should expose typed evidence and a path marker (`conversations`, `facts`, or `mixed`), with provenance metadata retained. Conversation and fact searches fail independently; an available tier may still answer, while the response reports which path actually contributed.

Use the current `internal/arcadedb/memory_vector.go:81-109` native fusion pattern for candidate lists:

```go
var fuseRIDsStatement = `
  LET $fused = vector.fuse(:rankings, :k)
  SELECT FROM $fused
`
```

Hydrate after fusion and restore rank as `SearchFactsHybrid` does. Do not concatenate two sorted lists or let one score scale dominate. Exact RRF parameters, over-fetch factor, caps, and abstention rules belong in Wave 0 tests.

Conversation browse/open/scroll should use stable PostgreSQL anchors and bounded `messages_before` / `messages_after` windows. Never put whole transcripts in the graph or in one recall response.

#### `internal/agent/mcptools/bridge_recall_context.go`

**Analog:** `internal/agent/mcptools/bridge_actor.go:25-92`

Copy the host-derived per-request metadata boundary:

```go
func actorHeaderFunc(ctx context.Context) map[string]string {
	actor, ok := identityctx.ActorFrom(ctx)
	if !ok {
		return nil
	}
	return map[string]string{
		actorIdentityHeader: actor.IdentityID,
		actorRoleHeader:     string(actor.Role),
	}
}
```

The new carrier should similarly derive active conversation IDs and bounded active turn source refs from trusted host context, never from model-visible arguments. The exact SDK interception/header mechanism has no current Aura analog and must be frozen after inventorying the installed SDK; keep this file provisional until that checkpoint passes.

### Graph-resident reasoning isolation

#### `internal/runner/runner_reasoning_persist.go`, `internal/runner/runner_reasoning_graph.go`, `internal/runner/runner_reasoning_graph_test.go`

**Analog:** `internal/runner/runner_reasoning_persist.go:20-88`, `internal/runner/runner_reasoning_persist_test.go:59-204`

Keep capture at the existing authorization boundary:

```go
func (r *Runner) observeReasoning(delta string) {
	if !r.cfg.ShowReasoning || r.cfg.MaxReasoningRunes <= 0 {
		return
	}
	// Accept only provider-authorized reasoning and enforce the rune cap.
}
```

The graph writer may observe the same authorized stream, but must build a separate trace model:

```text
ReasoningTrace -HAS_STEP-> ReasoningStep -NEXT-> ReasoningStep
ReasoningStep  -INVOKED->  ToolCall
ReasoningTrace -INITIATED_BY-> ConversationTurn
ReasoningStep  -TOUCHED-> Entity
```

Never derive this graph from display text, summaries, conversation projection, compaction, or fact extraction. Reset graph accumulation on retry exactly as persisted display reasoning resets at `runner_reasoning_persist.go:64-73`.

Tests should mirror the existing happy-path, disabled/redacted, cap, and retry-reset cases, then additionally assert no reasoning node is returned by ordinary `memory_recall` or background memory context.

#### `internal/toolinvocations/store_reasoning.go`, `internal/toolinvocations/store_reasoning_test.go`

**Analog:** `internal/toolinvocations/store.go:23-48`, `internal/toolinvocations/store.go:123-157`

Build tool-call references from persisted invocation metadata, applying redaction and caps at the persistence chokepoint:

```go
type Event struct {
	// Existing durable identity/run/turn/tool metadata.
}

func (e Event) toParams() params {
	// Redact and bound arguments/results before durable storage.
}
```

Expose a precise identity/run/turn-scoped lookup for the reasoning writer. Graph properties may include allowlisted tool name, bounded/redacted arguments, status, and timing. Raw tool results must not enter the reasoning graph.

#### `internal/arcadedb/memory_reasoning.go` and tests

**Analog:** typed identity/role/source validation in `internal/arcadedb/memory.go:81-240`; graph read abstention in `internal/arcadedb/memory_vector.go:111-131`

Define reasoning types separately from fact and conversation types. Writer input must carry host-derived identity, run/turn reference, authorized trace source, ordered step index, and already-redacted tool metadata. Reads must be explicit selectors; ordinary fact/conversation recall must not traverse reasoning classes or edges.

Use deterministic source IDs for trace/step/tool-call upserts. `TOUCHED` edge creation needs an explicit, testable resolution policy; do not infer entities from free-form chain-of-thought.

### Accepted-capture queue and completion barrier

#### `internal/runner/runner_memory_capture.go`, `internal/runner/runner_memory_capture_test.go`

**Ordering analog:** `internal/agent/llm_agent_dispatch.go:88-157`

The dispatch path executes calls concurrently but consumes results serially in original call order. Reuse that ordering property for acceptance records: acceptance happens in deterministic task order, and only accepted captures enter one serial async writer queue.

**Barrier analog:** `internal/runner/runner_resume.go:387-420`

```go
func waitWorkers(ctx context.Context, wg *sync.WaitGroup) error {
	// Reuse one waiter for the active drain, honor the caller timeout,
	// and report timeout rather than pretending workers completed.
}
```

The completion barrier must be stronger than projection shutdown: when a task reports successful completion, every previously accepted capture is durable. Repeated waits must not leak goroutines and a clean drain must re-arm for later work, as tested in `internal/runner/runner_stop_leak_test.go:63-134` and `:165-216`.

Acceptance rules are new and must be explicit:

- accept explicit user memory requests and allowlisted, reliable tool observations;
- reject assistant speculation, summaries, reasoning text, and raw tool output;
- derive identity, run, and role from the host;
- on duplicate durable fact, enrich provenance rather than silently dropping evidence;
- worker writes may add evidence but may not supersede higher-authority facts.

Queue write failure or barrier timeout is a task-completion failure, not a fail-soft projection warning.

### Final-state atomic batch compiler

#### `internal/arcadedb/memory_batch.go`, `internal/arcadedb/memory_batch_test.go`, `internal/arcadedb/memory_batch_live_test.go`

**Transaction analog:** `internal/arcadedb/transaction.go:9-98`

```go
tx, err := db.Begin(ctx)
if err != nil {
	return err
}
defer tx.Rollback(ctx)

// Load committed state, compile all operations into isolated working state,
// validate only the final working state, then emit graph writes in this tx.

return tx.Commit(ctx)
```

Use the existing transaction-session header for every command/query in the batch. The compiler shape should be:

```go
live, err := loadCommittedState(ctx, tx, identityID)
working := live.Clone()
for i, op := range ops {
	if err := applyToWorkingState(working, op); err != nil {
		return BatchError{Index: i, Err: err}
	}
}
if err := validateFinalState(working); err != nil {
	return err
}
return persistDiff(ctx, tx, live, working)
```

Do not validate transient intermediate states against final invariants. Do not write graph state while compiling. On the first malformed or unresolved operation, return its index and leave live state byte-for-byte/semantically unchanged.

**Retry analog:** `internal/arcadedb/write_retry.go:33-88`

Reuse conflict classification and bounded full-jitter backoff only after measuring Phase 49's batch retry behavior. A retry must restart the entire load -> compile -> validate -> apply transaction from committed state. Never retry an individual statement against a stale working copy, and never run embeddings, network calls, or other external effects inside the transaction retry loop.

**Atomic test analog:** `internal/runner/runner_resume_batch_atomic_test.go:12-117`

Copy its duplicate-idempotency and partially-resolved-batch/no-append assertions, then add final-state cases such as remove-then-add, add-then-remove, rename chains, conflicting duplicates, a forced late operation failure, a forced commit conflict, and a retry that recompiles from fresh committed state.

#### `cmd/arcadedb-mcp/tool_memory_batch.go`, `cmd/arcadedb-mcp/tool_memory_batch_test.go`

**Analog:** `cmd/arcadedb-mcp/tool_memory.go:125-203`

Keep the MCP handler thin: require identity first, validate the frozen typed operation envelope, call one atomic compiler method, and return a typed outcome. Do not loop over existing single-write handlers; that would expose partial commits.

#### `internal/arcadedb/memory.go`, `internal/arcadedb/merge.go`, `internal/arcadedb/forget.go`

**Analog:** `internal/arcadedb/memory.go:63-79`, `internal/arcadedb/memory.go:177-354`

Keep shared schema aggregation and legacy single-operation entry points, but route any behavior that must share Phase 49 final-state semantics through the batch compiler. Avoid extending the current multi-write `UpsertFact` sequence as the atomicity primitive. Schema extensions belong in the new split domain files and are called by `EnsureMemorySchema`.

### Interfaces and boot wiring

#### `internal/runner/interfaces.go`, `internal/runner/runner_deps.go`

**Analog:** `internal/runner/interfaces.go:30-64`, `internal/runner/interfaces.go:159-167`, `internal/runner/runner_deps.go:32`

Keep dependencies narrow and capability-specific. Extend the conversation interface only with the committed append reference and projection source reader needed by the runner. Add separate projector, accepted-capture sink/barrier, and reasoning trace sink interfaces rather than expanding `MemoryContextProvider`, whose current `Context` / `Search` role remains ordinary fail-soft fact context.

#### `cmd/aura/chat_boot_memory.go`, `cmd/aura/chat_boot_test.go`

**Analog:** construction in `cmd/aura/chat_boot.go:468-529`

Move Phase 49 memory-tier construction into a new boot helper because the existing file is near the project line limit. Return explicit lifecycle handles to the existing shutdown path:

```go
type memoryTierRuntime struct {
	Projector *MemoryProjector
	Capture   *CaptureQueue
}

func buildMemoryTiers(deps memoryTierDeps) (*memoryTierRuntime, error) {
	// Construct stores/providers, then start reconciler-backed workers.
}
```

Boot tests should prove startup order, missing optional ArcadeDB behavior, bounded shutdown, projector reconciliation, and that the accepted-capture barrier is awaited independently of the fail-soft projection worker.

## Shared Patterns

### Identity and trust boundary

**Sources:** `cmd/arcadedb-mcp/tool_memory.go:48-82`, `internal/agent/mcptools/bridge_actor.go:25-92`

Apply to every recall, batch, projection, capture, and reasoning entry point. Require identity before selector validation or graph access. Actor role, run, active conversation, and active turn refs come from trusted host context; none are model-supplied tool arguments.

### PostgreSQL authority and graph idempotency

**Sources:** `internal/conversations/store_append.go:60-147`, `internal/runner/runner_delete_reconcile.go:81-111`

Only a committed PostgreSQL turn reference can enqueue projection. ArcadeDB keys include stable source/version identity; duplicate delivery is harmless; periodic reconciliation repairs missed edits/deletes. Projection failure does not change the successful PostgreSQL commit.

### Typed evidence and bounded hydration

**Source:** `internal/arcadedb/memory_vector.go:111-237`

All recall results carry typed provenance. Candidate retrieval is capped, fusion occurs before hydration, final rank is explicitly restored, and an unavailable semantic path falls back or abstains deterministically.

### Reasoning is a separate security domain

**Sources:** `internal/runner/runner_reasoning_persist.go:20-88`, `internal/toolinvocations/store.go:123-157`

Authorization, bounds, and redaction occur before graph persistence. Reasoning classes are never included in ordinary recall, preloads, projection, compaction, summarization, or fact extraction. Tool data is allowlisted and capped; raw results are excluded.

### Failure semantics are tier-specific

- Conversation projection: asynchronous, retryable, reconciled, and fail-soft after the authoritative turn commit.
- Unified recall: tier-partial response is allowed and must report the contributing path.
- Accepted capture: serial, durable, and fail-honest at the task-completion barrier.
- Atomic batch: one transaction; first error rolls back everything; retries rebuild from committed state.

### Resource bounds and shutdown

**Sources:** `internal/runner/runner_delete_reconcile.go:64-87`, `internal/runner/runner_resume.go:387-420`, `internal/runner/runner_stop_leak_test.go:63-216`

Every worker owns an explicit stop path, bounded contexts, and leak tests. Recall windows, excerpts, candidate counts, reasoning runes, tool arguments, and queue drain time are all capped.

## Design-Only References (Not Aura Copy Analogs)

These files were inspected at pinned tracked revisions for semantics only. They must not appear as Aura implementation paths in plans.

| Corpus | Pinned HEAD | Useful behavior | Aura constraint |
|---|---|---|---|
| Hermes `tools/memory_tool.py:586-704` | `4f22543509d1b91dc45bcb369447126c5eb14fb7` | isolated working copy, sequential batch compile, final-only budget validation, one save, indexed all-or-nothing errors | implement with Aura's ArcadeDB transaction API |
| Hermes `agent/memory_manager.py:714-869` | same | serialized background work and sentinel-based flush barrier | Aura barrier must prove durability of accepted captures |
| Hermes `tools/session_search_tool.py:435-683,761-1041` | same | bounded browse/open/scroll, anchors, active-context suppression, typed paths | PostgreSQL remains Aura's transcript authority |
| LibreChat `packages/api/src/agents/memory.ts:123-285` | `240e9e920f5eaa0197448507540b1aa7bbdd1b79` | serial write chain and state update after success | do not copy its separate LLM memory extraction run at `:920-1012` |
| LibreChat `api/server/controllers/agents/client.js:2848-2905,4070-4072,4400-4425` | same | bounded recent context and bounded await of background memory work | Aura acceptance is typed and host-authorized |
| LibreChat `packages/api/src/agents/run.ts:294-430` | same | provider reasoning treated as a separate runtime concern | Aura additionally enforces graph non-retrievability from ordinary recall |

## No Analog Found

| File / concern | Role | Data Flow | Reason / planning action |
|---|---|---|---|
| `internal/agent/mcptools/bridge_recall_context.go` exact carrier | middleware | request-response | Aura has trusted actor headers but no active-turn metadata carrier. Inventory the installed MCP SDK and freeze either a supported interception/header hook or a bespoke bridge before coding. |
| Accepted durable-capture classification in `internal/runner/runner_memory_capture.go` | service | event-driven | Aura has ordered dispatch and worker barriers, but no typed acceptance policy. Freeze explicit user/tool observation rules; do not add an LLM extractor or summary harvester. |
| Reasoning segmentation and `TOUCHED` entity resolution | service / model | transform, graph traversal | Aura has authorized bounded reasoning persistence but no trace-step graph schema or deterministic entity-link policy. Freeze source-ID, step boundary, and explicit entity-resolution rules in Wave 0. |

## Metadata

**Analog search scope:** `internal/conversations`, `internal/runner`, `internal/arcadedb`, `internal/toolinvocations`, `internal/agent`, `cmd/arcadedb-mcp`, `cmd/aura`, `scripts`, and `prd.md`

**Tracked-source gate:** Every Aura analog named above was verified with `git ls-files`. External Hermes and LibreChat references were verified in their own repositories at the pinned HEADs and are labeled design-only.

**Strong analogs read:** 19 Aura implementation/test files plus the existing contract/evaluator; external design corpus stopped after six relevant files.

**Pattern extraction date:** 2026-08-31
