# Roadmap: Aura v4.0 Production Hardening

## Overview

The v4.0 Production Hardening milestone makes Aura safe for concurrent Telegram users, resilient against LLM provider failures, and cleanly migrated to Qdrant as the single source of truth for all embeddings. Four phases build on each other: concurrency foundations unlock safe multi-user operation, LLM/tool reliability makes wiki writes explicit and retries intelligent, the resilience layer protects against provider degradation with circuit breakers and token budgets, and cleanup removes all legacy chromem-go paths with build-tag-verified precision. Every hardening change uses composition (wrapping interfaces) -- zero modifications to the stable `agentruntime/agentloop` packages.

## Phases

- [ ] **Phase 1: Fondamenta (Concurrency Safety)** -- Per-user message serialization via UserGate, TryAcquire for notification paths, inactivity-based context eviction
- [ ] **Phase 2: LLM Reliability & Tool Intelligence** -- Explicit wiki write tool, variable-temperature retry with error classification, async reindex worker, git commit tracking, Qdrant-based tool retrieval
- [ ] **Phase 3: Resilience Layer** -- Circuit breaker per LLM provider with nanosecond lock scope, per-user token budget with atomic accounting inside UserGate
- [ ] **Phase 4: Cleanup & Consolidation** -- Chromem-go removal with build-tag verification, Qdrant startup health gate with warm cache detection

## Phase Details

### Phase 1: Fondamenta (Concurrency Safety)
**Goal**: Users cannot corrupt their conversation state through concurrent messages; system notifications never deadlock; inactive sessions release resources predictably.
**Depends on**: Nothing (first phase)
**Requirements**: CONC-01, CONC-02, CONC-03
**Success Criteria** (what must be TRUE):
  1. A user sending rapid consecutive Telegram messages has them processed sequentially (not in parallel) -- responses maintain causal order and state mutations are serialized
  2. System notifications (scheduler reminder, task dispatch) delivered to a user mid-conversation never deadlock the conversation handler -- the notification path uses `TryAcquire` and proceeds non-blocking when the gate is already held
  3. Sessions idle beyond the configurable threshold release their resources (context cancelled, per-user mutex entry cleared, memory freed) -- eviction uses a separate tracking structure, not `sync.Map.Range`
  4. A user whose message is queued behind a long-running turn receives a clear "still processing" response within the configurable timeout period rather than hanging indefinitely
**Plans**: TBD

### Phase 2: LLM Reliability & Tool Intelligence
**Goal**: The LLM writes wiki pages through explicit tool calls (not text heuristics), retries intelligently on content failures without burning temperature budget on transient errors, and the wiki reindexes asynchronously with git audit tracking.
**Depends on**: Phase 1
**Requirements**: WIKI-01, WIKI-02, LLM-01, LLM-02, INDEX-01, GIT-01, TOOL-01, TOOL-02
**Success Criteria** (what must be TRUE):
  1. The LLM creates or updates wiki pages exclusively by calling the `write_wiki_page` tool with full JSON Schema parameters -- no heuristic text-parsing (`looksLikeWikiYAML` or similar) remains in the codebase
  2. A wiki page edited manually via the dashboard during an LLM conversation is NOT silently overwritten by the tool -- the `expected_updated_at` check detects the concurrent edit and the tool write is rejected with a conflict error
  3. On schema validation or empty-output failure, the LLM retries with incremented temperature and structured error feedback in the prompt; on HTTP 429/5xx or timeout failures, it retries at the same temperature without incrementing
  4. Every wiki mutation creates a git commit via go-git v5.19.0; if the commit fails, the page frontmatter shows `unversioned: true` for audit visibility in the dashboard
  5. Wiki writes trigger async reindexing via a buffered-channel background worker with `select/default` coalescing -- the write call returns immediately without waiting for embedding API latency, and dropped reindex signals are safe (Qdrant already has the previous vector)
  6. The agent receives context-relevant tool definitions injected into the prompt via Qdrant semantic matching; the `tool_search` tool is removed and tool discovery is fully automatic
**Plans**: TBD

### Phase 3: Resilience Layer
**Goal**: LLM provider failures are isolated via circuit breakers without serializing concurrent users, and per-user token budgets prevent runaway cost while preserving accurate per-user accounting.
**Depends on**: Phase 2
**Requirements**: LLM-03, LLM-04, BUDGET-01, BUDGET-02
**Success Criteria** (what must be TRUE):
  1. After N consecutive failures to an LLM provider in a configurable window, the circuit breaker opens -- all subsequent requests to that provider fail fast with a clear error without touching the network
  2. After the configurable reset timeout expires, the circuit breaker enters half-open state and allows a single probe request through; a successful probe closes the breaker, a failure re-opens it and resets the timeout
  3. Ten concurrent LLM requests from different users complete in approximately the single-request network latency (~1x), not 10x -- the circuit breaker state lock is held for nanoseconds (state check and counter update only) and is released before any network I/O
  4. A user exceeding their per-user soft token budget is rejected inside the UserGate mutex region before any LLM call is made; the global hard cap operates as an absolute system maximum that cannot be exceeded regardless of individual user budgets
  5. Per-user budget accounting is atomic with conversation processing -- two rapid consecutive messages from the same user cannot both pass the budget check and cause overspend
**Plans**: TBD

### Phase 4: Cleanup & Consolidation
**Goal**: Qdrant is the single source of truth for all embeddings; no chromem-go vector storage references remain in any build configuration; Qdrant health is validated at startup with proper warm cache detection.
**Depends on**: Phase 3
**Requirements**: CLEAN-01, CLEAN-02, CLEAN-03
**Success Criteria** (what must be TRUE):
  1. `go build ./...` passes for `linux`, `windows`, and `integration` build tags with zero references to chromem-go vector storage, persistence, or embedding management paths
  2. Aura startup blocks until Qdrant `/health` passes (with a configurable timeout defaulting to 120 seconds); if Qdrant is unreachable after the timeout, Aura exits with a clear diagnostic message indicating the Qdrant endpoint and elapsed wait time
  3. If the Qdrant collection already contains vectors (`points_count > 0`), Aura skips the full re-embed pass and proceeds directly to serving traffic -- an empty collection (points_count == 0) triggers a full re-embed from the wiki manifest
  4. All `search_memory` tool calls return results exclusively from Qdrant vector search -- no fallback vector store exists, and the removal of chromem-go does not create gaps in any search or retrieval path
**Plans**: TBD

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Fondamenta (Concurrency Safety) | 0/TBD | Not started | - |
| 2. LLM Reliability & Tool Intelligence | 0/TBD | Not started | - |
| 3. Resilience Layer | 0/TBD | Not started | - |
| 4. Cleanup & Consolidation | 0/TBD | Not started | - |
