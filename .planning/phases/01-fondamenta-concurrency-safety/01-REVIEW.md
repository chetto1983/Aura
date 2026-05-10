---
phase: 01-fondamenta-concurrency-safety
reviewed: 2026-05-10T00:00:00Z
depth: standard
files_reviewed: 12
files_reviewed_list:
  - internal/search/qdrant.go
  - internal/search/qdrant_test.go
  - internal/tools/registry_search_vector.go
  - internal/tools/registry_search_vector_test.go
  - internal/search/compact_qdrant.go
  - internal/search/compact_qdrant_test.go
  - internal/config/config.go
  - internal/config/env.go
  - internal/concurrency/types.go
  - internal/concurrency/gate.go
  - internal/concurrency/gate_test.go
  - internal/telegram/setup.go
findings:
  critical: 1
  warning: 6
  info: 5
  total: 12
status: issues_found
---

# Phase 01: Code Review Report

**Reviewed:** 2026-05-10
**Depth:** standard
**Files Reviewed:** 12
**Status:** issues_found

## Summary

The phase 01 gap-closure work covers two themes: (1) warm-cache short-circuits in three Qdrant rebuild paths (`RebuildQdrantWikiDocuments`, `toolVectorIndex.Build`, `CompactMemoryQdrantIndex.Recreate`) gated on `CollectionInfo.PointsCount > 0`, and (2) a per-user `QueueNoticeAfter` timer in `concurrency.UserGate` plus four new env-tunable gate config knobs.

The warm-cache logic is symmetric across the three call sites and well-tested (cold/warm/not-found/probe-error matrices). The gate timer is mostly correct: the `startedCh` ownership rules between `Acquire`, `runActor`, and `dropOldestAndNotify` avoid double-close, and the documented Pitfall 4 hand-off (timer goroutine to caller goroutine) is preserved in `setup.go`.

However, there is **one critical correctness issue** in the gate timer: queued entries whose `startedCh` is never closed (because the actor was cancelled by Evict/Close before processing them) leak their timer goroutines for up to `QueueNoticeAfter`, and worse — the timer fires `OnQueueNotice` against an evicted/replaced user, producing a spurious "still working" Telegram message after the user was already dropped. Several lower-severity issues affect HTTP probe robustness, env-config validation parity, and edge cases in the test suite.

## Critical Issues

### CR-01: `runQueueNoticeTimer` leaks and fires spurious notices for evicted/closed users

**File:** `internal/concurrency/gate.go:104-117` (and trace through `gate.go:172-185`, `gate.go:137-158`, `gate.go:219-239`)

**Issue:** When `Evict` or `Close` cancels an actor's context, `runActor` exits via `case <-actor.ctx.Done()` without draining `actor.inbox`. Any entries queued behind the one currently being processed remain in the channel, and their `startedCh` is **never closed** by anyone. Each such entry has a live `runQueueNoticeTimer` goroutine spawned by `Acquire`, which:

1. Continues to wait up to `QueueNoticeAfter` (default 30s, but env-tunable up to any value).
2. Then fires `g.config.OnQueueNotice(userID)` — even though the user has been evicted (and possibly re-created with a fresh actor in the meantime).
3. In `telegram/setup.go:474-488`, that callback unconditionally sends *"Still working on your previous message — I'll get to this one shortly."* to the user, who in fact has nothing being processed.

This is a real-world misbehavior: after `InactivityTracker.sweep` evicts a quiet user, any in-flight queue-notice timer for a never-processed entry fires a misleading Telegram message. The same race also occurs around `Close()` (graceful shutdown will not wait for these timers because they are not registered with `g.wg`).

Secondary concern: if the same `userID` re-enters the gate before the timer fires (a fresh actor is spun up), the notice fires against the *new* actor — implying the new conversation is delayed when it is not.

**Fix:**

(a) Track the timer goroutines so `Close` waits for them, and (b) close `startedCh` for any entries left in the inbox when the actor exits. Sketch:

```go
// gate.go: drain remaining entries' startedCh on actor shutdown so timers exit cleanly.
func (g *UserGate) runActor(actor *userActor) {
    defer g.wg.Done()
    defer close(actor.done)
    defer func() {
        // Drain inbox: cancel queue-notice timers for any entries that never started.
        for {
            select {
            case entry := <-actor.inbox:
                if entry.startedCh != nil {
                    close(entry.startedCh)
                }
            default:
                return
            }
        }
    }()
    for {
        select {
        case <-actor.ctx.Done():
            return
        case entry, ok := <-actor.inbox:
            if !ok {
                return
            }
            if entry.startedCh != nil {
                close(entry.startedCh)
            }
            entry.Process(actor.ctx)
            g.tracker.Touch(actor.userID)
        }
    }
}
```

Additionally, `runQueueNoticeTimer` should consult the actor context (or a derived gate-shutdown signal) so it cannot fire after `Close`:

```go
func (g *UserGate) runQueueNoticeTimer(userID string, startedCh <-chan struct{}, actorCtx context.Context) {
    timer := time.NewTimer(g.config.QueueNoticeAfter)
    defer timer.Stop()
    select {
    case <-timer.C:
        if g.config.OnQueueNotice != nil {
            g.config.OnQueueNotice(userID)
        }
    case <-startedCh:
    case <-actorCtx.Done(): // evicted or shutdown — do NOT fire
    }
}
```

A test case should be added: enqueue two entries with a blocking first entry, then call `Evict`, wait `2 * QueueNoticeAfter`, and assert `OnQueueNotice` was *not* called.

## Warnings

### WR-01: `int(info.PointsCount)` truncates `uint64` on 32-bit platforms

**File:** `internal/search/qdrant.go:190`

**Issue:** `DocsIndexed: int(info.PointsCount)` — `CollectionInfo.PointsCount` is `uint64` (see `internal/qdrant/types.go:21`). On any 32-bit build (or a wasm/js target), `int` is 32 bits, so values above `math.MaxInt32` wrap. The same conversion exists in two log statements at `qdrant.go:186` (formatted via slog so safer there). Aura is unlikely to ever index >2 billion points, so impact is low — but the conversion is unguarded.

**Fix:** Either change `QdrantRebuildReport.DocsIndexed` to `uint64`, or saturate explicitly:

```go
docs := int(info.PointsCount)
if info.PointsCount > math.MaxInt32 {
    docs = math.MaxInt32
}
return QdrantRebuildReport{
    Collection:   collection,
    PagesIndexed: pages,
    DocsIndexed:  docs,
    VectorSize:   0,
}, nil
```

### WR-02: `getEnvDuration` rejects negative values silently but tests do not cover this

**File:** `internal/config/env.go:71-81` and `internal/config/config.go:286-293`

**Issue:** `getEnvDuration` falls back to default when `time.ParseDuration` errors *or* when the parsed value is `< 0`. There is no log/warning when an operator misconfigures `AURA_INACTIVITY_THRESHOLD=-30s` — they get the silent default. There is also no test for the negative-duration path or the unparseable-string path. Worse, the gate's defensive `if cfg.EvictionThreshold <= 0` in `concurrency.New` means an operator setting `AURA_INACTIVITY_THRESHOLD=0` (intent: disable inactivity eviction) silently gets the 30-minute default applied a second time, contrary to the comment in config.go that claims "defaults come from Default* constants applied inside Load() via getEnvInt / getEnvDuration".

**Fix:** Either log a warning when `getEnvDuration` rejects a value (parity with how the rest of the codebase surfaces config issues via `logger.Warn`), or push the validation to `Load()` and remove the duplicate guard in `concurrency.New`. Add unit tests in `internal/config/env_test.go` for `-30s`, `garbage`, and the empty string paths.

### WR-03: Warm-cache short-circuit ignores schema/vector-size drift

**File:** `internal/search/qdrant.go:172-193`, `internal/tools/registry_search_vector.go:126-137`, `internal/search/compact_qdrant.go:60-70`

**Issue:** When `CollectionInfo.PointsCount > 0`, the rebuild is skipped — but there is no check that the *existing* collection's vector size, distance, or payload schema matches what the current code would produce. If a user changes `EMBEDDING_MODEL` to one that produces a different vector dimensionality (e.g. 1024 → 1536), the collection retains the old 1024-dim vectors and every subsequent `Search` will fail with a Qdrant dimension mismatch. The warm-cache hit hides the regression.

This is silently load-bearing on operators not changing embedding models — but the codebase explicitly supports swapping `EMBEDDING_MODEL` at runtime via the dashboard settings page, so this is a realistic foot-gun.

**Fix:** Probe `CollectionInfo` for vector size (Qdrant's `/collections/{name}` already returns `config.params.vectors.size`), or compute one embedding upfront and compare to the collection's declared size. On mismatch, log a warning and proceed to the cold-path rebuild (drop+create+upsert). At minimum, document the behavior and the required ops procedure (`DELETE /collections/aura_memory_v1` before changing models) in `WARM_CACHE.md` or the relevant runbook.

### WR-04: `idx.mu.Lock()` held across HTTP I/O in `toolVectorIndex.Build` blocks all `Search`

**File:** `internal/tools/registry_search_vector.go:106-189`

**Issue:** `Build` acquires `idx.mu.Lock()` at line 110 and holds it through the warm-cache probe HTTP call (line 128), the embedding HTTP call (line 144), and three more Qdrant calls (delete/create/upsert). `Search` at line 195 takes `idx.mu.RLock()`. During the cold-path rebuild on startup (which may take many seconds for thousands of tools, though in practice tool count is small), all `Search` calls block.

This is technically a performance issue (out of v1 scope), but it also creates a correctness concern: if startup is slow and a user message arrives mid-build, `Search` blocks within the agent loop for the entire embedding round-trip rather than returning a "not ready" error.

**Fix:** Restructure so the lock guards only the in-memory state mutation (`docCount`, `lastRebuild`, `lastError`), not the network I/O. Pattern:

```go
func (idx *toolVectorIndex) Build(ctx context.Context, docs []toolVectorDoc) error {
    if idx == nil || idx.cfg.Backend == "fts" {
        return nil
    }
    // ... do all I/O without lock ...
    idx.mu.Lock()
    idx.docCount = len(docs)
    idx.lastRebuild = time.Now()
    idx.lastError = nil
    idx.mu.Unlock()
    return nil
}
```

This also requires guarding against concurrent `Build` calls — use a separate `buildMu sync.Mutex` for serialization.

### WR-05: `loadWikiDocuments` error in warm-cache path leaves `pages` zero, contradicting log message

**File:** `internal/search/qdrant.go:181-192`

**Issue:** When `loadWikiDocuments` errors during the warm-cache hit, the code logs the error and proceeds with `pages = 0` (Go's zero-value for the unset return). The next line logs `"qdrant warm-cache hit, skipping rebuild" ... "pages_on_disk", pages` — which will silently report `pages_on_disk=0` even when the disk has many pages but the listing failed. The returned report's `PagesIndexed` is also `0`, which downstream consumers (e.g. `/api/health`) may interpret as "no pages on disk" rather than "could not enumerate pages".

**Fix:** Either reverse the logic — surface the load error as a non-fatal report field — or omit `pages_on_disk` from the log line on load failure:

```go
_, pages, loadErr := loadWikiDocuments(wikiDir, logger)
if loadErr != nil {
    logger.Warn("warm-cache hit: pages_on_disk count unavailable", "error", loadErr, "collection", collection)
    pages = -1 // sentinel for "unknown"
}
report := QdrantRebuildReport{
    Collection:   collection,
    DocsIndexed:  int(info.PointsCount),
    VectorSize:   0,
}
if pages >= 0 {
    report.PagesIndexed = pages
    logger.Info("qdrant warm-cache hit", "pages_on_disk", pages, ...)
} else {
    logger.Info("qdrant warm-cache hit (pages count unavailable)", ...)
}
return report, nil
```

### WR-06: Test `TestQueueNoticeDoesNotFireOnOverflowDrop` is racy and asserts a weaker invariant than its name promises

**File:** `internal/concurrency/gate_test.go:621-716`

**Issue:** The test's stated purpose is to verify that `OnQueueNotice` is NOT fired for an entry that was dropped by `dropOldestAndNotify`. The actual assertion (`noticeCount > 1`) only checks that *at most* one notice fired total — the surviving (third) entry is allowed to fire one. This means the test passes even if the dropped entry's timer fires erroneously, *as long as* the surviving entry's timer happens not to fire. The intended invariant (dropped entry's timer never fires) is not isolated from the surviving entry's timer.

The narrative comments inside the test (lines 689-703) acknowledge this design limitation but do not provide a mitigation. With `threshold=200ms` and `time.Sleep(500ms)` while `blocker` is held, both timers will typically fire — so the `noticeCount > 1` assertion fails non-deterministically depending on goroutine scheduling.

**Fix:** Either:
- Set `QueueNoticeAfter` long enough that only the dropped entry could plausibly fire (e.g. 50ms threshold, 500ms wait) and unblock the actor *before* the wait so the third entry processes, then assert `noticeCount == 0`.
- Use a unique marker per entry (e.g., per-entry callback or separate `userID` per entry) to distinguish which timer fired.
- Verify directly that `dropOldestAndNotify` closed `startedCh` by inspecting goroutine count or using a test-only hook on `Entry.startedCh`.

### WR-07: `qdrantRepository.IndexWikiPages` clears `r.indexed` to true even on warm-cache hit, but `Index` resets it on every single-doc upsert

**File:** `internal/search/qdrant.go:126-135`, `qdrant.go:95-124`

**Issue:** `IndexWikiPages` calls `rebuildQdrantWikiDocumentsWithClient` and sets `r.indexed = true` regardless of warm-cache hit vs cold-path rebuild — fine. But `Index` (single-doc upsert) calls `r.client.CreateCollection` unconditionally on every call (line 103), with no check that the collection already exists. Qdrant's `CreateCollection` is idempotent at the HTTP level (returns 200 if collection exists with same params, or an error if dims differ), but this is wasted overhead and, more importantly, susceptible to the same dim-mismatch silent failure described in WR-03 — `CreateCollection` may *fail* if the existing collection has a different vector size, and `Index` propagates that error to the caller, but neither logs a hint that an embedding-model swap is the likely cause.

**Fix:** Defer `CreateCollection` to a `sync.Once` guarded by `r.indexed`, and on dim-mismatch error, surface a clear diagnostic suggesting the operator delete the collection or revert the embedding model.

## Info

### IN-01: `qdrantPointID` and `toolQdrantPointID` duplicate near-identical UUID-construction logic

**File:** `internal/search/qdrant.go:315-323`, `internal/tools/registry_search_vector.go:323-331`

**Issue:** Two different packages each implement the same SHA-256→UUID-v5-style transformation, with the only difference being the prefix (`""` vs `"tool:"`). Drift risk — if one is patched (e.g. to fix a bit-pattern bug in the variant byte), the other will diverge.

**Fix:** Hoist a shared helper into `internal/qdrant` (e.g. `qdrant.PointIDFromString(prefix, id string) string`) and call it from both sites.

### IN-02: `getEnvDuration` comment references "FOLLOW EXISTING PATTERNS" rationale that is opaque

**File:** `internal/config/env.go:67-70`

**Issue:** The doc comment cites CLAUDE.md's FOLLOW EXISTING PATTERNS rule, but the actual decision being explained (no `strings.TrimSpace` on the env value) is not justified by user value — it's a stylistic mirror of `getEnvInt`. A reader would benefit more from a one-line note: *"trailing whitespace in duration env vars yields ParseDuration error → fallback, which is fine."*

**Fix:** Trim the comment to its functional content; drop the meta-reference to CLAUDE.md.

### IN-03: `isNotFoundErr` uses string matching to detect 404 — fragile

**File:** `internal/search/compact_qdrant.go:312-320`

**Issue:** `isNotFoundErr` substrings the error message for `"404"` or `"not found"`. This is intentional (the qdrant error wrapping in `client.go` uses `fmt.Errorf` with the response status), but it's fragile to error wrapping changes. A typed sentinel error from `internal/qdrant` would be more durable.

**Fix:** Define `var ErrCollectionNotFound = errors.New("qdrant: collection not found")` in `internal/qdrant/client.go`, return it explicitly when the upsert/search response is 404, and use `errors.Is` here.

### IN-04: Test files duplicate large `httptest.NewServer` switch handlers across 5+ tests

**File:** `internal/search/qdrant_test.go:118-401`, `internal/search/compact_qdrant_test.go:159-391`, `internal/tools/registry_search_vector_test.go:179-399`

**Issue:** Each warm-cache/cold-path/not-found/info-error test case re-declares the full handler switch. ~80% of the bytes in these test files are copies of each other. A drift here would silently cause one test to assert a different protocol than its peer.

**Fix:** Extract a `qdrantTestServer(t, opts)` helper that takes a struct describing the desired responses (`PointsCount`, `CollectionInfoStatusCode`, `EmbedCallback`) and returns a configured `*httptest.Server`. Reduces ~400 lines per file by half.

### IN-05: Comment in `gate.go:101` wrongly claims `runQueueNoticeTimer` is called "in a separate goroutine by Acquire" but the call site uses `go` keyword inline

**File:** `internal/concurrency/gate.go:87-89` and gate.go:101-103

**Issue:** Minor doc inconsistency. The comment "Called in a separate goroutine by Acquire after successful enqueue (Pitfall 4)" is correct; `go g.runQueueNoticeTimer(...)` at line 89 is the spawn site. But the indirection makes it slightly harder to verify Pitfall 4 compliance at-a-glance for a future reviewer. Consider naming the spawn explicitly:

```go
case actor.inbox <- entry:
    if startedCh != nil {
        // Pitfall 4: spawn timer in its own goroutine; never block Acquire.
        go g.runQueueNoticeTimer(userID, startedCh)
    }
    return nil
```

(The current comment achieves this — this is purely a "make it easier to grep" suggestion.)

---

_Reviewed: 2026-05-10_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
