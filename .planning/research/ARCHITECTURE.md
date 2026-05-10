# Architecture Research: Production Hardening Integration

**Domain:** Go agent runtime with Telegram frontend, LLM loop, wiki store, Qdrant vector search
**Researched:** 2026-05-10
**Confidence:** HIGH

## System Overview

```text
External boundary
  Telegram conversation handler
    -> UserGate actor/inbox before session state mutation
    -> inactivity ticker evicts idle actors and conversation state

Agent boundary
  agentruntime and agentloop remain stable interface boundaries
  retry, circuit breaker, and budget behavior are composed around existing clients/options

Tool and memory boundary
  tools/registry.go keeps toolset gating
  tools/wiki_write.go adds explicit WriteWikiPage mutation
  wiki/store.go stays the sync write authority and emits one-way reindex notifications

Infrastructure boundary
  LLM layer: provider client + per-provider circuit breaker + retry wrapper
  Search/index layer: Qdrant as canonical vector store, async reindex worker, raw HTTP health/warm checks
  Budget layer: global tracker plus per-user budget inside the UserGate boundary

Storage boundary
  SQLite remains for app state, FTS, and metadata
  Qdrant is the canonical persistent vector store
  Wiki pages remain git-tracked markdown files
```

**Core principle:** Each hardening concern is added as a wrapper or boundary component. Stable `agentruntime` and `agentloop` logic stays unchanged unless a later plan proves a minimal interface extension is required.

## Component Responsibilities

| Component | Responsibility | Modified or New | Implementation |
|-----------|----------------|-----------------|----------------|
| `telegram/conversation.go` | Receives messages, owns UserGate actor/inbox gate | MODIFIED (entry point) | Add UserGate actor/inbox boundary before session.Begin |
| `telegram/cleanup.go` (new) | Inactivity-based context eviction | NEW | Background ticker clearing stale sessions |
| `agentruntime/runner.go` | Runtime invocation, event emission | UNCHANGED | Clean boundary, no hardening touches this |
| `agentloop/loop.go` | Core LLM-tool loop | UNCHANGED | Variable-temp retry via opts, no loop logic change |
| `tools/wiki_write.go` (new) | LLM-callable wiki page creation tool | NEW | Implements Tool interface, writes via wiki.Store |
| `tools/registry.go` | Tool registration | MODIFIED | Register WriteWikiPage in compute toolset |
| `llm/circuitbreaker.go` (new) | Stateful circuit breaker | NEW | Wraps llm.Client, tracks failures per provider |
| `llm/circuitbreaker.go` (new) | Provider health circuit breaker | NEW | Wrap provider client without serializing network I/O |
| `llm/temp_retry.go` (new) | Variable-temperature retry on schema/empty failure | NEW | Wraps ChatClient, increments temp on retry |
| `search/async_reindex.go` (new) | Async wiki reindex worker | NEW | Buffered channel, goroutine, handles backpressure |
| `search/qdrant_health.go` (new) | Qdrant startup health check + warm cache validation | NEW | Blocks startup until Qdrant healthy; skips re-embed if vectors present |
| `wiki/store.go` | Wiki CRUD and git tracking | MODIFIED (signaling) | Notify reindex worker after WritePage, add unversioned flag |
| `budget/peruser.go` (new) | Per-user token budget | NEW | Extension of existing budget package |
| `memoryindex/store.go` | FTS and doc storage | UNCHANGED | Already supports vector index interface |

## Key Architectural Patterns

### Pattern 1: UserGate Actor/Inbox (Composition, Not Modification)

**What:** A `UserGate` component placed at the top of `handleConversation` before any conversation state mutation. It owns one lightweight actor per active user. Each actor serializes that user's Telegram messages and notification entries through a bounded inbox.

**When to use:** Every incoming Telegram message and every user-targeted system notification before it can mutate conversation state or send ordered Telegram output.

**Implementation sketch:**
```go
// internal/telegram/usergate.go (NEW file)
type UserGate struct {
    mu      sync.Mutex
    actors  map[string]*userActor
    idleTTL time.Duration
}

type userActor struct {
    userID string
    inbox  chan gateEntry // buffer 8 by Phase 1 checkpoint decision
    done   chan struct{}
}

func (g *UserGate) Acquire(userID string, entry gateEntry) error {
    actor := g.actorFor(userID)
    select {
    case actor.inbox <- entry:
        return nil
    default:
        actor.dropOldestAndNotify(entry)
        return nil
    }
}

func (g *UserGate) TryAcquire(userID string, entry gateEntry) bool {
    actor := g.actorFor(userID)
    select {
    case actor.inbox <- entry:
        return true
    default:
        return false
    }
}
```

**Integration point in conversation.go:**
```go
func (b *Bot) handleConversation(c tele.Context) {
    userID := strconv.FormatInt(c.Sender().ID, 10)
    if err := b.userGate.Acquire(userID, conversationEntry{Context: c}); err != nil {
        c.Send("Sto ancora elaborando il messaggio precedente. Riprova tra qualche secondo.")
        return
    }
}
```

**Why this approach:** The Phase 1 checkpoint selected the actor pattern over a keyed mutex. It keeps same-user work FIFO, allows a generous bounded inbox (8), supports drop-oldest behavior when the inbox is full, and gives notification paths a non-blocking `TryAcquire` API. The actor owns the user's `conversation.Context` while alive and is destroyed by inactivity eviction.

### Pattern 2: Circuit Breaker as Client Wrapper

**What:** A `CircuitBreakerClient` that wraps an `llm.Client`, tracking consecutive provider failures and opening the circuit after a threshold.

**When to use:** Wrap the configured LLM provider client before retry logic.

**Implementation:**
```go
// internal/llm/circuitbreaker.go (NEW file)
type CircuitState int
const (
    CircuitClosed   CircuitState = iota // normal operation
    CircuitHalfOpen                      // testing recovery
    CircuitOpen                         // rejecting requests
)

type CircuitBreakerClient struct {
    inner           Client
    maxFailures     int
    resetTimeout    time.Duration
    mu              sync.Mutex
    failures        int
    state           CircuitState
    lastFailureTime time.Time
}
```

**Key design decisions:**
- State transitions: Closed -> (failures >= maxFailures) -> Open -> (resetTimeout expires) -> HalfOpen -> (success) -> Closed
- Half-open state allows one probe request through; success resets, failure re-opens
- `Stream` method: If circuit is open, return error immediately without touching the provider
- Per-provider breakers are keyed by provider name when multiple providers are configured.

**Why this approach:** The circuit breaker adds provider health state without changing the Client interface. This is the standard Go pattern (composition over inheritance).

### Pattern 3: Async Reindex via Channel-Based Signaling

**What:** A background goroutine that receives "reindex needed" signals from wiki writes and pushes updated vectors to Qdrant outside the hot path.

**When to use:** Every `wiki.Store.WritePage()` and `wiki.Store.DeletePage()` call.

**Implementation:**
```go
// internal/search/async_reindex.go (NEW file)
type ReindexWorker struct {
    engine    *Engine
    requests  chan reindexRequest
    done      chan struct{}
    mu        sync.Mutex
    coalescing bool  // true while waiting to batch
    logger    *slog.Logger
}

type reindexRequest struct {
    slug string // empty = full reindex
}

func (w *ReindexWorker) SignalPage(slug string) {
    select {
    case w.requests <- reindexRequest{slug: slug}:
    default:
        // Channel full, already coalescing -- drop
    }
}
```

**Backpressure mechanism:**
- Buffered channel (size 1) -- only the latest reindex request matters
- Coalescing: if a request arrives while processing, the worker batches
- Dropping signals is safe because Qdrant already has the previous vector; a dropped signal just means slightly stale results for one turn

**Integration with wiki.Store:**
The wiki store gains a new optional field:
```go
type Store struct {
    // ... existing fields
    reindexNotifier func(slug string) // nil = no async reindex
}
```
`WritePage` and `DeletePage` call `s.reindexNotifier(slug)` after the atomic write but before returning to the caller. If nil (not configured), behavior is unchanged -- backward compatible.

**Why this approach:** One-way signaling. The wiki store doesn't know about search; it just calls a notifier function injected at construction time. The reindex worker is owned by the search layer. This respects the existing package dependency direction (wiki does not import search).

### Pattern 4: Qdrant Health & Startup Validation (No New Vector Store)

**What:** Qdrant is the single source of truth for all embeddings. It persists to disk via Docker volume. The hardening concern is ensuring Qdrant is healthy at startup and that existing vectors are present (no unnecessary re-embedding).

**Current behavior:** Startup runs `compact memory mirror sync` which may re-embed if Qdrant is empty or missing vectors.

**What changes:**
- Startup health gate: probe Qdrant `/health` before accepting traffic. If unhealthy, retry with backoff (max 30s), then fail startup with clear error.
- Warm cache check: count vectors in `aura_memory_v1_compact`. If count > 0, skip the full re-embed pass -- vectors are already persisted. Only re-embed if the collection is genuinely empty.
- Embedding cache in `internal/search/embed_cache.go` already caches computed embeddings in-memory; this stays as-is (no disk serialization needed since Qdrant is the disk).

**Implementation:**
```go
// internal/search/qdrant_health.go (NEW file)
func WaitForQdrant(ctx context.Context, client *qdrant.Client, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if healthy, _ := client.Health(ctx); healthy {
            return nil
        }
        time.Sleep(2 * time.Second)
    }
    return ErrQdrantUnhealthy
}

func CollectionHasVectors(ctx context.Context, client *qdrant.Client, collection string) (bool, error) {
    info, err := client.GetCollectionInfo(ctx, collection)
    if err != nil {
        return false, err
    }
    return info.PointsCount > 0, nil
}
```

**Why no sqlite-vec:** Qdrant already persists to disk. Adding a second vector store (sqlite-vec, chromem-go serialization, pgvector) creates a synchronization problem -- which store is authoritative? Qdrant is the canonical store. The embedding cache in-process is sufficient for latency optimization. No redundant persistence layer.

**Why this approach:** The "startup re-embed problem" is solved by checking whether Qdrant already has data, not by adding another database. Zero new dependencies. Zero new sync problems.

### Pattern 5: Tool-Based Wiki Writes

**What:** A new `write_wiki_page` tool that the LLM can call directly, replacing any heuristic-based wiki content detection (`looksLikeWikiYAML`).

**When to use:** Registered in the `compute` toolset, gated behind explicit LLM intent to create/update wiki memory.

**Implementation:**
```go
// internal/tools/wiki_write.go (NEW file)
type WriteWikiPageTool struct {
    wiki wiki.PageWriter
}

func (t *WriteWikiPageTool) Name() string { return "write_wiki_page" }

func (t *WriteWikiPageTool) Description() string {
    return "Create or update a wiki page in Aura's durable memory..."
}

func (t *WriteWikiPageTool) Parameters() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "title":    map[string]any{"type": "string", "description": "Page title (becomes the slug)"},
            "body":     map[string]any{"type": "string", "description": "Markdown body with [[links]]"},
            "category": map[string]any{"type": "string", "description": "Category for organization"},
            "tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
            "related":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Related wiki page slugs"},
            "evidence_refs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Source IDs this page is based on"},
        },
        "required": []string{"title", "body"},
    }
}
```

**Toolset gating:** The tool is registered in the `compute` toolset. It is NOT in the default `search_memory + schedule_task` minimal set. This means the LLM must demonstrate intent (by being in compute mode) before it can mutate the wiki.

**Why this approach:** Makes wiki writes explicit, auditable, and reviewable. The LLM's decision to write a page is a visible tool call in the conversation log. No more heuristics detecting whether assistant text "looks like a wiki page." This also means the tool result is a standard tool message processed by the loop, not magic content parsing.

### Pattern 6: Variable-Temperature Retry (AgentLoop Options, Not Loop Change)

**What:** When the LLM returns an error or empty/invalid response, retry with a slightly higher temperature instead of the same temperature.

**Where:** Configured via `agentloop.Options` -- specifically a new `RetryConfig` field. The agentloop itself does not change.

**Implementation:**
```go
// In agentloop Options struct (add field):
RetryConfig *RetryConfig

type RetryConfig struct {
    MaxRetries      int
    BaseTemperature float64  // starting temp (e.g., 0.0)
    TempIncrement   float64  // add this per retry (e.g., 0.1)
    MaxTemperature  float64  // hard cap (e.g., 0.5)
    OnRetry         func(attempt int, newTemp float64) // logging hook
}
```

**Integration:** No change to `agentloop/loop.go`. The `ChatClient` passed from `telegram/conversation.go` is now a composed chain: `RetryingChatClient(CircuitBreakerClient(providerClient))`.

**Why this approach:** agentloop already accepts a `ChatClient` interface. Composition at the call site (telegram) avoids touching stable loop code. On schema validation failure, the first retry uses temperature 0.1 with error feedback, second uses 0.2, etc. -- instead of burning 3 identical calls at temperature 0.

## Data Flow Changes

### Before (Current Hot Path)
```
Telegram message arrives
  -> handleConversation
    -> session.Begin (sync.Map LoadOrStore)
    -> runToolCallingLoop
      -> agentruntime.Run
        -> agentloop.Run
          -> client.Chat (LLM call)
          -> executor.ExecuteToolCalls
            -> tool execution (search_memory, etc.)
    -> archive + cleanup
```

### After (With Hardening)
```
Telegram message arrives
  -> handleConversation
    -> [NEW] userGate.Acquire(userID) -- UserGate actor/inbox
    -> session.Begin (sync.Map LoadOrStore)
    -> [NEW] perUserBudget.Check(userID) -- before LLM calls
    -> runToolCallingLoop
      -> agentruntime.Run
        -> agentloop.Run
          -> [WRAPPED] client.Chat -> CircuitBreaker -> Provider
          -> [WRAPPED] variable-temp retry on failure
          -> executor.ExecuteToolCalls
            -> tool execution
              -> [NEW] write_wiki_page -> wiki.Store.WritePage
                -> [SIGNAL] reindexWorker.SignalPage(slug)
            -> search_memory
              -> Qdrant vector search (single store, no secondary vector store)
    -> [DEFERRED] reindex worker processes signal in background
    -> archive + cleanup
  -> [DEFERRED] userGate.Release(userID)
```

### Inactivity Eviction Flow (New Background Path)
```
Background ticker (every 5 minutes)
  -> Iterate sessionStore.active entries
  -> For each session inactive > 30 minutes:
    -> sessionStore.Clear(userID)
    -> Log eviction
```

## New Package Structure

```text
internal/
|-- agentloop/            # UNCHANGED -- core loop logic
|-- agentruntime/         # UNCHANGED -- runner + session store
|-- telegram/
|   |-- conversation.go   # MODIFIED -- enqueue conversation entries through UserGate
|   |-- usergate.go       # NEW -- per-user actor/inbox with TryAcquire
|   |-- cleanup.go        # NEW -- inactivity eviction ticker; stops idle actors
|   `-- ...               # existing files unchanged
|-- llm/
|   |-- client.go         # UNCHANGED -- core types
|   |-- retry.go          # UNCHANGED -- exponential backoff
|   |-- openai.go         # UNCHANGED -- OpenAI provider
|   |-- circuitbreaker.go # NEW -- circuit breaker wrapper
|   |-- temp_retry.go     # NEW -- variable-temperature retry client
|   `-- ...               # tests
|-- search/
|   |-- search.go         # MODIFIED -- integrate Qdrant readiness gate and async worker
|   |-- qdrant_health.go  # NEW -- raw HTTP Qdrant startup health + warm cache detection
|   |-- async_reindex.go  # NEW -- background reindex worker
|   `-- ...               # qdrant.go, sqlite.go, embed_cache.go
|-- tools/
|   |-- registry.go       # MODIFIED -- register write_wiki_page
|   |-- wiki_write.go     # NEW -- WriteWikiPage tool
|   |-- wiki_write_test.go # NEW
|   `-- ...               # existing tools unchanged
|-- wiki/
|   |-- store.go          # MODIFIED -- add reindexNotifier callback, unversioned commit flag
|   `-- ...               # existing unchanged
|-- budget/
|   |-- tracker.go        # UNCHANGED -- global budget tracking
|   |-- peruser.go        # NEW -- per-user token budget with global hard cap
|   `-- ...               # existing tests
`-- memoryindex/          # UNCHANGED -- already supports VectorIndex interface
```

## Build Order and Risk Assessment

The build order is calibrated to deliver value at each step while minimizing risk to the stable hot path.

### Phase 1: Fondamenta (Concurrency + Qdrant Readiness)
**Goal:** Add the synchronization and Qdrant readiness foundations before Phase 2 depends on them.

| Step | Component | Risk | Rationale |
|------|-----------|------|-----------|
| 1.1 | `telegram/usergate.go` | MEDIUM | New per-user actor/inbox component with tests. `handleConversation` changes only at the boundary. |
| 1.2 | `telegram/cleanup.go` | LOW | New inactivity eviction path stops idle actors and releases conversation resources. |
| 1.3 | `search/qdrant_health.go` | MEDIUM | Startup health and warm-cache validation, using raw HTTP and `points_count > 0`. |
| 1.4 | `search/search.go` / setup integration | MEDIUM | Wire readiness before Qdrant-backed features depend on it. |

**Validation gate:** Same-user messages are serialized, notification `TryAcquire` cannot deadlock, idle actors are evicted, and an empty Qdrant collection triggers re-embed while a populated one skips it.

### Phase 2: LLM Reliability & Tool Intelligence
**Goal:** Wire explicit wiki writes, retry classification, git audit tracking, async reindex, and Qdrant-backed tool retrieval into the hot path with minimal changes.

| Step | Component | Risk | Rationale |
|------|-----------|------|-----------|
| 2.1 | `tools/wiki_write.go` + `tools/registry.go` | MEDIUM | Exposes wiki mutation to the LLM behind the compute toolset with JSON Schema parameters. |
| 2.2 | `wiki/store.go` | MEDIUM | Adds `expected_updated_at`, optional reindex notifier, and git/unversioned tracking. |
| 2.3 | `search/async_reindex.go` | MEDIUM | New goroutine with lifecycle management and backpressure. |
| 2.4 | `llm/temp_retry.go` / setup wiring | MEDIUM | Classifies transient vs content failures before varying temperature. |
| 2.5 | Tool-vector retrieval | MEDIUM | Uses Qdrant readiness from Phase 1 before replacing `tool_search`. |

**Validation gate:** Full conversation turn test. Wiki writes go through the tool, concurrent dashboard edits conflict via `expected_updated_at`, transient HTTP failures do not increase temperature, and async reindex shuts down cleanly.

### Phase 3: Resilience Layer
**Goal:** Add provider resilience and cost governance without serializing unrelated users.

| Step | Component | Risk | Rationale |
|------|-----------|------|-----------|
| 3.1 | `llm/circuitbreaker.go` | MEDIUM | Per-provider breaker; lock scope must exclude network I/O. |
| 3.2 | provider concurrency caps / rate limiting | MEDIUM | Prevents breaker blind spots from too many in-flight requests. |
| 3.3 | `budget/peruser.go` | MEDIUM | Adds per-user budget with global hard cap. |
| 3.4 | Telegram setup/budget integration | MEDIUM | Budget check occurs inside the UserGate actor boundary before LLM calls. |

**Validation gate:** Ten concurrent different-user LLM sends complete in about one provider latency, breaker opens per provider, and two rapid same-user messages cannot both pass the budget check.

### Phase 4: Cleanup & Consolidation
**Goal:** Remove legacy chromem-go vector storage paths only after Qdrant-backed behavior is stable.

| Step | Component | Risk | Rationale |
|------|-----------|------|-----------|
| 4.1 | chromem-go call-site audit | MEDIUM | Full grep and build-tag check before deletion. |
| 4.2 | legacy secondary vector path removal | HIGH | Removes chromem-go vector paths; must preserve text search where still intentional. |
| 4.3 | dependency cleanup | MEDIUM | Remove chromem-go only when no production or debug utility still requires it. |

**Validation gate:** `go build ./...` passes for default, `GOOS=linux`, `GOOS=windows`, and integration-tag combinations listed in the cleanup plan; all search_memory vector results come from Qdrant.

## Anti-Patterns to Avoid

### Anti-Pattern 1: Modifying the Agent Loop for Retry Logic

**What people do:** Add retry/temperature logic inside `agentloop.Run()`.
**Why it's wrong:** The loop is stable, well-tested, and already has a clean `ChatClient` interface boundary.
**Do this instead:** Implement retry as a `ChatClient` wrapper. The agentloop doesn't know or care about retries.

### Anti-Pattern 2: Synchronous Reindex in the Write Path

**What people do:** Call `engine.ReindexWikiPage()` inside `wiki.Store.WritePage()` before returning.
**Why it's wrong:** Reindexing hits the embedding API (slow, costs quota). Doing it synchronously adds latency to every wiki write.
**Do this instead:** Signal the async worker and return immediately. The reindex completes in the background.

### Anti-Pattern 3: Global Mutex for Per-User Isolation

**What people do:** Use a single `sync.Mutex` protecting the entire `handleConversation` function.
**Why it's wrong:** Head-of-line blocking -- user A's long-running turn blocks user B's simple question.
**Do this instead:** UserGate actor/inbox (keyed by userID). Only concurrent messages from the same user are serialized.

### Anti-Pattern 4: Adding a Second Vector Store

**What people do:** Add sqlite-vec, pgvector, or chromem-go serialization alongside Qdrant.
**Why it's wrong:** Creates a synchronization problem -- which store is authoritative when they diverge? Adds complexity, dependencies, and startup cost. Qdrant already persists to disk.
**Do this instead:** Qdrant-only. Check Qdrant health at startup. Skip re-embed if collection is warm. The embedding cache in-process handles latency.

### Anti-Pattern 5: Direct Tool Access to Private Wiki Methods

**What people do:** Have the wiki write tool access `wiki.Store` internal fields or call private methods.
**Why it's wrong:** Breaks encapsulation. If wiki.Store changes internally, the tool breaks.
**Do this instead:** Use the existing `wiki.PageWriter` interface. The tool only calls `WritePage(ctx, page)`.

## Integration Points

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `telegram -> agentruntime` | Direct function call (`agentruntime.Run`) | Unchanged. Telegram is the sole consumer. |
| `telegram -> agentloop` | Indirect via agentruntime | Unchanged. agentruntime.Invocation carries all config. |
| `tools -> wiki.Store` | Interface-based (`wiki.PageWriter`) | New boundary for `write_wiki_page`. |
| `wiki.Store -> search (reindex)` | Callback function injection | New one-way signal. wiki does not import search. |
| `llm clients -> llm providers` | `llm.Client` interface | Unchanged. Circuit breaker and retry are transparent wrappers. |
| `search -> Qdrant` | Raw HTTP REST client | Unchanged. Single vector store. |

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Qdrant (vector search) | Raw HTTP REST client | Single source of truth. Docker volume persists data. |
| Embedding API (Mistral/OpenAI) | HTTP via embedding func | Cached in-process. No disk serialization needed. |
| LLM provider | HTTP via OpenAI-compatible API | Wrapped in circuit breaker + retry chain. |

---
*Architecture research for: Aura v4.0 Production Hardening*
*Researched: 2026-05-10*
