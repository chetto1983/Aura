---
phase: 01-fondamenta-concurrency-safety
plan: "07"
subsystem: concurrency
tags: [gap-closure, conc-01, queue-notice, config, telegram]
dependency_graph:
  requires: [01-02, 01-05]
  provides: [CONC-01-gap-closure, queue-notice-timer, env-tunable-gate]
  affects: [internal/config, internal/concurrency, internal/telegram]
tech_stack:
  added: []
  patterns: [actor-inbox, timer-goroutine, channel-cancellation, tdd-red-green]
key_files:
  created: []
  modified:
    - internal/config/env.go
    - internal/config/config.go
    - internal/concurrency/types.go
    - internal/concurrency/gate.go
    - internal/concurrency/gate_test.go
    - internal/telegram/setup.go
decisions:
  - "QueueNoticeAfter=0 disables the timer goroutine entirely (no channel allocation, no goroutine spawn)"
  - "dropOldestAndNotify closes dropped entry startedCh to prevent spurious OnQueueNotice on overflow path"
  - "go vet/build ./... fails in worktree due to pre-existing untracked icon_app.ico; touched packages all pass"
metrics:
  duration: ~25m
  completed: "2026-05-10"
  tasks_completed: 5
  tasks_total: 5
  files_changed: 6
---

# Phase 01 Plan 07: Queue-Notice Timer + Env-Tunable Gate Config Summary

One-liner: Per-entry queue-notice timer (startedCh cancellation) with four AURA_INBOX_* env vars wiring config into concurrency.UserGate.

## Tasks Completed

| # | Task | Commit | Status |
|---|------|--------|--------|
| 1 | Add four env-backed config fields + getEnvDuration helper | 67495111 | Done |
| 2 | Extend concurrency.Config with QueueNoticeAfter/OnQueueNotice; Entry.startedCh | 3835a13a | Done |
| 3 | Implement gate.go queue-notice timer + runActor/dropOldestAndNotify startedCh logic; 5 TDD tests | f225f064 | Done |
| 4 | Wire cfg fields + OnQueueNotice callback into setup.go | cf299a8e | Done |
| 5 | Whole-tree validation (touched packages) | — | Done |

## What Was Built

### Task 1: Config fields + getEnvDuration
- Added `getEnvDuration(key, fallback)` to `internal/config/env.go` following the existing `getEnvInt`/`getEnvBool`/`getEnvFloat` pattern (bare `os.Getenv`, no `strings.TrimSpace`, returns fallback on empty or parse error, rejects negative values).
- Added four `Default*` constants: `DefaultInboxSize=8`, `DefaultInboxQueueNoticeAfter=30s`, `DefaultInactivityThreshold=30m`, `DefaultInactivitySweepInterval=60s`.
- Added four fields to `Config` struct: `InboxSize int`, `InboxQueueNoticeAfter time.Duration`, `InactivityThreshold time.Duration`, `InactivitySweepInterval time.Duration` with `envconfig:"AURA_*"` tags.
- `Load()` populates them via `getEnvInt`/`getEnvDuration` with belt-and-braces `InboxSize<=0` guard.

### Task 2: concurrency.Config extension
- `Config.QueueNoticeAfter time.Duration`: feature disabled when <= 0.
- `Config.OnQueueNotice func(userID string)`: timer callback; must hand off to goroutine for I/O (Pitfall 4).
- `Entry.startedCh chan struct{}`: unexported; set by `Acquire`, closed by `runActor` or `dropOldestAndNotify`; callers zero-initialize.

### Task 3: gate.go implementation (TDD)
- `Acquire`: allocates `startedCh` only when `QueueNoticeAfter > 0 && OnQueueNotice != nil`; spawns `go g.runQueueNoticeTimer(userID, startedCh)` after successful enqueue.
- `runQueueNoticeTimer`: `select { case <-timer.C: OnQueueNotice(userID); case <-startedCh: /* no notice */ }`.
- `runActor`: `if entry.startedCh != nil { close(entry.startedCh) }` immediately before `entry.Process(...)`.
- `dropOldestAndNotify`: `if dropped.startedCh != nil { close(dropped.startedCh) }` when pulling dropped entry.
- Five tests covering all branches (threshold fires, early dequeue no-fire, overflow drop no-fire, zero disabled, nil callback safe).

### Task 4: setup.go wiring
- Replaced hardcoded `InboxSize: 8`, `EvictionThreshold: 30 * time.Minute`, `SweepInterval: 60 * time.Second` with `cfg.InboxSize`, `cfg.InactivityThreshold`, `cfg.InactivitySweepInterval`.
- Added `QueueNoticeAfter: cfg.InboxQueueNoticeAfter`.
- Added `OnQueueNotice` callback matching `OnOverflow` pattern: `go func() { b.bot.Send(...) }()`.
- Notice text: `"Still working on your previous message -- I'll get to this one shortly."`

## Deviations from Plan

None — plan executed exactly as written.

**Pre-existing worktree issue (out of scope):** `go vet ./...` and `go build ./...` fail in the worktree because `internal/tray/icon_app.ico` is an untracked file in the main repo that does not exist in the worktree. This failure predates all changes in this plan (verified by checking the main repo where `go build ./...` passes with the icon present on disk). All three touched packages (`internal/config`, `internal/concurrency`, `internal/telegram`) vet and build cleanly.

## Test Results

```
ok  github.com/aura/aura/internal/config       0.291s
ok  github.com/aura/aura/internal/concurrency  0.825s
ok  github.com/aura/aura/internal/telegram     2.151s
```

Five new tests:
- `TestQueueNoticeFiresAfterThreshold` — PASS
- `TestQueueNoticeDoesNotFireOnEarlyDequeue` — PASS
- `TestQueueNoticeDoesNotFireOnOverflowDrop` — PASS
- `TestQueueNoticeDisabledWhenZero` — PASS
- `TestQueueNoticeNilCallbackIsSafe` — PASS

## Known Stubs

None. All env fields are wired end-to-end from env → config.Load() → concurrency.Config → gate behaviour.

## Threat Flags

No new security surface introduced beyond what the threat model in the plan already covers (T-01-27 through T-01-32). All mitigations implemented as planned:
- T-01-27: `QueueNoticeAfter <= 0` guard in `Acquire` prevents timer goroutine spawn.
- T-01-28: `OnQueueNotice` hands off to inner `go func()` in setup.go so the gate's timer goroutine is never blocked on Telegram Send.
- T-01-29: `InboxSize <= 0` guard in `Load()` and pre-existing guard in `New()`.
- T-01-30: Log lines use only numeric `userID`, not message content.

## Success Criteria Verification

- [x] `internal/config/config.go` exposes InboxSize, InboxQueueNoticeAfter, InactivityThreshold, InactivitySweepInterval with AURA_* envconfig tags and Default* constants
- [x] `internal/config/env.go` has `getEnvDuration` used by Load()
- [x] `concurrency.Config` has QueueNoticeAfter and OnQueueNotice with semantic documentation
- [x] `concurrency.Entry` has unexported `startedCh chan struct{}`
- [x] `UserGate.Acquire` spawns `runQueueNoticeTimer` only when `QueueNoticeAfter > 0 && OnQueueNotice != nil`
- [x] `runQueueNoticeTimer` selects on `<-timer.C` vs `<-startedCh`, fires OnQueueNotice only on timer branch
- [x] `runActor` closes `entry.startedCh` immediately before `entry.Process` (when non-nil)
- [x] `dropOldestAndNotify` closes dropped entry's `startedCh` when non-nil
- [x] Five new tests cover all branches
- [x] `internal/telegram/setup.go` reads all four fields from cfg; no hardcoded 8/30m/60s remain
- [x] `OnQueueNotice` callback hands off to separate goroutine before `b.bot.Send`
- [x] No existing test was modified
- [x] Touched package tests exit 0

## Self-Check: PASSED

Files exist:
- FOUND: internal/config/config.go (has AURA_INBOX_SIZE, AURA_INBOX_QUEUE_NOTICE_AFTER, AURA_INACTIVITY_THRESHOLD, AURA_INACTIVITY_SWEEP_INTERVAL)
- FOUND: internal/config/env.go (has getEnvDuration)
- FOUND: internal/concurrency/types.go (has QueueNoticeAfter, OnQueueNotice, startedCh)
- FOUND: internal/concurrency/gate.go (has runQueueNoticeTimer, startedCh close in runActor and dropOldestAndNotify)
- FOUND: internal/concurrency/gate_test.go (has 5 new TestQueueNotice* functions)
- FOUND: internal/telegram/setup.go (has cfg.InboxSize, cfg.InactivityThreshold, cfg.InactivitySweepInterval, cfg.InboxQueueNoticeAfter, OnQueueNotice callback)

Commits exist:
- FOUND: 67495111 (feat(01-07): add four env-backed gate config fields and getEnvDuration helper)
- FOUND: 3835a13a (feat(01-07): extend concurrency.Config with QueueNoticeAfter/OnQueueNotice; add Entry.startedCh)
- FOUND: f225f064 (feat(01-07): implement queue-notice timer in UserGate; add 5 TDD tests)
- FOUND: cf299a8e (feat(01-07): wire cfg gate fields + OnQueueNotice callback into setup.go)
