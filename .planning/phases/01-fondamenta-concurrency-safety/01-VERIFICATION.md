---
phase: 01-fondamenta-concurrency-safety
verified: 2026-05-10T00:00:00Z
status: gaps_found
score: 7/9 must-haves verified
overrides_applied: 0
gaps:
  - truth: "Warm-cache check uses points_count > 0 before skipping a full re-embed pass"
    status: failed
    reason: "CollectionInfo is implemented and tested in internal/qdrant/ but is never called from any consumer path during startup. IndexWikiPages always calls rebuildQdrantWikiDocumentsWithClient which always deletes and recreates the collection. No code path reads PointsCount > 0 to skip re-embedding."
    artifacts:
      - path: "internal/search/qdrant.go"
        issue: "IndexWikiPages (line 127) calls rebuildQdrantWikiDocumentsWithClient unconditionally — no warm-cache check"
      - path: "internal/tools/registry_search_vector.go"
        issue: "Build (line 142) always calls DeleteCollection + CreateCollection — no warm-cache check"
      - path: "internal/search/compact_qdrant.go"
        issue: "Recreate (line 49) always calls DeleteCollection — no warm-cache check"
    missing:
      - "Call client.CollectionInfo(ctx, collection) before rebuilding; skip rebuild if PointsCount > 0"
      - "Wire warm-cache check into IndexWikiPages, toolVectorIndex.Build, and CompactMemoryQdrantIndex.Recreate"

  - truth: "A user whose message is queued behind a long-running turn receives a clear still-processing response within the configurable timeout period"
    status: failed
    reason: "Acquire uses context.Background() — no configurable timeout. The OnOverflow notice is only sent when the inbox is completely full (all 8 slots). A single message queued behind a long-running turn does NOT trigger any user-visible notice. The 8-slot inbox size and 30-minute eviction threshold are hardcoded, not configurable via environment."
    artifacts:
      - path: "internal/telegram/handlers.go"
        issue: "gate.Acquire(context.Background(), userID, entry) at line 57 — no timeout, user gets no notice when queued"
      - path: "internal/telegram/setup.go"
        issue: "InboxSize: 8 hardcoded (line 447); no config key in internal/config for user gate parameters"
    missing:
      - "Add AURA_INBOX_TIMEOUT or similar config to send a notice after a configurable wait, or document that the 8-entry overflow-drop is the intended 'still processing' mechanism"
      - "Alternatively: add a QueueTimeout to Config and use context.WithTimeout in Acquire to guarantee bounded wait + user notice"
      - "Add config fields AURA_INBOX_SIZE, AURA_EVICTION_THRESHOLD, AURA_SWEEP_INTERVAL to internal/config so they are environment-configurable"
---

# Phase 1: Fondamenta (Concurrency + Qdrant Readiness) Verification Report

**Phase Goal:** Users cannot corrupt their conversation state through concurrent messages; system notifications never deadlock; inactive sessions release resources predictably; Qdrant readiness is known before Qdrant-dependent features are built on top.
**Verified:** 2026-05-10
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #  | Truth | Status | Evidence |
|----|-------|--------|---------|
| 1  | Per-user message serialization via UserGate — same user messages processed sequentially | VERIFIED | `internal/concurrency/gate.go`: `runActor` processes inbox entries one at a time via a single for-select loop; FIFO channel. Tests in `gate_test.go` confirm serialization. |
| 2  | TryAcquire on notification paths prevents deadlocks | VERIFIED | `internal/telegram/scheduler_handlers.go` lines 70, 244: both `dispatchReminder` and `notifyAgentJob` use `gate.TryAcquire`; return nil/false on full inbox. |
| 3  | InactivityTracker uses separate map+RWMutex, NOT sync.Map.Range | VERIFIED | `internal/concurrency/tracker.go`: uses `map[string]time.Time` with `sync.RWMutex`. `grep -c "sync.Map" tracker.go` = 0. `sweep()` collects stale users under RLock then evicts outside the lock. |
| 4  | Qdrant startup health gate — WaitForReady called before bot.Start() | VERIFIED | `internal/telegram/setup.go` lines 115-123: `qdrant.WaitForReady(healthCtx, qcli, 120*time.Second)` is called inside `telegram.New()`, which is invoked from `main.go` before `go bot.Start()` (line 378 of `cmd/aura/main.go`). |
| 5  | CollectionInfo returns PointsCount; warm-cache check uses PointsCount > 0 before skipping re-embed | PARTIAL | `qdrant.Client.CollectionInfo` is implemented and returns `PointsCount`. However, NO consumer (search, tools, compact) calls `CollectionInfo` or checks `PointsCount > 0` before rebuilding. All rebuild paths always delete and recreate the collection on every startup. **BLOCKER for QDRANT-01 success criterion.** |
| 6  | Duplicate Qdrant HTTP implementations removed | VERIFIED | No `qdrantClient` struct in search package; no `doQdrantJSON`/`authorizeQdrant`/`qdrantBase`/`recreateQdrantCollection` methods in tools package. `internal/search/qdrant.go` and `internal/tools/registry_search_vector.go` both import and use `qdrant.Client`. |
| 7  | UserGate constructed in setup.go and wired into Bot before Start | VERIFIED | `internal/telegram/setup.go` lines 446-475: `concurrency.New(...)` creates gate with OnEvict and OnOverflow callbacks; `b.gate = userGate`; `b.sessions = agentruntime.NewSessionStore(userGate)`. Bot.Stop() calls `gate.Close()` at line 212. |
| 8  | onMessage routes through UserGate.Acquire | VERIFIED | `internal/telegram/handlers.go` lines 41-63: gate is retrieved, Entry wraps `handleConversation`, `gate.Acquire(context.Background(), userID, entry)` is called. Fallback to direct goroutine when gate is nil. |
| 9  | A user whose message is queued behind a long-running turn receives a clear still-processing response within the configurable timeout | FAILED | `gate.Acquire` uses `context.Background()` — no timeout. The overflow notice is only sent when all 8 inbox slots are full. A single queued message gets no notice. InboxSize and thresholds are hardcoded, not configurable. |

**Score:** 7/9 truths verified (truths 5 and 9 failed)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/qdrant/types.go` | Point, ScoredPoint, CollectionInfo, Config structs | VERIFIED | All structs present with correct JSON tags |
| `internal/qdrant/config.go` | DefaultConfig() with BaseURL, APIKey, Timeout defaults | VERIFIED | Exports DefaultConfig() returning Config{Timeout: 30s, MaxRetryDelay: 10s} |
| `internal/qdrant/client.go` | Client interface + httpClient + NewClient + WaitForReady | VERIFIED | All 7 interface methods, NewClient returns error on empty BaseURL, WaitForReady uses exponential backoff |
| `internal/qdrant/client_test.go` | Mock server tests for Health, WaitForReady, CollectionInfo | VERIFIED | 10 tests covering all scenarios; all pass |
| `internal/concurrency/types.go` | Entry struct, Config struct with callbacks | VERIFIED | Entry.Process is func(context.Context); Config has InboxSize, EvictionThreshold, SweepInterval, OnEvict, OnOverflow |
| `internal/concurrency/gate.go` | UserGate with Acquire, TryAcquire, Evict, IsActive, Close | VERIFIED | All methods present; runActor calls tracker.Touch after each entry; dropOldestAndNotify calls OnOverflow in separate goroutine |
| `internal/concurrency/tracker.go` | InactivityTracker with map+RWMutex+ticker | VERIFIED | map[string]time.Time + sync.RWMutex; no sync.Map anywhere |
| `internal/concurrency/gate_test.go` | Tests for CONC-01, CONC-02 | VERIFIED | Sequential processing, concurrent users, overflow, TryAcquire, context cancellation, Close tests |
| `internal/concurrency/tracker_test.go` | Tests for CONC-03 | VERIFIED | Eviction, active skip, cleanup callback tests |
| `internal/search/qdrant.go` | Migrated to qdrant.Client | VERIFIED | Uses qdrant.Client; no qdrantClient struct remains |
| `internal/search/compact_qdrant.go` | Migrated to qdrant.Client | VERIFIED | CompactMemoryQdrantIndex.client field is qdrant.Client |
| `internal/tools/registry_search_vector.go` | Migrated to qdrant.Client | VERIFIED | toolVectorIndex.qclient is qdrant.Client; all duplicate HTTP methods removed |
| `internal/telegram/bot.go` | Bot struct with gate *concurrency.UserGate field | VERIFIED | gate field present at line 73; userGate() accessor at line 102; Stop() calls gate.Close() at line 212 |
| `internal/telegram/handlers.go` | onMessage routes through UserGate.Acquire | VERIFIED | Lines 41-63 |
| `internal/telegram/scheduler_handlers.go` | dispatchReminder and notifyAgentJob use TryAcquire | VERIFIED | Lines 70 and 244 |
| `internal/agentruntime/session.go` | SessionStore delegates IsActive to UserGate | VERIFIED | IsActive delegates to gate.IsActive when gate is set; Begin no longer calls active.Store when gate present |
| `internal/telegram/setup.go` | UserGate + Qdrant health gate wired | PARTIAL | UserGate correctly wired; health gate present; BUT qdrantCli is discarded with `_ = qdrantCli` — search/compact create own clients from QdrantConfig, not the shared client |
| `cmd/aura/main.go` | WaitForReady before bot.Start() | VERIFIED | Health gate inside telegram.New() which is called before go bot.Start() |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/telegram/handlers.go onMessage` | `internal/concurrency/gate.go Acquire` | `gate.Acquire(context.Background(), userID, entry)` | WIRED | Line 57 of handlers.go |
| `internal/telegram/scheduler_handlers.go dispatchReminder` | `internal/concurrency/gate.go TryAcquire` | `gate.TryAcquire(task.RecipientID, concurrency.Entry{...})` | WIRED | Line 70 of scheduler_handlers.go |
| `internal/telegram/scheduler_handlers.go notifyAgentJob` | `internal/concurrency/gate.go TryAcquire` | `gate.TryAcquire(recipientID, concurrency.Entry{...})` | WIRED | Line 244 of scheduler_handlers.go |
| `internal/concurrency/gate.go runActor` | `internal/concurrency/tracker.go Touch` | `g.tracker.Touch(actor.userID)` after each entry | WIRED | Line 195 of gate.go |
| `internal/concurrency/tracker.go sweep` | `internal/concurrency/gate.go Evict` | `t.onEvict(userID)` → gate.Evict | WIRED | Line 92 of tracker.go; onEvict set to g.Evict in New() |
| `internal/agentruntime/session.go IsActive` | `internal/concurrency/gate.go IsActive` | `s.gate.IsActive(strings.TrimSpace(userID))` | WIRED | Line 114 of session.go |
| `internal/telegram/setup.go New` | `qdrant.WaitForReady` | Called before consumers, returns error on failure | WIRED | Lines 115-119 of setup.go |
| `internal/search/qdrant.go` | `qdrant.Client` | `qdrant.NewClient(...)` via `newQdrantClientFromConfig` | WIRED | Line 243 of search/qdrant.go |
| `internal/tools/registry_search_vector.go` | `qdrant.Client` | `qdrant.NewClient(...)` in `NewToolVectorIndex` | WIRED | Lines 78-82 of registry_search_vector.go |
| `cmd/aura/main.go startAura` | `qdrant.WaitForReady` (indirectly via telegram.New) | `telegram.New()` returns error if Qdrant unreachable | WIRED | Health gate inside setup.go, error propagates to main.go |
| Any consumer → `qdrant.CollectionInfo` → warm-cache check | `qdrant.Client.CollectionInfo` | `client.CollectionInfo(ctx, collection)` then check `PointsCount > 0` | NOT_WIRED | CollectionInfo is implemented but zero consumers call it for startup warm-cache logic |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|--------------------|--------|
| `internal/concurrency/gate.go` | actor.inbox (chan Entry) | onMessage via Acquire | Yes — real message closures | FLOWING |
| `internal/concurrency/tracker.go` | lastActivity map | tracker.Touch after each entry | Yes — time.Now() on real processing | FLOWING |
| `internal/qdrant/client.go CollectionInfo` | CollectionInfo.PointsCount | HTTP GET /collections/{name} | Yes — real Qdrant response | FLOWING in tests; DISCONNECTED in production startup |
| `internal/telegram/setup.go` UserGate.OnEvict | sessionStore.Clear(userID) | gate.Evict → OnEvict callback | Yes — eviction event | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full project builds | `go build ./...` | Exit 0, no errors | PASS |
| go vet clean | `go vet ./internal/concurrency/ ./internal/qdrant/ ./internal/telegram/ ./internal/agentruntime/ ./internal/search/ ./internal/tools/` | Exit 0, no errors | PASS |
| concurrency package tests | `CGO_ENABLED=0 go test -count=1 -short ./internal/concurrency/` | `ok ... 0.323s` | PASS |
| qdrant package tests | `CGO_ENABLED=0 go test -count=1 -short ./internal/qdrant/` | `ok ... 2.263s` | PASS |
| search package tests | `CGO_ENABLED=0 go test -count=1 -short ./internal/search/` | `ok ... 3.355s` | PASS |
| tools package tests | `CGO_ENABLED=0 go test -count=1 -short ./internal/tools/` | `ok ... 7.284s` | PASS |
| telegram package tests | `CGO_ENABLED=0 go test -count=1 -short ./internal/telegram/` | `ok ... 13.523s` | PASS |
| agentruntime package tests | `CGO_ENABLED=0 go test -count=1 -short ./internal/agentruntime/` | `ok ... 0.084s` | PASS |
| Full test suite (all packages) | `CGO_ENABLED=0 go test -count=1 -short ./...` | All 29 test packages pass | PASS |
| No sync.Map in tracker.go | `grep -c "sync.Map" internal/concurrency/tracker.go` | 0 | PASS |
| No duplicate Qdrant HTTP | `grep -c "func.*qdrantBase\|func.*doQdrantJSON" internal/tools/registry_search_vector.go` | 0 | PASS |
| No residual qdrantClient struct | `grep "type qdrantClient struct" internal/search/qdrant.go` | Not found | PASS |
| Zero Aura deps in concurrency | `grep "github.com/aura/aura" internal/concurrency/*.go` | No matches | PASS |

Note: Race detector could not run — CGO unavailable (`D:\tmp\w64devkit\bin\gcc.exe` not found). Test results from `go test -count=1 -short` without `-race`. The validation plan specified `-race` tests; this should be verified in a Docker/Linux environment where CGO is available.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|---------|
| CONC-01 | 01-02, 01-04 | Per-user message serialization — same-user messages queued, not parallel | PARTIALLY SATISFIED | Serialization implemented and wired correctly. Gap: no user-visible notice when a single message is queued behind a slow turn (only notified on 8-entry overflow). |
| CONC-02 | 01-02, 01-04 | TryAcquire on notification paths — never deadlock | SATISFIED | dispatchReminder and notifyAgentJob both use TryAcquire with graceful drop |
| CONC-03 | 01-02 | Context leak cleanup — eviction with separate tracking structure | SATISFIED | InactivityTracker uses map[string]time.Time + sync.RWMutex; eviction cancels actor context; OnEvict clears session |
| QDRANT-01 | 01-01, 01-03, 01-05 | Qdrant startup health validation + warm-cache check | PARTIALLY SATISFIED | Health gate (WaitForReady) SATISFIED. Warm-cache check (PointsCount > 0 before skip) NOT SATISFIED — CollectionInfo implemented but never used to skip re-embed. Configurable timeout NOT satisfied — hardcoded 120s. |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/telegram/setup.go` | 477 | `_ = qdrantCli` — shared client created but discarded; search/compact create own clients from QdrantConfig | Warning | Misleading comment claims qdrantCli is "used by search/compact consumers above" — it is not. Only the health gate uses it. This represents a design intent gap: Plan 01-03 aimed for one shared client; actual implementation creates separate clients per consumer. Functionally correct but architecturally inconsistent. |
| `internal/telegram/handlers.go` | 57 | `gate.Acquire(context.Background(), userID, entry)` — unbounded context | Warning | If a user's inbox perpetually overflows, Acquire could loop forever dropping-and-retrying in the same goroutine. In practice this resolves quickly since drop+enqueue is O(1), but there is no circuit-breaker. |
| `internal/telegram/setup.go` | 447-449 | `InboxSize: 8`, `EvictionThreshold: 30 * time.Minute`, `SweepInterval: 60 * time.Second` hardcoded | Info | REQUIREMENTS.md says "configurable threshold"; these should be environment variables in internal/config. Currently not user-adjustable without code change. |

### Human Verification Required

None — all verifiable items were checked programmatically.

## Gaps Summary

**Two gaps block full phase-goal achievement:**

**Gap 1 — Warm-cache check missing (QDRANT-01 BLOCKER):**
`qdrant.Client.CollectionInfo` exists and is tested in 2 mock-server tests. The `CollectionInfo.PointsCount` field is correctly defined with `json:"points_count"`. However, zero production code paths call `CollectionInfo` to check `PointsCount > 0` before running a full re-embed. On every restart with Qdrant URL configured, `IndexWikiPages` deletes and recreates the collection, triggering a full embedding pass regardless of existing data. The ROADMAP success criterion states "the warm-cache check uses `points_count > 0` before skipping a full re-embed pass" — this is unmet.

Fix: In `rebuildQdrantWikiDocumentsWithClient`, add a `client.CollectionInfo` call before `DeleteCollection`. If `info.PointsCount > 0`, return early with the existing count. Similarly fix `toolVectorIndex.Build` and `CompactMemoryQdrantIndex.Recreate`.

**Gap 2 — No user notice on queue (CONC-01 SC#4 WARNING):**
Success criterion #4 requires "a user whose message is queued behind a long-running turn receives a clear 'still processing' response within the configurable timeout period." The current implementation only sends an overflow notice when the inbox has 8 full entries (all slots occupied). A single message queued behind a slow turn receives no notice. Additionally, InboxSize/EvictionThreshold/SweepInterval are hardcoded in setup.go — the "configurable" aspect from REQUIREMENTS.md is not implemented.

Fix options: (a) add a `QueueTimeout` to `concurrency.Config` and use `context.WithTimeout` in `onMessage`'s Acquire call — on timeout, send "I'm still processing" before re-trying with Background context; or (b) document that the overflow-drop mechanism IS the intended "still processing" signal and update the ROADMAP SC#4 wording.

**Informational — `qdrantCli` not actually shared:**
The shared `qdrant.Client` created in setup.go (for the health gate) is never passed to `search.NewQdrantRepository` or `search.NewCompactMemoryQdrantIndexWithBatch` — both receive a `search.QdrantConfig` and create their own internal `qdrant.Client`. The comment at line 477 (`_ = qdrantCli // shared client used by search/compact consumers above`) is misleading. The net effect is 3 separate `http.Client` pools where 1 was planned. This is not a correctness bug but wastes connections on small deployments.

---

_Verified: 2026-05-10_
_Verifier: Claude (gsd-verifier)_
