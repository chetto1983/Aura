# Phase 49: Memory tiers - Research

**Researched:** 2026-08-31
**Domain:** PostgreSQL-authoritative conversation projection, ArcadeDB graph memory, unified MCP recall, explicit reasoning traces, durable capture, and atomic mutation batches
**Confidence:** HIGH for Aura's current state and plan order; MEDIUM for new retention and retrieval tuning until the required live measurements land

<user_constraints>
## User Constraints (from CONTEXT.md)

The following is copied verbatim from the authoritative Phase 49 context. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:15-58]

### Locked Decisions

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

### Deferred Ideas (OUT OF SCOPE)

No new feature ideas were deferred; the discussion stayed within Phase 49.

### Reviewed Todos (not folded)
- `.planning/todos/pending/approval-resume-defects.md` — reviewed against the live tree and confirmed already closed by the Phase 52 resume implementation and `RESUME-01` live E2E. It is not Phase 49 scope. Its all-or-nothing validation pattern is an existing precedent for `HARN-05`, not a folded deliverable.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| HARN-05 | “Several memory operations apply as one atomic call, validated on the final state, so a correction cannot destroy what it was meant to replace.” [VERIFIED: .planning/REQUIREMENTS.md:33] | Reuse ArcadeDB explicit sessions and striped fact/topology locks, but compile existing upsert/merge/forget semantics into one isolated working state rather than calling their currently multi-transaction public methods. |
| TOOL-05 | “Recalling memory takes one question; the host chooses graph traversal or hybrid search and reports which it used.” [VERIFIED: .planning/REQUIREMENTS.md:41] | Preserve `memory_recall`; extend its typed result and path reporting, then add a live mixed-tier measurement. Do not build another read tool. |
| AUTO-03 | “A durable fact revealed during a turn is captured as part of doing the work — and never harvested from reasoning traces (see CTX-05).” [VERIFIED: .planning/REQUIREMENTS.md:68] | Add an ordered serial capture queue and task-completion flush barrier around the existing host-attributed fact write seam; test user and allowed-tool evidence directly. |
| CTX-05 | “Reasoning traces never reach a summarizer or fact extraction, so scratch-work conclusions cannot be preserved as facts.” [VERIFIED: .planning/REQUIREMENTS.md:116] | Keep `llm.Message` and compaction structurally reasoning-free and make ordinary recall/proactive memory APIs incapable of selecting reasoning records. |
| MEM-01 | “Past conversation is semantically searchable, with Postgres remaining the system of record for turns and ArcadeDB holding a derived per-identity projection.” [VERIFIED: .planning/REQUIREMENTS.md:123] | Add source-ID-addressed eligible-turn projection, deletion propagation, replay, and reconciliation. |
| MEM-02 | “One retrieval call spans short-term conversation and long-term facts.” [VERIFIED: .planning/REQUIREMENTS.md:124] | Independently rank both tiers, fuse all enabled candidates with server-side RRF, hydrate typed evidence, and suppress turns still active in context. |
| MEM-03 | “Reasoning traces are persisted to the graph with edges to the entities they touched, and enter context only when explicitly retrieved.” [VERIFIED: .planning/REQUIREMENTS.md:125] | Project authorized provider-visible reasoning and sanitized tool invocation records into an isolated trace schema; expose only explicit reasoning modes. |
| MEM-06 | “The PRD amendment extending #91 (reasoning persisted to the graph, retrieved only on demand, never summarized or harvested) is committed **before** any reasoning-tier code.” [VERIFIED: .planning/REQUIREMENTS.md:128] | Make live baseline measurement and the #91 extension the first standalone commit; mechanically verify commit order. |
</phase_requirements>

## Summary

Phase 49 is not eight greenfield requirements. Current source confirms one model-facing read (`memory_recall`) already reports fact retrieval as `"graph"`, `"hybrid"`, or `"lexical"`; hidden raw reads are `"graph_schema"`, `"memory_digest"`, `"memory_entities"`, `"memory_facts_about"`, `"memory_reembed"`, and `"memory_search"`. The other model-facing memory tools are mutations, so the roadmap's claim that *every* other memory tool is hidden is too broad. [VERIFIED: internal/agent/mcptools/bridge_policy.go:39-53; cmd/arcadedb-mcp/tool_memory.go:230-307] `CTX-05` is structurally pinned for PostgreSQL history and compaction, but is vacuous until graph-resident reasoning exists. [VERIFIED: internal/conversations/history_reasoning_free_test.go:12-45; internal/conversations/compaction.go:107-156] Explicit ArcadeDB transaction/session primitives and fact locks already exist, while the public upsert, merge, and forget operations still issue several independent writes; therefore HARN-05 is a final-state batch compiler/API task, not a database-substrate task. [VERIFIED: internal/arcadedb/transaction.go:18-98; internal/arcadedb/fact_lock.go:8-55; internal/arcadedb/memory.go:271-354; internal/arcadedb/merge.go:64-114; internal/arcadedb/forget.go:31-89]

The genuinely large work is: (1) a PostgreSQL-authoritative, rebuildable ArcadeDB turn projection; (2) actual cross-tier recall with active-context suppression and progressive exploration; (3) graph reasoning plus an explicit-only read boundary; and (4) the atomic multi-op API. The August 31 locked decisions also make `AUTO-03` genuine implementation work: the current prompt says to write durable facts and provenance is host-derived, but no source defines an ordered capture queue or terminal flush barrier, and tool batches execute concurrently up to four calls. [VERIFIED: internal/agent/prompt.go:110-115; cmd/arcadedb-mcp/tool_memory.go:53-82; internal/agent/llm_agent_parallel.go:40-126] This later decision supersedes the August 30 roadmap sentence “missing only the live measurement.” [VERIFIED: .planning/ROADMAP.md:434-441; .planning/phases/49-memory-tiers/49-CONTEXT.md:40-49]

**Primary recommendation:** plan one measurement/governance commit first, then five implementation slices—projection, unified recall, reasoning graph/isolation, ordered durable capture, and atomic batch—followed by a single live-stack evidence gate. Keep projection failure fail-soft; make only the accepted-capture completion barrier fail honest.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|--------------|----------------|-----------|
| Conversation source, eligible-turn replay, edit/delete authority | PostgreSQL storage | Runner | PostgreSQL is locked as system of record; the runner observes successful commits and supplies active-context information. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:18-26] |
| Conversation and reasoning projection | ArcadeDB storage | Runner/composition root | Each authenticated identity already owns a separate ArcadeDB database; projections must be rebuildable and fail-soft. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:19-26; internal/arcadedb/tenant.go:20-98] |
| Unified recall and progressive exploration | MCP/API backend | ArcadeDB | The published model surface is already `memory_recall`; the MCP host resolves the authenticated identity before a client is selected. [VERIFIED: cmd/arcadedb-mcp/tool_memory.go:243-307; cmd/arcadedb-mcp/identity.go:18-86] |
| Reasoning capture boundary | Runner | ArcadeDB | Provider-visible reasoning and tool events already terminate in the runner; the debug JSONL observer is not an authority. [VERIFIED: internal/runner/runner_reasoning_persist.go:1-112; internal/reasoningtrace/reasoningtrace.go:1-115] |
| Durable fact capture queue and barrier | Runner/task lifecycle | MCP/ArcadeDB | Capture is accepted while work runs, serialized, and joined before completion; fact persistence remains the existing identity-scoped memory API. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:40-44] |
| Atomic memory batch | ArcadeDB client + MCP/API backend | Runner callers | Final-state validation and one transaction belong beside current memory semantics, locks, and authenticated MCP handlers. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:46-49] |

## Project Constraints (from AGENTS.md / CLAUDE.md)

`AGENTS.md` delegates project guidance to `CLAUDE.md`. [VERIFIED: AGENTS.md:1-3]

- Measure on the live stack first; the PRD amendment records the measurement and explicitly says what it did not prove. A unit-only green result closes nothing. [VERIFIED: CLAUDE.md:7-30]
- Read official external-system documentation before implementation, inventory the full installed/dependency surface, and stop for human confirmation before a bespoke component, adapter, wrapper, or protocol. [VERIFIED: CLAUDE.md:149-185]
- Follow existing patterns, scope exactly, avoid dark/dead code, and split/refactor every touched production file to no more than 600 lines. [VERIFIED: CLAUDE.md:186-219]
- Tests require realistic fixtures, race detection, goleak, property tests where indicated, tagged integrations, no CI skip-as-green, aggregate 85% coverage, package-local policy, and at least 70% mutation kill on critical code. Container-gated runtime logic also needs daemon-free unit tests. [VERIFIED: CLAUDE.md:220-224; CLAUDE.md:246-269]
- After each Go edit run `go vet ./...`, `go build ./...`, the touched package test, and touched package race test; WSL is the primary full-quality environment. [VERIFIED: CLAUDE.md:237-264]
- Never plan a migration number. At execution, list `internal/db/migrations/` and take the next free slot. [VERIFIED: CLAUDE.md:54; CLAUDE.md:278-280]
- Any large agent tool follows the deferred-tool pattern. [VERIFIED: CLAUDE.md:227-235]
- The #91-extension amendment is its own commit before reasoning code. [VERIFIED: CLAUDE.md:271-276; .planning/ROADMAP.md:468-471]

## Measured Current State: Shipped vs Open

| Requirement | What current source proves | What it does **not** prove | Planning disposition |
|-------------|----------------------------|---------------------------|----------------------|
| TOOL-05 | `memory_recall` accepts `query` or `entity`; fact results report `"graph"`, `"hybrid"`, or `"lexical"`. [VERIFIED: cmd/arcadedb-mcp/tool_memory.go:230-307; cmd/arcadedb-mcp/tool_memory.go:384-409] | It does not span conversation and fact tiers, suppress active-context turns, or prove the live success criterion through the published recall path. | Preserve and extend in place; add mixed live evidence and bounded telemetry. |
| CTX-05 | `llm.Message` history is reasoning-free and compaction invokes a no-reasoning summarizer. [VERIFIED: internal/conversations/history_reasoning_free_test.go:12-45; internal/conversations/compaction.go:107-156] | No guard yet proves that future graph reasoning cannot enter ordinary recall, preload, compaction, or fact capture. | Retain regression tests and add negative API/integration guards with real graph reasoning present. |
| HARN-05 | Remote session begin/commit/rollback and striped locks exist. Exact session header: `"arcadedb-session-id"`; exact lock stripe count: `256`. [VERIFIED: internal/arcadedb/transaction.go:18-98; internal/arcadedb/fact_lock.go:8-55] | Existing mutators do not evaluate a shared working state or commit once; merge and forget visibly execute multiple commands. | Add one batch surface and an internal pure planning/apply layer; do not reimplement transport. |
| AUTO-03 | Prompt mandate exists; exact actor roles are `"parent"` and `"worker"`; MCP derives run/role from host headers. [VERIFIED: internal/agent/prompt.go:110-115; internal/arcadedb/memory.go:81-97; cmd/arcadedb-mcp/tool_memory.go:53-82] | There is no serial queue, accepted-write ledger, completion barrier, live shell/file capture proof, or source-reference provenance beyond current `MemoryIDs`. | Implement queue/barrier and direct-evidence provenance, then measure live. |
| MEM-01 | PostgreSQL already has owner/RLS-scoped lexical turn search, but it excludes spilled content and deleted turns. [VERIFIED: internal/conversations/store_search.go:16-95] | ArcadeDB schema currently creates only `"Entity"` vertices and `"FACT"` edges; there is no eligible-turn projection, reconciliation, or semantic conversation index. [VERIFIED: internal/arcadedb/memory.go:33-60] | Add a paged source reader plus idempotent derived projection and deletion/rebuild reconciliation. |
| MEM-02 | Existing facts already use dense/full-text fusion and the deployed embedding width is exactly `768`; exact fact retrieval paths are `"hybrid"` and `"lexical"`. [VERIFIED: internal/arcadedb/memory_vector.go:29-60; internal/arcadedb/memory_vector.go:81-131] | No independently ranked conversation tier or cross-tier RRF exists. | Reuse the server-side fusion pattern and hydrate a typed union; tune by measurement. |
| MEM-03 | Authorized final-answer reasoning is persisted in PostgreSQL display-only form and tool invocation events already carry bounded metadata. [VERIFIED: internal/conversations/store_append.go:49-57; internal/agent/event.go:112-166; internal/toolinvocations/store.go:1-108] | `internal/reasoningtrace` is an env-gated JSONL debug sink; no trace/step/tool/entity graph exists. [VERIFIED: internal/reasoningtrace/reasoningtrace.go:1-115] | Project from runner-authorized data and sanitized tool events, never from the debug observer or a second LLM. |
| MEM-06 | Amendment #91 currently authorizes bounded final reasoning, display projection, and exclusion from `llm.Message`, while explicitly not ratifying graph persistence. [VERIFIED: prd.md:1848-1855] | No graph amendment exists; current tests cannot ratify a future storage/retrieval boundary. | First commit only: live measurements plus #91 extension; verify commit ancestry before any reasoning code. |

### What the Baseline Must Measure Before the Amendment

1. Run the real `memory_recall` path on the live authenticated stack and record returned path, empty/weak behavior, latency, and which currently published memory tools are actually visible. Do not infer visibility from the server registration list. [VERIFIED: .planning/ROADMAP.md:425-452]
2. Measure the distribution of eligible PostgreSQL turn bytes, spill frequency, conversation lengths, and current-ladder membership so projection caps and anchored windows are evidence-based. The current turn search silently omits spilled rows, so it cannot be the only backfill reader. [VERIFIED: internal/conversations/store_search.go:24-95; internal/conversations/store_append.go:60-105]
3. Measure provider-visible reasoning sizes, tool-event observation sizes, successful/failed/cancelled trace volume, and target ArcadeDB storage/index growth before freezing segmentation, observation caps, and retention. [VERIFIED: internal/runner/runner_reasoning_persist.go:1-112; internal/reasoningtrace/reasoningtrace.go:42-66]
4. Measure mixed-tier retrieval quality with independent ranks before choosing candidate counts, RRF constants, score thresholds, or forced abstention thresholds. Aura already has one measured server-side fusion implementation to extend. [VERIFIED: internal/arcadedb/document_retrieval.go:24-46; internal/arcadedb/document_retrieval.go:89-151]
5. Record negative scope in the amendment: the baseline does not prove semantic quality at production scale, retention sufficiency, zero future reasoning leakage, crash recovery, or concurrent batch correctness. Those require implementation-time live and adversarial tests. [VERIFIED: CLAUDE.md:7-30]

## Recommended Plan and Order Constraints

| Order | Deliverable | Why it is ordered here | Required evidence before moving on |
|-------|-------------|------------------------|------------------------------------|
| 1 | Live baseline + standalone PRD Amendment #91 extension | MEM-06 is a hard governance predecessor. No reasoning schema/code may be in this commit. [VERIFIED: .planning/ROADMAP.md:442-444; .planning/ROADMAP.md:468-471] | `git log` proves amendment commit precedes every reasoning implementation commit; amendment lists measurements and non-proofs. |
| 2 | PostgreSQL eligible-turn source/read contract + asynchronous ArcadeDB projection/reconciliation | Unified recall needs real short-term candidates and stable source IDs. | Unit replay/idempotency/deletion tests; live rebuild comparison; backend outage never rolls back a committed turn. |
| 3 | Extend `memory_recall` with conversation discovery/open/scroll and cross-tier RRF | Depends on projected conversation data; preserves the one-read surface. | Live question beyond active ladder returns typed evidence and reports `conversations`, `facts`, or `mixed`. |
| 4 | Reasoning graph writer + explicit-only retrieval | Amendment is landed and recall isolation now has a place to enforce reasoning-specific modes. | Live trace/step/tool/entity graph; default preload, ordinary recall, history, compaction, and fact capture remain reasoning-free. |
| 5 | Ordered automatic capture + completion flush barrier | Uses final provenance/source-reference contracts established above; no secondary extraction model. | Live shell/file task proves direct user/tool source, durable-before-completion, serial behavior, worker authority, and honest timeout/failure. |
| 6 | Atomic multi-operation memory API | Can share final graph schema/client work but is independently verifiable; keep external effects out of retries. | Property/concurrency/live rollback tests prove first error and unchanged state; retry restarts from committed state. |
| 7 | Full live-stack reliability and quality gate | Cross-feature success criteria need the final composed system. | Agent-memory evaluator updated to exercise published `memory_recall`, full race/integration/coverage/mutation/goleak gates, real E2E score >9.8. |

## Standard Stack

No new runtime package is needed. Planning should use the installed stack and existing clients.

### Core

| Component | Version / exact contract | Purpose | Why standard here |
|-----------|--------------------------|---------|-------------------|
| Go | `"1.26.6"` [VERIFIED: go.mod:1-4] | Services, runner, MCP host, projectors, transaction planner | Project language and quality toolchain. |
| ArcadeDB | `"arcadedata/arcadedb:26.8.1"` [VERIFIED: compose.yaml:551-555] | Per-identity fact, conversation projection, reasoning graph, full-text/vector/RRF retrieval | Already deployed authority for derived memory; supports native transactional vector/full-text graph data. [CITED: https://docs.arcadedb.com/arcadedb/concepts/vector-search] |
| PostgreSQL | `"postgres:18.4-alpine3.24"` [VERIFIED: compose.yaml:505-520] | Authoritative conversations and owner/RLS source scans | Locked system of record for turns. |
| MCP Go SDK | `"github.com/modelcontextprotocol/go-sdk v1.7.0"` [VERIFIED: go.mod:22-30] | Existing authenticated `memory_recall` and mutation tool contracts | Extends the published surface without a second protocol. |
| pgx | `"github.com/jackc/pgx/v5 v5.10.0"` [VERIFIED: go.mod:19-23] | PostgreSQL source pagination and RLS transactions | Existing DB client; no ORM or new queue dependency needed. |
| OpenTelemetry | `"go.opentelemetry.io/otel v1.46.0"` [VERIFIED: go.mod:34-41] | Bounded retrieval-path/count/error telemetry | Existing tracing surface; never attach queries, content, reasoning, or observations. |

### Supporting

| Component | Version / exact contract | Purpose | When to use |
|-----------|--------------------------|---------|-------------|
| `go.uber.org/goleak` | `"v1.3.0"` [VERIFIED: go.mod:42] | Queue/projector/reconciler shutdown tests | Every background worker or barrier test. |
| `pgregory.net/rapid` | `"v1.3.0"` [VERIFIED: go.mod:52] | Final-state batch and cursor property tests | Generate operation sequences, replay/retry cases, and cursor corruption cases. |
| Existing embedding sidecar | `"EmbeddingGemma-300M Q8_0"`, `"768d"` [VERIFIED: compose.yaml:546-549] | Conversation and reasoning vectors | Reuse initially; tune candidate counts and thresholds by the Phase 49 live corpus. |
| ArcadeDB `vector.fuse` | Default RRF constant documented as `60` [CITED: https://docs.arcadedb.com/arcadedb/concepts/vector-search] | Server-side fusion of dense/full-text and cross-tier RID/score sources | Prefer over a Go rank-fusion implementation; measure Aura-specific source weights/candidate counts. |

### Alternatives Considered

| Instead of | Rejected alternative | Why rejected |
|------------|----------------------|--------------|
| Existing ArcadeDB graph | Neo4j Agent Memory runtime/SDK | It is a design reference only; the phase boundary explicitly rejects it as a dependency. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:6-12] |
| Narrow asynchronous projector + source reconciliation | New general event bus or independent retention store | PostgreSQL is complete authority and D-04 requires rebuildability; a new subsystem adds authority and failure modes. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:18-26] |
| Server-side `vector.fuse` | Hand-written Go RRF | Aura already uses the native function, and official docs define its scored-source contract. [VERIFIED: internal/arcadedb/document_retrieval.go:89-151] [CITED: https://docs.arcadedb.com/arcadedb/concepts/vector-search] |
| Provider-visible reasoning + structured tool events | Post-task reasoning summarizer/extraction LLM | Explicitly prohibited; it can manufacture facts and blur authorization. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:34-44] |

## Package Legitimacy Audit

Not triggered. This phase should install **no external packages**; every recommended component is already in `go.mod` or the deployed Compose stack. If implementation proposes a new dependency, stop and run the project inventory/bespoke checkpoint and the full package-legitimacy protocol before planning installation. [VERIFIED: CLAUDE.md:149-185]

## Architecture Patterns

### System Architecture Diagram

```text
user / provider / tool events
             |
             v
Runner ---- PostgreSQL turn commit (authority)
  |                    |
  | active context     +--> ordered fail-soft projection --> identity ArcadeDB
  | refs                    | upsert eligible turn            | Conversation projection
  |                         | delete/reconcile/rebuild         | FACT graph
  |                                                               Reasoning trace graph
  |
  +--> accepted durable fact candidate --> serial capture queue --> existing fact mutation
  |                                               |
  |                                  completion flush barrier
  |
  +--> authorized reasoning + sanitized tool events --> trace projector

model --> one memory_recall --> authenticated identity resolution --> mode decision
                                                               | semantic: query conversation + facts independently
                                                               | browse/open/scroll: conversation IDs/cursors
                                                               | explicit reasoning: trace similarity/traversal
                                                               v
                                                    typed evidence + effective path
```

### Component Responsibilities

| Existing seam | Extend with | Do not make it responsible for |
|---------------|-------------|--------------------------------|
| `internal/conversations.Store` | A paged owner-scoped eligible-turn reader and an additive committed-turn result/reference. | Embeddings or ArcadeDB writes. Current `AppendTurn` allocates sequence inside its transaction but returns only `error`; never infer the committed sequence with `CountTurns()+1`. [VERIFIED: internal/conversations/store_append.go:31-105; internal/runner/interfaces.go:35-56] |
| Runner/composition root | Post-commit notification, active-context refs, capture barrier lifecycle, authorized trace assembly. | Memory storage semantics or arbitrary graph queries. |
| `internal/arcadedb.Client` | Idempotent projection writes/deletes, trace schema/write/read, mixed recall, pure batch plan + one-session apply. | Tenant identity fallback or a second source of conversation truth. |
| `cmd/arcadedb-mcp` | Additive `memory_recall` schema/modes and one authenticated multi-op mutation handler. | Model-supplied identity/run/worker authority. Current exact host actor headers are `"X-Aura-Actor-Run-Id"` and `"X-Aura-Actor-Role"`. [VERIFIED: cmd/arcadedb-mcp/tool_memory.go:53-82] |
| Existing deletion lifecycle | Projection/trace cleanup signal plus boot/periodic reconciliation. | Rollback of the already-completed PostgreSQL delete because ArcadeDB is down. Current delete finalizes PostgreSQL after ordered teardown and already has a reconciliation pattern. [VERIFIED: internal/runner/runner_delete.go:62-70; internal/runner/runner_delete.go:202-207; internal/runner/runner_delete_reconcile.go:14-110] |

### Pattern 1: Source-Keyed, Fail-Soft Projection

Use the authoritative committed `(conversation ID, turn sequence)` as the stable projection key, store source timestamp/role/provenance, and upsert idempotently after commit. Add boot and periodic reconciliation that scans authoritative eligible turns and removes projection records whose source no longer exists. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:18-26]

Prefer a narrow ordered in-process dispatcher plus source reconciliation over the generic ingestion job table: source reconstruction makes the crash window repairable without changing the turn transaction. Aura's existing delete reconciler proves this lifecycle shape. [VERIFIED: internal/runner/runner_delete_reconcile.go:14-110] The generic job queue is type-scoped and transactional, but should be used only if execution-time inventory proves an enqueue can be atomic with the turn without widening persistence ownership. [VERIFIED: internal/documents/jobs_store.go:20-48; internal/documents/jobs_store.go:125-170; internal/documents/jobs_worker.go:66-112]

### Pattern 2: Independent Retrieval, Native Fusion, Typed Hydration

Run conversation and fact searches independently for semantic mode; hand ArcadeDB scored sources to `vector.fuse`, then hydrate RIDs into the locked `conversation`/`fact` evidence union and derive the locked `conversations`/`facts`/`mixed` effective path. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:28-32] Aura already uses `vector.fuse` with dense and full-text sources and restores returned rank order after hydration. [VERIFIED: internal/arcadedb/memory_vector.go:81-109; internal/arcadedb/memory_vector.go:133-237]

Retrieve a bounded anchor window from PostgreSQL using the hit's source key, not from duplicated neighboring content in ArcadeDB. This keeps edit/delete authority and spill handling in PostgreSQL while the graph stores discovery data. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:18-32] Active-context suppression must be host-derived: the model must not provide conversation/sequence exclusions. Extend an existing trusted runtime metadata/context seam only after the required bespoke-protocol checkpoint; do not add model-visible identity or active-window arguments. [VERIFIED: CLAUDE.md:170-185; cmd/arcadedb-mcp/tool_browse.go:18-20]

### Pattern 3: Authorized Reasoning Trace Builder

Build one trace from the final authorized reasoning already accumulated by the runner and join structured tool events by source/call ID. Segment deterministically by stable content/tool boundaries rather than streaming chunk boundaries. Tool records should derive name/status/duration and already-sanitized bounded fields from the invocation event/ledger, then apply an explicit per-tool allowlist before graph persistence. [VERIFIED: internal/runner/runner_reasoning_persist.go:1-112; internal/agent/event.go:112-166; internal/toolinvocations/store.go:1-108]

Port the reviewed reference shape—trace, ordered steps, tool calls, initiating message, and `TOUCHED` entity edges—but not its dependency. The pinned reference declares `ToolCall`, `ReasoningStep`, and `ReasoningTrace` records and writes explicit touched-entity relationships. [CITED: https://github.com/neo4j-labs/agent-memory/blob/5b4e00af88342707d011bb9d4f2b34503f43a8c3/src/neo4j_agent_memory/memory/reasoning.py] `TOUCHED` edges must come only from trusted entity IDs in allowed tool arguments/structured metadata, never LLM inference over reasoning prose. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:34-44]

### Pattern 4: Serial Accepted-Capture Queue with Terminal Barrier

The queue owns a single worker per task/run, accepts typed candidates from explicit user evidence or allowed tool observations, stamps host-derived run/worker/source references, calls the existing fact persistence path, and records completion/error by sequence. A terminal sentinel/barrier waits only for captures already accepted. The reviewed Hermes implementation uses a single-thread executor and a sentinel flush barrier; this is a lifecycle reference, not a library dependency. [VERIFIED: D:/tmp/hermes-agent/agent/memory_manager.py:714-770; D:/tmp/hermes-agent/agent/memory_manager.py:774-869]

Do not introduce LibreChat's separate `MemoryRun` LLM: its useful analog is only serialized writes and a bounded join, while Aura's locked decision forbids a second reasoning/fact-harvesting model. [VERIFIED: D:/tmp/LibreChat/packages/api/src/agents/memory.ts:149-158; D:/tmp/LibreChat/packages/api/src/agents/memory.ts:969-1009; D:/tmp/LibreChat/api/server/controllers/agents/client.js:4070-4072; D:/tmp/LibreChat/api/server/controllers/agents/client.js:4414-4418]

### Pattern 5: Pure Final-State Batch Compiler + One Transaction

Validate request shape and authenticated authority before opening a transaction. Under existing fact/topology locks, read committed state into an isolated working copy, apply every operation in order, report the first error without writes, validate the final state, then emit all database changes through one explicit ArcadeDB session and commit once. On retryable conflict, discard the working copy and rebuild from newly committed state; embeddings/network calls and other external effects stay outside the retried transaction. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:46-49; internal/arcadedb/write_retry.go:1-88]

Hermes' reviewed batch copies state, applies operations sequentially, validates only the final result, saves once, and returns an unchanged-state error on the first invalid operation. [VERIFIED: D:/tmp/hermes-agent/tools/memory_tool.py:586-703] Existing Aura mutators cannot simply be called from inside the batch because upsert, merge, and forget issue their own reads/writes/reindex steps; extract/reuse their validation and planning rules while leaving legacy entry points as thin single-operation callers. [VERIFIED: internal/arcadedb/memory.go:271-354; internal/arcadedb/merge.go:64-114; internal/arcadedb/forget.go:31-89]

### Anti-Patterns to Avoid

- **A second recall tool:** violates D-09 and fragments published behavior. Extend `memory_recall`. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:28-32]
- **Using `SearchConversationTurns` as the projector's complete source:** it is query-driven and excludes spilled content; write a paged authoritative reader. [VERIFIED: internal/conversations/store_search.go:24-95]
- **Inferring a just-committed sequence:** `AppendTurn` may allocate under a lock but returns only `error`; return the committed reference additively. [VERIFIED: internal/conversations/store_append.go:60-105]
- **Using `internal/reasoningtrace` as graph input:** it is an env-gated debug JSONL sink with capped fields, not the authorized storage contract. [VERIFIED: internal/reasoningtrace/reasoningtrace.go:1-115]
- **Putting raw tool results or reasoning into the conversation tier:** violates D-05 and creates secret/context leakage. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:23-26]
- **A post-task summarizer or fact extraction LLM:** explicitly prohibited and would let generated prose become evidence. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:34-44]
- **Calling current mutators one-by-one and rolling back manually:** cannot guarantee final-state validation or unchanged live state. Use one session commit. [VERIFIED: internal/arcadedb/transaction.go:23-98]
- **Running external effects inside a transaction retry:** current retry comments prohibit it; rebuild from committed DB state only. [VERIFIED: internal/arcadedb/write_retry.go:1-88]
- **Making projection failure invalidate PostgreSQL turns:** derived memory is fail-soft and repairable. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:18-26]
- **Treating the capture barrier like projection delivery:** only accepted AUTO-03 captures have the hard completion guarantee; ordinary turn projection remains asynchronous/fail-soft. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:23-26; .planning/phases/49-memory-tiers/49-CONTEXT.md:40-44]

## Don't Hand-Roll

| Problem | Don't build | Use instead | Why |
|---------|-------------|-------------|-----|
| Rank fusion | A Go RRF/reranker layer | ArcadeDB `vector.fuse` and existing Aura hydration pattern | Native scored-source fusion already exists and is transactional. [CITED: https://docs.arcadedb.com/arcadedb/concepts/vector-search] |
| Remote transaction protocol | A custom session transport | Existing `beginTx`, `commandInTx`, `queryInTx`, `commitTx`, `rollbackTx` | Exact HTTP session handling already exists. [VERIFIED: internal/arcadedb/transaction.go:18-98] |
| Embedding service | A new model/client | Existing 768-dimensional embedding sidecar/client | Facts already use it and enforce exact width. [VERIFIED: internal/arcadedb/memory_vector.go:29-71] |
| Identity or actor propagation | Model-visible tenant/run fields | Existing authenticated `_meta` identity and host-derived actor headers | Prevents cross-tenant/worker authority spoofing. [VERIFIED: cmd/arcadedb-mcp/identity.go:18-86; cmd/arcadedb-mcp/tool_memory.go:53-82] |
| Reasoning extraction | Hidden-CoT reconstruction or summarizer | Authorized provider-visible reasoning already held by the runner | Required by D-11 and CTX-05. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:34-44] |
| Durable source queue database | A new authority/event bus | Post-commit ordered dispatcher plus authoritative reconciliation | Source is PostgreSQL and replay is complete; choose the narrowest existing mechanism. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:18-26; .planning/phases/49-memory-tiers/49-CONTEXT.md:51-56] |
| Batch compensation | Inverse operations after partial writes | Pure final-state planner plus one ArcadeDB transaction | Compensation cannot recreate deleted provenance or eliminate observation windows. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:46-49] |

## Runtime State Inventory

| Category | Items found | Action required |
|----------|-------------|-----------------|
| Stored data | Existing PostgreSQL turns are authoritative; existing per-identity ArcadeDB databases contain `"Entity"` + `"FACT"` only. [VERIFIED: internal/arcadedb/memory.go:33-60] | Schema migration/widening plus paged backfill/reconciliation; do not migrate authority or duplicate neighbors as retained conversation content. |
| Live service config | Live `docker compose ps` on 2026-08-31 reported PostgreSQL, ArcadeDB, embedder, and MCP healthy; the configured images are PostgreSQL `18.4-alpine3.24` and ArcadeDB `26.8.1`. [VERIFIED: live `docker compose ps`; compose.yaml:505-520; compose.yaml:551-555] | Run baseline and final E2E against this stack; rebuild/publish the MCP image after implementation. |
| OS-registered state | No Phase 49 memory schema or task scheduler/service registration was found; services are Compose-managed. [VERIFIED: compose.yaml:505-650] | None beyond normal Compose rebuild/restart. |
| Secrets/env vars | Identity, ArcadeDB credentials, actor run/role, and embedding settings already exist; no rename is required. [VERIFIED: cmd/arcadedb-mcp/identity.go:18-86; cmd/arcadedb-mcp/tool_memory.go:53-82; .env.example:315-348] | Preserve names and fail-closed derivation; do not log values or content. Add configuration only for measured bounds/timeouts if no existing limit fits. |
| Build artifacts | MCP Compose service is pinned to a commit/digest image. [VERIFIED: compose.yaml:628-646] | Execution must rebuild and final evidence must state candidate commit/image provenance; source tests alone do not prove the running service changed. |

## Common Pitfalls

### Pitfall 1: Losing the Actual Committed Turn Key

**What goes wrong:** the projector guesses a sequence after `AppendTurn`, races another append, and overwrites or links the wrong source. **Why:** sequence allocation occurs inside the transaction while the interface returns only `error`. [VERIFIED: internal/conversations/store_append.go:60-105; internal/runner/interfaces.go:35-56] **Avoid:** add an additive committed-turn result and keep legacy wrappers for existing callers; never use `CountTurns()+1`.

### Pitfall 2: Projecting Only Searchable Inline Rows

**What goes wrong:** rebuild silently loses large spilled turns. **Why:** current lexical search excludes `content_sidecar_path IS NOT NULL`. [VERIFIED: internal/conversations/store_search.go:24-95] **Avoid:** build a paginated eligible-turn source reader that resolves sidecars under existing guards and is independent of a query.

### Pitfall 3: Duplicate Active Context

**What goes wrong:** a current turn appears both in the deterministic ladder and memory recall, consuming context and overweighting it. **Avoid:** send bounded host-derived active source refs to recall and filter them before fusion/window hydration; never ask the model to declare exclusions. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:23-32]

### Pitfall 4: Reasoning Leaks Through an “Ordinary” Shared Query

**What goes wrong:** adding trace types to a generic graph search makes proactive preload, compaction, or fact extraction see them. **Avoid:** separate ordinary memory query functions/types from explicit reasoning query functions and assert negative paths in unit and live tests. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:34-44]

### Pitfall 5: Turning Sanitized Preview into a Secret Archive

**What goes wrong:** a bounded preview still contains tokens, file contents, or credentials. **Avoid:** persist only per-tool allowlisted arguments and a second redaction/cap at the graph write chokepoint; sidecar/blob references remain references. Existing tool invocation persistence already centralizes caps/redaction and should be reused. [VERIFIED: internal/toolinvocations/store.go:1-108]

### Pitfall 6: AUTO-03 “Accepted” Means Only Enqueued

**What goes wrong:** the assistant publishes completion before a queued fact fails or the process exits. **Avoid:** monotonically sequence accepted captures, flush through the highest accepted sequence, use a bounded terminal barrier, and return an honest completion error/notice when durability cannot be proven. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:40-44]

### Pitfall 7: Final-State Batch Implemented as Public Mutator Loop

**What goes wrong:** users observe intermediate states, and rollback cannot restore deleted edges/provenance. **Avoid:** pure in-memory plan first, one transaction second; refactor current single operations to share rules rather than nesting their transports. [VERIFIED: internal/arcadedb/memory.go:271-354; internal/arcadedb/merge.go:64-114; internal/arcadedb/forget.go:31-89]

### Pitfall 8: Retrying the Same Stale Working Copy

**What goes wrong:** a concurrent change is overwritten or repeated. **Avoid:** acquire deterministic locks, and on retry discard the plan, reread committed state, reapply all operations, revalidate final state, then commit. Existing retry policy uses exact attempt cap `20` for `503`/`409`; batch logic may reuse only after live contention measurement. [VERIFIED: internal/arcadedb/write_retry.go:1-88]

## Code Examples

These are implementation skeletons, not frozen public schemas. Future exact type/operation/mode names remain a Wave 0 contract task and must be ratified by tests before publication. [ASSUMED]

### Post-Commit Projection Contract

```go
// Recommendation: the append implementation returns the source key allocated
// inside the authoritative PostgreSQL transaction. Legacy callers may discard it.
type CommittedTurn struct {
    ConversationID string
    Seq            int
    CreatedAt      time.Time
}

func (r *Runner) persistEligibleTurn(ctx context.Context, p conversations.AppendTurnParams) error {
    committed, err := r.conversations.AppendTurnResult(ctx, p)
    if err != nil {
        return err
    }
    // Offer after commit. Failure is observed and reconciled; it does not undo PG.
    r.turnProjector.Offer(committed)
    return nil
}
```

Source pattern: current allocation is transaction-local and returns only `error`. [VERIFIED: internal/conversations/store_append.go:60-105]

### Native Fusion then Typed Hydration

```go
func (c *Client) RecallSemantic(ctx context.Context, q RecallQuery) (RecallResult, error) {
    // 1. independently produce scored conversation and fact sources
    // 2. call the existing ArcadeDB vector.fuse pattern
    // 3. remove host-provided active source keys
    // 4. hydrate fact records and PG-backed anchored conversation windows
    // 5. derive effective path and explicit abstention from returned evidence
    return c.recallSemantic(ctx, q)
}
```

Source pattern: native fusion sources return RIDs/scores and Aura restores rank after hydration. [VERIFIED: internal/arcadedb/memory_vector.go:81-109; internal/arcadedb/memory_vector.go:133-237]

### Final-State Batch Retry Boundary

```go
func (c *Client) ApplyBatch(ctx context.Context, req BatchRequest, actor Actor) (BatchResult, error) {
    if err := validateRequest(req, actor); err != nil {
        return BatchResult{}, err
    }
    prepared, err := prepareExternalInputs(ctx, req) // embeddings, no DB writes
    if err != nil {
        return BatchResult{}, err
    }
    return withWriteRetry(ctx, func() (BatchResult, error) {
        committed, err := c.loadBatchState(ctx, req)
        if err != nil {
            return BatchResult{}, err
        }
        plan, err := compileFinalState(committed, prepared)
        if err != nil {
            return BatchResult{}, unchangedStateError(err)
        }
        return c.applyPlanInOneSession(ctx, plan)
    })
}
```

Source pattern: whole-decision retry and no external effects inside the retry. [VERIFIED: internal/arcadedb/write_retry.go:1-88] ArcadeDB explicit sessions use one returned session header for commands, queries, commit, and rollback. [CITED: https://docs.arcadedb.com/arcadedb/reference/http-api/http]

## State of the Art

| Old/current approach | Phase 49 approach | Impact |
|----------------------|-------------------|--------|
| Fact-only `memory_recall` with `query`/`entity` | One additive typed surface covering conversation, facts, browse/open/scroll, and explicit reasoning | Preserves the published entry point while making MEM-02 real. [VERIFIED: cmd/arcadedb-mcp/tool_memory.go:243-307] |
| PostgreSQL lexical turn search | PG authority + ArcadeDB semantic discovery projection + PG anchored-window hydration | Searchable history becomes semantic without moving source-of-truth ownership. |
| Display-only PostgreSQL reasoning + JSONL debug observer | Explicit graph trace projection from authorized runner data | Reasoning gains audit/reuse only by explicit retrieval. |
| Prompt-directed synchronous memory write | Ordered accepted-capture queue + terminal durability barrier | AUTO-03 becomes a host-enforced lifecycle guarantee. |
| Independently transactional upsert/merge/forget | Pure final-state compilation + one ArcadeDB transaction | HARN-05 gets unchanged-state failure and correct concurrency retries. |

**Deprecated/outdated for planning:** `AURA_CONTEXT_MEMORY_RECALL` is dead; the live preload seam is the digest plus `AURA_MEMORY_PRELOAD_ENABLED`. [VERIFIED: .planning/ROADMAP.md:445-448; internal/runner/runner_context.go:78-141] The current latency evaluator's exact path is `"cli_identity_mcp_search"` and its live test targets `"^TestAgentMemoryCLILiveSearchP95$"`; it proves raw search latency, not published mixed-tier `memory_recall`. [VERIFIED: scripts/agent_memory_eval.py:23-37; scripts/agent_memory_eval.py:48-55]

## Assumptions Log

| # | Claim | Section | Risk if wrong |
|---|-------|---------|---------------|
| A1 | Exact future Go type names and new file/package boundaries in the code examples are illustrative only. [ASSUMED] | Code Examples | Planner could freeze an invented API instead of fitting the smallest current seam. |
| A2 | A narrow in-process ordered projector plus source reconciliation is sufficient; a durable outbox is unnecessary if boot/periodic reconciliation meets measured recovery objectives. [ASSUMED] | Architecture Pattern 1 | Crash visibility/recovery SLO may require an atomic outbox after measurement. |
| A3 | Initial reasoning retention should start near the reference's example of 90 days for useful traces and 30 days for failures, then be replaced by Aura's measurement. [ASSUMED] [CITED: https://neo4j.com/labs/agent-memory/how-to/reasoning-traces/] | Open Questions | Storage/privacy cost could be materially different for Aura. |
| A4 | Existing trusted MCP/runtime metadata can carry bounded active-turn references without inventing a parallel protocol. [ASSUMED] | Architecture Pattern 2 | If no supported extension point exists, implementation requires the bespoke-protocol human checkpoint. |

## Open Questions

1. **Which existing trusted runtime seam carries active-context source keys into `memory_recall`?**
   - What we know: identity already arrives outside model-visible input; active-context suppression is locked. [VERIFIED: cmd/arcadedb-mcp/tool_browse.go:18-20; .planning/phases/49-memory-tiers/49-CONTEXT.md:23-32]
   - Gap: current MCP recall receives identity but no active ladder references. [VERIFIED: cmd/arcadedb-mcp/tool_memory.go:243-307]
   - Recommendation: inventory SDK request metadata and the bridge call envelope during Wave 0; extend an existing trusted metadata field if supported. If not, stop for the CLAUDE bespoke-protocol checkpoint before code.

2. **What are the exact public mode/operation/cursor contracts?**
   - What we know: required behaviors and discriminator/path values are locked, but exact enum spellings and batch operation set are not. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:28-32; .planning/phases/49-memory-tiers/49-CONTEXT.md:46-49]
   - Recommendation: first test in each API slice freezes the smallest additive schema, backwards compatible with current `query`/`entity`; do not expose implementation RIDs as durable cursors.

3. **What trace retention is justified?**
   - What we know: shorter failed/cancelled retention is allowed, identity/conversation deletion must dominate. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:51-56]
   - Recommendation: measure actual trace byte/index growth, start with bounded defaults only after the amendment records the result, and make deletion propagation invariant.

4. **Does projection dispatch need a transactional outbox?**
   - What we know: projection is reconstructible and failure cannot invalidate the source turn. [VERIFIED: .planning/phases/49-memory-tiers/49-CONTEXT.md:18-26]
   - Recommendation: prove boot/periodic convergence under crash-after-commit first; use an outbox only if measured repair latency or source paging cannot meet the operational target.

## Environment Availability

| Dependency | Required by | Available | Version / observation | Fallback |
|------------|-------------|-----------|-----------------------|----------|
| Go | All implementation/tests | ✓ | `go1.26.6 windows/amd64` [VERIFIED: live `go version`; go.mod:1-4] | WSL primary full gate. |
| WSL | race/quality/mutation | ✓ | WSL `2.7.1.2`, Linux kernel `6.18` [VERIFIED: live `wsl --version`] | CI Linux; Windows race is alternate only. [VERIFIED: CLAUDE.md:246-269] |
| Docker / Compose stack | Live DB/MCP/evaluator | ✓ | Docker `29.7.2`; relevant services healthy on 2026-08-31. [VERIFIED: live `docker --version`; live `docker compose ps`] | None for live acceptance. |
| PostgreSQL | MEM-01 authority | ✓ | configured `18.4-alpine3.24` [VERIFIED: compose.yaml:505-520] | None. |
| ArcadeDB | MEM-01/02/03/HARN-05 | ✓ | configured/live `26.8.1` [VERIFIED: compose.yaml:551-555; live `docker compose ps`] | None. |
| Embedding sidecar | Semantic/vector legs | ✓ | configured 768-dimensional EmbeddingGemma [VERIFIED: compose.yaml:546-549; internal/arcadedb/memory_vector.go:29-71] | Lexical fallback already exists for facts; final MEM-01/02 acceptance still requires semantic live proof. |
| Node/npm | GSD tooling only | ✓ | Node `24.18.0`, npm `12.0.0` [VERIFIED: live version probes] | Not a Phase 49 runtime dependency. |

**Missing dependencies with no fallback:** none detected. The live acceptance remains blocked if the Compose services or real authenticated identity route are not running at execution time.

## Validation Architecture

Nyquist validation, security enforcement, TDD mode, and ASVS Level 1 are enabled. Exact config values: `"nyquist_validation": true`, `"security_enforcement": true`, `"security_asvs_level": 1`, `"tdd_mode": true`. [VERIFIED: .planning/config.json:20-54]

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go `testing`; `go.uber.org/goleak v1.3.0`; `pgregory.net/rapid v1.3.0` [VERIFIED: go.mod:42-52] |
| Config | Build tags and package-local test files; evaluator in `scripts/agent_memory_eval.py`. [VERIFIED: scripts/agent_memory_eval.py:40-85] |
| Quick baseline | `go test ./internal/arcadedb ./internal/conversations ./internal/runner ./cmd/arcadedb-mcp` — passed 2026-08-31. [VERIFIED: live command output] |
| Live ArcadeDB | `go test -race -tags=arcadedb_integration -count=1 ./internal/arcadedb/` [VERIFIED: scripts/agent_memory_eval.py:48-55] |
| Live MCP | `go test -race -tags=arcadedb_integration -count=1 -run '^TestAgentMemoryMCPLive' ./cmd/arcadedb-mcp/` [VERIFIED: scripts/agent_memory_eval.py:48-55] |
| Full evaluator | `python scripts/agent_memory_eval.py --tier all` [VERIFIED: scripts/agent_memory_eval.py:560-594] |
| Project gate | WSL `make quality-full`, then `bash scripts/coverage_docker.sh`; mutation spot-check on critical files. [VERIFIED: CLAUDE.md:246-269] |

### Phase Requirements → Test Map

| Req ID | Behavior | Type | Automated command / target | Exists? |
|--------|----------|------|----------------------------|---------|
| MEM-01 | Eligible user/final assistant turns project idempotently; spill/edit/delete/rebuild converge; PG failure/backend failure semantics correct | unit + live integration | New focused tests in conversations/ArcadeDB/runner; package commands above | ❌ Wave 0 |
| MEM-02 | Both tiers queried independently, RRF order stable, active turns suppressed, anchored windows/cursors bounded | unit/property + live E2E | Extend MCP live suite and evaluator to call published `memory_recall` | ❌ Wave 0 |
| MEM-03 | Authorized trace/steps/tools/TOUCHED persisted; ordinary paths cannot read them | unit + live integration | New ArcadeDB/runner/MCP tests plus history regression | ❌ Wave 0 |
| MEM-06 | Amendment commit precedes reasoning code | repository contract | Scripted `git log` ancestry/path check | ❌ Wave 0 |
| TOOL-05 | One question; host path reported; weak/empty abstains | unit + live MCP | Extend existing `tool_memory_test.go` and `TestAgentMemoryMCPLive*` | ⚠️ fact-only coverage exists |
| AUTO-03 | User/tool fact captured directly, serially, durable before completion; no reasoning source | unit/goleak/race + live shell/file E2E | New runner queue/barrier tests and evaluator scenario | ❌ Wave 0 |
| CTX-05 | Reasoning absent from history, compaction, proactive context, ordinary recall, capture | structural/unit + live negative | Keep `TestHistoryTypesAreStructurallyReasoningFree`; add graph-resident negatives | ⚠️ pre-graph regression exists |
| HARN-05 | Final-state validation, first error, unchanged state, idempotent replay, concurrent retry from committed state | rapid/property + race + live integration | New pure planner tests and ArcadeDB live transaction tests | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** touched package unit tests and race tests; fast full four-package baseline where practical.
- **Per wave merge:** `go vet ./...`, `go build ./...`, touched integration tags, and the agent-memory deterministic evaluator.
- **Phase gate:** full evaluator with updated published-path cases, `make quality-full`, Docker coverage, mutation ≥70% on batch/projection/isolation/capture critical files, goleak, and real E2E >9.8. [VERIFIED: CLAUDE.md:220-224; CLAUDE.md:246-269]

### Wave 0 Gaps

- [ ] Freeze additive `memory_recall` mode/evidence/cursor schema, active-context trusted metadata route, and atomic batch operation schema in tests.
- [ ] Add an authoritative paged eligible-turn source fixture including spilled content, edit/delete, and replay.
- [ ] Update the evaluator: the current exact latency path `"cli_identity_mcp_search"` exercises raw search, not the final one-read mixed recall contract. [VERIFIED: scripts/agent_memory_eval.py:23-37; scripts/agent_memory_eval.py:48-55]
- [ ] Add daemon-free pure tests for projection decisions, trace segmentation/redaction, cursor codec, capture sequencing/barrier, and final-state batch compiler.
- [ ] Add live authenticated cases for past-ladder mixed recall, explicit-only reasoning, mid-task shell/file capture, and atomic rollback/concurrency.

## Security Domain

### Applicable ASVS Categories

| ASVS category | Applies | Standard control |
|---------------|---------|------------------|
| V2 Authentication | No new mechanism | Preserve existing OAuth subject/authenticated identity; no fallback identity. [VERIFIED: cmd/arcadedb-mcp/identity.go:18-86] |
| V3 Session Management | Yes, service transaction session only | Use existing opaque ArcadeDB session header and always commit/rollback; never expose it to model output. Exact header `"arcadedb-session-id"`. [VERIFIED: internal/arcadedb/transaction.go:18-98] |
| V4 Access Control | Yes | PostgreSQL owner/RLS plus one ArcadeDB database per authenticated identity; host-derived `"parent"`/`"worker"` authority and principal-only supersession. [VERIFIED: internal/arcadedb/memory.go:81-97; internal/arcadedb/fact_authority.go:1-64] |
| V5 Input Validation | Yes | MCP typed schema, rune/candidate caps, parameterized DB calls, Lucene escaping, cursor validation bound to the authenticated identity/source. [VERIFIED: internal/arcadedb/client.go:48-70; internal/arcadedb/forget.go:92-143; internal/arcadedb/memory_vector.go:133-176] |
| V6 Cryptography | No new cryptography | Reuse established OAuth/credential derivation. If a tamper-resistant client-visible cursor is required, use an existing project crypto primitive after inventory; do not hand-roll signing. |

### Threat Model

| Threat | STRIDE | Mitigation / verification |
|--------|--------|---------------------------|
| Model spoofs identity, worker role, run, active-turn exclusions | Spoofing / elevation | Keep all authority in authenticated `_meta`/host headers; reject missing/invalid actor before reads/writes. |
| Cross-identity projection or cursor replay | Information disclosure | Per-identity DB selection, PG RLS, cursor/source lookup under current identity, adversarial foreign-ID tests. |
| Recalled conversation/tool text injects instructions | Elevation / disclosure | Return typed quoted evidence with provenance; never treat memory content as system/tool authority; bound windows. |
| Reasoning enters ordinary context or capture | Information disclosure / repudiation | Separate query types and explicit mode gate; negative tests across recall, preload, history, compaction, capture. |
| Tool record persists secrets/blob/raw output | Information disclosure | Per-tool argument allowlist, redaction and cap at persistence chokepoint, reference sidecars rather than copy. |
| Duplicate/replayed projection or capture | Tampering | Stable source keys, idempotent upsert/provenance enrichment, serial accepted sequence, replay tests. |
| Batch leaks intermediate state or retry side effects | Tampering | Isolated working copy, one transaction, rollback, whole-plan retry, no external effects inside retry. |
| Delete leaves conversation/reasoning copies | Information disclosure | Source-authoritative reconciliation, explicit projection/trace deletion, identity purge invariant, live delete tests. |
| Telemetry leaks query/content/reasoning | Information disclosure | Record only bounded retrieval path, tier counts, status/error class, and timing; no content or identifiers beyond approved correlation. |

## Sources

### Primary (HIGH confidence)

- Aura current source and planning contracts cited inline, read from `ef2279548d961f4be28e9649156c02eb37a5649a` on 2026-08-31.
- Hermes Agent local reference at `4f22543509d1b91dc45bcb369447126c5eb14fb7` — batch final-state behavior, ordered off-thread writes, sentinel flush, progressive session search.
- LibreChat local reference at `240e9e920f5eaa0197448507540b1aa7bbdd1b79` — serialized writes and bounded memory-run join; explicitly not adopted as an extraction architecture.
- Neo4j Agent Memory pinned source `5b4e00af88342707d011bb9d4f2b34503f43a8c3` — trace/step/tool/message/TOUCHED schema reference only.

### Official Documentation (MEDIUM confidence through documentation fallback)

- [ArcadeDB vector search](https://docs.arcadedb.com/arcadedb/concepts/vector-search) — vector index, `vector.fuse`, scored sources, RRF behavior.
- [ArcadeDB HTTP API](https://docs.arcadedb.com/arcadedb/reference/http-api/http) — remote transaction sessions, session header, commit/rollback/expiration.
- [PostgreSQL transaction retry guidance](https://www.postgresql.org/docs/current/mvcc-serialization-failure-handling.html) — complete decision-transaction retry semantics.
- [Neo4j Agent Memory reasoning traces](https://neo4j.com/labs/agent-memory/how-to/reasoning-traces/) — design and retention examples, not an Aura runtime dependency.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — exact installed dependencies, configured images, and live service availability were inspected; no new package is recommended.
- Architecture: HIGH — locked context, current public symbols, both required local reference corpora, and official ArcadeDB behavior align.
- Shipped/open boundary: HIGH — based on current source rather than roadmap prose; current four-package baseline passed.
- Tuning/retention/active-context transport: MEDIUM — intentionally left measurement- or checkpoint-gated.
- Pitfalls/security: HIGH for code-path boundaries; MEDIUM for unmeasured production thresholds.

**Research date:** 2026-08-31  
**Valid until:** 2026-09-30, or immediately stale after Phase 49 implementation begins or the deployed ArcadeDB/MCP image changes.
