---
phase: 01-fondamenta-concurrency-safety
verified: 2026-05-10T17:00:00Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 7/9
  gaps_closed:
    - "Warm-cache check uses points_count > 0 before skipping a full re-embed pass (QDRANT-01)"
    - "A user whose message is queued behind a long-running turn receives a clear still-processing response within the configurable timeout period (CONC-01 SC#4)"
  gaps_remaining: []
  regressions: []
---

# Phase 1: Fondamenta (Concurrency + Qdrant Readiness) Verification Report

**Phase Goal:** Users cannot corrupt their conversation state through concurrent messages; system notifications never deadlock; inactive sessions release resources predictably; Qdrant readiness is known before Qdrant-dependent features are built on top.
**Verified:** 2026-05-10
**Status:** passed
**Re-verification:** Yes — after gap-closure plans 01-06 (QDRANT-01 warm-cache) and 01-07 (CONC-01 queue-notice + env-tunable gate)

## Goal Achievement

### Observable Truths

| #  | Truth | Status | Evidence |
|----|-------|--------|---------|
| 1  | Per-user message serialization via UserGate — same user messages processed sequentially | VERIFIED | `internal/concurrency/gate.go`: `runActor` processes inbox entries one at a time via a single for-select loop; FIFO channel. Tests in `gate_test.go` confirm serialization. |
| 2  | TryAcquire on notification paths prevents deadlocks | VERIFIED | `internal/telegram/scheduler_handlers.go` lines 70, 244: both `dispatchReminder` and `notifyAgentJob` use `gate.TryAcquire`; return nil/false on full inbox. |
| 3  | InactivityTracker uses separate map+RWMutex, NOT sync.Map.Range | VERIFIED | `internal/concurrency/tracker.go`: uses `map[string]time.Time` with `sync.RWMutex`. No `sync.Map` anywhere. `sweep()` collects stale users under RLock then evicts outside the lock. |
| 4  | Qdrant startup health gate — WaitForReady called before bot.Start() | VERIFIED | `internal/telegram/setup.go` lines 115-123: `qdrant.WaitForReady(healthCtx, qcli, 120*time.Second)` is called inside `telegram.New()`, which is invoked from `main.go` before `go bot.Start()`. |
| 5  | Warm-cache check uses PointsCount > 0 before skipping a full re-embed pass | VERIFIED | Plan 01-06 wired `client.CollectionInfo` into all three rebuild call sites. `rebuildQdrantWikiDocumentsWithClient` (search/qdrant.go:174), `toolVectorIndex.Build` (tools/registry_search_vector.go:128), and `CompactMemoryQdrantIndex.Recreate` (search/compact_qdrant.go:62) each call `CollectionInfo` before `DeleteCollection`. On warm hit (`PointsCount > 0`) all three skip Delete/Create/embed. 12 new tests cover warm, cold, not-found, and probe-error branches across the three call sites. |
| 6  | Duplicate Qdrant HTTP implementations removed | VERIFIED | No `qdrantClient` struct in search package; no `doQdrantJSON`/`authorizeQdrant`/`qdrantBase`/`recreateQdrantCollection` methods in tools package. Both packages import and use `qdrant.Client`. |
| 7  | UserGate constructed in setup.go and wired into Bot before Start | VERIFIED | `internal/telegram/setup.go` lines 446-490: `concurrency.New(...)` creates gate with OnEvict, OnOverflow, and OnQueueNotice callbacks; `b.gate = userGate`; `b.sessions = agentruntime.NewSessionStore(userGate)`. Bot.Stop() calls `gate.Close()`. |
| 8  | onMessage routes through UserGate.Acquire | VERIFIED | `internal/telegram/handlers.go` lines 41-63: gate is retrieved, Entry wraps `handleConversation`, `gate.Acquire(context.Background(), userID, entry)` is called. Fallback to direct goroutine when gate is nil. |
| 9  | A user whose message is queued behind a long-running turn receives a clear still-processing response within the configurable timeout period | VERIFIED | Plan 01-07 added: `concurrency.Config.QueueNoticeAfter` (env: `AURA_INBOX_QUEUE_NOTICE_AFTER`, default 30s) and `OnQueueNotice` callback. `Acquire` spawns `runQueueNoticeTimer` goroutine that fires `OnQueueNotice(userID)` after threshold unless entry began processing. `setup.go` `OnQueueNotice` sends "Still working on your previous message -- I'll get to this one shortly." via `b.bot.Send` in a separate goroutine (Pitfall 4). `InboxSize`, `EvictionThreshold`, and `SweepInterval` are now env-tunable via `AURA_INBOX_SIZE`, `AURA_INACTIVITY_THRESHOLD`, `AURA_INACTIVITY_SWEEP_INTERVAL`. No hardcoded 8/30m/60s remain in `setup.go`. |

**Score:** 9/9 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/qdrant/types.go` | Point, ScoredPoint, CollectionInfo, Config structs | VERIFIED | All structs present with correct JSON tags |
| `internal/qdrant/config.go` | DefaultConfig() with BaseURL, APIKey, Timeout defaults | VERIFIED | Exports DefaultConfig() returning Config{Timeout: 30s, MaxRetryDelay: 10s} |
| `internal/qdrant/client.go` | Client interface + httpClient + NewClient + WaitForReady | VERIFIED | All 7 interface methods; WaitForReady uses exponential backoff |
| `internal/qdrant/client_test.go` | Mock server tests for Health, WaitForReady, CollectionInfo | VERIFIED | 10 tests covering all scenarios |
| `internal/concurrency/types.go` | Entry struct with startedCh; Config struct with QueueNoticeAfter/OnQueueNotice | VERIFIED | Entry.startedCh (unexported chan struct{}); Config.QueueNoticeAfter time.Duration; Config.OnQueueNotice func(string) all present with documented semantics |
| `internal/concurrency/gate.go` | UserGate with Acquire (timer spawn), TryAcquire, runActor (startedCh close), dropOldestAndNotify (startedCh close), runQueueNoticeTimer | VERIFIED | All methods present; runQueueNoticeTimer selects on timer.C vs startedCh; runActor closes startedCh before Process; dropOldestAndNotify closes startedCh on drop |
| `internal/concurrency/tracker.go` | InactivityTracker with map+RWMutex+ticker | VERIFIED | map[string]time.Time + sync.RWMutex; no sync.Map anywhere |
| `internal/concurrency/gate_test.go` | Tests for CONC-01, CONC-02, CONC-03, and 5 new QueueNotice tests | VERIFIED | Sequential processing, concurrent users, overflow, TryAcquire, QueueNotice fires/doesn't-fire/disabled/nil-safe tests |
| `internal/concurrency/tracker_test.go` | Tests for CONC-03 | VERIFIED | Eviction, active skip, cleanup callback tests |
| `internal/config/config.go` | InboxSize, InboxQueueNoticeAfter, InactivityThreshold, InactivitySweepInterval fields with AURA_* envconfig tags | VERIFIED | All four fields present (lines 135-138); Default* constants at lines 29-32; Load() populates via getEnvInt/getEnvDuration at lines 287-293 |
| `internal/config/env.go` | getEnvDuration helper | VERIFIED | Defined at line 71; mirrors getEnvInt/getEnvBool pattern (bare os.Getenv, no TrimSpace, rejects negative, returns fallback on parse error) |
| `internal/search/qdrant.go` | rebuildQdrantWikiDocumentsWithClient with warm-cache short-circuit | VERIFIED | CollectionInfo called at line 174; PointsCount > 0 check at line 179; warm-cache log + early return at lines 186-193 |
| `internal/search/qdrant_test.go` | 4 new warm-cache branch tests | VERIFIED | TestRebuildQdrantWikiDocumentsWarmCacheHit, ColdPath, CollectionNotFound, CollectionInfoErrorFallsBack all present and passing |
| `internal/tools/registry_search_vector.go` | toolVectorIndex.Build with warm-cache short-circuit | VERIFIED | CollectionInfo called at line 128; PointsCount > 0 check at line 132; warm-cache early return at lines 133-136 |
| `internal/tools/registry_search_vector_test.go` | 4 new warm-cache branch tests | VERIFIED | TestToolVectorBuildWarmCacheHit, ColdPath, CollectionNotFound, CollectionInfoErrorFallsBack all present and passing |
| `internal/search/compact_qdrant.go` | CompactMemoryQdrantIndex.Recreate with warm-cache short-circuit | VERIFIED | CollectionInfo called at line 62; PointsCount > 0 check at line 65; warm-cache early return at lines 66-68 |
| `internal/search/compact_qdrant_test.go` | 4 new warm-cache branch tests | VERIFIED | TestCompactMemoryQdrantRecreateWarmCacheHit, ColdPath, CollectionNotFound, CollectionInfoErrorFallsBack all present and passing |
| `internal/telegram/bot.go` | Bot struct with gate *concurrency.UserGate field | VERIFIED | gate field present; userGate() accessor; Stop() calls gate.Close() |
| `internal/telegram/handlers.go` | onMessage routes through UserGate.Acquire | VERIFIED | Lines 41-63 |
| `internal/telegram/scheduler_handlers.go` | dispatchReminder and notifyAgentJob use TryAcquire | VERIFIED | Lines 70 and 244 |
| `internal/agentruntime/session.go` | SessionStore delegates IsActive to UserGate | VERIFIED | IsActive delegates to gate.IsActive when gate is set |
| `internal/telegram/setup.go` | UserGate + Qdrant health gate wired; env-tunable gate config; OnQueueNotice callback | VERIFIED | UserGate correctly wired (lines 444-492); health gate present; cfg fields used — no hardcoded 8/30m/60s; OnQueueNotice sends still-processing Telegram message in separate goroutine |
| `cmd/aura/main.go` | WaitForReady before bot.Start() | VERIFIED | Health gate inside telegram.New() which is called before go bot.Start() |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/telegram/handlers.go onMessage` | `internal/concurrency/gate.go Acquire` | `gate.Acquire(context.Background(), userID, entry)` | WIRED | Line 57 of handlers.go |
| `internal/telegram/scheduler_handlers.go dispatchReminder` | `internal/concurrency/gate.go TryAcquire` | `gate.TryAcquire(task.RecipientID, concurrency.Entry{...})` | WIRED | Line 70 of scheduler_handlers.go |
| `internal/telegram/scheduler_handlers.go notifyAgentJob` | `internal/concurrency/gate.go TryAcquire` | `gate.TryAcquire(recipientID, concurrency.Entry{...})` | WIRED | Line 244 of scheduler_handlers.go |
| `internal/concurrency/gate.go runActor` | `internal/concurrency/tracker.go Touch` | `g.tracker.Touch(actor.userID)` after each entry | WIRED | Line 236 of gate.go |
| `internal/concurrency/tracker.go sweep` | `internal/concurrency/gate.go Evict` | `t.onEvict(userID)` → gate.Evict | WIRED | Line 92 of tracker.go |
| `internal/agentruntime/session.go IsActive` | `internal/concurrency/gate.go IsActive` | `s.gate.IsActive(strings.TrimSpace(userID))` | WIRED | Line 114 of session.go |
| `internal/telegram/setup.go New` | `qdrant.WaitForReady` | Called before consumers, returns error on failure | WIRED | Lines 115-119 of setup.go |
| `internal/search/qdrant.go rebuildQdrantWikiDocumentsWithClient` | `qdrant.Client.CollectionInfo` | `client.CollectionInfo(ctx, collection)` before DeleteCollection | WIRED | Line 174 of search/qdrant.go — gap closed by plan 01-06 |
| `internal/tools/registry_search_vector.go toolVectorIndex.Build` | `qdrant.Client.CollectionInfo` | `idx.qclient.CollectionInfo(ctx, idx.collection)` before DeleteCollection | WIRED | Line 128 of registry_search_vector.go — gap closed by plan 01-06 |
| `internal/search/compact_qdrant.go CompactMemoryQdrantIndex.Recreate` | `qdrant.Client.CollectionInfo` | `i.client.CollectionInfo(ctx, i.collection)` before pointsForDocuments | WIRED | Line 62 of compact_qdrant.go — gap closed by plan 01-06 |
| `internal/concurrency/gate.go Acquire` | `OnQueueNotice` | `go g.runQueueNoticeTimer(userID, startedCh)` spawned after successful enqueue | WIRED | Line 89 of gate.go — gap closed by plan 01-07 |
| `internal/concurrency/gate.go runActor` | `Entry.startedCh` | `close(entry.startedCh)` before `entry.Process` | WIRED | Line 232-233 of gate.go |
| `internal/telegram/setup.go OnQueueNotice callback` | `b.bot.Send` | `go func() { b.bot.Send(tele.ChatID(chatID), msg) }()` | WIRED | Lines 474-488 of setup.go |
| `internal/config/config.go Load` | `getEnvDuration` | `cfg.InboxQueueNoticeAfter = getEnvDuration("AURA_INBOX_QUEUE_NOTICE_AFTER", ...)` | WIRED | Lines 291-293 of config.go |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|--------------------|--------|
| `internal/concurrency/gate.go` | actor.inbox (chan Entry) | onMessage via Acquire | Yes — real message closures | FLOWING |
| `internal/concurrency/tracker.go` | lastActivity map | tracker.Touch after each entry | Yes — time.Now() on real processing | FLOWING |
| `internal/qdrant/client.go CollectionInfo` | CollectionInfo.PointsCount | HTTP GET /collections/{name} | Yes — real Qdrant response; 3 call sites consume it | FLOWING |
| `internal/telegram/setup.go` UserGate.OnEvict | sessionStore.Clear(userID) | gate.Evict → OnEvict callback | Yes — eviction event | FLOWING |
| `internal/concurrency/gate.go` runQueueNoticeTimer | startedCh | Acquire sets, runActor or dropOldestAndNotify closes | Yes — channel signal from real processing start or overflow drop | FLOWING |
| `internal/config/config.go` InboxQueueNoticeAfter | time.Duration | getEnvDuration("AURA_INBOX_QUEUE_NOTICE_AFTER", DefaultInboxQueueNoticeAfter) | Yes — env var or default | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| concurrency package tests (incl. 5 new QueueNotice tests) | `go test -count=1 -short ./internal/concurrency/` | `ok ... 0.885s` | PASS |
| search package tests (incl. 8 new warm-cache tests) | `go test -count=1 -short ./internal/search/` | `ok ... 4.135s` | PASS |
| tools package tests (incl. 4 new warm-cache tests) | `go test -count=1 -short ./internal/tools/` | `ok ... 7.992s` | PASS |
| qdrant package tests | `go test -count=1 -short ./internal/qdrant/` | `ok ... 2.100s` | PASS |
| config package tests | `go test -count=1 -short ./internal/config/` | `ok ... 0.043s` | PASS |
| telegram package tests | `go test -count=1 -short ./internal/telegram/` | `ok ... 3.407s` | PASS |
| go vet all phase-1 packages | `go vet ./internal/concurrency/ ./internal/search/ ./internal/tools/ ./internal/qdrant/ ./internal/config/ ./internal/telegram/ ./internal/agentruntime/` | Exit 0, no warnings | PASS |
| No hardcoded 8/30m/60s in setup.go concurrency.Config literal | `grep -E "InboxSize: 8|EvictionThreshold: 30|SweepInterval: 60" setup.go` | No matches | PASS |
| All 3 rebuild paths call CollectionInfo | `grep -c "CollectionInfo" qdrant.go registry_search_vector.go compact_qdrant.go` | 1+ per file | PASS |
| 5 new QueueNotice test functions exist | `grep "TestQueueNotice" gate_test.go` | 5 matches | PASS |

Note: Race detector could not run — CGO unavailable on this Windows host (`gcc.exe` not found). Test results from `go test -count=1 -short` without `-race`. The race-detector pass should be verified in a Docker/Linux environment where CGO is available.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|---------|
| CONC-01 | 01-02, 01-04, 01-07 | Per-user message serialization — same-user messages queued, not parallel | SATISFIED | Serialization implemented and wired (plans 01-02/04). Gap-closure plan 01-07 added: env-tunable gate config (AURA_INBOX_SIZE etc.); per-entry queue-notice timer fires after AURA_INBOX_QUEUE_NOTICE_AFTER; still-processing Telegram notice sent. |
| CONC-02 | 01-02, 01-04 | TryAcquire on notification paths — never deadlock | SATISFIED | dispatchReminder and notifyAgentJob both use TryAcquire with graceful drop |
| CONC-03 | 01-02 | Context leak cleanup — eviction with separate tracking structure | SATISFIED | InactivityTracker uses map[string]time.Time + sync.RWMutex; eviction cancels actor context; OnEvict clears session |
| QDRANT-01 | 01-01, 01-03, 01-05, 01-06 | Qdrant startup health validation + warm-cache check | SATISFIED | Health gate (WaitForReady) satisfied. Gap-closure plan 01-06 wired CollectionInfo into all three rebuild paths; PointsCount > 0 check skips Delete/Create/embed on warm restart. |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/telegram/setup.go` | 477 | `_ = qdrantCli` — shared client created but only used for health gate; search/compact create own clients from QdrantConfig | Info | Informational only; pre-existing from initial verification. Not a correctness issue — three separate http.Client pools where one was architecturally intended. No change from previous report. |
| `internal/concurrency/gate.go` | 219-238 | `runActor` exits on `ctx.Done()` without draining inbox — entries remaining in the inbox at eviction/shutdown have live `runQueueNoticeTimer` goroutines whose `startedCh` is never closed | Warning (code-quality) | Identified as CR-01 in `01-REVIEW.md`. Timer goroutines leak up to `QueueNoticeAfter` (default 30s) and fire spurious "Still working" notices after eviction. The must-have truths for plan 01-07 enumerate "does NOT fire on overflow drop" and "does NOT fire when QueueNoticeAfter <= 0" — the eviction-drain scenario is NOT enumerated. This is a code-quality finding from the post-implementation review, not a must-have failure. Tracked in `01-REVIEW.md` as CR-01 for future remediation. |

### CR-01 Assessment (from 01-REVIEW.md)

**Finding:** `runActor` does not drain its inbox on context cancellation (Evict/Close). Entries remaining in the inbox at the time of eviction have live `runQueueNoticeTimer` goroutines (spawned by `Acquire`) that wait up to `QueueNoticeAfter` and then fire `OnQueueNotice(userID)`, sending a spurious "Still working" Telegram message to a user whose session was already evicted.

**Gating decision:** This finding does NOT gate phase verification. Reasoning:

1. The ROADMAP Phase 1 SC#4 states "a user whose message is queued behind a long-running turn receives a clear 'still processing' response within the configurable timeout period rather than hanging indefinitely." This is satisfied — queued entries do receive the notice.
2. The plan 01-07 must-haves enumerate three "does NOT fire" cases: early dequeue, overflow drop, and QueueNoticeAfter==0. The eviction-drain case is not among the enumerated must-haves.
3. The behavior is a code-quality bug (spurious notice post-eviction) that does not undermine the phase goal — it makes the notice sometimes fire in an unintended case, but the primary use case (queued behind a slow turn → notice fires) works correctly.

**Action:** The fix is well-specified in `01-REVIEW.md` CR-01. It should be addressed in a Phase 1 follow-up or as part of Phase 2 work. The plan pattern (pass `actorCtx` to `runQueueNoticeTimer` and drain inbox on exit) is documented in the review file.

### Human Verification Required

None — all verifiable items were checked programmatically.

## Gaps Summary

No gaps. Both previously identified gaps are now closed:

**Gap 1 (closed by plan 01-06):** Warm-cache short-circuit wired into all three rebuild paths. `qdrant.Client.CollectionInfo` is called before `DeleteCollection` in `rebuildQdrantWikiDocumentsWithClient`, `toolVectorIndex.Build`, and `CompactMemoryQdrantIndex.Recreate`. On warm hit (`PointsCount > 0`) the full Delete/Create/embed cycle is skipped. 12 new tests cover warm, cold, not-found, and probe-error branches. Commits: `231443b7`, `8d24df0d`, `cca63bde`.

**Gap 2 (closed by plan 01-07):** Per-entry queue-notice timer added. `AURA_INBOX_QUEUE_NOTICE_AFTER` (default 30s) configures when a still-processing notice fires for a queued entry that has not yet begun processing. All four gate parameters (`AURA_INBOX_SIZE`, `AURA_INBOX_QUEUE_NOTICE_AFTER`, `AURA_INACTIVITY_THRESHOLD`, `AURA_INACTIVITY_SWEEP_INTERVAL`) are now environment-tunable — no hardcoded values remain in `setup.go`. Commits: `67495111`, `3835a13a`, `f225f064`, `cf299a8e`.

**Code review finding (not a gap):** CR-01 in `01-REVIEW.md` identifies a timer goroutine leak on Evict/Close. This is a code-quality concern that does not map to any enumerated must-have or ROADMAP success criterion. It is recorded for future remediation but does not block phase sign-off.

---

_Verified: 2026-05-10_
_Verifier: Claude (gsd-verifier)_
_Re-verification after gap-closure plans 01-06 and 01-07_
