---
phase: 1
slug: fondamenta-concurrency-safety
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-10
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|

| **Framework** | Go testing (stdlib) + race detector |
| **Config file** | none — Go test files inline |
| **Quick run command** | `go test -race -count=1 -short ./internal/concurrency/... ./internal/qdrant/...` |
| **Full suite command** | `go test -race -count=10 ./internal/concurrency/... ./internal/qdrant/...` |
| **Estimated runtime** | ~30 seconds (quick), ~120 seconds (full with -count=10) |

---

## Sampling Rate

- **After every task commit:** Run `go test -race -count=1 -short ./internal/concurrency/... ./internal/qdrant/...`
- **After every plan wave:** Run `go test -race -count=10 ./internal/concurrency/... ./internal/qdrant/...`
- **Before `/gsd-verify-work`:** Full suite must be green + race detector clean
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 01-01-01 | 01 | 1 | CONC-01 | T-01-01 | Same-user messages serialized via actor inbox | unit + race | `go test -race -run TestSequentialProcessing -count=50 ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-01-02 | 01 | 1 | CONC-01 | T-01-02 | Different-user messages process concurrently | unit + race | `go test -race -run TestConcurrentUsers -count=50 ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-01-03 | 01 | 1 | CONC-01 | T-01-03 | Overflow drops oldest + calls OnOverflow callback | unit | `go test -race -run TestOverflowDropOldest ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-02-01 | 02 | 1 | CONC-02 | T-01-04 | TryAcquire returns true when inbox has space | unit | `go test -race -run TestTryAcquire ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-02-02 | 02 | 1 | CONC-02 | T-01-05 | TryAcquire returns false when inbox full (non-blocking) | unit + race | `go test -race -run TestTryAcquireFull -count=50 ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-02-03 | 02 | 1 | CONC-02 | T-01-06 | Notification dispatchTask calls TryAcquire, returns nil on false | integration | `go test -race -run TestNotificationNoDeadlock ./internal/telegram/` | ❌ W0 | ⬜ pending |
| 01-03-01 | 03 | 2 | CONC-03 | T-01-07 | InactivityTracker evicts after threshold exceeded | unit | `go test -race -run TestEviction ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-03-02 | 03 | 2 | CONC-03 | T-01-08 | Active user (recent touch) is NOT evicted | unit | `go test -race -run TestNoEvictionActive ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-03-03 | 03 | 2 | CONC-03 | T-01-09 | Eviction cancels context + calls OnEvict callback | unit | `go test -race -run TestEvictionCleanup ./internal/concurrency/` | ❌ W0 | ⬜ pending |
| 01-03-04 | 03 | 2 | CONC-03 | — | InactivityTracker does NOT use sync.Map.Range | inspection | grep for `sync.Map` and `Range` in `internal/concurrency/` | N/A | ⬜ pending |
| 01-04-01 | 04 | 2 | QDRANT-01 | T-01-10 | Health gate blocks until /readyz returns 2xx | unit (mock) | `go test -run TestWaitForReady ./internal/qdrant/` | ❌ W0 | ⬜ pending |
| 01-04-02 | 04 | 2 | QDRANT-01 | T-01-11 | Health gate times out with diagnostic after timeout | unit (mock) | `go test -run TestWaitForReadyTimeout ./internal/qdrant/` | ❌ W0 | ⬜ pending |
| 01-04-03 | 04 | 2 | QDRANT-01 | T-01-12 | Warm check: points_count > 0 skips re-embed | unit (mock) | `go test -run TestWarmCheckSkipped ./internal/qdrant/` | ❌ W0 | ⬜ pending |
| 01-04-04 | 04 | 2 | QDRANT-01 | T-01-13 | Warm check: points_count == 0 triggers re-embed | unit (mock) | `go test -run TestWarmCheckReEmbed ./internal/qdrant/` | ❌ W0 | ⬜ pending |
| 01-04-05 | 04 | 2 | QDRANT-01 | T-01-14 | Warm check: collection 404 triggers re-embed (first startup) | unit (mock) | `go test -run TestWarmCheckNotFound ./internal/qdrant/` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/concurrency/gate_test.go` — covers CONC-01 (serial, concurrent, overflow), CONC-02 (TryAcquire)
- [ ] `internal/concurrency/tracker_test.go` — covers CONC-03 (eviction, active skip, cleanup callback)
- [ ] `internal/qdrant/client_test.go` — covers QDRANT-01 (health gate, warm check, collection info)
- [ ] `internal/qdrant/mock_server_test.go` — httptest server for Qdrant API mock
- [ ] `internal/telegram/concurrency_integration_test.go` — covers end-to-end CONC-01/CONC-02 with real Bot

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| "Still processing" response within timeout | CONC-01 | Requires real Telegram round-trip latency | Send rapid messages from same user; verify queued message receives "still processing" notice within configured timeout |
| Telegram overflow notice delivery | CONC-01 | Requires real Telegram bot instance | Flood user inbox beyond capacity 8; verify overflow notice arrives in Telegram chat |
| Scheduler retry on TryAcquire failure | CONC-02 | Requires real scheduler tick cycle | Block user gate; fire scheduler reminder; verify scheduler retries on next 30s tick |
| Startup diagnostic message on Qdrant timeout | QDRANT-01 | Requires real Qdrant unavailability | Stop Qdrant; start Aura; verify clear diagnostic with endpoint and elapsed time |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
