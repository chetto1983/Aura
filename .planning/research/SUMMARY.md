# Project Research Summary

**Project:** Aura v4.0 Production Hardening
**Domain:** Production hardening a Go Telegram assistant with LLM agent loop, wiki knowledge base, and Qdrant vector search
**Researched:** 2026-05-10
**Confidence:** HIGH

## Executive Summary

Aura v4.0 hardens a single-process Go Telegram bot by adding two CGO-free dependencies (`golang.org/x/time/rate` and `sony/gobreaker/v2`) plus stdlib concurrency patterns to fix per-user message corruption, LLM provider fragility, unbounded resource consumption, and the chromem-go/Qdrant split-brain. Qdrant is the single canonical vector store -- no sqlite-vec, no pgvector, no chromem-go persistence. All hardening uses composition (wrapping existing interfaces) rather than modifying the stable agentruntime/agentloop packages.

## Key Findings

### Recommended Stack

- `golang.org/x/time/rate` -- Per-provider token-bucket rate limiter. Stdlib-adjacent, CGO-free.
- `sony/gobreaker/v2` v2.4.0 -- Circuit breaker state machine with LLM-specific error callbacks. CGO-free.
- `go-git/go-git/v5` v5.19.0 (upgrade from v5.18.0) -- Git commit tracking for wiki pages.
- **Stdlib patterns (no dependencies):** per-user actor/inbox UserGate, context timeouts, buffered channel backpressure with `select/default` drop, channel semaphore for in-flight cap.

**What is deliberately NOT used:** qdrant/go-client (gRPC overhead), any second vector store, worker pool libraries.

### Architecture Approach

All hardening uses composition -- zero changes to stable agentruntime/agentloop packages. The current Phase 1 checkpoint makes the per-user UserGate actor/inbox model canonical: spawn an actor on first use, serialize that user's conversation entries through a bounded inbox, expose `TryAcquire` for notification paths, and stop the actor during inactivity eviction.

### Critical Pitfalls (Top 5)

| # | Pitfall | Prevention | Verification |
|---|---------|------------|--------------|
| 1 | UserGate deadlock via re-entrant acquire | `TryAcquire` from day one | Notification to active user does not deadlock |
| 2 | Circuit breaker lock serializing all users | Lock state only (ns), release before I/O | 10 concurrent sends with 1s mock = ~1.1s |
| 3 | Qdrant warm check false positive | Check `points_count > 0`, not just collection exists | Empty collection triggers re-embed |
| 4 | Temperature retry on HTTP 429/5xx | Error classification: temp only for content failures | HTTP 429 does not increment temperature |
| 5 | Wiki write overwriting manual dashboard edits | `expected_updated_at` parameter in WritePage tool | Concurrent dashboard + tool write detected |

## Roadmap Implications

### Phase 1: Fondamenta (Concurrency + Qdrant Readiness)
**Features:** UserGate actor/inbox (#1), context eviction (#2), Qdrant health/warm check (#8)
**Rationale:** UserGate is the synchronization foundation. Qdrant readiness must ship before async reindex, tool-vector retrieval, or legacy cleanup depend on it.
**Must prevent:** Pitfall 1 (TryAcquire), Pitfall 3 (`points_count > 0` warm check), separate tracking for eviction.

### Phase 2: Core Hardening
**Features:** Tool-based wiki writes (#3), git tracking (#9), async reindex (#5), Qdrant-backed tool retrieval
**Rationale:** #3 triggers #9 and #5. Ship tool first, then wire async worker and semantic tool retrieval on top of the Phase 1 Qdrant readiness gate.
**Must prevent:** Reindex worker goroutine leak, `expected_updated_at` omission, Qdrant-dependent work without health validation.

### Phase 3: Resilience Layer
**Features:** Circuit breaker (#6), variable-temp retry (#4), per-user budget (#7)
**Rationale:** Deeply interacting triad -- breaker stops retries, retries consume budget, budget gates breaker recovery.
**Must prevent:** Pitfall 2 (lock scope), Pitfall 4 (error classification for temp retry), budget check outside UserGate.

### Phase 4: Cleanup
**Features:** Legacy chromem-go removal (#10)
**Rationale:** Ships last after all Qdrant features are stable. Strangler Fig with feature flag until Qdrant-only behavior is verified.
**Must prevent:** Build-tag verification gaps for all supported platforms.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | CGO-free validated, Go 1.26.2 compatible |
| Features | HIGH | All 10 features mapped to codebase locations, dependency graph validated |
| Architecture | HIGH | Zero modification to stable packages, package dependency direction preserved |
| Pitfalls | HIGH | Verified against Go stdlib, official issue trackers, community post-mortems |

**Overall confidence:** HIGH -- Research outputs converge after normalizing Qdrant readiness into Phase 1 and actor/inbox as the canonical UserGate design.

## Gaps

- Embedding API batch semantics (Phase 2 planning)
- Provider HTTP cancellation behavior when a circuit opens mid-request (Phase 3 planning)
- Concurrent user ceiling recalibration for 100+ users

---
*Research summary for: Aura v4.0 Production Hardening*
*Synthesized: 2026-05-10*
