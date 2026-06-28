---
phase: 30-retrieval-memory-hardening
plan: 03
subsystem: documents
tags: [retrieval, rerank, two-stage, vector-seed, next-chunk, graph-expand, fail-soft, agent-memory, fulltext, fastpath, kv-cache-invariant]

# Dependency graph
requires:
  - phase: 30-retrieval-memory-hardening (30-01 rerank foundation)
    provides: internal/rerank.RerankClient + rerank.Scored (the fail-soft identity contract Service.Retrieve and the Python recall hook wire into) and config.RerankBaseURL
  - phase: 30-retrieval-memory-hardening (30-02 widened ingest + NEXT_CHUNK)
    provides: the idempotent (:Chunk)-[:NEXT_CHUNK]->(:Chunk) reading-order edges the graph-expand stage traverses, plus the widened-format chunk corpus
  - phase: 11-memory-ingestion (internal/documents)
    provides: Service/Searcher/EmbeddingClient/Indexer, the SearchHit projection, and the chunk_embedding (384d cosine) + chunk_text fulltext indexes the seed stage queries
provides:
  - "documents.Service.Retrieve(ctx, req) — two-stage retrieval: vector/BM25 SEED (<=15) -> rerank the SEEDS -> 1-hop :NEXT_CHUNK graph-expand only the WINNERS; fail-soft to fulltext seeds (no embedder) and to seed order (rerank absent/identity/below-threshold); message-prefix-safe"
  - "documents.Reranker interface (mirrors *rerank.RerankClient) + Service.{Knowledge,QueryEmbedder,Reranker,RerankThreshold} optional fields; with Reranker==nil Retrieve IS Search (no regression)"
  - "document_search tool routed through Retrieve (DocumentSearchBackend widened Search->Retrieve); docsToolSearcher wires &rerank.RerankClient{BaseURL: cfg.RerankBaseURL}"
  - "neo4j_agent_memory.rerank.rerank(query, docs, base_url) -> list[int] — stdlib-only fail-soft Python mirror; BaseMemory.rerank_results hook wired into ShortTermMemory.search_messages (reorders embedding recall off the event loop, fail-soft to embedding order)"
affects: [30-04, 30-05, gsd-verify-work, gsd-secure-phase]

# Tech tracking
tech-stack:
  added:
    - "No new Go module (internal/documents now imports internal/rerank, both first-party, stdlib-only). No new Python dependency — the agent-memory rerank client uses stdlib urllib (faithful mirror of the Go net/http client; httpx is only an optional extra)."
  patterns:
    - "Two-stage fast-path retrieval (spike 070 Q4): rerank the small SEED pool (not the expanded pool), then graph-expand only the winners for context -- 5x faster than expand-then-rerank, quality preserved"
    - "Fail-soft collaborator fields on a Service: optional Knowledge/QueryEmbedder/Reranker degrade the pipeline stage-by-stage (vector->fulltext seed, rerank->seed order, expand->winners-only) so optional infra never blocks retrieval; Reranker==nil is byte-for-byte the prior Search path"
    - "Non-monotonic guard hook (RerankThreshold) reserved in the structural seam so RET-05 can tune the rerank-trust gate without re-plumbing"
    - "Python recall rerank mirrors the Go identity contract and runs the blocking stdlib HTTP call off the asyncio event loop via asyncio.to_thread; it only REORDERS already-scoped results (no row added, scope cannot widen)"

key-files:
  created:
    - internal/documents/retrieve.go
    - internal/documents/retrieve_test.go
    - docker/agent-memory/src/neo4j_agent_memory/rerank.py
    - docker/agent-memory/tests/test_rerank.py
  modified:
    - internal/documents/service.go
    - internal/documents/search.go
    - internal/agent/tools/document_search.go
    - internal/agent/tools/document_search_test.go
    - cmd/aura/docs.go
    - docker/agent-memory/src/neo4j_agent_memory/core/memory.py
    - docker/agent-memory/src/neo4j_agent_memory/memory/short_term.py
    - docs/document-ingestion.md

key-decisions:
  - "Reranker==nil short-circuits Retrieve to Search, guaranteeing the no-regression unit gate literally (no-reranker == current fulltext order). The rerank-absent degraded path (reranker present but sidecar down/identity) keeps the pre-rerank RRF/vector seed order, matching the host directive 'rerank-absent => RRF/vector order'. This reconciles the plan prohibition ('default path stays the current fulltext order') with the host note in one design: nil reranker => fulltext; degraded reranker => seed order."
  - "Expansion traverses :NEXT_CHUNK only, NOT the plan's literal (:Chunk)-[:NEXT_CHUNK|:HAS_CHUNK]-(:Chunk). :HAS_CHUNK is a Document->Chunk edge (verified in indexer.go), so it can never match a 1-hop chunk-to-chunk pattern; including it would be dead code (CLAUDE.md no-dead-code). :NEXT_CHUNK is the reading-order chunk-to-chunk context edge the spike used. The grep acceptance (':NEXT_CHUNK for expansion') is satisfied."
  - "The Reranker hook + RerankThreshold gate are added but the threshold defaults to 0 (permissive): rerank applies whenever its top score is non-negative, identity (all-0) keeps seed order, and a high threshold keeps seed order. RET-05 tunes the guard with eval data; this plan only ships the structural seam (per the plan prohibition 'the non-monotonic guard is enforced in RET-05; here the structural hook + a score-threshold gate must exist')."
  - "The agent-memory recall rerank is wired in BOTH the plan's named file (core/memory.py: the reusable fail-soft BaseMemory.rerank_results hook) AND the genuine recall path (short_term.py ShortTermMemory.search_messages, where SEARCH_MESSAGES_BY_EMBEDDING results are produced). core/memory.py is the abstract base and produces no embedding results itself, so the call-site must live in the concrete recall path; short_term.py was edited as a necessary extension of the plan's file list."
  - "Python rerank is stdlib urllib (sync), called off the event loop via asyncio.to_thread, rather than httpx-async. This faithfully mirrors the Go stdlib net/http client, matches the plan's sync signature, avoids the optional httpx extra (only a 'nams' dep, not base), and is testable with a stdlib threaded HTTP server (the Python analogue of Go's httptest)."

patterns-established:
  - "documents.Service.Retrieve: stage-isolated, each stage fail-soft, message-prefix-safe; the canonical shape Wave 4 (GraphRAG) and Wave 5 (eval + guard) build on"
  - "Stdlib-only fail-soft sidecar client in Python mirroring the Go identity contract, unit-proven across all degradation branches with a threaded HTTP stub"

requirements-completed: [RET-02]

coverage:
  - id: D1
    description: "Service.Retrieve runs SEED->RERANK->EXPAND in the fast order: rerank-on reorders seeds by rerank score (and attaches the rerank score); identity keeps seed order+scores; below-threshold keeps seed order; embedder error and vector-seed error fall back to fulltext seeds; only the top-K winners are :NEXT_CHUNK-expanded; winners-first then unique neighbours; empty seeds short-circuit; no-reranker == Search"
    requirement: "RET-02"
    verification:
      - kind: unit
        ref: "internal/documents/retrieve_test.go (TestRetrieveRerankReordersSeeds, TestRetrieveRerankIdentityKeepsSeedOrder, TestRetrieveEmbedderErrorFallsBackToFulltext, TestRetrieveExpandsOnlyWinners, TestRetrieveBelowThresholdKeepsSeedOrder, TestRetrieveExpansionAppendsNeighborContext, TestRetrieveNoRerankerMatchesSearch, TestRetrieveVectorSeedErrorFallsBackToFulltext, TestRetrieveSeedErrorPropagates, TestRetrieveEmptySeedsReturnsEmpty) — go test -race; retrieve.go Retrieve/seedHits/queryVector/vectorSeed 100%, package 92.0%"
        status: pass
    human_judgment: false
  - id: D2
    description: "retrieve.go seeds via db.index.vector.queryNodes('chunk_embedding', ...) and expands via :NEXT_CHUNK; no symbol references messages[0] / system-prompt types (cache-invariant); retrieve.go and search.go each <= 600 LOC"
    requirement: "RET-02"
    verification:
      - kind: other
        ref: "grep: 1x db.index.vector.queryNodes('chunk_embedding', 3x NEXT_CHUNK, 0x messages[0]/SystemPrompt; wc -l retrieve.go=225 search.go=164"
        status: pass
    human_judgment: false
  - id: D3
    description: "document_search routes through Retrieve (Spec unchanged: Deferred:true, name document_search); a real no-reranker Service through the tool yields the fulltext Search order (no regression); the production docsToolSearcher sets Reranker=&rerank.RerankClient{BaseURL: cfg.RerankBaseURL}"
    requirement: "RET-02"
    verification:
      - kind: unit
        ref: "internal/agent/tools/document_search_test.go (TestDocumentSearchToolNoRerankerMatchesSearchOrder + the existing tool tests routed through Retrieve) — go test -race ./internal/agent/tools/, 86.4%; grep: docs.go sets Reranker to rerank.RerankClient{BaseURL: s.cfg.RerankBaseURL}"
        status: pass
    human_judgment: false
  - id: D4
    description: "Python neo4j_agent_memory.rerank.rerank reorders by descending relevance on success and returns identity on every failure mode (no base_url, HTTP 503, malformed JSON, length mismatch, out-of-range index, empty docs); BaseMemory.rerank_results reorders recall fail-soft and is wired into ShortTermMemory.search_messages"
    requirement: "RET-02"
    verification:
      - kind: unit
        ref: "docker/agent-memory/tests/test_rerank.py (7 cases) — python -m pytest in the live aura-agent-memory-mcp container: 7 passed; edited core/memory.py + short_term.py import cleanly (IMPORTS_OK)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Live (GPU host): end-to-end document retrieval p95 < 500ms on the seed-rerank path; the rerank-off path matches today's order"
    requirement: "RET-02"
    verification:
      - kind: e2e
        ref: "Deferred to 30-05 on a GPU host (documented). This Windows host has no GPU running aura-rerank (server-cuda), so the live rerank quality+latency assertion cannot run here; the fail-soft degraded path (rerank-absent => RRF/vector seed order) IS exercised by the unit tests."
        status: unknown
    human_judgment: true
    rationale: "aura-rerank (server-cuda) cannot run on this 4GB-GPU host; p95<500ms with rerank reordering needs the GPU sidecar. The degraded RRF/vector path is unit-proven; the live quality+latency number is a 30-05 GPU-host step."

# Metrics
duration: ~50min
completed: 2026-06-28
status: complete
---

# Phase 30 Plan 03: Two-stage retrieval (seed -> rerank seeds -> expand winners) Summary

**Wired RET-02's value: `documents.Service.Retrieve` runs the spike-070 Q4 fast order — vector/BM25 SEED (<=15) -> rerank the SEEDS -> 1-hop `:NEXT_CHUNK` graph-expand only the WINNERS for context — behind the `document_search` tool, and a stdlib fail-soft rerank post-processor reorders the agent-memory MCP's embedding recall; every stage degrades cleanly (no embedder -> fulltext seeds, rerank absent/identity/below-threshold -> seed order, Reranker==nil -> exactly today's Search), and the `messages[0]` KV-cache prefix is never touched.**

## Performance
- **Duration:** ~50 min
- **Tasks:** 2 (Task 1 tdd=true) + 1 coverage-polish commit
- **Files:** 12 (4 created, 8 modified)
- **Gates:** `go vet`/`go build ./...` clean; `go test -race ./internal/documents/ ./internal/agent/tools/` green (documents 92.0%, tools 86.4%, both >=85%); `golangci-lint run` on the 3 touched Go packages = 0 issues; `gofmt` clean; lefthook pre-commit (gofmt+vet+file-size<=600) green on all 3 commits; `ast.parse` OK for all Python; `python -m pytest tests/test_rerank.py` = 7 passed inside the live aura-agent-memory-mcp container.

## Accomplishments
- **Service.Retrieve two-stage pipeline (Task 1, tdd):** New `retrieve.go` adds `Retrieve` + a `Reranker` interface (mirrors `*rerank.RerankClient`) and four optional `Service` fields (`Knowledge`, `QueryEmbedder`, `Reranker`, `RerankThreshold`). The seed prefers the dense `chunk_embedding` HNSW index (`db.index.vector.queryNodes`) and falls back to the sparse `chunk_text` fulltext `Search` on any embed/vector failure; the ~15 seeds are reranked (with the non-monotonic threshold gate), and only the top-K winners are 1-hop `:NEXT_CHUNK`-expanded, winners-first with unique neighbours appended. `search.go` was refactored to share `hitsFromRows` (DRY, refactor-on-touch).
- **document_search + production wiring (Task 2, Go):** `DocumentSearchBackend` widened `Search`->`Retrieve` (the deferred Spec is byte-identical); `docsToolSearcher.Retrieve` builds a `Service` with the embed client, graph client, and `Reranker=&rerank.RerankClient{BaseURL: cfg.RerankBaseURL}`. A new tool test wires a real no-reranker `Service` to prove `Retrieve==Search` (no regression).
- **Agent-memory recall rerank (Task 2, Python):** New stdlib-only `neo4j_agent_memory.rerank.rerank()` mirrors the Go fail-soft identity contract (warn-once, ~480-char wire truncation, `AURA_RERANK_BASE_URL`). `BaseMemory.rerank_results()` reorders recall off the event loop (`asyncio.to_thread`) and is wired into `ShortTermMemory.search_messages` after the embedding results are built — fail-soft to the embedding order, reorder-only (scope cannot widen).
- **Docs:** `docs/document-ingestion.md` gains a "Two-stage retrieval (RET-02)" subsection (seed->rerank->expand + RRF/embedding fallback + message-prefix safety) and a "Memory-recall reranking" note.

## Task Commits
1. **Task 1 (tdd): two-stage documents.Service.Retrieve** — `b948181e` (feat) — RED demonstrated live (retrieve_test fails to build before retrieve.go), collapsed to one atomic feat commit (lefthook go-vet pre-commit forbids a compile-failing RED commit; --no-verify forbidden) — same convention as 30-01/30-02.
2. **Task 2: wire Retrieve into document_search + fail-soft memory-recall rerank** — `f3087b22` (feat) — Go tool + production wiring + Python rerank.py + core/memory.py hook + short_term.py recall + docs.
3. **Coverage polish** — `d55a5f0d` (test) — seed-fallback, seed-error, empty-seed branches; lifts retrieve.go core funcs to 100% and the package to 92.0%.

**Plan metadata:** this SUMMARY + STATE/ROADMAP (docs commit).

## Decisions Made
See `key-decisions` frontmatter. Load-bearing: (1) `Reranker==nil` => `Search` (literal no-regression gate) while a degraded reranker keeps the RRF/vector seed order (host directive); (2) `:NEXT_CHUNK`-only expansion because `:HAS_CHUNK` is Document->Chunk (indexer.go-verified) and cannot be a 1-hop chunk edge; (3) the rerank hook + threshold are a structural seam, tuned in RET-05; (4) the memory recall rerank lives in core/memory.py (reusable hook) + short_term.py (real call-site); (5) stdlib urllib (sync, off the event loop) faithfully mirrors the Go stdlib client.

## Deviations from Plan

### Auto-fixed / structural

**1. [Rule 3 - Blocking] service.go edited (not in Task 1's file list)**
- **Why:** `Service.Retrieve` is a method on `Service` and the plan requires adding `Reranker`/`RerankThreshold` (and, for the pipeline, `Knowledge`/`QueryEmbedder`) fields. Go struct fields and methods must live with the struct definition (service.go). The Task 1 file list (retrieve.go/retrieve_test.go/search.go) was incomplete; editing service.go is mandatory and backward-compatible (new optional fields default to zero values; all existing `&documents.Service{...}` sites use named fields).

**2. [Rule 1 - Correctness] Expansion uses :NEXT_CHUNK only, not the plan's literal :NEXT_CHUNK|:HAS_CHUNK**
- **Why:** `:HAS_CHUNK` is a Document->Chunk edge (indexer.go `MERGE (d)-[:HAS_CHUNK]->(c)`), so `(:Chunk)-[:HAS_CHUNK]-(:Chunk)` never matches — including it would be dead code (CLAUDE.md no-dead-code). `:NEXT_CHUNK` (reading-order, chunk-to-chunk) is the meaningful 1-hop context edge and the one the spike used. The grep acceptance (`:NEXT_CHUNK for expansion`) is satisfied.

**3. [Rule 3 - Blocking] short_term.py edited (not in Task 2's file list)**
- **Why:** The plan names core/memory.py as the recall wiring file, but core/memory.py is the ABSTRACT base — it produces no embedding-search results. The genuine recall path that runs `SEARCH_MESSAGES_BY_EMBEDDING` is `ShortTermMemory.search_messages` in short_term.py. Resolution: the reusable fail-soft hook lives in core/memory.py (`BaseMemory.rerank_results`, the plan's file) and the call-site is short_term.py (the real recall), satisfying "the memory recall path calls it guarded".

**4. [design] Python rerank is stdlib urllib (sync + asyncio.to_thread), not httpx-async**
- **Why:** Faithful mirror of the Go stdlib net/http client (30-01 contract), matches the plan's sync `rerank(...) -> list[int]` signature, avoids the optional `httpx` extra (not a base dep), and is testable with a stdlib threaded HTTP server. The async recall path calls it off the event loop via `asyncio.to_thread`.

**Total deviations:** 4 (2 file-list extensions forced by where Go/Python definitions live, 1 correctness :NEXT_CHUNK-only, 1 stdlib-vs-httpx design). No scope creep; the deferred-tool Spec and all existing behaviour are preserved.

## Threat Model Handling
- **T-30-07 (Tampering — messages[0] mutation): MITIGATED.** Retrieve is a pure chunk-retrieval function; grep confirms 0 references to messages[0]/system-prompt types. Results are appended downstream, never injected into the cached prefix.
- **T-30-08 (DoS — rerank latency): MITIGATED.** Only the <=15 SEEDS are reranked (not the expanded pool), with ~480-char truncation (the 267ms fast-path); any rerank error/identity falls back to seed order and never wedges the loop.
- **T-30-09 (Info disclosure — recall rerank logs): MITIGATED.** The Python rerank logs only a short static reason once per process (warn-once), truncates the wire text, and never logs document/memory content or secrets.
- **T-30-10 (EoP — scope bypass via reranked memory): ACCEPT (as planned).** `rerank_results` only REORDERS the already-scoped `SEARCH_*_BY_EMBEDDING_SCOPED` results passed to it; it adds no rows and cannot widen user/session scope.

## Issues Encountered
- **Live GPU rerank + p95 (D5) not runnable here:** this host's GPU cannot run the `aura-rerank` server-cuda sidecar, so the live seed-rerank quality + p95<500ms assertion is deferred to a GPU host in 30-05. The fail-soft degraded path (rerank-absent => RRF/vector seed order) is fully unit-proven.
- **Container had the pre-edit code baked at build time + no pytest:** to run the Python tests against the live libs without a full rebuild, the 4 changed Python files were `docker cp`-ed into the running `aura-agent-memory-mcp`, `pip install pytest` was run in-container, then `pytest tests/test_rerank.py` (7 passed) — the running MCP process keeps its old in-memory modules, so live behaviour is unchanged until the next legitimate rebuild (the committed source already matches what was copied). No boot-race-triggering restart was performed.
- **SDK `state.record-metric` / `state.update-progress` no-op'd:** record-metric rejected the positional args and update-progress reported "Progress field not found" (the milestone percent is phase-based, 8/9=89%, unchanged while phase 30 is in progress). The metric row and `completed_plans` 42->43 were applied manually; `state.advance-plan` (3->4), `state.record-session`, and `roadmap.update-plan-progress 30` succeeded.

## User Setup Required
None for boot — the reranker is optional and every stage is fail-soft. On a GPU host: `docker compose up -d aura-rerank`, then document retrieval and memory recall use it automatically (`AURA_RERANK_BASE_URL`). To make the agent-memory MCP use the reranker live, set `AURA_RERANK_BASE_URL` in its container env (e.g. `http://aura-rerank:8085`) and rebuild that sidecar incrementally.

## Known Stubs
None. retrieve.go, the rerank wiring, rerank.py, and the recall hook are complete fail-soft implementations. The identity/seed-order fallbacks are the INTENDED degraded paths (the upstream RRF/vector order), not stubs — each is exercised by the unit tests and is the documented RET-02 contract.

## Threat Flags
None. No new network endpoint, auth path, file-access pattern, or trust-boundary schema change beyond the plan's `<threat_model>`: Retrieve reads the existing chunk_embedding/chunk_text indexes and the :NEXT_CHUNK graph with bound `$`-params only; the recall rerank reorders already-scoped results; the rerank sidecar surface was introduced (and threat-modelled) in 30-01.

## Next Phase Readiness
- **30-04 (GraphRAG connected-nodes):** builds directly on Service.Retrieve's seed->expand seam and the :NEXT_CHUNK expansion; the per-stage shape (cheap vector+graph, bounded rerank) is in place.
- **30-05 (eval + non-monotonic guard + live tiers):** the RerankThreshold seam is shipped for the guard to tune; the live p95<500ms + rerank-quality E2E (D5) is the GPU-host step to close.
- No open blockers.

## Self-Check: PASSED
- Created files verified present: internal/documents/{retrieve.go, retrieve_test.go}, docker/agent-memory/src/neo4j_agent_memory/rerank.py, docker/agent-memory/tests/test_rerank.py.
- Commits verified in git log: b948181e (Task 1, feat), f3087b22 (Task 2, feat), d55a5f0d (coverage, test).
- Modified files present in commits: service.go, search.go, document_search.go, document_search_test.go, docs.go, core/memory.py, short_term.py, docs/document-ingestion.md.

---
*Phase: 30-retrieval-memory-hardening*
*Completed: 2026-06-28*
