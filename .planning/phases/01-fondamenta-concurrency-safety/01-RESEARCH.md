# Phase 1: Fondamenta (Concurrency + Qdrant Readiness) - Research

**Researched:** 2026-05-10
**Domain:** Go concurrency patterns (actor/gate) and Qdrant client unification
**Confidence:** HIGH

## Summary

This phase introduces per-user message serialization using the actor/stateful-goroutine pattern, prevents notification deadlocks via TryAcquire, and releases inactive sessions on a configurable timer. Concurrently, two duplicate Qdrant REST client implementations (in `internal/search/qdrant.go` and `internal/tools/registry_search_vector.go`) are unified into a single shared `internal/qdrant/` package with a clean `Client` interface, a blocking startup health gate, and a warm-cache check based on `points_count > 0`.

The existing `onMessage -> handleConversation` path currently launches every user message in its own goroutine (`go b.handleConversation(c)`) with no per-user serialization. The scheduler's `dispatchTask` fires notifications without any awareness of conversation state. Session state lives in three `sync.Map` instances (`active`, `context`, `snapshots`) with no eviction mechanism.

**Primary recommendation:** Build `internal/concurrency/` as a zero-dependency, pure-Go package with the actor pattern at its core. Build `internal/qdrant/` as a lightweight REST client with a clean interface. Wire UserGate between `onMessage` and `handleConversation`. Run the Qdrant health gate before `bot.Start()` in `main.go`'s `startAura`.

## User Constraints (from CONTEXT.md)

### Locked Decisions

All 20 decisions (D-01 through D-20) are locked. No areas were left to Claude's discretion. Every structural, naming, behavioral, and integration decision was made explicitly. Research focuses on HOW to implement these decisions correctly in Go, not WHETHER to implement them.

### Claude's Discretion

None -- all decisions were made explicitly.

### Deferred Ideas (OUT OF SCOPE)

None -- discussion stayed within phase scope.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Per-user message serialization | API/Backend (Telegram handler) | -- | Gate sits in the process memory between onMessage and handleConversation; no external dependency |
| Notification non-blocking delivery | API/Backend (Scheduler dispatch) | -- | Scheduler dispatchTask calls TryAcquire; Telegram send is always non-blocking regardless |
| Session inactivity eviction | API/Backend (Background goroutine) | -- | InactivityTracker ticker runs in-process; cleans up per-user goroutines |
| Qdrant health probe + warm check | API/Backend (Startup sequence) | Database/Storage | Probes Qdrant REST API; blocking before bot.Start() |
| Qdrant vector operations (search/upsert/delete) | Database/Storage (Qdrant) | -- | All vector I/O goes through shared qdrant.Client; Qdrant is the remote service |

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| CONC-01 | Per-user message serialization -- concurrent messages from same user are queued, not processed in parallel | Actor pattern (Pattern 1), D-01 through D-04, inbox channel architecture |
| CONC-02 | UserGate exposes TryAcquire -- notification paths never deadlock re-entering the same user's gate | TryAcquire pattern (Pattern 2), D-05 through D-08, scheduler integration |
| CONC-03 | Context leak cleanup -- sessions inactive longer than configurable threshold are evicted with resource release | InactivityTracker pattern (Pattern 3), D-09 through D-12, separate tracking structure |
| QDRANT-01 | Qdrant startup health validation -- block startup until Qdrant /readyz passes; skip full re-embed only if collection has points_count > 0 | Qdrant health gate (Pattern 4), D-17 through D-20, collection info endpoint |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `sync` | go1.26.2 | Mutex/RWMutex for gate map and InactivityTracker map | Zero external deps mandated by CONTEXT.md D-14 |
| Go stdlib `context` | go1.26.2 | Context cancellation for per-user goroutine lifecycle | Idiomatic Go goroutine teardown |
| Go stdlib `net/http` | go1.26.2 | Raw HTTP client for Qdrant REST API | Existing codebase pattern; avoids gRPC/protobuf dependency |
| Go stdlib `time` | go1.26.2 | Ticker for InactivityTracker sweeper | No external scheduler needed for 60s tick |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/aura/aura/internal/config` | existing | Config struct for UserGate + Qdrant settings | Always -- settings flow from env to CONC-01/CONC-03/QDRANT-01 |
| `github.com/aura/aura/internal/conversation` | existing | conversation.Context for per-user state | Owned by per-user goroutine (D-04) |
| `gopkg.in/telebot.v4` | v4.0.0-beta.7 | Telegram message sending for overflow notice | When inbox buffer is full (D-03) |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Actor/gate pattern (stdlib) | Keyed mutex (per-user sync.Mutex) | Keyed mutex lacks FIFO ordering, bounded queueing, and TryAcquire. Actor pattern handles all three with channels. |
| InactivityTracker with map+RWMutex | sync.Map iteration (Range) | sync.Map.Range is documented as safe for concurrent use but has known edge cases (PITFALLS.md, golang/go#60216). Separate tracking structure is explicit and testable. |
| Raw HTTP Qdrant client (stdlib) | qdrant/go-client gRPC SDK v1.17.1 | gRPC SDK adds protobuf + gRPC deps. Existing codebase already uses raw HTTP successfully. STACK.md explicitly recommends against gRPC migration. |

**Installation:**
```bash
# No new external dependencies. Phase 1 uses stdlib only (Go 1.26.2).
# Verify:
go build ./internal/concurrency/...
go build ./internal/qdrant/...
```

## Architecture Patterns

### System Architecture Diagram

```
                        Telegram Update (telebot)
                               |
                               v
                        onMessage (handlers.go)
                               |
                               v
                     [NEW] UserGate.Acquire(ctx, userID, entry)
                               |
                    +----------+-----------+
                    |                      |
               [actor exists]        [actor missing]
                    |                      |
                    v                      v
          select { inbox <- entry }   create actor goroutine
          : enqueued                  + start processing loop
          default: dropOldest()       then enqueue
                    |
                    v
          Per-user goroutine inbox loop
          for entry := range inbox {
                    |
                    v
             handleConversation(c)
             (existing logic unchanged)
                    |
                    v
             [after turn completes]
             InactivityTracker.Touch(userID)
          }
          // goroutine exits when ctx is cancelled (eviction)

               Scheduler Tick (scheduler.go)
                    |
                    v
               dispatchTask
                    |
       +------------+-----------+
       |                        |
  KindReminder           KindAgentJob
       |                        |
       v                        v
  [NEW] UserGate.TryAcquire   [NEW] UserGate.TryAcquire
       |                        |
   +---+---+                +---+---+
   |       |                |       |
[true]  [false]          [true]  [false]
   |       |                |       |
   v       v                v       v
enqueue  return nil     enqueue  return nil
to inbox (scheduler     to inbox (scheduler
processes)  retries)    processes)  retries


          InactivityTracker Sweeper (60s ticker)
                    |
                    v
          range gate.lastActivity map
          for each userID exceeding EvictionThreshold
                    |
                    v
          UserGate.Evict(userID)
                    |
                    v
          cancel(user goroutine context)
          delete from UserGate.actors map
          persist conversation snapshot
          delete from InactivityTracker.lastActivity map


          Startup Sequence (main.go startAura)
                    |
                    v
          [NEW] qdrant.WaitForReady(ctx, 120s)
          GET /readyz with exponential backoff
                    |
               +---+---+
               |       |
            [ok]    [timeout]
               |       |
               v       v
          check      exit with
          warm       diagnostic
               |
          GET /collections/{name}
          points_count > 0?
               |
          +----+----+
          |         |
        [yes]     [no]
          |         |
          v         v
       skip full  trigger full
       re-embed   re-embed pass
          |         |
          +----+----+
               |
               v
          bot.Start()
```

### Recommended Project Structure

```
internal/
├── concurrency/               # NEW -- zero-dependency, pure Go
│   ├── gate.go                # UserGate type + Acquire/TryAcquire/Evict
│   ├── gate_test.go           # Unit + race tests
│   ├── tracker.go             # InactivityTracker (created internally by UserGate)
│   ├── tracker_test.go        # Unit tests for eviction logic
│   ├── types.go               # Entry, Config struct
│   └── helpers_test.go        # Test helpers (mock OnEvict callback)
├── qdrant/                    # NEW -- shared Qdrant REST client
│   ├── client.go              # Client interface + concrete impl
│   ├── client_test.go         # Integration tests (mock HTTP server)
│   ├── config.go              # Config struct (BaseURL, APIKey, Timeout)
│   └── types.go               # Point, ScoredPoint, Vector, CollectionInfo
├── telegram/
│   ├── handlers.go            # MODIFIED -- onMessage calls userGate.Acquire
│   ├── conversation.go        # MODIFIED -- per-user goroutine runs handleConversation
│   ├── bot.go                 # MODIFIED -- add userGate, qdrantClient fields
│   └── setup.go               # MODIFIED -- wire UserGate + Qdrant client
├── search/
│   ├── qdrant.go              # MODIFIED -- use shared qdrant.Client internally
│   ├── compact_qdrant.go      # MODIFIED -- use shared qdrant.Client internally
│   └── qdrant_health.go       # NEW or inline -- warm-cache check via shared client
├── tools/
│   └── registry_search_vector.go  # MODIFIED -- use shared qdrant.Client internally
├── scheduler/
│   └── scheduler_handlers.go  # MODIFIED -- dispatchTask calls TryAcquire
└── agentruntime/
    └── session.go             # MODIFIED -- delegate active tracking to UserGate
```

### Pattern 1: Stateful Goroutine (Actor) for Per-User Serialization

**What:** Each active user gets one long-lived goroutine with a private buffered channel inbox. The goroutine processes entries one at a time -- serialization is implicit. All conversation state for that user is private to the goroutine (no locks needed).

**When to use:** On first Acquire for a userID with no existing actor. Destroyed on Evict (inactivity timeout).

**Implementation:**
```go
// Source: stdlib channels + context pattern
// File: internal/concurrency/gate.go

type UserGate struct {
    mu      sync.Mutex
    actors  map[string]*userActor
    config  Config
    tracker *InactivityTracker
}

type userActor struct {
    userID string
    inbox  chan Entry       // buffered, capacity from Config.InboxSize
    ctx    context.Context
    cancel context.CancelFunc
}

func (g *UserGate) Acquire(ctx context.Context, userID string, entry Entry) error {
    g.mu.Lock()
    actor, exists := g.actors[userID]
    if !exists {
        actor = g.spawnActorLocked(userID)
        g.actors[userID] = actor
    }
    g.mu.Unlock()

    select {
    case actor.inbox <- entry:
        return nil
    default:
        // Buffer full: drop oldest, send Telegram notice
        g.dropOldest(actor, entry)
        return nil
    }
}

func (g *UserGate) spawnActorLocked(userID string) *userActor {
    ctx, cancel := context.WithCancel(context.Background())
    actor := &userActor{
        userID: userID,
        inbox:  make(chan Entry, g.config.InboxSize),
        ctx:    ctx,
        cancel: cancel,
    }
    go g.runActor(actor)
    return actor
}

func (g *UserGate) runActor(actor *userActor) {
    for {
        select {
        case <-actor.ctx.Done():
            return
        case entry := <-actor.inbox:
            g.processEntry(actor, entry)
            g.tracker.Touch(actor.userID)  // reset inactivity clock
        }
    }
}

func (g *UserGate) Evict(userID string) {
    g.mu.Lock()
    actor, exists := g.actors[userID]
    if !exists {
        g.mu.Unlock()
        return
    }
    delete(g.actors, userID)
    g.mu.Unlock()

    actor.cancel()  // signals runActor to exit
    g.tracker.Remove(userID)
    if g.config.OnEvict != nil {
        g.config.OnEvict(userID)
    }
}
```

**Why this approach:** The Phase 1 checkpoint selected the actor pattern over a keyed mutex. It gives FIFO ordering, bounded queueing, TryAcquire, and actor shutdown on inactivity -- all with zero external dependencies. The goroutine owns the conversation.Context while alive (D-04).

### Pattern 2: TryAcquire for Notification Paths (Non-Blocking Send)

**What:** A non-blocking send to the per-user inbox channel. Returns true on success, false when the inbox is full. Callers handle false by dropping and letting their own retry mechanism handle it.

**When to use:** Every notification path (scheduler reminder, agent job dispatch, wiki maintenance result delivery).

**Implementation:**
```go
// Source: select/default channel pattern (same as BufferedAppender)
// File: internal/concurrency/gate.go

func (g *UserGate) TryAcquire(userID string, entry Entry) bool {
    g.mu.Lock()
    actor, exists := g.actors[userID]
    if !exists {
        // No active session for this user -- create one
        actor = g.spawnActorLocked(userID)
        g.actors[userID] = actor
    }
    g.mu.Unlock()

    select {
    case actor.inbox <- entry:
        return true
    default:
        return false
    }
}
```

**Why this approach:** The non-blocking send pattern (`select { case ch <- item: default: return false }`) is identical to the existing `BufferedAppender.Append` in `internal/conversation/archive.go`. This is the idiomatic Go pattern for backpressure-aware channel operations. The scheduler retries on the next tick (D-05), the agent job notification is optional (D-06), and all notification entries flow through the same FIFO inbox as user messages (D-07).

### Pattern 3: InactivityTracker with Separate Tracking Structure

**What:** A `map[string]time.Time` guarded by `sync.RWMutex` with a background 60s ticker goroutine. Calls `UserGate.Evict` via an `onEvict` callback for users whose last activity exceeds the threshold.

**When to use:** Created internally by `UserGate.New(Config)`. Not wired separately.

**Implementation:**
```go
// Source: stdlib map + RWMutex + time.Ticker
// File: internal/concurrency/tracker.go

type InactivityTracker struct {
    mu              sync.RWMutex
    lastActivity    map[string]time.Time
    threshold       time.Duration
    sweepInterval   time.Duration
    onEvict         func(userID string)
    ctx             context.Context
    cancel          context.CancelFunc
}

func (t *InactivityTracker) Start() {
    ticker := time.NewTicker(t.sweepInterval)
    defer ticker.Stop()
    for {
        select {
        case <-t.ctx.Done():
            return
        case <-ticker.C:
            t.sweep()
        }
    }
}

func (t *InactivityTracker) sweep() {
    now := time.Now()
    t.mu.RLock()
    stale := make([]string, 0)
    for userID, last := range t.lastActivity {
        if now.Sub(last) > t.threshold {
            stale = append(stale, userID)
        }
    }
    t.mu.RUnlock()

    for _, userID := range stale {
        t.onEvict(userID)
    }
}

func (t *InactivityTracker) Touch(userID string) {
    t.mu.Lock()
    t.lastActivity[userID] = time.Now()
    t.mu.Unlock()
}

func (t *InactivityTracker) Remove(userID string) {
    t.mu.Lock()
    delete(t.lastActivity, userID)
    t.mu.Unlock()
}

func (t *InactivityTracker) Stop() {
    t.cancel()
}
```

**Why this approach:** D-09 defines inactivity as time since last turn completion (the Touch call after each inbox entry is processed). D-11 mandates a standalone tracker with map+RWMutex+ticker -- cleanly separated from UserGate internals. D-12 specifies 60s sweep interval. The separate tracking structure avoids sync.Map.Range entirely (PITFALLS.md, CONC-03 requirement).

### Pattern 4: Shared Qdrant Client with Blocking Health Gate

**What:** A unified `Client` interface in `internal/qdrant/` with a single concrete implementation. Three callers migrate to it: the wiki search `qdrantRepository`, the compact memory mirror `CompactMemoryQdrantIndex`, and the tool vector search `toolVectorIndex`. At startup, `WaitForReady` blocks until Qdrant `/readyz` responds OK.

**When to use:** Qdrant health gate before `bot.Start()`. Client used by all Qdrant consumers throughout the process lifetime.

**Implementation:**
```go
// Source: existing net/http pattern from internal/search/qdrant.go
// File: internal/qdrant/client.go

type Client interface {
    Health(ctx context.Context) error
    Search(ctx context.Context, collection string, vector []float32, topK int, withPayload bool) ([]ScoredPoint, error)
    Upsert(ctx context.Context, collection string, points []Point) error
    Delete(ctx context.Context, collection string, ids []string) error
    CreateCollection(ctx context.Context, collection string, vectorSize int) error
    DeleteCollection(ctx context.Context, collection string) error
    CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error)
}

type httpClient struct {
    baseURL  string
    apiKey   string
    http     *http.Client
}

func (c *httpClient) Health(ctx context.Context) error {
    // GET {baseURL}/readyz
    // Returns error if status != 2xx
}

func (c *httpClient) CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error) {
    // GET {baseURL}/collections/{collection}
    // Parses response, returns CollectionInfo with PointsCount
}

// WaitForReady blocks until Qdrant is healthy or timeout expires.
func WaitForReady(ctx context.Context, client Client, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    backoff := 500 * time.Millisecond
    for time.Now().Before(deadline) {
        if err := client.Health(ctx); err == nil {
            return nil
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(backoff):
            backoff = min(backoff*2, 10*time.Second)
        }
    }
    return fmt.Errorf("qdrant not ready after %v", timeout)
}
```

**File: internal/qdrant/types.go:**
```go
type Point struct {
    ID      string            `json:"id"`
    Vector  []float32         `json:"vector"`
    Payload map[string]string `json:"payload"`
}

type ScoredPoint struct {
    ID      any               `json:"id"`
    Score   float32           `json:"score"`
    Payload map[string]string `json:"payload"`
}

type CollectionInfo struct {
    Status              string `json:"status"`
    PointsCount         uint64 `json:"points_count"`
    IndexedVectorsCount uint64 `json:"indexed_vectors_count"`
}

type Config struct {
    BaseURL string
    APIKey  string
    Timeout time.Duration
}
```

**Why this approach:** D-17 through D-20 mandate this exact structure. The `CollectionInfo` method returning `PointsCount` enables the warm-cache check: `points_count > 0` means the collection has data and we can skip the full re-embed pass (QDRANT-01). The 120-second default timeout (D-20) prevents the startup loop trap documented in PITFALLS.md Pitfall 10.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Per-user FIFO serialization with bounded queue | Custom mutex queue | stdlib buffered channel as inbox | Channels natively provide FIFO ordering, blocking send, non-blocking try-send, and buffer management. Building a custom queue on a mutex would reimplement channel semantics with more bugs. |
| Goroutine lifecycle management | Custom goroutine registry with channels | context.WithCancel + goroutine select on ctx.Done() | context is the standard Go mechanism for cancellation propagation. The actor goroutine's select loop checks ctx.Done() for clean teardown. |
| Inactivity timeout tracking | sync.Map iteration (Range) | map[string]time.Time + sync.RWMutex | D-11 explicitly mandates separate tracking structure. PITFALLS.md documents sync.Map concurrent iteration edge cases. |
| Qdrant point counting | Custom HTTP endpoint parsing | Qdrant REST `GET /collections/{name}` response field `points_count` | Official Qdrant API [VERIFIED: api.qdrant.tech]. The field is guaranteed present on all Qdrant versions supported by the project. |
| Exponential backoff for health retry | Custom retry loop with sleep | time.After + multiplicative backoff in WaitForReady | Simple, correct, stdlib-only. No retry library needed for a single startup gate. |

**Key insight:** Go's channel primitive is already an actor inbox. The `make(chan Entry, 8)` pattern with `select { case ch <- item: default: }` covers enqueue, try-enqueue, and overflow detection in three lines. Building custom queue logic on top of mutexes or slices would add complexity without improving correctness.

## Common Pitfalls

### Pitfall 1: UserGate Deadlock via Re-Entrant Acquire

**What goes wrong:** handleConversation calls Acquire which blocks on the inbox channel send. If the current goroutine IS the per-user actor goroutine (re-entrant call), the send blocks forever -- the actor is waiting for itself to receive.

**Why it happens:** Go channels have no re-entrancy detection. A blocking send from the receiver's goroutine is a self-deadlock.

**How to avoid:** The integration pattern (D-15) ensures Acquire is called from `onMessage`'s goroutine, NOT from the per-user actor goroutine. The actor goroutine runs `handleConversation` directly -- it never calls Acquire. This separation is structural, not runtime-detected.

**Warning signs:** Goroutine profile shows `[chan send]` on a goroutine whose own stack also contains the matching `[chan receive]`. Single user becomes permanently unresponsive.

### Pitfall 2: InactivityTracker Not Stopped on Shutdown

**What goes wrong:** The InactivityTracker's background goroutine leaks on process restart. The time.Ticker is never stopped, keeping the goroutine alive indefinitely.

**Why it happens:** UserGate.Stop() cancels the context but never calls tracker.Stop() if they're separate lifecycle methods.

**How to avoid:** UserGate.Close() (the public shutdown method) must call tracker.Stop() and wait for the sweeper goroutine to exit before returning. Use a sync.WaitGroup or done channel.

**Warning signs:** runtime.NumGoroutine() grows by 1 on each restart cycle. Goroutine profile shows goroutines parked on `time.sendTime` with InactivityTracker in the stack.

### Pitfall 3: Qdrant CollectionInfo Called Before Collection Exists

**What goes wrong:** Calling `GET /collections/{name}` before the collection has been created returns 404. The code treats this as an error and fails the warm check, triggering a re-embed. But on the FIRST startup, the collection genuinely doesn't exist yet -- this is expected, not an error.

**Why it happens:** The warm-cache check conflates "collection doesn't exist" (first startup, expected) with "collection exists but has zero points" (data loss, unexpected).

**How to avoid:** Treat 404 on CollectionInfo as "not warm" (trigger re-embed) without logging an error. Distinguish between:
- 404: Collection not created yet -> proceed with re-embed (normal first startup)
- 200 with points_count == 0: Collection exists but empty -> proceed with re-embed (recovery)
- 200 with points_count > 0: Collection is warm -> skip re-embed (normal restart)

**Warning signs:** Startup log shows "collection info failed: 404" as an error. Re-embed fires on every restart even when vectors exist.

### Pitfall 4: Channel Overflow Without Telegram Notice Delivery

**What goes wrong:** When the inbox buffer is full and an entry is dropped, D-03 requires sending a Telegram notice. But the code that sends the notice calls `b.bot.Send()` which itself may fail or block.

**Why it happens:** The `dropOldest` logic tries to send a Telegram message to the user saying "your message was dropped." If Telegram is slow or the send fails, the gate goroutine blocks.

**How to avoid:** Send the Telegram notice in a new goroutine (`go b.sendOverflowNotice(userID)`). The gate goroutine must never block on external I/O. Use `tele.Bot.Send` with a reasonable timeout context.

**Warning signs:** The gate goroutine blocks on Telegram API calls. All users behind the same gate stall if one overflow notice hangs.

## Code Examples

Verified patterns from existing codebase and Go stdlib:

### Channel-Based Non-Blocking Send (TryAcquire Pattern)

```go
// Source: internal/conversation/archive.go (BufferedAppender.Append, line 357-365)
// [VERIFIED: codebase] -- identical pattern used for archive writes
func (g *UserGate) TryAcquire(userID string, entry Entry) bool {
    actor := g.getOrCreateActor(userID)
    select {
    case actor.inbox <- entry:
        return true
    default:
        return false
    }
}
```

### Buffered Channel with Drop-Oldest

```go
// Source: Go stdlib channel operations
// [VERIFIED: Go spec] -- non-blocking receive + send for ring-buffer behavior
func (g *UserGate) dropOldest(actor *userActor, newEntry Entry) {
    select {
    case <-actor.inbox:
        // Dropped oldest successfully
    default:
        // Inbox was somehow empty (shouldn't happen if default send triggered)
    }
    // Now enqueue the new entry
    select {
    case actor.inbox <- newEntry:
    default:
        // Still full after drop -- should not happen with cap >= 2
    }
}
```

### Context-Based Goroutine Lifecycle

```go
// Source: Go stdlib context package
// [VERIFIED: Go docs] -- idiomatic goroutine teardown
func (g *UserGate) runActor(actor *userActor) {
    defer close(actor.done)
    for {
        select {
        case <-actor.ctx.Done():
            return  // eviction cancelled us
        case entry, ok := <-actor.inbox:
            if !ok {
                return  // inbox closed
            }
            g.handleEntry(actor, entry)
            g.tracker.Touch(actor.userID)
        }
    }
}
```

### Qdrant Collection Info (Warm-Cache Check)

```go
// Source: Qdrant REST API documentation [VERIFIED: api.qdrant.tech]
// GET /collections/{collection_name}
// Response includes "result.points_count" (uint64)
func (c *httpClient) CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET",
        c.baseURL+"/collections/"+url.PathEscape(collection), nil)
    c.authorize(req)
    resp, err := c.http.Do(req)
    if err != nil {
        return CollectionInfo{}, fmt.Errorf("collection info: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotFound {
        return CollectionInfo{}, nil  // PointsCount will be 0
    }
    var result struct {
        Result CollectionInfo `json:"result"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return CollectionInfo{}, err
    }
    return result.Result, nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| goroutine-per-message (no serialization) | Per-user actor with channel inbox | Phase 1 | Same-user messages serialize; different-user messages still parallel |
| Notification direct Telegram send | TryAcquire into same inbox channel | Phase 1 | Notifications queue behind user messages; no re-entrancy deadlock |
| sync.Map.Range for cleanup (none currently) | InactivityTracker with separate map | Phase 1 | CONC-03 requirement; avoids sync.Map iteration edge cases |
| Duplicate Qdrant client in each consumer | Shared qdrant.Client interface | Phase 1 | Single source of truth; consumers get consistent health/error behavior |

**Deprecated/outdated:**
- Direct `go b.handleConversation(c)` in `onMessage` -- replaced by `userGate.Acquire` gate
- `qdrantClient` (unexported in search package) -- migrated to `qdrant.Client` interface
- `toolVectorIndex` (unexported in tools package) -- migrated to `qdrant.Client` interface
- `CheckQdrantReady` (exported function in search package) -- migrated to `qdrant.Client.Health()`

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Qdrant's `GET /collections/{name}` response field is `points_count` (not `vectors_count` or another name) | Code Examples | Warm cache check uses wrong field name; always triggers re-embed |
| A2 | telebot v4's `b.bot.Send()` is safe to call from a goroutine spawned by the gate overflow handler | Pitfall 4 | Telegram notice delivery panics or blocks the gate goroutine |
| A3 | The Go 1.26.2 runtime's HashTrieMap-based sync.Map does not crash on concurrent Range+Store (we still avoid Range per D-11, but the risk is mitigated) | Common Pitfalls | If Go version is downgraded, sync.Map.Range crash risk returns |
| A4 | Scheduler's `dispatchTask` is called outside any per-user gate -- notifications originate from the scheduler goroutine, not the per-user actor goroutine | Pattern 2 | If a notification is dispatched from within handleConversation (re-entrant), TryAcquire still works since it's a non-blocking channel send |

**If this table is empty:** All claims in this research were verified or cited -- no user confirmation needed.

## Open Questions

1. **How should the per-user goroutine's `Entry` type expose tele.Context for handleConversation?**
   - What we know: D-08 mandates a uniform inbox entry type -- no kind discriminator. The entry must carry enough data for handleConversation to run.
   - What's unclear: Whether to embed tele.Context directly in the Entry (couples concurrency package to telebot) or use a callback/function pattern.
   - Recommendation: Define Entry as a struct with a `Process func(ctx context.Context)` field. onMessage wraps `handleConversation` in a closure. This keeps `internal/concurrency/` telebot-free (D-14).

2. **What happens to pending inbox entries when a user is evicted mid-queue?**
   - What we know: D-09 says users with pending inbox entries are never evicted. But the sweeper checks lastActivity timestamps, not inbox depth.
   - What's unclear: How the sweeper determines "has pending entries" without inspecting the channel (which is owned by the actor goroutine).
   - Recommendation: The actor goroutine sets a flag `actor.processing` when it dequeues an entry and clears it after Touch. The sweeper checks `actor.processing` under the gate mutex. If true, the user is actively processing -- skip eviction.

3. **Qdrant CollectionInfo: Should the client expose `GET /collections/{name}` or `POST /collections/{name}/points/count`?**
   - What we know: Both endpoints exist. `GET /collections/{name}` returns full info including points_count. `POST .../points/count` returns only the count.
   - What's unclear: Which is faster/more reliable for the startup warm check.
   - Recommendation: Use `GET /collections/{name}` -- it provides points_count AND status (green/yellow/red). A yellow/red status could indicate an unhealthy collection even with points_count > 0. This extra signal may be useful for debugging.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Building all packages | Yes | go1.26.2 | -- |
| Qdrant (running) | Integration tests, warm-cache check | Unknown at research time | -- | Mock HTTP server for unit tests |
| telebot (dependency) | Telegram integration tests | Yes | v4.0.0-beta.7 | -- |

**Missing dependencies with no fallback:**
- Qdrant for integration tests -- planner must note that integration tests require a running Qdrant instance or a mock HTTP server.

**Missing dependencies with fallback:**
- None -- all code dependencies are in go.mod.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing (stdlib) + race detector |
| Config file | none -- Go test files inline |
| Quick run command | `go test -race -count=1 -short ./internal/concurrency/...` |
| Full suite command | `go test -race -count=10 ./internal/concurrency/... ./internal/qdrant/...` |

### Phase Requirements -> Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CONC-01 | Same-user messages are processed sequentially (not in parallel) | unit + race | `go test -race -run TestSequentialProcessing -count=50 ./internal/concurrency/` | No -- Wave 0 |
| CONC-01 | Different-user messages process concurrently (not serialized) | unit + race | `go test -race -run TestConcurrentUsers -count=50 ./internal/concurrency/` | No -- Wave 0 |
| CONC-01 | Inbox overflow drops oldest entry and calls OnOverflow callback | unit | `go test -race -run TestOverflowDropOldest ./internal/concurrency/` | No -- Wave 0 |
| CONC-02 | TryAcquire returns true when inbox has space | unit | `go test -race -run TestTryAcquire ./internal/concurrency/` | No -- Wave 0 |
| CONC-02 | TryAcquire returns false when inbox is full (non-blocking) | unit + race | `go test -race -run TestTryAcquireFull -count=50 ./internal/concurrency/` | No -- Wave 0 |
| CONC-02 | Notification path (scheduler dispatchTask) calls TryAcquire and returns nil on false (does not deadlock) | integration | `go test -race -run TestNotificationNoDeadlock ./internal/telegram/` | No -- Wave 0 |
| CONC-03 | InactivityTracker evicts user after threshold exceeded | unit | `go test -race -run TestEviction ./internal/concurrency/` | No -- Wave 0 |
| CONC-03 | Active user (recent touch) is NOT evicted | unit | `go test -race -run TestNoEvictionActive ./internal/concurrency/` | No -- Wave 0 |
| CONC-03 | Eviction cancels goroutine context and calls OnEvict callback | unit | `go test -race -run TestEvictionCleanup ./internal/concurrency/` | No -- Wave 0 |
| CONC-03 | InactivityTracker does NOT use sync.Map.Range | inspection | Manual code review / grep for `sync.Map` and `Range` in `internal/concurrency/` | N/A |
| QDRANT-01 | Health gate blocks until Qdrant /readyz returns 2xx | unit (mock server) | `go test -run TestWaitForReady ./internal/qdrant/` | No -- Wave 0 |
| QDRANT-01 | Health gate times out with clear diagnostic message after timeout | unit (mock server) | `go test -run TestWaitForReadyTimeout ./internal/qdrant/` | No -- Wave 0 |
| QDRANT-01 | Warm check: points_count > 0 skips re-embed | unit (mock server) | `go test -run TestWarmCheckSkipped ./internal/qdrant/` | No -- Wave 0 |
| QDRANT-01 | Warm check: points_count == 0 triggers re-embed | unit (mock server) | `go test -run TestWarmCheckReEmbed ./internal/qdrant/` | No -- Wave 0 |
| QDRANT-01 | Warm check: collection 404 triggers re-embed (first startup) | unit (mock server) | `go test -run TestWarmCheckNotFound ./internal/qdrant/` | No -- Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -race -count=1 -short ./internal/concurrency/... ./internal/qdrant/...`
- **Per wave merge:** `go test -race -count=10 ./internal/concurrency/... ./internal/qdrant/...`
- **Phase gate:** Full suite green + race detector clean + `go test -race -count=50` stress pass

### Wave 0 Gaps
- [ ] `internal/concurrency/gate_test.go` -- covers CONC-01 (serial, concurrent, overflow), CONC-02 (TryAcquire)
- [ ] `internal/concurrency/tracker_test.go` -- covers CONC-03 (eviction, active skip, cleanup callback)
- [ ] `internal/qdrant/client_test.go` -- covers QDRANT-01 (health gate, warm check, collection info)
- [ ] `internal/qdrant/mock_server_test.go` -- httptest server for Qdrant API mock
- [ ] `internal/telegram/concurrency_integration_test.go` -- covers end-to-end CONC-01/CONC-02 with real Bot

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | Phase 1 doesn't touch authentication -- allowlist check remains in onMessage before Acquire |
| V3 Session Management | Yes | UserGate owns session lifecycle; context cancellation on eviction prevents session data leaks |
| V4 Access Control | Yes | Gate is keyed by userID; no cross-user state access is possible (per-user goroutines own private state) |
| V5 Input Validation | Yes | Entry type is internal (not user-controlled); userID is validated before Acquire; channel capacity bounds memory |
| V6 Cryptography | No | No cryptographic operations in this phase |

### Known Threat Patterns for Actor/Gate + Qdrant

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Resource exhaustion via many userIDs | Denial of Service | Config.InboxSize caps per-user memory; eviction cleans up inactive users; map size is bounded by active users |
| Channel buffer overflow leading to dropped messages | Denial of Service | Drop-oldest with Telegram notice (D-03) is graceful degradation, not silent loss |
| Goroutine leak via actor creation without eviction | Denial of Service | InactivityTracker guarantees eventual cleanup of all actors |
| Qdrant health gate timeout blocking startup indefinitely | Denial of Service | Configurable timeout (120s default, D-20) with exponential backoff and clear diagnostic |

## Sources

### Primary (HIGH confidence)
- Codebase: `internal/telegram/handlers.go` -- verified onMessage launches goroutine-per-message with no serialization [VERIFIED: codebase read]
- Codebase: `internal/telegram/conversation.go` -- verified handleConversation flow, session.Begin, runToolCallingLoop [VERIFIED: codebase read]
- Codebase: `internal/telegram/bot.go` -- verified Bot struct has sessions (*agentruntime.SessionStore), schedDB, no UserGate [VERIFIED: codebase read]
- Codebase: `internal/agentruntime/session.go` -- verified SessionStore uses three sync.Map instances, no eviction [VERIFIED: codebase read]
- Codebase: `internal/search/qdrant.go` -- verified qdrantClient (unexported) with ready/queryPoints/upsertPoints/deletePoints/createCollection/deleteCollection methods, raw HTTP [VERIFIED: codebase read]
- Codebase: `internal/tools/registry_search_vector.go` -- verified toolVectorIndex (unexported) with duplicate Qdrant client implementation, same REST pattern [VERIFIED: codebase read]
- Codebase: `internal/search/compact_qdrant.go` -- verified CompactMemoryQdrantIndex with separate qdrantClient instance [VERIFIED: codebase read]
- Codebase: `internal/conversation/archive.go` -- verified BufferedAppender uses select/default non-blocking channel send (same pattern as TryAcquire) [VERIFIED: codebase read]
- Codebase: `internal/telegram/scheduler_handlers.go` -- verified dispatchTask calls b.bot.Send() directly for reminders, no gate check [VERIFIED: codebase read]
- Codebase: `internal/scheduler/scheduler.go` -- verified 30s tick, fireOne calls dispatcher, advance handles recurring/one-shot tasks [VERIFIED: codebase read]
- Codebase: `internal/telegram/setup.go` -- verified Bot construction, scheduler wiring, Qdrant integration points [VERIFIED: codebase read]
- Codebase: `cmd/aura/main.go` -- verified startAura flow: bootstrap -> DB -> settings -> telegram.New -> healthServer -> bot.Start() [VERIFIED: codebase read]
- Codebase: `internal/tools/memory_search.go` -- verified SearchMemoryTool uses search.Searcher interface for wiki, compactMemorySearcher for compact [VERIFIED: codebase read]
- Codebase: `internal/search/search.go` -- verified Repository/Searcher/Queryer interfaces and Result type [VERIFIED: codebase read]
- Codebase: `go.mod` -- verified Go 1.26.2, telebot v4.0.0-beta.7, no gRPC or Qdrant SDK deps [VERIFIED: codebase read]
- Qdrant API docs: `GET /collections/{name}` returns `points_count` in response body [VERIFIED: api.qdrant.tech/api-reference/collections/get-collection]
- Qdrant API docs: `GET /readyz` returns 2xx when Qdrant is healthy [VERIFIED: api.qdrant.tech]
- Go docs: context.WithCancel is the idiomatic goroutine lifecycle mechanism [VERIFIED: pkg.go.dev/context]
- Go docs: buffered channels support non-blocking send via select/default [VERIFIED: Go language spec]

### Secondary (MEDIUM confidence)
- PITFALLS.md: Pitfall 1 (UserGate deadlock), Pitfall 2 (sync.Map iteration), Pitfall 5 (Qdrant warm check false positive), Pitfall 10 (startup timeout loop) [CITED: .planning/research/PITFALLS.md]
- STACK.md: UserGate actor/inbox pattern selected over keyed mutex; raw HTTP Qdrant client selected over gRPC SDK [CITED: .planning/research/STACK.md]
- ARCHITECTURE.md: UserGate sits between onMessage and handleConversation; inactivity eviction is separate background path [CITED: .planning/research/ARCHITECTURE.md]
- ARCHITECTURE.md: Phase 1 build order: usergate.go -> cleanup.go -> qdrant_health.go -> integration wiring [CITED: .planning/research/ARCHITECTURE.md]

### Tertiary (LOW confidence)
- None -- all claims are either verified against codebase or cited from project planning documents.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all deps verified in go.mod and codebase; no new external dependencies
- Architecture: HIGH -- patterns verified against existing codebase structure and Go stdlib idioms
- Pitfalls: HIGH -- verified against PITFALLS.md research and codebase inspection
- Qdrant API: HIGH -- verified against official Qdrant REST API docs (api.qdrant.tech)

**Research date:** 2026-05-10
**Valid until:** 2026-06-10 (30 days -- stable Go concurrency patterns and Qdrant REST API)
