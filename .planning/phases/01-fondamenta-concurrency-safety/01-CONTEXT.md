# Phase 1: Fondamenta (Concurrency + Qdrant Readiness) - Context

**Gathered:** 2026-05-10
**Status:** Ready for planning

<domain>
## Phase Boundary

Make Aura safe for concurrent Telegram users by serializing per-user message processing via stateful goroutines, preventing notification deadlocks with TryAcquire, and releasing inactive sessions predictably. Additionally, establish a shared Qdrant client contract with a blocking startup health gate — replacing two duplicate Qdrant client implementations in `internal/search/` and `internal/tools/`.

**Requirements:** CONC-01, CONC-02, CONC-03 (from REQUIREMENTS.md)
</domain>

<decisions>
## Implementation Decisions

### Queue Behavior & Serialization
- **D-01:** Use the **stateful goroutine (actor) pattern** for per-user serialization. One goroutine per active user with a private channel inbox. All conversation state is private to that goroutine — no locks needed. Serialization is implicit: the goroutine processes one inbox entry at a time.
- **D-02:** Messages arriving while a user's turn is running are **enqueued silently** in a buffered channel inbox (capacity 8). No immediate feedback to the user — they see the typing indicator when processing starts.
- **D-03:** When the inbox buffer is full, **drop the oldest entry** and send a Telegram notice to the user. No per-message deadline — queued messages wait indefinitely for their turn.
- **D-04:** Per-user goroutine is **created on first Acquire** and **destroyed on eviction** (inactivity timeout). The goroutine owns the `conversation.Context` while alive.

### Notification Paths
- **D-05:** When a scheduler notification (reminder) fires and TryAcquire fails, **drop + scheduler retry** on the next tick (30s default). The scheduler guarantees eventual delivery.
- **D-06:** Notifications use **TryAcquire via the same inbox channel** — a non-blocking send. On success, the notification is queued alongside user messages. On failure, the caller gets `false` and handles accordingly.
- **D-07:** **FIFO processing** — no special handling for notifications vs user messages. Inbox order strictly determines processing order. A notification arriving during a turn is dequeued after the turn completes.
- **D-08:** **Uniform inbox entry type** — no kind discriminator. Notifications and user messages use the same struct. No behavioral difference in the processing loop.

### Eviction Strategy
- **D-09:** Inactivity is defined as **time since last turn completion**. The clock resets every time the per-user goroutine finishes processing an inbox entry. Users with pending inbox entries are never evicted.
- **D-10:** Default threshold is **30 minutes**. On eviction: cancel goroutine context, delete from UserGate and InactivityTracker maps, persist conversation snapshot to SessionStore. Budget tracker state and archived turns (SQLite) are kept.
- **D-11:** **Standalone InactivityTracker** with `map[string]time.Time` + `sync.RWMutex` + background ticker goroutine. Calls `UserGate.Evict` via an `onEvict` callback. Cleanly separated from UserGate internals.
- **D-12:** Sweeper ticks every **60 seconds**. On eviction: cancel goroutine context, persist conversation snapshot, clean up all maps.

### Gate API Design
- **D-13:** UserGate exposes three methods: `Acquire(ctx, userID, entry) error` (blocks until enqueued or ctx cancelled), `TryAcquire(userID, entry) bool` (non-blocking, for notifications), `Evict(userID)` (cancels goroutine, called by InactivityTracker).
- **D-14:** Package location: **`internal/concurrency/`** — zero Aura dependencies, pure Go, testable in isolation.
- **D-15:** Integration pattern: **Gate wraps SessionStore entry**. `onMessage` spawns a lightweight goroutine that calls `Acquire`, which delivers the entry to the per-user goroutine's inbox. The per-user goroutine runs `handleConversation`.
- **D-16:** Configuration via a **single Config struct** with `InboxSize` (default 8), `EvictionThreshold` (default 30min), `SweepInterval` (default 60s), and `OnEvict` callback. UserGate creates InactivityTracker internally via `New(Config)`.

### Qdrant Readiness Contract
- **D-17:** Create a shared **`internal/qdrant/`** package with a `Client` interface and single concrete implementation. Replaces the two duplicate Qdrant clients in `internal/search/qdrant.go` (unexported `qdrantClient`) and `internal/tools/registry_search_vector.go` (unexported `toolVectorIndex`).
- **D-18:** Client interface exposes all needed operations: `Health`, `Search`, `Upsert`, `Delete`, `CreateCollection`, `DeleteCollection`.
- **D-19:** Package layout: `client.go` (interface + concrete impl), `config.go` (Config struct with BaseURL, APIKey, Timeout), `types.go` (Point, ScoredPoint, Vector).
- **D-20:** **Blocking health gate at startup** — before `bot.Start()`, probe Qdrant `/readyz` with a configurable timeout (default 120s). If unreachable, Aura exits with a clear diagnostic message indicating the Qdrant endpoint and elapsed wait time.

### Claude's Discretion
No areas were left to Claude's discretion — all decisions were made explicitly.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & Roadmap
- `.planning/REQUIREMENTS.md` — Phase 1 requirements: CONC-01 (per-user serialization), CONC-02 (TryAcquire for notifications), CONC-03 (inactivity eviction)
- `.planning/ROADMAP.md` — Phase 1 success criteria and phase boundary
- `.planning/research/SUMMARY.md` — v4.0 research context

### Existing Qdrant Code (to be unified)
- `internal/search/qdrant.go` — Current wiki search Qdrant client (unexported, to be migrated to shared package)
- `internal/search/compact_qdrant.go` — Compact memory Qdrant index
- `internal/tools/registry_search_vector.go` — Tool vector search Qdrant client (duplicate implementation, to be migrated)

### Concurrency Foundations
- `internal/agentruntime/session.go` — Current SessionStore (active/context/snapshots via sync.Map)
- `internal/telegram/handlers.go` — Message entry point (onMessage → goroutine-per-message, no serialization)
- `internal/telegram/conversation.go` — handleConversation (to be gated by UserGate)
- `internal/telegram/scheduler_handlers.go` — dispatchTask (notification delivery path)
- `internal/scheduler/scheduler.go` — Scheduler tick loop and task firing
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`SessionStore`** (`internal/agentruntime/session.go`): Existing session lifecycle management. Will be extended to call through to UserGate. The `sync.Map`-based active tracking will be replaced by the gate's goroutine map.
- **`BufferedAppender`** (`internal/conversation/archive.go`): Already uses `select/default` coalescing pattern. Same pattern applies to inbox TryAcquire.
- **`CheckQdrantReady`** (`internal/search/qdrant.go`): Existing health probe function. Will be migrated to the shared `qdrant.Client.Health()`.

### Established Patterns
- **Composition over modification**: The `agentruntime/agentloop` packages are stable. UserGate wraps, doesn't modify them.
- **Go stdlib-only concurrency**: `sync.Mutex`, `sync.Map`, channels — no external concurrency libraries.
- **Channel-based lifecycle**: `context.WithCancel` for goroutine teardown.

### Integration Points
- **`onMessage` → `handleConversation`**: UserGate sits between these. `onMessage` calls `Acquire` instead of `go handleConversation`.
- **Scheduler → dispatchTask**: Notification path calls `TryAcquire`. On failure, returns nil (scheduler retries).
- **`main.go startAura`**: Qdrant health gate added before `bot.Start()`. Blocks startup with configurable timeout.
- **`telegram.setup.go` Bot struct**: New fields: `userGate *concurrency.UserGate`, `qdrantClient qdrant.Client`.
</code_context>

<specifics>
## Specific Ideas

- UserGate inbox is a buffered channel (cap 8), drop oldest on overflow with Telegram notice.
- InactivityTracker is created internally by UserGate, not wired separately by the caller.
- The shared Qdrant `Client` interface lives in `internal/qdrant/` — three files: client.go, config.go, types.go.
- Startup health gate: blocking, 120s default timeout, clear diagnostic on failure.
</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.
</deferred>

---

*Phase: 1-Fondamenta (Concurrency + Qdrant Readiness)*
*Context gathered: 2026-05-10*
