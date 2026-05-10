# Project Research Summary

**Project:** Aura v4.0 Production Hardening
**Domain:** Production hardening a Go Telegram assistant with LLM agent loop, wiki knowledge base, and Qdrant vector search
**Researched:** 2026-05-10
**Confidence:** HIGH

## Executive Summary

Aura v4.0 hardens a single-process Go Telegram bot by adding two CGO-free dependencies (`golang.org/x/time/rate` and `sony/gobreaker/v2`) plus stdlib concurrency patterns to fix per-user message corruption, LLM provider fragility, unbounded resource consumption, and the chromem-go/Qdrant split-brain. Qdrant is the single canonical vector store — no sqlite-vec, no pgvector, no chromem-go persistence. All hardening uses composition (wrapping existing interfaces) rather than modifying the stable agentruntime/agentloop packages.

## Key Findings

### Recommended Stack

- `golang.org/x/time/rate` — Per-provider token-bucket rate limiter. Stdlib-adjacent, CGO-free.
- `sony/gobreaker/v2` v2.4.0 — Circuit breaker state machine with LLM-specific error callbacks. CGO-free.
- `go-git/go-git/v5` v5.19.0 (upgrade from v5.18.0) — Git commit tracking for wiki pages.
- **Stdlib patterns (no dependencies):** per-user mutex via `sync.Map` + lazy `*sync.Mutex`, context timeouts, buffered channel backpressure with `select/default` drop, channel semaphore for in-flight cap.

**What is deliberately NOT used:** qdrant/go-client (gRPC overhead), any second vector store, worker pool libraries.

### Architecture Approach

All hardening uses composition — zero changes to stable agentruntime/agentloop packages. Eight new files, modifications only at well-defined integration points (e.g., two lines in `handleConversation` for UserGate).

### Critical Pitfalls (Top 5)

| # | Pitfall | Prevention | Verification |
|---|---------|------------|--------------|
| 1 | UserGate deadlock via re-entrant acquire | `TryAcquire` from day one | Notification to active user does not deadlock |
| 2 | Circuit breaker lock serializing all users | Lock state only (ns), release before I/O | 10 concurrent sends with 1s mock = ~1.1s |
| 3 | Qdrant warm check false positive | Check `points_count > 0`, not just collection exists | Empty collection triggers re-embed |
| 4 | Temperature retry on HTTP 429/5xx | Error classification: temp only for content failures | HTTP 429 does not increment temperature |
| 5 | Wiki write overwriting manual dashboard edits | `expected_updated_at` parameter in WritePage tool | Concurrent dashboard + tool write detected |

## Roadmap Implications

### Phase 1: Foundation
**Features:** Qdrant health (#8), per-user mutex (#1), context eviction (#2)
**Rationale:** Hard prerequisites. Qdrant health blocks everything. Per-user mutex is the synchronization foundation. Lowest risk.
**Must prevent:** Pitfall 1 (TryAcquire), Pitfall 2 (separate tracking for eviction)

### Phase 2: Core Hardening
**Features:** Tool-based wiki writes (#3), git tracking (#9), async reindex (#5)
**Rationale:** #3 triggers #9 and #5. Ship tool first, then wire async worker.
**Must prevent:** Pitfall 4 (goroutine leak), Pitfall 5 (expected_updated_at)

### Phase 3: Resilience Layer
**Features:** Circuit breaker (#6), variable-temp retry (#4), per-user budget (#7)
**Rationale:** Deeply interacting triad — breaker stops retries, retries consume budget, budget gates breaker.
**Must prevent:** Pitfall 3 (lock scope), Pitfall 4 (error classification for temp retry)

### Phase 4: Cleanup
**Features:** Legacy chromem-go removal (#10)
**Rationale:** Ships last after all Qdrant features stable. Strangler Fig with feature flag.
**Must prevent:** Build-tag verification for all platforms.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Verified via Context7, CGO-free validated, Go 1.26.2 compatible |
| Features | HIGH | All 10 features mapped to codebase locations, dependency graph validated |
| Architecture | HIGH | Zero modification to stable packages, package dependency direction preserved |
| Pitfalls | HIGH | Verified against Go stdlib, official issue trackers, community post-mortems |

**Overall confidence:** HIGH — All research outputs converge on the same four-phase structure.

## Gaps

- Embedding API batch semantics (Phase 2 planning)
- Parallel racing HTTP cancellation behavior (Phase 3 planning)
- Concurrent user ceiling recalibration for 100+ users

---
*Research summary for: Aura v4.0 Production Hardening*
*Synthesized: 2026-05-10*
