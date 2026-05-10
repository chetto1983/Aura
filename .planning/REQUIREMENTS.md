# Requirements: Aura

**Defined:** 2026-05-10
**Core Value:** Aura remembers what you tell it and answers questions from durable, searchable memory -- without losing context, corrupting state, or exposing internal machinery to the user.

## v4.0 Requirements

Requirements for v4.0 Production Hardening. Each maps to one roadmap phase.

### Phase 1 -- Fondamenta (Concurrency + Qdrant Readiness)

- [ ] **CONC-01**: Per-user message serialization -- concurrent messages from same user are queued, not processed in parallel
- [ ] **CONC-02**: UserGate exposes `TryAcquire` -- notification paths (scheduler, task dispatch) never deadlock re-entering the same user's gate
- [ ] **CONC-03**: Context leak cleanup -- sessions inactive longer than a configurable threshold are evicted with resource release; eviction uses separate tracking structure, not `sync.Map.Range`
- [ ] **QDRANT-01**: Qdrant startup health validation -- block startup until Qdrant `/health` passes (with configurable timeout); skip full re-embed pass only if collection has `points_count > 0`

### Phase 2 -- LLM Reliability & Tool Intelligence

- [ ] **WIKI-01**: `write_wiki_page` tool with JSON Schema parameters -- replaces any `looksLikeWikiYAML` or text-heuristic wiki page detection
- [ ] **WIKI-02**: Wiki write tool carries `expected_updated_at` to detect concurrent manual edits from the dashboard and prevent silent overwrites
- [ ] **LLM-01**: Variable-temperature retry -- on schema validation or content-quality failure, retry with incremented temperature and error feedback, not blind same-temperature retry
- [ ] **LLM-02**: Error classification separates transient failures (HTTP 429/5xx, timeout) from content failures; variable temperature applies only to content failures
- [ ] **INDEX-01**: Async wiki reindex with backpressure -- wiki writes signal a background worker via buffered channel; coalescing via `select/default` drop; dropped signals are safe
- [ ] **GIT-01**: Explicit git commit tracking on every wiki mutation -- failed commits mark the page as `unversioned` in frontmatter for audit visibility
- [ ] **TOOL-01**: Tool definitions embedded in Qdrant for semantic retrieval -- agent gets context-relevant tools injected into the prompt rather than a full static toolset
- [ ] **TOOL-02**: `tool_search` removed -- tool discovery is automatic via Qdrant semantic matching against user intent and conversation context

### Phase 3 -- Resilience Layer

- [ ] **LLM-03**: Circuit breaker per LLM provider -- opens after N consecutive failures in a configurable window; half-open probe before recovery
- [ ] **LLM-04**: Circuit breaker lock scope is nanoseconds -- state check and update only; lock released before network I/O; concurrent users are never serialized
- [ ] **BUDGET-01**: Per-user token budget with global hard cap -- each user has a configurable soft limit; the global cap is the absolute system maximum
- [ ] **BUDGET-02**: Budget check is inside the UserGate serialization region for atomicity with conversation processing and accurate per-user accounting

### Phase 4 -- Cleanup & Consolidation

- [ ] **CLEAN-01**: All legacy chromem-go vector storage paths removed -- Qdrant is the single source of truth for all embeddings; no second vector store remains
- [ ] **CLEAN-02**: Legacy removal verified across all build tags -- `go build ./...` passes for `linux`, `windows`, and `integration` tags

## Out of Scope

| Feature | Reason |
|---------|--------|
| Redis or external rate limiter | Single-process bot; in-memory token bucket sufficient |
| pgvector, sqlite-vec, or any second vector store | Qdrant is canonical; second store creates sync problems |
| gRPC-based Qdrant client migration | Existing raw HTTP client covers all operations |
| Git push integration | Local commits only for v4.0; push handling deferred |
| Horizontal scaling (100+ users) | Current 1-50 user range; scale concerns noted for future |
| Worker pool libraries (ants, pond) | Single-purpose background worker; stdlib channels sufficient |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| CONC-01 | Phase 1 | Pending |
| CONC-02 | Phase 1 | Pending |
| CONC-03 | Phase 1 | Pending |
| QDRANT-01 | Phase 1 | Pending |
| WIKI-01 | Phase 2 | Pending |
| WIKI-02 | Phase 2 | Pending |
| LLM-01 | Phase 2 | Pending |
| LLM-02 | Phase 2 | Pending |
| INDEX-01 | Phase 2 | Pending |
| GIT-01 | Phase 2 | Pending |
| TOOL-01 | Phase 2 | Pending |
| TOOL-02 | Phase 2 | Pending |
| LLM-03 | Phase 3 | Pending |
| LLM-04 | Phase 3 | Pending |
| BUDGET-01 | Phase 3 | Pending |
| BUDGET-02 | Phase 3 | Pending |
| CLEAN-01 | Phase 4 | Pending |
| CLEAN-02 | Phase 4 | Pending |

**Coverage:**
- v4.0 requirements: 18 total
- Mapped to phases: 18
- Unmapped: 0

---
*Requirements defined: 2026-05-10*
*Last updated: 2026-05-10 after research/roadmap normalization*
