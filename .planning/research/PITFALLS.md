# Pitfalls Research: Production Hardening Integration

**Domain:** Go agent runtime hardening -- Telegram + LLM + Qdrant (single vector store)
**Researched:** 2026-05-10
**Confidence:** HIGH (verified against codebase + documentation + community post-mortems)

---

## Critical Pitfalls

### Pitfall 1: UserGate Deadlock via Re-Entrant Acquire

**What goes wrong:**
A user sends a message that is being processed by that user's actor, the LLM loop calls a tool (e.g., `schedule_task`), and that tool later sends a Telegram notification back to the SAME user. If the notification path performs a blocking enqueue/acquire into the same actor without `TryAcquire`, the notification path can wait behind itself or create an unbounded queue.

**Why it happens:**
The notification delivery path (`SendToUser`, `sendGeneratedToUser`, or dispatched scheduler tasks) has no awareness of whether the caller already holds a per-user gate. The bot's `dispatchTask` sends reminders and wiki maintenance results directly to users -- the same users who may be mid-conversation. Go mutexes (`sync.Mutex`) are non-recursive by design.

**How to avoid:**
Use a channel-based gate (not `sync.Mutex`) that tracks held state, or implement a `TryAcquire` method that returns `false` (do not block) when already held:

```go
func (g *UserGate) TryAcquire(userID string) (release func(), acquired bool) {
    g.mu.Lock()
    if _, ok := g.inflight[userID]; ok {
        g.mu.Unlock()
        return func() {}, false  // already held, do not deadlock
    }
    done := make(chan struct{})
    g.inflight[userID] = done
    g.mu.Unlock()
    return func() {
        g.mu.Lock()
        delete(g.inflight, userID)
        close(done)
        g.mu.Unlock()
    }, true
}
```

Scheduler notifications and `request_dashboard_token` delivery must call `TryAcquire`. If `acquired == false`, the notification is a non-blocking send -- which works because Telegram message delivery is already independent of the conversation lock.

**Warning signs:**
- Goroutine profile shows goroutines parked at `[chan receive]` or `[sync.Mutex.Lock]` for minutes
- A user's bot stops responding but other users work fine
- `handleConversation` never reaches its `conversation complete` log line for a given userID

**Phase to address:**
Phase 1 (Concurrency + Qdrant Readiness): Implement `TryAcquire` from the start. Do not ship `Acquire` (blocking) without `TryAcquire` as the sole entry point for notification paths.

---

### Pitfall 2: Context Leak via Sync.Map Iteration During Concurrent Store Operations

**What goes wrong:**
The inactivity eviction ticker iterates `sessionStore.active` via `sync.Map.Range`, and concurrently, `handleConversation` calls `sessionStore.Begin` which does `active.Store(userID, true)`. If the Go runtime version has a bug with concurrent Range + Store (documented in golang/go#60216, fixed in later patch releases but still present in older environments), this causes a `fatal error: concurrent map iteration and map write` crash.

**Why it happens:**
Even though `sync.Map` is documented as safe for concurrent use, edge cases in Go versions prior to 1.24's HashTrieMap rewrite could trigger this crash under specific contention patterns. The eviction ticker calling `Range` while `Begin` calls `Store` creates exactly this pattern.

**How to avoid:**
Do not iterate `sync.Map` for eviction. Instead, maintain a separate `map[string]time.Time` protected by a `sync.RWMutex` for last-activity tracking. The eviction ticker takes a read lock of this auxiliary map, collects stale userIDs, then clears them from the `sync.Map` one at a time.

Even better: use a single `map[string]*userSessionState` with `sync.Mutex` where each value contains both the conversation context and a `lastActivity time.Time` field. This avoids maintaining two synchronized data structures.

**Warning signs:**
- Random `fatal error: concurrent map iteration and map write` crashes in production
- Crashes correlate with eviction ticker interval (e.g., crash every 5 minutes on the dot)
- Only reproduces at scale with many active sessions

**Phase to address:**
Phase 1 (Concurrency + Qdrant Readiness): Build eviction on a separate tracking structure from day one. Do not iterate `sync.Map` for cleanup.

---

### Pitfall 3: Circuit Breaker Lock Contention Blocking the Hot Path

**What goes wrong:**
The circuit breaker wraps `llm.Client` and checks state on every `Stream` and `Send` call. If the breaker holds a mutex during the actual LLM network call (not just state check), every concurrent user turn blocks on the LLM's latency regardless of circuit state. At 10+ concurrent users with a 3-second LLM response, all turns serialize behind a single mutex.

**Why it happens:**
A common implementation mistake holds the state lock while calling `inner.Send(ctx, req)`:

```go
// WRONG -- every concurrent LLM call serializes on mu
func (cb *CircuitBreaker) Send(ctx context.Context, req Request) (Response, error) {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    if cb.state == CircuitOpen { return Response{}, ErrCircuitOpen }
    resp, err := cb.inner.Send(ctx, req)  // 3-second network call while holding lock!
    cb.recordResult(err)
    return resp, err
}
```

**How to avoid:**
Lock only for state read/update. Release before the network call:

```go
func (cb *CircuitBreaker) Send(ctx context.Context, req Request) (Response, error) {
    cb.mu.Lock()
    rejected := cb.state == CircuitOpen || cb.state == CircuitHalfOpen
    cb.mu.Unlock()
    if rejected {
        return Response{}, ErrCircuitOpen
    }
    resp, err := cb.inner.Send(ctx, req)
    cb.mu.Lock()
    cb.recordResult(err)  // fast: a few integer operations
    cb.mu.Unlock()
    return resp, err
}
```

The half-open state requires limiting concurrent probes (max 1 or small N). Use an atomic counter for the probe count, not the state mutex.

**Warning signs:**
- Per-turn latency grows linearly with concurrent user count
- p99 latency spikes when >5 users active simultaneously
- Goroutine profile shows `[sync.Mutex.Lock]` with identical stack traces accumulating

**Phase to address:**
Phase 3 (Resilience Layer): Review the lock scope in code review. A test that launches 10 concurrent `Send` calls with a slow mock provider (1-second delay) should complete all 10 in ~1.1 seconds, not ~10 seconds.

---

### Pitfall 4: Async Reindex Worker Goroutine Leak on Shutdown

**What goes wrong:**
The reindex worker is a `for { select { case req := <-requests: ... } }` goroutine. If the worker is processing a slow embedding call (embedding API has a 30-second timeout) when the bot shuts down, and the shutdown path only closes the `requests` channel without cancelling the in-flight context, the worker goroutine blocks on the embedding call for the full timeout duration. Accumulating leaked goroutines across restarts leads to memory exhaustion.

**Why it happens:**
Channel closure alone does not cancel in-flight operations. The worker receives a closed-channel zero value and exits its select loop, but by then it is inside `i.embedFn(ctx, text)` with a `context.Background()` or an uncancelled context. The embedding call has its own HTTP timeout but the goroutine remains alive until that expires.

**How to avoid:**
Use a dedicated `context.Context` for the worker's lifetime, cancelled on shutdown:

```go
type ReindexWorker struct {
    ctx    context.Context
    cancel context.CancelFunc
    // ...
}

func (w *ReindexWorker) Stop() {
    w.cancel()  // cancels in-flight embedding calls
    // Don't close the channel -- let GC handle it after goroutines exit
}

func (w *ReindexWorker) Run() {
    defer close(w.done)
    for {
        select {
        case <-w.ctx.Done():
            return
        case req := <-w.requests:
            w.processWithContext(w.ctx, req)
        }
    }
}
```

Then pass `w.ctx` (not `context.Background()`) to every embedding call in `processWithContext`.

**Warning signs:**
- `runtime.NumGoroutine()` grows by 1 on every restart
- Goroutine profile shows goroutines parked on HTTP client `net.Dial` or `tls handshake` with stack trace ending in the embedding function
- After 10 restarts, 10 goroutines blocked on the same embedding URL

**Phase to address:**
Phase 2 (Core Hardening): Worker lifecycle must be part of the first async reindex implementation pass. Include a test that verifies `runtime.NumGoroutine()` returns to baseline after `Stop()`.

---

### Pitfall 5: Qdrant Warm Check False Positive -- Empty Collection, Full Embedding Cache

**What goes wrong:**
The startup warm check queries Qdrant's collection point count. If the count is zero, the code skips re-indexing, assuming vectors are present. But `CountPoints` returns zero for a collection that exists but has never been populated. The warm check "passes" (collection exists, count >= 0), but `search_memory` returns zero results for every query because there are no vectors. All semantic search silently fails.

**Why it happens:**
The check `CollectionHasVectors` conflates "collection exists" with "vectors are present." A collection created by a previous startup attempt that failed before upsert, or a collection that had all points deleted, passes the existence check but has no data.

**How to avoid:**
Explicitly check for a non-zero point count:

```go
func CollectionIsWarm(ctx context.Context, client *qdrantClient, collection string) (bool, error) {
    resp, err := httpGet(ctx, client.baseURL+"/collections/"+collection)
    if err != nil {
        return false, err
    }
    var info struct {
        Result struct {
            PointsCount uint64 `json:"points_count"`
        } `json:"result"`
    }
    json.Unmarshal(resp, &info)
    // Only consider "warm" if there are actually vectors
    return info.Result.PointsCount > 0, nil
}
```

The startup flow must be:
1. Wait for Qdrant to be healthy (retry `/readyz` with timeout)
2. Check if collection exists AND has points > 0
3. If NO: re-embed from wiki manifest
4. If YES: skip re-embed, log count, proceed

Do NOT skip step 2. "Collection exists" !== "Collection is warm."

**Warning signs:**
- `search_memory` returns empty results on every query after restart
- Compact memory sync logs "0 docs indexed" but the startup check logged "collection warm, skipping"
- Dashboard /health shows `compact_memory.docs_indexed: 0` after startup

**Phase to address:**
Phase 1 (Concurrency + Qdrant Readiness): The warm check must explicitly test for `points_count > 0`. A failing test: start with an empty collection, verify re-embed fires.

---

### Pitfall 6: Tool-Based Wiki Write + Concurrent Manual Write Race

**What goes wrong:**
The LLM calls `write_wiki_page` for slug "meeting-notes". Simultaneously, the user edits the same wiki page via the dashboard API (`api/conversations_write.go` -> `wiki.Store.WritePage`). The per-file mutex in `wiki.Store` serializes the disk writes (they use `fileMutex(slug)`), but the tool has no way to signal "page was modified externally since the LLM read it." The LLM writes its version, overwriting the user's manual edit, with NO merge and NO warning.

**Why it happens:**
The `wiki.Store.WritePage` method uses a per-file mutex (`sync.Map` loaded by slug) that prevents concurrent writes to the same file. But it does not implement an optimistic concurrency check (e.g., comparing `updated_at` before write). The serialization only prevents file corruption, not data loss from stale writes.

**How to avoid:**
The `write_wiki_page` tool should include an optional `expected_updated_at` parameter. When provided, `wiki.Store.WritePage` checks that the on-disk page's `updated_at` matches before writing:

```go
func (s *Store) WritePage(ctx context.Context, page *Page, expectedUpdatedAt ...string) error {
    // ...
    if len(expectedUpdatedAt) > 0 && expectedUpdatedAt[0] != "" {
        existing, err := s.ReadPage(slug)
        if err == nil && existing.UpdatedAt != expectedUpdatedAt[0] {
            return fmt.Errorf("page %s was modified since last read (expected %s, got %s)", slug, expectedUpdatedAt[0], existing.UpdatedAt)
        }
    }
    // ... proceed with write
}
```

The tool description should document this: "If you just read the page, include the `updated_at` from the read response to prevent overwriting changes."

**Warning signs:**
- User reports that manual wiki edits "disappeared" after using the bot
- Wiki page has LLM-authored content but user-added sections are gone
- Git log shows the tool write came after the manual write, with no merge commit between them

**Phase to address:**
Phase 2 (Modify Existing Components): Implement `expected_updated_at` in `WritePage` from the first version of the tool. This is a one-hour change that prevents data loss. Do not defer this to later phases.

---

### Pitfall 7: Variable-Temperature Retry Burned on Non-Schema Failures

**What goes wrong:**
The retry logic increments temperature and retries on ANY error (HTTP 429, rate limit, context timeout, model returns 500). For a rate limit error, a higher temperature retry produces a different output that will also hit the same rate limit. The user sees: retry-1 at 0.0 (rate limited), retry-2 at 0.1 (rate limited), retry-3 at 0.2 (rate limited). 3 failed calls, triple the cost, no progress.

**Why it happens:**
The retry strategy was designed for "model refused to produce structured output" (a content problem) but gets applied to ALL failures without distinguishing between "content problem" (temperature helps) and "infrastructure problem" (temperature is irrelevant).

**How to avoid:**
Classify errors before retrying:

```go
func shouldRetryWithTemp(err error) bool {
    // Schema validation failure, empty output, hallucinations -> yes, temperature helps
    if errors.As(err, &SchemaValidationError{}) { return true }
    if errors.As(err, &EmptyOutputError{}) { return true }
    if errors.As(err, &RefusedContentError{}) { return true }

    // Rate limits, timeouts, provider errors -> no, retry but don't burn temperature budget
    return false
}
```

For infrastructure errors, retry with the SAME temperature (or skip the LLM call entirely and wait for backoff). Only increment temperature for content-quality failures. The `TempRetryClient` wrapper should have two retry paths: `retrySameTemp` for transient errors, `retryWithIncrement` for content errors.

**Warning signs:**
- Token cost during incident periods is 3x normal cost per turn
- Rate limit HTTP 429 shows up in logs with `temp=0.1`, `temp=0.2`, `temp=0.3` successively
- Per-turn LLM call count metric shows 3+ calls even for simple queries during provider degradation

**Phase to address:**
Phase 2 (LLM Reliability & Tool Intelligence): Implement error classification in the retry wrapper. A test with a mock that returns HTTP 429 should show 3 retries at the SAME temperature, not incrementing.

---

### Pitfall 8: Per-User Token Budget Atomicity Gap

**What goes wrong:**
The per-user budget tracks tokens per userID. After each LLM call, `RecordUsage` is called from `agentloop.Options`. Two goroutines for the same user (different Telegram messages processed sequentially but budget check runs outside the mutex) both pass the `Check` budget threshold at nearly the same time. Both proceed. The user exceeds their budget because the check and the record are not atomic.

**Why it happens:**
The budget check in `handleConversation` happens before `userGate.Acquire`. By the time the mutex is acquired and the LLM call completes, another goroutine could have also passed the budget check. The `RecordUsage` in `agentloop.Options` records usage after the call, but the gap between `CanAfford` and `RecordUsage` allows overspend if two messages are in-flight for the same user.

**How to avoid:**
Move the budget check inside the UserGate actor boundary -- after the entry is accepted and before `runToolCallingLoop`. Then make `RecordUsage` use `atomic.AddInt64` on a per-user counter stored in a `sync.Map`. The check-read and atomic-add sequence is race-free because only one goroutine per userID can be in this section at a time (enforced by `UserGate`).

Alternatively: use `sync.Map` `LoadOrStore` to create a per-user `atomic.Int64` budget tracker, and have `RecordUsage` atomically add to it. The check `CanAfford` reads the atomic value. This works even without the actor boundary because atomic operations are individually race-free -- though you still need the UserGate boundary for the "two concurrent messages both check then both record" scenario.

**Warning signs:**
- Per-user actual token usage exceeds configured budget by 10-20% (not a large overshoot)
- Overspend only happens when user sends rapid-fire messages (two within 1 second)
- Budget tracker shows `used: 100500` when limit is `100000` -- exactly one extra LLM call's worth

**Phase to address:**
Phase 3 (Resilience Layer): Move budget check inside the UserGate-protected region. The budget tracker and UserGate must be co-designed so the gate is the budget enforcement point.

---

### Pitfall 9: Legacy Code Removal -- Missing Callers in Build-Tag-Gated Files

**What goes wrong:**
A function suspected as dead code is removed. Build passes. Tests pass. `grep` across all `.go` files shows no callers. But a week later, running with `-tags=integration` or on `GOOS=linux` fails because the function was called from a file gated behind `//go:build integration` or `//go:build linux`.

**Why it happens:**
`grep` on `.go` files finds all callers regardless of build tags, but the developer only checks files that compile under the default build configuration. The removal is verified with `go build ./...` which does NOT compile build-tag-gated files whose constraints don't match the current `GOOS`/`GOARCH`. The function is considered dead in the default build but live in the integration or linux build.

**Affected files in THIS codebase:**
- `internal/skill/sandbox_linux.go` (`//go:build linux`)
- `internal/tray/tray_windows.go` (`//go:build windows`)
- `internal/tray/tray_other.go` (`//go:build !windows`)
- `internal/skill/sandbox_other.go` (`//go:build !linux`)
- Any file using `//go:build integration` if present

**How to avoid:**
Verification checklist before removing ANY function:

```bash
# 1. Grep ALL .go files regardless of build tags
grep -rn "FunctionName" --include="*.go" internal/ cmd/

# 2. Build with ALL build tag combinations
go build -tags=integration ./...
GOOS=linux go build ./...
GOOS=windows go build ./...

# 3. Check for test-only callers (test-only = candidate for removal with test)
grep -rn "FunctionName" --include="*_test.go" internal/ cmd/

# 4. Check for string/reflection-based references (tool names in map literals, etc.)
grep -rn '"function_name"' --include="*.go" internal/ cmd/
```

**Warning signs:**
- Build passes locally but CI fails with "undefined: FunctionName" on a different platform
- Integration test suite breaks after a "dead code removal" commit
- The removal passed `go build` but the function name appears in a Go string literal (tool name registry, config defaults, etc.)

**Phase to address:**
Phase 4 (Cleanup): Every dead code removal PR must include a comment listing which build tag combinations were verified, and the output of `grep -rn` across ALL files.

---

### Pitfall 10: Qdrant Health Gate Blocking Startup During Qdrant Restart

**What goes wrong:**
The startup health gate (`WaitForQdrant`) blocks process startup until Qdrant is healthy. If Qdrant restarts due to a Docker policy (`--restart=always`) while Aura restarts simultaneously, Qdrant may take 40 seconds to load a large collection from disk. The health gate with a 30-second timeout fails startup, Aura exits, Docker restarts Aura, and the loop repeats. The service never comes back up.

**Why it happens:**
The startup health gate timeout (30s) is shorter than Qdrant's collection load time under high memory pressure or large datasets. The gate treats "Qdrant not ready in 30s" as a fatal startup error, which triggers a restart loop.

**How to avoid:**
Use a generous timeout (120 seconds minimum) and differentiate between "Qdrant is starting up" (keep waiting) and "Qdrant is unreachable" (fast failure):

```go
func WaitForQdrant(ctx context.Context, cfg QdrantConfig, logger *slog.Logger) error {
    deadline := time.Now().Add(2 * time.Minute)
    backoff := 500 * time.Millisecond
    for time.Now().Before(deadline) {
        if err := CheckQdrantReady(ctx, cfg); err == nil {
            logger.Info("qdrant ready")
            return nil
        }
        time.Sleep(backoff)
        backoff = min(backoff*2, 10*time.Second)
    }
    return fmt.Errorf("qdrant not ready after 2 minutes")
}
```

Also: make the health gate timeout configurable via environment variable (`QDRANT_STARTUP_TIMEOUT_SEC`). Document the minimum expected value based on collection size.

**Warning signs:**
- Aura log shows "qdrant not ready" followed by exit every 30 seconds
- Docker `docker ps` shows Aura and Qdrant both restarting in lockstep
- Qdrant logs show "loading collection" still in progress when Aura times out

**Phase to address:**
Phase 1 (Concurrency + Qdrant Readiness): Use 120-second timeout minimum. Configurable. Document the relationship between collection size and startup time.

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| `time.Sleep` in eviction ticker instead of `time.Ticker` with `Stop()` | Zero lines of lifecycle code | Goroutine leak on every restart because ticker is never stopped | Never -- use `time.NewTicker` + `defer t.Stop()` |
| Single `sync.Mutex` for all users instead of per-user gate | 10 lines of code vs. 80 | Head-of-line blocking: user A's 30-second code execution blocks user B's "hello" | Only if max 1 user (testing) |
| `defer close(ch)` in reindex worker without cancel context | Simpler worker loop | In-flight embedding call blocks shutdown for up to 30 seconds | Only if embedding timeout < 1 second |
| Hardcoding Qdrant collection name `aura_memory_v1_compact` in health check | Avoids one config field | Collection rename on Qdrant side breaks health check silently | Acceptable if collection name is in a single constant referenced everywhere |
| Removing legacy code without a "this was called from" comment in the commit | Saves 30 seconds per removal | Future developer cannot trace why the function existed or what it was replaced by | Never -- always include the grep output and replacement rationale in commit message |
| `panic` on circuit breaker state inconsistency instead of logging + reset | "Fails fast" mentality | Kills the entire bot for a recoverable state bug in one provider's breaker | Never -- log error, reset breaker state to Closed, continue serving |
| Storing budget as `int64` (tokens) without overflow check | Simpler arithmetic | At current models (200K context), 100 turns = 20M tokens. Fits in int64 easily but grows | Acceptable if documented max value is 2^63 tokens (impossible to reach) |

---

## Integration Gotchas

Common mistakes when connecting hardening components to existing infrastructure.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| `UserGate` -> `session.Begin` | UserGate acquires BEFORE Begin, but Begin panics and release never happens | `defer release()` immediately after `Acquire`. The `defer` must be on the line right after the nil-check for acquire error. |
| Circuit breaker -> provider client | Breaker wraps too broad a client boundary, so unrelated errors can trip provider health state | Each configured provider gets its OWN breaker keyed by provider name. |
| Variable-temp retry -> agentloop | Retry wrapper calls `client.Chat` multiple times but agentloop's `RecordUsage` hook fires once per loop iteration, missing intermediate retries | Retry wrapper calls `RecordUsage` for each retry call. The `ChatClient` implementation must accept a usage callback. |
| `write_wiki_page` tool -> `wiki.Store` | Tool directly imports `wiki` and calls `wikiStore.WritePage`, creating a hard dependency | Tool accepts `wiki.PageWriter` interface. The existing tool registry already uses this pattern (`wikiStore` is passed as interface). |
| Reindex worker -> embedding cache | Reindex sends embedding request, embedding cache has the same text cached, but cache key uses full content hash -- different from the search query hash | Embedding cache `keyFunc` must be deterministic for reindex content. Current `EmbedCacheNamespace` keying on `contentSHA` works because same content -> same SHA. Verify cache hit rate monitors reindex path. |
| Qdrant health -> existing `/readyz` probe | Startup health check uses `/readyz` which returns 200 even for Qdrant instances that are accepting connections but haven't finished loading collections | Use `GET /collections/{name}` instead. A 200 response means the collection is fully loaded. A 404 means the collection doesn't exist yet. Any other code means Qdrant is not ready for that collection. |
| Git commit tracking -> `wiki.Store` concurrent writes | Setting `unversioned: true` in frontmatter during a write that also triggers `gitCommit` causes two git operations on the same repo | git operations are serialized by `gitMu`. But the `unversioned` flag is set in the page frontmatter BEFORE the git commit, and if the git commit fails, the flag remains. Ensure the flag is only set AFTER git commit failure by re-reading the page. |

---

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Eviction ticker scanning all sessions on every tick | CPU spike every 5 minutes, linearly growing with active sessions | Track `lastActivity` per session; only scan entries with stale timestamps using a priority queue or sorted map | At ~500 active sessions |
| Circuit breaker mutex contention under load | p99 LLM latency grows with concurrent user count | Lock only state struct (nanoseconds), never during I/O | At 5+ concurrent LLM calls |
| Reindex worker serializing one-at-a-time embeddings | Reindex queue backing up after bulk wiki imports | Batch embeddings using `batchEmbedFn`. The batch function is already available in `CompactMemoryQdrantIndexWithBatch` | At 20+ wiki pages written in rapid succession |
| Per-user budget map growing without bound | Memory leak: every new userID stored in budget map forever | Evict users with zero budget used and >30 days since last activity. Use `weak.Pointer` (Go 1.24+) or manual cleanup ticker | At 10K+ distinct users over months |
| Channel-based UserGate with unbuffered channel for waiting goroutines | Multiple goroutines waiting on same user's completion channel; GC pressure from goroutine stacks (each ~2KB) | The per-user gate should enqueue at most ONE waiting goroutine. Send "busy" response to others. | At 10+ concurrent messages from the same user (unlikely but possible with bot commands) |

---

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| UserGate timeout exposes "user is busy" message with timing information | Attacker can probe which users are active based on response timing | Use constant-time busy response. Always reply with the same message regardless of whether user exists. |
| Per-user token budget can be probed by sending messages to detect budget exhaustion | Attacker can infer budget limits by testing query sizes | Budget exhaustion response is identical to "processing" response. Never reveal remaining budget or threshold. |
| `write_wiki_page` tool accepts arbitrary Markdown body | LLM could be prompt-injected to write malicious content to wiki pages | Wiki body is rendered as Markdown in dashboard. Sanitize HTML in rendered output (dashboard already handles this). The git history provides audit trail for any LLM-authored content. |
| Circuit breaker state is in-memory only | If the process restarts, circuit breaker state is lost and failures start fresh | Acceptable for this scale. If a provider is down, the breaker will re-trip within the configured failure threshold. No need for persistent breaker state. |

---

## UX Pitfalls

Common user experience mistakes in this domain.

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| "Budget limit reached" message after a single word query ("hello") | User thinks the bot is broken or their budget was wasted by someone else | Distinguish between "your per-user budget is exhausted" and "global hard budget is exhausted." Per-user budget should reset daily/weekly. Show reset time. |
| Circuit breaker "service unavailable" without context | User has no idea if this is temporary or permanent | "I'm having trouble reaching my language model right now. This usually resolves in a minute. You can try again or I'll be back shortly." |
| Wiki page created by `write_wiki_page` tool is not immediately searchable | User asks about something, bot writes a wiki page, user asks again, bot says "no results" | The tool result should note: "This page will be searchable in a few seconds (it's being indexed now)." |
| `unversioned` flag on wiki page shows as "not tracked" in dashboard | User worries their pages are lost | `unversioned` only means "not in git." Pages are on disk. Show "saved, git tracking pending" instead of "not tracked." The distinction matters. |

---

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **UserGate:** Often missing timeout/rejection path -- verify `TryAcquire` returns immediately when busy (no indefinite blocking).
- [ ] **Circuit Breaker:** Often missing per-provider isolation -- verify breaker tracks state per provider name, not globally.
- [ ] **Reindex Worker:** Often missing `Stop()` method or graceful shutdown -- verify `runtime.NumGoroutine()` returns to baseline after stop.
- [ ] **Qdrant Warm Check:** Often missing `points_count > 0` check -- verify a zero-point collection triggers re-embed.
- [ ] **Variable-Temp Retry:** Often missing error classification -- verify HTTP 429 does NOT increment temperature.
- [ ] **Per-User Budget:** Often missing atomic check-then-record -- verify rapid consecutive messages cannot overspend.
- [ ] **Tool-Based Wiki Write:** Often missing `expected_updated_at` -- verify concurrent manual write is detected and rejected.
- [ ] **Legacy Code Removal:** Often missing build-tag-gated file verification -- verify `GOOS=linux go build` and `-tags=integration` pass after removal.
- [ ] **Git Commit Tracking:** Often missing cleanup on commit failure -- verify `unversioned` flag is removed if commit retry succeeds.
- [ ] **Inactivity Eviction:** Often missing context cancellation on stored contexts -- verify evicted session's context is cancelled.

---

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| UserGate actor boundary deadlock (Pitfall 1) | LOW | Restart bot. The deadlocked goroutine is cleaned up by process exit. Implement `TryAcquire` before next deploy. |
| sync.Map iteration crash (Pitfall 2) | MEDIUM | Restart bot. Upgrade Go to 1.24+ which uses HashTrieMap backend. If on older Go, switch to auxiliary tracking map. |
| Circuit breaker lock contention (Pitfall 3) | LOW | Deploy fix (lock scope reduction). No data corruption. Latency returns to normal on restart. |
| Reindex worker goroutine leak (Pitfall 4) | LOW | Restart clears leaked goroutines. Fix lifecycle in next deploy. Monitor goroutine count to detect recurrence. |
| Qdrant warm check false positive (Pitfall 5) | MEDIUM | Trigger manual re-index via maintenance API (`/api/maintenance/reindex`). Verify points count in health dashboard. |
| Wiki write race with manual edit (Pitfall 6) | HIGH | Recover content from git history (`git checkout HEAD~1 -- meeting-notes.md`). No automatic recovery -- data is lost if not detected. |
| Temperature retry on infra errors (Pitfall 7) | LOW | Deploy fix. The cost impact (extra 2-3 LLM calls per turn) is bounded and temporary. |
| Per-user budget atomicity gap (Pitfall 8) | LOW | Budget overshoot is bounded (~1 extra LLM call per gap). Reset budgets at next billing period. Fix in next deploy. |
| Missing build-tag callers (Pitfall 9) | MEDIUM | Revert the dead code removal commit. Restore the function. Run verify script with all build tags. |
| Qdrant startup timeout loop (Pitfall 10) | LOW | Restart Qdrant first, let it finish loading, then start Aura. Increase timeout config. |

---

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Pitfall 1: UserGate deadlock | Phase 1 -- implement `TryAcquire` from day one | Test: notification to same user mid-conversation does not deadlock |
| Pitfall 2: sync.Map iteration crash | Phase 1 -- separate tracking structure for eviction | Test: no crash under `go test -race -count=100` with concurrent Begin + Range |
| Pitfall 3: Circuit breaker lock contention | Phase 3 -- code review lock scope | Test: 10 concurrent Send calls complete in ~1.1s with 1s mock provider |
| Pitfall 4: Reindex worker goroutine leak | Phase 2 -- worker lifecycle with context cancellation | Test: `runtime.NumGoroutine()` returns to baseline after Stop() |
| Pitfall 5: Qdrant warm check false positive | Phase 1 -- explicit `points_count > 0` check | Test: empty collection triggers re-embed, populated collection skips |
| Pitfall 6: Wiki write race with manual edits | Phase 2 -- `expected_updated_at` in WriteWikPage tool | Test: concurrent dashboard write + tool write detects change |
| Pitfall 7: Temperature retry on infra errors | Phase 2 -- error classification in retry wrapper | Test: HTTP 429 does not increment temperature |
| Pitfall 8: Per-user budget atomicity gap | Phase 3 -- budget check inside UserGate actor boundary | Test: two rapid messages cannot both pass budget check |
| Pitfall 9: Missing build-tag callers | Phase 4 -- verification script before each removal | Process: every removal PR includes grep output for all build tags |
| Pitfall 10: Qdrant startup timeout loop | Phase 1 -- 120s timeout, configurable | Test: simulated slow Qdrant load does not cause restart loop |

---

## Sources

- [Go sync.Map LoadOrStore race conditions discussion](https://stackoverflow.com/questions/78601357/is-sync-map-loadorstore-subject-to-race-conditions) -- HIGH confidence: official docs and community confirmation
- [Go sync.Map concurrent iteration crash issue](https://github.com/golang/go/issues/60216) -- HIGH confidence: official Go issue tracker
- [Go circuit breaker pattern implementation mistakes](https://alamrafiul.com/posts/go-circuit-breaker/) -- MEDIUM confidence: community best practices
- [Go goroutine leak patterns and Go 1.26 leak profile](https://dev.to/gabrielanhaia/goroutine-leaks-in-go-the-4-patterns-and-the-new-profile-in-go-126-5e73) -- HIGH confidence: references official Go proposal
- [Go channel backpressure and goroutine leak prevention](https://unixy.io/blog/go-channels-are-not-queues/) -- MEDIUM confidence: community engineering practices
- [Qdrant Go client v1.17.1 API reference](https://pkg.go.dev/github.com/qdrant/go-client) -- HIGH confidence: official package documentation
- [Qdrant health check endpoint availability issue](https://github.com/qdrant/qdrant/issues/2672) -- HIGH confidence: official Qdrant issue tracker
- [Structured output from LLMs in Go -- retry patterns](https://lawzava.com/blog/2024-04-29-structured-output-patterns/) -- MEDIUM confidence: practitioner guide with concrete Go patterns
- [GitHub dead code removal verification checklist](https://github.com/github/gh-aw/blob/main/DEADCODE.md) -- MEDIUM confidence: GitHub's own internal process documentation
- [Go deadcode analyzer tool documentation](https://go.dev/blog/deadcode) -- HIGH confidence: official Go blog
- Codebase analysis of internal/telegram/conversation.go, internal/agentloop/loop.go, internal/agentruntime/session.go, internal/llm/retry.go, internal/search/qdrant.go, internal/search/compact_qdrant.go, internal/wiki/store.go -- HIGH confidence: direct code inspection
- [LLM tool calling JSON validation retry strategies](https://medium.com/@hariomshahu101/building-production-ready-llm-applications-bulletproof-llm-tool-calling-with-advanced-json-b95ce8889f4e) -- MEDIUM confidence: practitioner guide

---
*Pitfalls research for: Aura v4.0 Production Hardening integration*
*Researched: 2026-05-10*
