# Feature Research: Aura v4.0 Production Hardening

**Domain:** Production hardening a Go Telegram assistant with Qdrant vector store
**Researched:** 2026-05-10
**Confidence:** HIGH

## Feature Landscape

The 10 hardening areas fall naturally into four categories: concurrency correctness, LLM resilience, resource governance, and codebase hygiene. Each is analyzed below as a table-stakes feature, differentiator, or anti-feature with implementation complexity and dependency notes.

---

### 1. Per-User Message Serialization (UserGate Actor/Inbox)

**Category:** Table Stakes
**Complexity:** MEDIUM
**Existing foundation:** Aura uses `internal/agentruntime` as the agent loop hub and `internal/telegram` as a thin adapter. No per-user serialization exists currently.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| One in-flight agent turn per user at a time | Users expect sequential responses; concurrent messages from same user cause agent state corruption | Per-user UserGate actor with bounded inbox |
| Out-of-order send prevention | Telegram delivers multiple messages rapidly; concurrent sends to the same chat can arrive out of order | Same per-user actor serializes conversation and notification entries |
| Non-blocking across users | One user's long-running agent loop must not block another user | One actor per active user, not a global mutex |

#### Pattern: Chat-Isolated Actor Model (Canonical)

The Phase 1 checkpoint selects a per-user stateful goroutine (actor pattern). Messages from the same user are naturally serialized by sequential execution inside that user's actor. Aura should layer this at the Telegram boundary:

```text
agentruntime receives message {chatID, userID, content}
  -> UserGate finds or spawns user actor
  -> enqueue conversation entry in the user's bounded inbox
  -> actor runs each entry to completion in FIFO order
  -> actor is stopped by inactivity eviction
```

The bounded inbox defaults to 8 entries. When full, the actor drops the oldest queued entry and sends a Telegram notice. Notification paths use `TryAcquire` so a scheduler reminder or task dispatch never blocks forever behind the same user's active turn.

#### Anti-Feature: Global Mutex

A single `sync.Mutex` wrapping all agent loop execution. This serializes _all_ users, not just the requesting user. Under moderate load (3+ concurrent users), latency becomes linear in the number of users. Never use this.

#### Dependencies

- Requires the existing `agentruntime` entry point to be invoked from the per-user actor boundary
- Interacts with feature #2 (context leak cleanup): eviction must stop the idle actor before clearing its session state
- Interacts with feature #7 (token budget): per-user budget counters need the same user-scoped gate boundary

---

### 2. Context Leak Cleanup (Inactivity Eviction)

**Category:** Table Stakes
**Complexity:** MEDIUM
**Existing foundation:** `internal/conversation/context.go` manages session state and `internal/conversation/archive.go` handles archival. No inactivity-based eviction exists.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| Inactivity timeout with automatic cleanup | Long-running bots accumulate dead sessions that leak memory and degrade performance | Configurable TTL (default 30-60 min) with wall-clock timer per session |
| Session handoff/reset on timeout | User returning after timeout should get a clean session, not stale state | Clear context, optional summary save, reset session counter |
| Differentiated timer sources | Only user messages reset the inactivity timer; bot messages, system events, and background jobs do not | Track `lastActivityAt` per session, only update on incoming user messages |
| Garbage collection of ephemeral outputs | Shell results, search outputs, and tool results with no stable identity should not persist across session boundaries | Silent removal or paging-out with retrieval handles |

#### Pattern: Two-Stage Monitoring (Best Practice)

Research from Zylos and the Pichay paper establishes a graduated pressure approach:

| Stage | Threshold | Action |
|-------|-----------|--------|
| **Observation** | <60% of inactivity TTL | Normal operation |
| **Early Warning** | ~64% of TTL (e.g., 20 min of 30 min TTL) | Trigger memory sync to durable storage |
| **Session Eviction** | 100% of TTL | Clear agent context, stop UserGate actor/inbox, optionally save summary |

The gap between early warning and eviction prevents race conditions where a session is being synced to disk while simultaneously being evicted.

#### Anti-Feature: Aggressive Eviction Without Handoff

Immediately deleting all session state without saving a summary. Users returning after a short break lose all conversational context. Instead, save a structured summary (intent, decisions, actions taken, next steps) to durable storage before eviction, and surface it when the user returns with a new session.

#### Anti-Feature: Inactivity = Last Message Time

Using the last _any_ message (including bot responses, tool outputs) as the activity timestamp. Bots that generate long responses or run multi-step tool chains will evict themselves. Only user-initiated messages should reset the timer.

#### Dependencies

- Requires feature #1 (UserGate actor/inbox): eviction must stop the idle actor before clearing state
- Enhances feature #7 (per-user budget): eviction should release budget tracking resources
- No dependency on wiki or search subsystems

---

### 3. Tool-Based Wiki Page Creation (Replacing Text Heuristics)

**Category:** Table Stakes (correctness fix) / Differentiator (quality)
**Complexity:** HIGH
**Existing foundation:** The `looksLikeWikiYAML` heuristic was searched for and NOT found (already removed or named differently). Wiki pages exist in `internal/wiki/store.go` with schema and graph support. Pages are created through some path that this feature replaces.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| Structured output via tool definition with JSON Schema | LLM must produce machine-parseable wiki pages with guaranteed field presence | Define `create_wiki_page` tool with required fields: `title`, `body`, `slug`, `category`, `tags` |
| Validation before persistence | Malformed wiki pages corrupt the knowledge base and break search | Schema validation gate before write; reject and retry with feedback on failure |
| Idempotent upsert | Same tool call repeated (retry, replay) must not create duplicates | Upsert by slug with content hash comparison; skip write if unchanged |

#### Pattern: Tool-Based Structured Output with Feedback Loop

The dominant pattern from Karpathy's LLM Wiki and `llm-wiki-compiler` uses tool definitions with constrained decoding:

```
LLM decides to create a wiki page
  -> calls create_wiki_page(title, slug, body, category, tags)
  -> tool handler validates schema (required fields, slug format, cross-link integrity)
  -> if valid: upsert to wiki store, add to Qdrant index job queue
  -> if invalid: return structured error, LLM retries with error feedback
```

Key design decisions:
1. **Use enums for `category`** (concept, entity, person, event, project, tool) -- far more reliable than free-form strings
2. **Make `body` markdown with `[[slug]]` links** -- continues existing wiki pattern
3. **Include `description` fields in the tool schema** -- models read these as inline instructions during tool selection
4. **Prefer flat typed arrays** over deeply nested structures

#### Why Tool-Based Beats Heuristics

The RLM-on-KG paper (Volpini & Raad) provides empirical evidence: LLM-driven adaptive decisions outperform rule-based heuristics by +2.47 to +4.37 pp F1 score when evidence is scattered and the model has strong tool-calling capability. For Aura's use case (synthesizing wiki pages from extracted source evidence), evidence is inherently scattered across documents, making tool-based creation the correct approach.

#### Anti-Feature: Free-Text YAML/JSON Heuristics

Parsing structured data from free-text LLM output via regex or markdown code fences. Brittle; common failure modes include fence-wrapping, missing braces, hallucinated fields, and YAML indentation errors. Tool-based structured output with provider-level constrained decoding achieves ~99%+ conformance for frontier models versus ~70-80% for heuristic parsing. Free-form text parsing of structured data is a "production liability."

#### Dependencies

- Requires feature #8 (Qdrant health): new wiki pages must be pushed to Qdrant
- Enhances feature #5 (async wiki reindex): tool handler enqueues reindex jobs rather than blocking on Qdrant upsert
- No dependency on features #1, #2, #4, #6, #7, #9, #10

---

### 4. Variable-Temperature LLM Retry

**Category:** Differentiator (when done correctly) / Anti-Feature (when done naively)
**Complexity:** MEDIUM
**Existing foundation:** `internal/llm/retry.go` implements a `RetryClient` with fixed exponential backoff (5 retries, 1s base, 30s max). No error classification, no temperature variation, no input modification on retry.

#### The Nuance

Variable temperature retry is NOT a universal improvement. The research is clear:

- For **transient failures** (HTTP 429, 5xx, network timeout): temperature is irrelevant -- the call never reached the model. Fixed-temperature retry with exponential backoff is correct.
- For **semantic/structural failures** (malformed JSON, schema mismatch, refusal): varying temperature alone produces correlated failures. The model will likely fail again because the _prompt_ is the problem, not the sampling. Empirically, retry-resolves-it rates are around 20% for structural failures.
- For **uncertainty quantification** (Monte Carlo Temperature, Cecere et al. 2025): sampling across a temperature distribution (0.1 to 1.0) achieves statistical parity with oracle temperatures. But this is for multi-sample generation, not retry.

#### Recommended Pattern: Classify-Then-Retry with Staged Temperature

The approach Aura should implement is **error-classified staged retry**:

| Stage | Temperature | Error Class | What Changes |
|-------|-------------|-------------|--------------|
| Attempt 1 | 0.0 (deterministic) | N/A (first attempt) | Nothing |
| Attempt 2 | 0.0 | Transient (429, 5xx, timeout) | Nothing (same prompt, just retry) |
| Attempt 3 | 0.3 + error feedback | Structural (malformed output, schema fail) | Feed validation error back into prompt + slightly warmer sampling |
| Attempt 4 | 0.5 + truncated context | Content filter, context overflow | Truncate oversized context, warmer sampling |
| Attempt 5 | 0.7 | Repeated structural failure | Warmer sampling as last resort before surfacing a structured failure |

The critical insight: temperature variation is coupled with **actually changing the input** (feeding back validation errors, truncating context). The input change is what gives the model a genuine reason to produce different output; temperature variation amplifies diversity only when the input has changed.

#### Anti-Feature: Blind Temperature Jittering

Retrying with `temperature = random(0.0, 1.0)` without error classification or input modification. This converts deterministic failure into a probabilistic bill. A retry that does not change the input has no entitlement to a different outcome. As Tian Pan notes: "treating temperature noise as a retry strategy is mostly a way to convert determinism into a bill."

#### Anti-Feature: Always 0-Temperature Retry

The existing pattern (fixed temperature, no input modification). For transient errors this is correct. For semantic errors it burns tokens retrying identical requests that will produce identical failures. The fix is not to always vary temperature but to classify errors before retrying.

#### Dependencies

- Requires feature #6 (circuit breaker): retries that trip the circuit breaker should stop early
- Interacts with feature #7 (token budget): retries consume budget; the budget gate should account for retry multiplier
- No dependency on features #1, #2, #3, #5, #8, #9, #10

---

### 5. Async Wiki Reindex with Backpressure

**Category:** Table Stakes (performance) / Differentiator (non-blocking UX)
**Complexity:** HIGH
**Existing foundation:** `internal/search/qdrant.go` has a `RebuildQdrantWikiDocuments` function. `internal/search/embed_cache.go` wraps chromem-go embedding with SQLite caching. These are part of the legacy path being replaced.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| Non-blocking wiki writes | Wiki page creation (#3) or edits should not block the user waiting for Qdrant reindex | Write to wiki store synchronously, enqueue reindex job asynchronously |
| Backpressure when queue is full | An unbounded queue under heavy load is a memory leak; a blocking queue deadlocks the agent loop | Buffered channel + `select/default` non-blocking send with graceful rejection |
| Configurable worker pool | Concurrency must be tunable for different hardware and Qdrant configurations | N workers (default 2) consuming from shared job channel |
| Incremental reindex (not full scan) | Reindexing the entire wiki on every page change does not scale past ~50 pages | Reindex only changed/added pages; use content hash to skip unchanged |
| Graceful shutdown | Worker pool must drain on process shutdown without losing jobs | `stopCh` channel + `sync.WaitGroup` |

#### Pattern: Buffered Channel Backpressure (vllm-project Model)

The vllm-project's `semantic-router` implements the canonical Go pattern for embedding ingestion with backpressure:

```
Wiki page created/updated
  -> write to wiki store (synchronous, ~1ms)
  -> enqueue reindex job to buffered channel
     -> if channel has capacity: job accepted, user gets immediate response
     -> if channel full: reject with "queue_full" status, log, user still gets response (wiki write succeeded)
  -> worker goroutine: read job, chunk text, embed, upsert to Qdrant
```

Job structure:
```go
type ReindexJob struct {
    Slug      string
    Body      string    // wiki page body text
    Priority  int       // 0 = live user edit, 1 = bulk/background
    CreatedAt time.Time
}
```

Key parameters:
- Queue capacity: 100 (tunable)
- Worker count: 2 (default, tunable)
- Embedding batch size: collect up to N jobs and embed as single batch for GPU efficiency if the embedding API supports batching

#### Why Backpressure Matters

Without backpressure, a bulk import of 500 wiki pages would either:
1. **Unbounded queue:** consume gigabytes of RAM holding 500 pending embedding payloads
2. **Blocking queue:** deadlock the agent loop waiting for Qdrant to catch up
3. **No queue (synchronous reindex):** 500 sequential Qdrant upserts, each ~200ms = 100 seconds of blocked UI

With backpressure: jobs flow through the pipe at the rate Qdrant can accept them. If the pipe fills up, new jobs are rejected (wiki write already succeeded). The system degrades gracefully under load.

#### Anti-Feature: Synchronous Reindex

Performing Qdrant upsert inside the wiki write handler. Every page creation blocks the user's agent loop for 100-500ms (embedding API call + Qdrant upsert). Under bulk import this compounds to minutes of blocked response. The write must succeed synchronously; the reindex must happen asynchronously.

#### Anti-Feature: Full Reindex on Every Change

Calling `RebuildQdrantWikiDocuments` (which scans the entire wiki directory) every time a single page changes. For 500 pages, this means 500 unnecessary embedding calls and Qdrant upserts for every single-page edit. Use incremental reindexing by slug.

#### Dependencies

- Triggered by feature #3 (tool-based wiki creation): tool handler enqueues the job
- Requires feature #8 (Qdrant health): worker validates Qdrant connection before processing jobs
- Interacts with feature #10 (legacy removal): replaces chromem-go based reindexing with Qdrant-native reindexing
- No dependency on features #1, #2, #4, #6, #7, #9

---

### 6. Circuit Breaker on LLM Provider Health

**Category:** Table Stakes (cost protection) / Differentiator (provider health protection)
**Complexity:** MEDIUM
**Existing foundation:** The LLM package provides provider clients and retry wrappers. No circuit breaker exists.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| Per-provider circuit breaker | A failing provider must be quickly isolated so requests fail fast instead of burning latency/cost | One circuit breaker per configured provider |
| Three-state model (Closed/Open/Half-Open) | Industry standard from Michael Nygard's Release It! | Circuit starts Closed; opens after N consecutive failures; transitions to Half-Open after timeout; closes after M consecutive successes |
| Differentiated failure counting | Not all errors indicate provider failure | Count 5xx and 429 as failures; count `context.Canceled` as non-failure; count 4xx (except 429) as non-failure (bad request, not provider fault) |

#### Pattern: Sony gobreaker Provider Health Gate

The provider-health gate keeps circuit breaker state outside the core agent loop:

```
Request arrives at provider client
  -> Check circuit state for primary provider
     -> Closed: Send to primary
     -> Open: Return provider-health error immediately
  -> If primary returns transient failure (5xx/429/timeout):
      -> Record failure for that provider
      -> Retry only according to the retry policy and budget
      -> If threshold is hit: open circuit
```

Configuration:
```go
breaker := gobreaker.NewTwoStepCircuitBreaker[struct{}](gobreaker.Settings{
    Name:        "openai",
    MaxRequests: 3,                // half-open probe count
    Timeout:     30 * time.Second, // how long circuit stays open
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        return counts.ConsecutiveFailures >= 5
    },
    IsSuccessful: func(err error) bool {
        return err == nil || errors.Is(err, context.Canceled)
    },
})
```

State transitions must be logged at WARN level (Closed->Open) and INFO level (HalfOpen->Closed) with metrics for circuit state and provider failure counts.

#### Anti-Feature: Global Circuit Breaker

A single circuit breaker for all providers. When it trips, all LLM calls fail even if another provider is later configured. Circuit breakers must be per provider.

#### Anti-Feature: Treating All Errors as Failures

Counting every non-nil error as a trip-worthy failure. `context.Canceled` (user cancelled), `context.DeadlineExceeded` (user-set timeout), and 400-level errors (bad prompt) are not provider failures and should not count toward the breaker threshold.

#### Dependencies

- Requires existing provider client wiring
- Enhances feature #4 (variable temperature retry): if breaker opens, stop retrying immediately rather than exhausting retry budget
- Interacts with feature #7 (per-user budget): breaker events should be visible in budget/usage reporting
- No dependency on features #1, #2, #3, #5, #8, #9, #10

---

### 7. Per-User Token Budget with Global Hard Cap

**Category:** Table Stakes (single-user bot) / Differentiator (multi-user)
**Complexity:** HIGH
**Existing foundation:** `internal/budget/budget.go` has a `Tracker` with global soft/hard budget, cost tracking, and pre-flight `CanAfford` checks. No per-user dimension. No time-windowed limits.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| Per-user token tracking | Without per-user budgets, one heavy user (or a user running an infinite retry loop) can consume the entire global budget, starving all other users | Per-user counters (tokens consumed, cost) keyed by user ID |
| Per-user configurable limits | Different users may have different quotas (admin vs. regular, free vs. premium) | Per-user limits configurable per user ID or tier |
| Global hard cap as circuit breaker | A runaway agent loop or prompt injection attack can burn through budget in minutes | Global hard cap that rejects ALL requests when hit, regardless of individual remaining quota |
| Time-windowed budgets | Daily limits prevent a user from burning a month's quota in one session | Sliding window counters per user (hourly, daily, monthly) alongside cumulative totals |
| Graceful degradation on near-limit | Hard-rejecting users at exactly 100% of budget creates poor UX | Model downgrade (Claude Opus -> Haiku), output clamping, user warning at 80% threshold |

#### Pattern: Multi-Level Token Budgeting

The industry standard (Azure API Management, Kong AI Gateway, Cloudflare AI Gateway) uses four levels:

```
Level 1: Per-Request (max input + output tokens per single call)
Level 2: Per-User   (tokens per minute/hour/day/month per user)
Level 3: Per-Group  (optional, per tenant/team if multi-tenant)
Level 4: Global Hard Cap (total system budget, emergency shutoff)
```

For Aura (single-bot, multi-user), levels 1, 2, and 4 are needed.

The existing `budget.Tracker` handles level 4 (global). It needs to be extended with:

```go
type UserBudget struct {
    UserID           string
    TokensPerMinute  int
    TokensPerHour    int
    TokensPerDay     int
    TokensPerMonth   int
    CostPerDay       float64
    // Running counters (reset with windows)
    MinuteTokens     int
    HourTokens       int
    DayTokens        int
    MonthTokens      int
    CumulativeCost   float64
}
```

Pre-flight check:
1. Estimate input tokens (from message length)
2. Check per-user minute/hour/day limits (reject if exceeded with 429-level response to user)
3. Check global hard cap (reject all if exceeded)
4. Pre-deduct from counters (reconcile with actual usage post-response)

#### Anti-Feature: Request-Count Rate Limiting

Limiting based on number of requests rather than tokens consumed. A single 100K-token context injection costs the same as a 50-token "hello" under request-count limiting. Token-aware budgeting is mandatory for LLM cost control.

#### Anti-Feature: Post-Deduct Only

Only counting tokens after the LLM response returns. Under concurrent requests, users can burst past limits before any response has been counted. Pre-deduct estimated input tokens, reconcile with actual usage post-response. Failed calls should be credited back.

#### Dependencies

- Requires the existing `budget.Tracker` infrastructure (global budget already exists)
- Must be integrated into the agentruntime LLM call pipeline (pre-flight check before every LLM call)
- Interacts with feature #1 (UserGate actor/inbox): budget counters for a user should be updated inside that user's actor boundary
- Interacts with feature #2 (context eviction): evicted sessions should release budget tracking state
- Interacts with feature #4 (retry): retries consume budget; pre-flight check must account for retry multiplier
- Interacts with feature #6 (circuit breaker): breaker events may indicate budget/abuse patterns

---

### 8. Qdrant Startup Health Validation + Warm Cache Check

**Category:** Table Stakes (reliability)
**Complexity:** LOW
**Existing foundation:** `internal/search/qdrant.go` has Qdrant client integration. `internal/health/` exists. No startup health validation for Qdrant exists.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| Startup health probe before accepting traffic | If Qdrant is unreachable at startup, the bot should not start serving requests that will silently fail search/reindex | Block on Qdrant health check during bootstrap; log WARN with retry count until healthy |
| Connection liveness check | Qdrant can become unreachable during runtime (network partition, OOM restart) | Periodic health probe (every 30s) with alerting; fail-open on search (return "search unavailable") but fail-closed on reindex (no point upserting to dead store) |
| Collection existence validation | Missing collection (`aura_memory_v1_compact`) means all vector search fails silently | Verify collection exists and has expected vector dimension (768) at startup |
| Skip re-embed if vectors persist | On container restart with persistent Docker volume, Qdrant retains all vectors; re-embedding the entire wiki is wasteful and slow | Check collection point count against wiki page count; if counts match and last-modified timestamps are newer than last reindex, skip full reindex |
| Warm page cache readiness | Qdrant with `on_disk: true` loads vectors via mmap; the page cache warms in background | Probe a known query at startup to ensure Qdrant is responsive (not just TCP-healthy); log warm-up latency |

#### Pattern: Bootstrap Health Gate

```
Aura process startup
  1. Start Qdrant client connection
  2. Health check loop: GET /readyz on port 6333 (REST)
     - Retry every 2s up to 30 attempts (60s)
     - Log each attempt with attempt number and error
     - If timeout: log FATAL, exit (can't operate without Qdrant)
  3. Collection validation:
     - GET /collections/aura_memory_v1_compact
     - Verify vector size = 768, distance = Cosine
     - Log point count, segments, disk usage
  4. Warm cache check:
     - Run a probe search ("health check query") and measure latency
     - If latency > 500ms: log WARN "Qdrant page cache still warming"
     - If point count > 0 and point count ~= expected: skip full reindex
  5. Register health endpoint for orchestrator (Docker healthcheck)
```

#### Anti-Feature: Silent Fail-Open on Qdrant Unavailable

Starting the bot and serving user requests when Qdrant is unreachable. Every search returns empty results; every wiki reindex job fails silently. Users perceive the bot as "broken" with no clear error. The bot should either refuse to start (FATAL at bootstrap) or return explicit "search unavailable" messages to users.

#### Anti-Feature: Re-Embed on Every Restart

Assuming vectors are lost on restart and re-embedding the entire wiki. With a Docker volume mount, Qdrant persists all data. Re-embedding a 500-page wiki on every restart is minutes of unnecessary embedding API calls and cost. Check vector counts before reindexing.

#### Dependencies

- Uses existing `internal/health/` package structure
- Must run before features #3, #5, and any search queries can function
- No dependency on features #1, #2, #4, #6, #7, #9, #10

---

### 9. Explicit Git Commit Tracking with Unversioned Flag on Failures

**Category:** Differentiator (audit trail)
**Complexity:** LOW
**Existing foundation:** The project is a git repository. No automated commit tracking exists for wiki changes.

#### Table-Stakes Behaviors

| Behavior | Why Expected | Implementation |
|----------|-------------|----------------|
| Commit on successful wiki mutation | Every wiki page creation, update, or deletion changes durable knowledge; without versioning there is no undo | `git add <slug>.md && git commit -m "wiki: create <slug> by <source>"` after successful write |
| Descriptive commit messages | Commit history is the audit trail; "update" tells nothing | Include: action (create/update/delete), slug, source (user, tool, import), and optionally a summary line |
| Unversioned flag on write failure | If a wiki write succeeds but git commit fails (disk full, permission denied, concurrent push conflict), the system must know the page is unversioned | Set `unversioned: true` flag in page metadata; surface in dashboard audit view; retry commit on next wiki write |

#### Pattern: Atomic Write-Commit (BSWEN Model)

```
Wiki write (create/update/delete)
  -> Write page to disk (wiki store)
  -> git add <path>
  -> git commit -m "wiki: <action> <slug> [source: <source>]"
  -> If commit succeeds: clear unversioned flag, log INFO
  -> If commit fails: set unversioned flag on page metadata, log ERROR
     -> Next successful commit for ANY page: re-attempt failed commits
     -> Dashboard shows unversioned count with warning
```

The `[unversioned]` flag is analogous to Git AI's `[pending]` prefix pattern: a visible marker in the system that a change is in an intermediate state. Unlike Git AI's background polishing, Aura's flag means "this page exists on disk but has no git history -- disaster recovery hazard."

#### Anti-Feature: Mega-Commits

Committing many unrelated changes in a single commit. If a batch import creates 50 pages, each should be its own commit (or logically grouped by source). A single "batch import" commit loses per-page provenance. At minimum, group by source document with a commit message listing affected slugs.

#### Anti-Feature: Commit on Every LLM Call

Committing the entire repository after every agent loop iteration. Agent loops may make multiple LLM calls per user message; only wiki mutations should trigger commits. Changes to ephemeral state (conversation context, tool outputs) should not be committed.

#### Dependencies

- Triggered by feature #3 (tool-based wiki creation): tool handler calls git commit after write
- Interacts with feature #5 (async reindex): commit happens before reindex job is enqueued (so reindex always runs against committed state)
- No dependency on features #1, #2, #4, #6, #7, #8

---

### 10. Removal of Legacy Code Paths Superseded by Qdrant

**Category:** Table Stakes (correctness)
**Complexity:** MEDIUM
**Existing foundation:** The codebase contains extensive chromem-go usage (`internal/search/search.go`, `internal/search/embed_cache.go`, `internal/search/qdrant.go`, `internal/search/graph_documents.go`) that duplicates Qdrant functionality. Config references to `sqlite-vec` exist in settings. `chromem-go` is imported as the primary vector engine in search.

#### What Must Be Removed

| Legacy Component | Files | Superseded By | Risk of Removal |
|-----------------|-------|---------------|-----------------|
| `chromem-go` as primary vector engine | `internal/search/search.go` (`Engine` struct, `coll *chromem.Collection`) | Qdrant `search_memory` queries directly against Qdrant | MEDIUM: search.go is the main search entry point |
| `EmbedCache` wrapping chromem embeddings | `internal/search/embed_cache.go` | Qdrant stores vectors natively; no local embedding cache needed | LOW: Qdrant is the store |
| `sqlite-vec` references in config/settings | `internal/config/config.go`, `internal/settings/defaults.go`, `internal/api/settings.go` | Qdrant only | LOW: config-level changes |
| `chromem-go` import in rebuild logic | `internal/search/qdrant.go` (`RebuildQdrantWikiDocuments` uses `chromem.EmbeddingFunc`) | Replace with direct Qdrant Go client calls using the embedding API directly | MEDIUM: this function bridges chromem and Qdrant |
| chromem-go dependency | `go.mod` | Remove entirely | HIGH: must verify no other consumers |

#### Pattern: Strangler Fig with Feature Flag

The safest migration is validation-first: add the Qdrant-native search path, validate it explicitly, then remove the old chromem vector path after validation:

```
Phase 1: Add Qdrant-native search path and validation probes
Phase 2: Enable flag for internal testing (100% Qdrant)
Phase 3: Verify search quality parity (same queries, compare results)
Phase 4: Remove chromem-go code, imports, and dependency
```

Since this is a single-user/small-team bot (not a SaaS with gradual rollout), the flag can be a simple environment variable or config boolean, toggled during a maintenance window.

#### Anti-Feature: Big-Bang Removal

Deleting all chromem-go code before validating Qdrant-native search. If the new path has a bug (wrong distance metric, dimension mismatch, collection name typo), all vector search breaks. Validate Qdrant quality before removal rather than keeping a long-lived secondary path.

#### Anti-Feature: Keeping Dead Code "Just in Case"

Leaving chromem-go imports and unused functions in the codebase after migration. Dead code confuses future readers, bloats binary size, and creates false positives in code search. Remove all traces after the flag is verified.

#### Dependencies

- Requires feature #8 (Qdrant health): the new Qdrant-only path depends on Qdrant being healthy
- Feature #5 (async reindex) must use Qdrant-native APIs, not chromem-go
- Interacts with feature #3 (tool-based wiki): wiki writes should go directly to Qdrant reindex queue, not chromem collection
- Must run AFTER features #5 and #8 are stable

---

## Feature Dependencies

```text
Feature #8 (Qdrant startup health)
    -> required by -> Feature #5 (Async wiki reindex)
    -> required by -> Feature #10 (Legacy code removal)
    -> required by -> TOOL-01 (Qdrant-backed tool retrieval)

Feature #3 (Tool-based wiki creation)
    -> triggers -> Feature #5 (Async wiki reindex)
    -> triggers -> Feature #9 (Git commit tracking)

Feature #1 (UserGate actor/inbox)
    -> required by -> Feature #2 (Context leak cleanup -- stop idle actor before clearing state)
    -> required by -> Feature #7 (Per-user budget -- counter under user gate)

Feature #6 (Circuit breaker)
    -> enhances -> Feature #4 (Variable temperature retry -- breaker stops retries early)

Feature #7 (Per-user budget)
    -> enhances -> Feature #4 (Variable temperature retry -- budget-aware retry limits)
    -> enhances -> Feature #6 (Circuit breaker -- breaker events affect budget)
```

### Dependency Notes

- **Feature #8 (Qdrant health) is a Phase 1 foundational dependency.** Both async reindex (#5), Qdrant-backed tool retrieval, and legacy removal (#10) require a validated, healthy Qdrant instance. It must ship before Phase 2 depends on Qdrant.
- **Feature #1 (UserGate actor/inbox) is the synchronization foundation.** The Phase 1 checkpoint selects a per-user actor with a bounded inbox over a keyed gate. Context eviction (#2) and per-user budget (#7) must use that same gate boundary to operate safely.
- **Feature #3 (wiki tool) is the producer.** It triggers both reindex jobs (#5) and git commits (#9). It can ship before the consumers are fully built (reindex can be synchronous initially, then made async).
- **Features #4, #6, and #7 form a resilience triad.** They interact deeply (breaker stops retries, retries consume budget, budget gates breaker recovery). They benefit from being in the same phase.

---

## MVP Recommendation (Phase Ordering)

Based on dependency analysis and risk, the 10 features should be grouped into phases:

### Phase 1: Fondamenta (Concurrency + Qdrant Readiness)
1. **#1 Per-user message serialization** -- fixes the most critical concurrency bug (race conditions) using the actor/inbox UserGate selected in the Phase 1 checkpoint
2. **#2 Context leak cleanup** -- depends on #1, prevents memory leaks in long-running deployments, and stops idle per-user actors
3. **#8 Qdrant startup health validation** -- foundational dependency for async reindex, Qdrant-backed tool retrieval, and eventual chromem-go removal

### Phase 2: Core Hardening
4. **#3 Tool-based wiki page creation** -- correctness fix, replaces fragile heuristic
5. **#9 Explicit git commit tracking** -- triggered by #3, low complexity
6. **#5 Async wiki reindex with backpressure** -- triggered by #3, requires #8

### Phase 3: Resilience Layer
7. **#6 Circuit breaker on LLM provider health** -- protects against provider degradation
8. **#4 Variable-temperature LLM retry** -- requires #6 for the breaker-to-retry integration
9. **#7 Per-user token budget with global hard cap** -- interacts with both #4 and #6

### Phase 4: Cleanup
10. **#10 Removal of legacy code paths** -- must ship last, after all Qdrant-dependent features are stable

### Anti-Feature: Shipping Cleanup (#10) Before Resilience (#4, #6, #7)

Removing chromem-go while the LLM retry and circuit breaker patterns are still using the old code path would break the retry loop. Wait until the resilience layer is stable with Qdrant-native APIs, then remove legacy code.

---

## Feature Prioritization Matrix

| # | Feature | User Value | Implementation Cost | Risk of Regression | Priority |
|---|---------|------------|---------------------|--------------------|-------|
| 8 | Qdrant startup health + warm cache | HIGH -- prevents silent search failures | LOW | LOW -- additive, no existing path changes | P1 |
| 1 | UserGate actor/inbox | HIGH -- prevents corrupt agent state | MEDIUM | MEDIUM -- touches Telegram conversation entry point | P1 |
| 2 | Context leak cleanup | HIGH -- prevents memory bloat | MEDIUM | LOW -- additive to conversation lifecycle | P1 |
| 3 | Tool-based wiki creation | HIGH -- correctness fix | HIGH | HIGH -- replaces wiki write path entirely | P1 |
| 9 | Git commit tracking | MEDIUM -- audit trail | LOW | LOW -- additive, post-write hook | P2 |
| 5 | Async wiki reindex | MEDIUM -- UX improvement | HIGH | MEDIUM -- replaces synchronous reindex path | P2 |
| 6 | Circuit breaker | MEDIUM -- provider resilience | MEDIUM | LOW -- wraps existing provider calls | P2 |
| 4 | Variable temp retry | HIGH -- reduces wasted retries | MEDIUM | MEDIUM -- modifies retry behavior | P2 |
| 7 | Per-user token budget | HIGH -- cost governance | HIGH | MEDIUM -- extends existing budget tracker | P2 |
| 10 | Legacy code removal | LOW (internal hygiene) | MEDIUM | HIGH -- removes legacy vector path | P3 |

**Priority key:**
- P1: Must ship in v4.0 for correctness and basic reliability
- P2: Important hardening, ship in v4.0
- P3: Cleanup, can follow after v4.0 if timeline constrained

---

## What "Reliable" Means to Users

When users describe a bot as "reliable," they expect these observable behaviors, each mapped to hardening features:

| User Expectation | Observable Behavior | Hardening Feature |
|------------------|--------------------|--------------------|
| "It never loses what I told it" | Wiki pages persist and are searchable across restarts | #8 Qdrant health + persistence, #9 git tracking |
| "It doesn't get confused if I send multiple messages" | Sequential, coherent responses even under rapid-fire messages | #1 UserGate actor/inbox |
| "It remembers things from last week" | Long-lived wiki pages survive session boundaries and restarts | #2 Context eviction (clean, not destructive), #5 Qdrant reindex |
| "It doesn't cost a fortune" | Predictable token usage, no runaway loops | #7 Per-user budget with global cap |
| "It doesn't hang when a provider is down" | Fast, clear provider-health failure instead of repeated slow calls | #6 Circuit breaker |
| "It doesn't repeat itself when confused" | Retry with variation, not identical re-attempts | #4 Variable temperature retry |
| "Its memory is searchable and durable" | Wiki pages embedded once, searchable immediately | #3 Tool-based wiki creation, #5 async reindex |
| "It starts up cleanly after a restart" | No re-embedding of existing content, fast readiness | #8 Warm cache check |
| "I can see what changed" | Audit trail of wiki mutations, versioned history | #9 Git commit tracking |

---

## Sources

- [Go Telegram bot per-chat goroutine pattern (ufy-it/go-telegram-bot)](https://pkg.go.dev/github.com/ufy-it/go-telegram-bot)
- [Echotron per-chat instance model with sync.Map](https://github.com/NicoNex/echotron)
- [Zylos Research: Context Window Management and Session Lifecycle](https://zylos.ai/research/2026-03-31-context-window-management-session-lifecycle-long-running-agents)
- [Botpress Conversation Lifecycle Management](https://botpress.com/docs/adk/conversations/lifecycle)
- [Pichay Paper: Demand Paging for LLM Context Windows](https://arxiv.org/html/2603.09023)
- [Karpathy LLM Wiki Pattern](https://wiki.charleschen.ai/ai/processed/wiki/karpathy/llm-wiki/raw/web/karpathy-llm-wiki-pattern)
- [LLM Wiki Compiler with Schema Layer](https://github.com/atomicmemory/llm-wiki-compiler)
- [RLM-on-KG: Heuristics First, LLMs When Needed (Volpini & Raad)](https://arxiv.org/html/2604.17056v1)
- [Tian Pan: Retries Aren't Free -- The FinOps Math of LLM Retry Policies](https://tianpan.co/blog/2026-04-28-retries-arent-free-llm-finops-math)
- [Monte Carlo Temperature (Cecere et al., 2025)](https://arxiv.org/html/2502.18389v1)
- [vllm-project/semantic-router Go async job queue with backpressure](https://github.com/vllm-project/semantic-router)
- [Weaviate Go async indexing with dynamic semaphore](https://github.com/weaviate/weaviate)
- [Implementing Circuit Breakers for LLM Services in Go (dasroot)](https://dasroot.net/posts/2026/02/implementing-circuit-breakers-for-llm-services-in-go/)
- [sony/gobreaker v2 Go circuit breaker library](https://github.com/sony/gobreaker)
- [mercari/go-circuitbreaker with Prometheus metrics](https://github.com/mercari/go-circuitbreaker)
- [LLM Rate Limiting in Production: Token Budgets & Per-User Quotas](https://www.systemshardening.com/articles/kubernetes/llm-rate-limiting/)
- [Azure API Management llm-token-limit policy](https://learn.microsoft.com/en-us/azure/api-management/llm-token-limit-policy)
- [Qdrant Administration: Storage and Startup Optimization](https://qdrant.tech/documentation/guides/administration)
- [Qdrant PR #8053: Cold-data loading optimization (v1.17.0)](https://github.com/qdrant/qdrant/pull/8053)
- [Qdrant Issue #2358: Fast loading of collections after restart](https://github.com/qdrant/qdrant/issues/2358)
- [Checkpoint Commit Patterns: Git Strategies for AI-Assisted Development](https://understandingdata.com/posts/checkpoint-commit-patterns/)
- [Git AI daidi: Async Commit Polisher with unversioned flag pattern](https://github.com/daidi/git-ai)
- [KeygraphHQ/shannon Crash-Safe Audit System with self-healing reconciliation](https://github.com/KeygraphHQ/shannon)
- [Feature Flags for Safe Refactoring (TechDebt.solutions)](https://techdebt.solutions/playbooks/feature-flags/)
- [Feature Flag Cleanup in Go: Patterns and Automation](https://flagshark.com/blog/feature-flag-cleanup-golang-patterns-automation/)

---

*Feature research for: Aura v4.0 Production Hardening*
*Researched: 2026-05-10*
*Confidence: HIGH*
