# Phase 49: Memory tiers - Context

**Gathered:** 2026-08-31
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 49 completes Aura's three-tier memory contract without changing the storage authorities already chosen: PostgreSQL remains the system of record for conversation turns, while the identity-scoped ArcadeDB database receives rebuildable conversation and reasoning projections alongside long-term facts. The phase delivers semantic conversation recall, one model-facing `memory_recall` surface spanning conversations and facts, an explicitly retrieved reasoning graph, automatic capture of durable facts, and final-state atomic memory batches. The amendment extending PRD Amendment #91 must be committed before reasoning-tier implementation.

This phase does not move conversation persistence out of PostgreSQL, inject reasoning into ordinary context, add a second model-facing recall tool, add a cockpit UI, or adopt Neo4j Agent Memory as a runtime dependency.

</domain>

<decisions>
## Implementation Decisions

### Governance and source ownership
- **D-01:** Extend PRD Amendment #91 and commit that amendment before any reasoning-tier code. The extension must ratify graph persistence, explicit-only retrieval, and the prohibition on summarization or fact harvesting.
- **D-02:** PostgreSQL remains authoritative for turns; ArcadeDB stores a per-identity derived projection that is fully rebuildable from PostgreSQL. — **Reversibility:** costly — changing the authority later would require a data migration and replacement of the deletion, replay, and reconciliation contracts.
- **D-03:** Conversation edits and deletions propagate to the ArcadeDB projection. ArcadeDB has no independent retention authority for conversation data.

### Conversation projection
- **D-04:** Project each completed turn asynchronously after the PostgreSQL commit. The projector is ordered, idempotent, retryable, and backed by reconciliation so a memory-backend failure never invalidates the already-durable turn.
- **D-05:** Project only user messages and final assistant answers. Preserve source IDs and provenance, but exclude reasoning, tool calls, and raw tool results from the searchable conversation tier.
- **D-06:** All committed eligible turns may be projected immediately, but ordinary recall suppresses content still present in the model's active context. Prior conversations and current-conversation turns that have left context through compaction remain eligible.

### Unified recall and conversation exploration
- **D-07:** Query conversation and long-term fact tiers independently, then fuse their ranked candidates with Reciprocal Rank Fusion. Every enabled tier is queried, but the final result has no forced 50/50 quota.
- **D-08:** A conversation hit returns a bounded window anchored on the matching turn, with conversation ID, turn sequence, timestamp, rank, and provenance. Facts remain atomic evidence records.
- **D-09:** Keep one model-facing `memory_recall` tool. Extend it with progressive-disclosure modes for semantic search, browsing recent conversations, opening a conversation, and scrolling before or after an anchor using IDs and cursors. All operations remain fail-closed and identity-scoped. — **Reversibility:** costly — this becomes a published MCP tool contract consumed by prompts, tests, and model behavior.
- **D-10:** `memory_recall` returns typed evidence, not a second LLM-generated answer. Results identify `conversation` or `fact`, include source metadata, and report an effective path of `conversations`, `facts`, or `mixed`; weak or empty retrieval abstains explicitly.

### Reasoning graph
- **D-11:** A graph trace may contain only reasoning content the provider explicitly exposes and Aura is already authorized to show or persist. Use a provider summary when that is the exposed form. Never reconstruct hidden chain-of-thought and never introduce a post-task reasoning summarizer.
- **D-12:** Model the reasoning tier after the validated Neo4j Agent Memory shape, ported to ArcadeDB: one `ReasoningTrace` per answer, ordered bounded `ReasoningStep` nodes, structured `ToolCall` records, a link to the initiating message/turn, and explicit entity audit edges equivalent to `TOUCHED`. — **Reversibility:** costly — changing the vertex and edge contract after rollout requires a graph migration and reindexing.
- **D-13:** Ordinary recall, proactive injection, compaction, summarization, and fact extraction never query or receive reasoning data. Reasoning enters context only when the caller explicitly selects reasoning in `memory_recall`; explicit mode supports similarity search and progressive trace traversal by ID.
- **D-14:** Reasoning tool-call records store tool name, status, duration, allowed arguments, a bounded redacted observation, source references, and touched-entity edges. Do not duplicate secrets, blobs, or large raw results.

### Automatic durable-fact capture
- **D-15:** Automatically capture only durable, attributable evidence: explicit user statements and reliable observations from allowed tools, with confidence and direct provenance. Exclude hypotheses, temporary instructions, secrets, generated prose, and all reasoning content.
- **D-16:** Queue capture asynchronously and serially while the task runs. Before publishing task completion, enforce a flush barrier that proves all accepted captures are durable; this is the `AUTO-03` completion guarantee.
- **D-17:** Duplicate evidence enriches the existing fact's provenance instead of creating a duplicate fact. Contradictions remain temporal evidence and may supersede only after validation by the principal host; workers cannot supersede directly, carrying forward Phase 51's authority boundary.
- **D-18:** Host-owned provenance fields such as run ID and worker identity remain host-derived. A reasoning summarizer is never a source.

### Atomic memory operations
- **D-19:** The `HARN-05` multi-operation API evaluates all operations against an isolated working state, validates the complete final state, and commits once in a single ArcadeDB transaction. Intermediate overflow or temporary constraint violation does not matter if the final state is valid. — **Reversibility:** costly — partial-success semantics would change the public mutation contract and every retrying caller.
- **D-20:** Any malformed operation, missing match, ambiguous match, authorization failure, or invalid final state rolls back the whole batch. The failure identifies the first error and states that live state is unchanged.
- **D-21:** Batch retries are idempotent. Concurrent-modification retries restart from committed state; external side effects must not occur inside the retried transaction.

### the agent's Discretion
- Choose the narrowest existing Aura mechanism for dispatching and reconciling the derived projection (for example, the existing transactional/outbox patterns); do not introduce a new subsystem when an existing seam fits.
- Choose embedding model, dimensions, candidate counts, RRF constants, score thresholds, anchored-window sizes, and cursor encoding from measured retrieval quality and the already deployed ArcadeDB version.
- Choose deterministic segmentation and entity resolution for reasoning steps while preserving the `Trace -> Step -> ToolCall` and entity-edge contract.
- Define bounded defaults for reasoning-trace retention, with shorter retention permitted for failed/cancelled traces and explicit deletion propagation. Retention must not weaken identity deletion or reasoning isolation.
- Set concrete redaction allowlists, observation caps, transaction isolation, retry counts, and flush timeouts using existing Aura configuration and fail-closed patterns.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Product and phase contracts
- `.planning/PROJECT.md` — project-level invariants, PRD-first workflow, quality bar, and scope posture.
- `.planning/ROADMAP.md` — Phase 49 goal, success criteria, sequencing, and shipped-vs-open boundary.
- `.planning/REQUIREMENTS.md` — authoritative `HARN-05`, `TOOL-05`, `AUTO-03`, `CTX-05`, and `MEM-01` through `MEM-06` contracts.
- `.planning/STATE.md` — current milestone state and session continuity.
- `prd.md` Amendment #91 — existing display-only reasoning persistence contract; MUST be extended and committed before reasoning-tier code.
- `.planning/phases/45-harness-correctness/45-CONTEXT.md` — existing harness and memory-correction decisions.
- `.planning/phases/45.1-native-mcp-client/45.1-CONTEXT.md` — official MCP SDK and fail-closed authenticated identity decisions.
- `.planning/phases/46-mcp-trust-and-facade/46-CONTEXT.md` — curated MCP surface and identity boundary.
- `.planning/phases/51-durable-delegation/51-CONTEXT.md` — duplicate suppression, host-derived provenance, worker identity, and no-worker-supersede rules.

### Codebase maps and current Aura seams
- `.planning/codebase/STACK.md` — deployed stack and infrastructure versions.
- `.planning/codebase/ARCHITECTURE.md` — service boundaries and data ownership.
- `.planning/codebase/INTEGRATIONS.md` — PostgreSQL, ArcadeDB, MCP, and provider integration map.
- `internal/arcadedb/memory.go` — current Fact/FactSource/FactWrite model, fact upsert, hybrid search, and graph lookup.
- `internal/arcadedb/transaction.go` — existing explicit begin/commit/rollback session primitives and read-your-writes behavior.
- `cmd/arcadedb-mcp/tool_memory.go` — current single `memory_recall` tool and retrieval metadata contract.
- `internal/conversations/store_search.go` — owner/RLS-scoped PostgreSQL conversation-turn search and current large-content boundary.
- `internal/conversations/store_reasoning.go` — display-only reasoning read projection kept separate from `llm.Message`.
- `internal/runner/runner_context.go` — current proactive long-term recall and transient-context fencing.
- `internal/runner/runner_reasoning_persist.go` — bounded provider-visible reasoning accumulation and persistence gates.
- `internal/reasoningtrace/reasoningtrace.go` — current debug JSONL observer; it is not the new graph source of truth.

### Mandatory local reference corpora
- `D:/tmp/hermes-agent/tools/memory_tool.py` at commit `4f22543509d1b91dc45bcb369447126c5eb14fb7` — final-state atomic batch behavior and unchanged-live-state errors.
- `D:/tmp/hermes-agent/tools/session_search_tool.py` at commit `4f22543509d1b91dc45bcb369447126c5eb14fb7` — search/browse/read/scroll progressive disclosure, anchored windows, lineage deduplication, and active-context suppression.
- `D:/tmp/hermes-agent/agent/memory_provider.py` at commit `4f22543509d1b91dc45bcb369447126c5eb14fb7` — provider lifecycle for prefetch, turn sync, pre-compression, writes, and delegation.
- `D:/tmp/hermes-agent/agent/memory_manager.py` at commit `4f22543509d1b91dc45bcb369447126c5eb14fb7` — off-thread ordered sync, flush, and memory-provider orchestration.
- `D:/tmp/LibreChat/packages/api/src/agents/memory.ts` at commit `240e9e920f5eaa0197448507540b1aa7bbdd1b79` — server-bound identity, serialized writes, explicit memory guard, and separate background MemoryRun.
- `D:/tmp/LibreChat/api/server/controllers/agents/client.js` at commit `240e9e920f5eaa0197448507540b1aa7bbdd1b79` — bounded recent-message window supplied to MemoryRun without reasoning sidecars.
- `D:/tmp/LibreChat/packages/api/src/agents/run.ts` at commit `240e9e920f5eaa0197448507540b1aa7bbdd1b79` — provider-specific reasoning replay kept separate from memory extraction.

### Neo4j Agent Memory design reference
- `https://neo4j.com/labs/agent-memory/` — experimental three-tier graph-memory overview; reference only, not a runtime dependency.
- `https://neo4j.com/labs/agent-memory/explanation/memory-types/` — short-term, long-term, and reasoning schemas and their graph connections.
- `https://neo4j.com/labs/agent-memory/how-to/reasoning-traces/` — trace lifecycle, similar-task retrieval, outcomes, retention examples, and audit edges.
- `https://neo4j.com/labs/agent-memory/reference/mcp-tools/` — memory search/context tool shapes; note that Aura intentionally keeps reasoning opt-in even where Neo4j context defaults differ.
- `https://neo4j.com/labs/agent-memory/reference/nams-limits/` — asynchronous extraction visibility and operational limitations.
- `https://github.com/neo4j-labs/agent-memory/tree/5b4e00af88342707d011bb9d4f2b34503f43a8c3` — source repository pinned at the reviewed revision.
- `https://github.com/neo4j-labs/agent-memory/blob/5b4e00af88342707d011bb9d4f2b34503f43a8c3/src/neo4j_agent_memory/memory/reasoning.py` — concrete ReasoningTrace/ReasoningStep/ToolCall model, streaming recorder, embeddings, message links, and `TOUCHED` edges.

### Database behavior
- `https://docs.arcadedb.com/arcadedb/concepts/vector-search` — transactional dense/sparse/full-text hybrid retrieval and server-side RRF via `vector.fuse`.
- `https://docs.arcadedb.com/arcadedb/reference/http-api/http` — explicit remote transaction sessions, commit, rollback, expiration, and retry behavior.
- `https://www.postgresql.org/docs/16/logicaldecoding-explanation.html` — replay behavior and the idempotency requirement for derived change consumers.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/arcadedb.Client` transaction helpers already provide the begin/command/query/commit/rollback seam required for an atomic memory batch.
- `internal/arcadedb.Fact`, `FactSource`, and `FactWrite` already carry identity-scoped fact data and provenance; extend this contract rather than creating a parallel memory model.
- `memory_recall` already owns the single model-facing read surface and reports the retrieval path and abstention metadata.
- `SearchConversationTurns` already provides PostgreSQL-owner-scoped lexical discovery and a source read path for projection/reconciliation.
- The final assistant row already carries bounded, authorized reasoning separately from normal history, giving the reasoning projector a structurally isolated source.

### Established Patterns
- PostgreSQL RLS and authenticated `_meta` identity fail closed; ArcadeDB uses one database per identity. No fallback identity is allowed.
- `llm.Message` is structurally reasoning-free, and compaction uses a non-reasoning summarizer. Preserve this type-level `CTX-05` boundary.
- Derived stores must tolerate replay and duplicate delivery. Stable source keys and host-derived provenance are required before semantic deduplication.
- Background work may not hold up normal response streaming, but task completion can carry a bounded durability barrier where the requirement explicitly demands it.
- Memory writes from concurrent workers already require worker provenance and duplicate safety; only the principal host may authorize supersession.

### Integration Points
- Hook conversation projection after the authoritative PostgreSQL turn commit, then reconcile from the source store without changing the turn transaction.
- Extend ArcadeDB schema/migrations and the Go memory client with conversation, trace, step, tool-call, and audit-edge operations.
- Extend `cmd/arcadedb-mcp/tool_memory.go` in place for mixed recall and progressive conversation/reasoning modes.
- Feed automatic fact candidates from user messages and allowed tool observations into an ordered capture queue; join it at the task-completion seam with a bounded flush barrier.
- Reuse OTel/tool invocation evidence and existing source IDs for `TOOL-05` path reporting, `AUTO-03` provenance, and live E2E verification.

</code_context>

<specifics>
## Specific Ideas

- The operator explicitly requires every downstream Phase 49 design/planning pass to inspect both `D:/tmp/hermes-agent` and `D:/tmp/LibreChat`; do not rely on remembered descriptions alone.
- Make conversation exploration feel like Hermes session search while keeping Aura's already-published single `memory_recall` name.
- Port Neo4j Agent Memory's useful graph semantics (`Trace -> Step -> ToolCall`, initiating message, similar successful traces, and entity `TOUCHED` audit edges) to the deployed ArcadeDB backend rather than adding Neo4j or its SDK.
- Preserve the current separation observed in Hermes and LibreChat: reasoning transport/replay data is not ordinary searchable conversation memory and is never an input to fact extraction.

</specifics>

<deferred>
## Deferred Ideas

No new feature ideas were deferred; the discussion stayed within Phase 49.

### Reviewed Todos (not folded)
- `.planning/todos/pending/approval-resume-defects.md` — reviewed against the live tree and confirmed already closed by the Phase 52 resume implementation and `RESUME-01` live E2E. It is not Phase 49 scope. Its all-or-nothing validation pattern is an existing precedent for `HARN-05`, not a folded deliverable.

</deferred>

---

*Phase: 49-memory-tiers*
*Context gathered: 2026-08-31*
