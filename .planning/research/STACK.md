# Stack Research: Production Hardening Libraries

**Domain:** Go Telegram bot production hardening (v4.0)
**Researched:** 2026-05-10
**Confidence:** HIGH

## Recommended Stack

### Core Technologies (New Dependencies)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `golang.org/x/time/rate` | latest (stdlib-adjacent) | Token-bucket rate limiter for per-user throttling and LLM provider ingress control | Official Go supplementary package; thread-safe token bucket used by Kubernetes, Docker, Consul, CockroachDB (9,876+ importers). No CGO. Blocks on `Wait(ctx)` with full context deadline respect. |
| `github.com/sony/gobreaker/v2` | v2.4.0 | Circuit breaker state machine for LLM provider failover | De-facto Go CB library (3,594 GitHub stars, MIT). Generics-enabled `CircuitBreaker[T]`, `ReadyToTrip` + `IsExcluded` callbacks for LLM-specific error classification (429 rate-limit vs 400 bad prompt), `OnStateChange` for metrics. Minimal dep tree (only `testify` for tests). CGO-free. |
| `github.com/go-git/go-git/v5` | v5.19.0 (upgrade from v5.18.0) | Git commit integrity tracking for wiki pages | Already in go.mod. v5.19.0 is latest (May 6, 2026). Pure Go implementation, no CGO, no libgit2. Uses `StatusWithOptions(StatusOptions{Strategy: Preload})` for correct worktree status of committed files. |

### Standard Library Patterns (No Dependencies)

| Pattern | Mechanism | Purpose | Why Stdlib |
|---------|-----------|---------|-----------|
| Per-user mutex | `sync.Map` + lazy `*sync.Mutex` creation via `LoadOrStore(userID, &sync.Mutex{})` | Serialize Telegram message processing per user, preventing race conditions on concurrent messages | Already proven in codebase (`wiki/store.go:22`, `source/store.go:37`). Zero allocations on hot path after first access. Thread-safe. No external dep. |
| Request timeout | `context.WithTimeout(ctx, 30*time.Second)` wrapping the per-user mutex critical section | Prevent a hung LLM call from blocking one user's queue indefinitely | Built-in to `context` package. Composes naturally with mutex lock + LLM call + tool execution. |
| Async worker backpressure | Buffered channel (`make(chan T, 2000)`) + non-blocking `select/default` drop for coalescing | Wiki reindex worker that drops duplicate reindex requests when queue is full | stdlib `chan` is idiomatic Go for producer-consumer with backpressure. No pool library needed -- reindex is a single-purpose background worker, not a dynamic workload. |
| Coalescing via drop | `select { case ch <- item: default: drops.Add(1) }` | Merge duplicate reindex requests (same slug queued twice = latest wins) | Non-blocking channel send via `select/default`. `atomic.Int64` for drop counter. Pure stdlib. |
| Qdrant health+collection check | Extend existing `net/http` Qdrant client in `internal/search/qdrant.go` with `GET /collections/{name}` REST call | Startup validation: readiness probe + collection existence + vector count | Codebase already has a working raw HTTP Qdrant client. Adding a collection info endpoint call is ~15 lines. Official `qdrant/go-client` is gRPC-based, pulls in protobuf + gRPC deps -- heavyweight and unnecessary. |
| In-flight concurrency cap | Buffered channel as semaphore: `make(chan struct{}, maxConcurrent)` | Cap concurrent LLM calls per provider to prevent the "30,000 in-flight requests" circuit breaker blind spot | stdlib channel as semaphore is the idiomatic Go pattern. No external semaphore library needed. |

## Installation

```bash
# New dependencies for v4.0 hardening
go get golang.org/x/time/rate@latest
go get github.com/sony/gobreaker/v2@v2.4.0

# Upgrade existing
go get github.com/go-git/go-git/v5@v5.19.0
```

## Alternatives Considered

| Recommended | Alternative | Why Not Alternative |
|-------------|-------------|---------------------|
| `sony/gobreaker/v2` v2.4.0 | Hand-rolled circuit breaker state machine | State machine correctness is notoriously hard (incorrect half-open transitions, race conditions on failure counts). gobreaker handles this with 20+ contributors and 10 years of field testing. Hand-rolled adds weeks of debugging for a solved problem. |
| Raw HTTP Qdrant client (existing) | `qdrant/go-client` v1.17.1 official SDK | The official SDK is gRPC-based (adds protobuf, gRPC runtime, generated client stubs). The existing raw HTTP client already covers all operations the app needs (search, upsert, delete, ready check). Adding collection info is one extra REST endpoint. Switching to the official SDK would be a full rewrite of `qdrant.go` with no functional benefit. |
| `sync.Map` per-user mutex (stdlib) | Third-party mutex pool library like `go-per-user-mutex` | stdlib pattern is simpler, more auditable, and already proven in the codebase. Third-party wrapper adds abstraction for no gain. |
| Buffered channel + coalescing (stdlib) | `ants`, `pond`, `go-adaptive-pool` worker pool libraries | Wiki reindex is a single-purpose background worker -- not a dynamic workload. Pool libraries add complexity (adaptive scaling, config tuning) for a problem that doesn't need it. A buffered channel is trivial to get right. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `sqlite-vec` or any second vector store | Qdrant is the CANONICAL single source of truth for embeddings. Persistence is via Qdrant's Docker volume. Adding sqlite-vec, pgvector, or chromem-go serialization creates a split-brain problem where two stores can disagree. | Qdrant only. SQLite FTS for exact/fallback text search (already exists). |
| `chromem-go` persistence or embedding storage | `chromem-go` v0.7.0 is in go.mod but should only be used for its `EmbeddingFunc` type signature if needed. Any chromem-managed vector persistence is redundant with Qdrant and should be removed. | Qdrant for all vector storage. chromem-go to be removed or scoped to embedding function interface only. |
| `qdrant/go-client` official gRPC SDK | Adds heavy dependency chain (gRPC, protobuf, generated code) when the existing raw HTTP client already covers 100% of needed Qdrant operations. Switching mid-milestone risks introducing gRPC connection management bugs. | Extend existing `internal/search/qdrant.go` raw HTTP client with `GET /collections/{name}`. |
| `git2go` (libgit2 CGO bindings) | Requires CGO (C compiler, libgit2 shared library). Breaks CGO-free guarantee of the codebase. Would make cross-compilation painful. | `go-git/go-git/v5` v5.19.0 -- already in go.mod, pure Go, no CGO. |
| `github.com/sethvargo/go-limiter` | Distributed rate limiter with Redis backend -- unnecessary for single-process bot. Adds operational complexity (Redis dependency). | `golang.org/x/time/rate` for in-process token bucket. |
| Hand-rolled circuit breaker state machine | Tempting "minimal dependency" instinct, but LLM failover requires correct half-open probe logic, failure counting windows, and state transition callbacks. Getting these wrong means the breaker either never opens (hammering a dead provider) or never closes (permanently degraded). | `sony/gobreaker/v2` v2.4.0 for the state machine. Custom layer on top for provider-aware routing. |

## Version Compatibility

| Package | Required Go Version | CGO Required? | Notes |
|---------|---------------------|---------------|-------|
| `golang.org/x/time/rate` | Go 1.18+ | No | Pure Go token bucket. Thread-safe. |
| `sony/gobreaker/v2` v2.4.0 | Go 1.18+ (generics) | No | Pure Go. Only test dep is testify. |
| `go-git/go-git/v5` v5.19.0 | Go 1.22+ | No | Pure Go Git implementation. Compatible with existing `modernc.org/sqlite` CGO-free setup. |
| `qdrant/go-client` v1.17.1 (NOT using) | Go 1.24+ | No (gRPC is pure Go) | Listed for reference -- we are intentionally NOT adopting this. |
| Current project `go.mod`: Go 1.26.2 | N/A | N/A | All recommended packages are well within version requirements. |

## CGO-Free Guarantee

All recommended additions are CGO-free:

- `golang.org/x/time/rate` -- pure Go
- `sony/gobreaker/v2` -- pure Go (only `testify` for tests)
- `go-git/go-git/v5` -- pure Go by design (no libgit2)

The codebase stays CGO-free. `modernc.org/sqlite` continues as the only SQLite implementation. No C compiler or system libraries are needed for any new dependency.

## Architecture: Layered LLM Protection

The recommended approach layers three mechanisms, each responsible for one concern:

```
[LLM Router: provider selection + fallback ordering]  <- Custom: already exists in failover.go
        |
[Rate Limiter: golang.org/x/time/rate]                <- NEW: per-provider token bucket
        |
[Circuit Breaker: sony/gobreaker/v2]                  <- NEW: per-provider state machine
        |
[Semaphore: chan struct{} in-flight cap]              <- NEW: prevents breaker blind spot
        |
[Retry: existing RetryClient]                         <- EXISTING: exponential backoff
        |
[HTTP Client Call]
```

This layering means:
- **Rate limiter** prevents overwhelming a provider (proactive)
- **Circuit breaker** detects when a provider is unhealthy (reactive)
- **Semaphore** caps in-flight calls so the breaker sees errors promptly (protective)
- **Retry** handles transient failures within a healthy provider window
- **Router** chooses which provider chain to use

## Sources

- Context7: `sony/gobreaker` -- verified v2.4.0 features (Counts.TotalExclusions, Settings.IsExcluded), CGO-free, MIT license, generics support
- Context7: `go-git/go-git/v5` -- verified v5.19.0 release (May 6, 2026), CGO-free by design, StatusWithOptions API with Preload strategy for correct committed-file tracking
- Official docs: `pkg.go.dev/golang.org/x/time/rate` -- 9,876+ importers (Kubernetes, Docker, Consul), pure Go token bucket
- Official GitHub: `qdrant/go-client` -- v1.17.1 gRPC-based SDK (evaluated, deliberately NOT adopted)
- Official GitHub: `go-git/go-git` -- Issues #119/#1140 documented the committed-file-as-untracked bug; StatusWithOptions+Preload strategy is the confirmed fix
- Community: `sony/gobreaker` Issue #91 -- documented in-flight request blindness; buffered channel semaphore is the mitigation
- WebSearch: Go Telegram bot concurrency patterns -- `sync.Map` per-user mutex is the idiomatic stdlib approach (used by `go-telegram/bot` and `echotron`)

---
*Stack research for: Aura v4.0 Production Hardening*
*Researched: 2026-05-10*
