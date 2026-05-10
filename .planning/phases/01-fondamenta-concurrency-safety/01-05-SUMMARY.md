---
phase: 01-fondamenta-concurrency-safety
plan: "05"
subsystem: telegram
tags: [integration, usergate, qdrant, health-gate, actor-pattern, concurrency]
dependency_graph:
  requires:
    - "01-01: internal/qdrant.Client, NewClient, WaitForReady"
    - "01-02: internal/concurrency.UserGate, New, Config"
    - "01-04: Bot.gate field, userGate() accessor, NewSessionStore variadic"
  provides:
    - "internal/telegram/setup.go: UserGate created with OnEvict + OnOverflow callbacks"
    - "internal/telegram/setup.go: shared qdrant.Client created once; WaitForReady health gate"
    - "internal/telegram/bot.go: Stop() calls gate.Close() before bot/scheduler shutdown"
  affects:
    - "cmd/aura/main.go: telegram.New() now blocks on Qdrant health gate (no main.go changes needed)"
tech_stack:
  added: []
  patterns:
    - "Shared Qdrant client created once in New(); health gate fires before returning Bot to caller"
    - "UserGate callbacks close over b after Bot struct literal -- avoids chicken-and-egg problem"
    - "OnOverflow goroutine pattern: Telegram API call in separate goroutine to avoid gate blocking"
    - "Stop() ordering: gate.Close() -> bot.Stop() -> sched.Stop() (T-01-21)"
key_files:
  created: []
  modified:
    - internal/telegram/setup.go
    - internal/telegram/bot.go
decisions:
  - "WaitForReady placed inside setup.go New() not main.go: telegram.New() already called before go bot.Start(); error propagates through existing startAura error handling"
  - "Shared qdrant.Client created for health gate only; search/compact consumers still use QdrantConfig (plan 03 preserved public API); client suppressed with _ = qdrantCli"
  - "No main.go changes needed: existing error handling for telegram.New() already exits with diagnostic message containing Qdrant endpoint and elapsed time"
  - "Bot struct literal created without sessions field; sessions set after UserGate creation with NewSessionStore(userGate) for gate-aware delegation"
  - "OnEvict logs eviction with userID only (T-01-22 no PII); calls sessionStore().Clear() (T-01-23 per-user only)"
metrics:
  duration: "10 minutes"
  completed_date: "2026-05-10T13:06:11Z"
  tasks_completed: 1
  tasks_total: 1
  files_created: 0
  files_modified: 2
---

# Phase 01 Plan 05: Final Integration Wiring Summary

**One-liner:** UserGate with real OnEvict/OnOverflow callbacks and shared Qdrant WaitForReady health gate wired into Bot construction in setup.go; Stop() closes the gate before bot/scheduler shutdown.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Wire UserGate + Qdrant health gate into setup.go; add gate.Close() to Stop() | 1127f64a | internal/telegram/setup.go, internal/telegram/bot.go |

## What Was Built

### setup.go changes

**Shared Qdrant client + WaitForReady health gate (D-20, QDRANT-01, T-01-19):**

Added immediately after the LLM client creation block:
- `qdrant.NewClient(...)` with `cfg.QdrantURL` and `cfg.QdrantAPIKey`
- `qdrant.WaitForReady(ctx, qcli, 120*time.Second)` with a 120s context timeout
- On health gate failure: `New()` returns an error containing the Qdrant endpoint and elapsed time
- The error propagates through `startAura`'s existing error handling which logs and exits with a diagnostic
- When `QDRANT_URL` is empty: health gate is skipped entirely (Qdrant is optional)
- On success: logs `"qdrant health gate passed"` before continuing

**UserGate creation with real callbacks (D-16, D-03, D-10):**

Created after the Bot struct literal (after `b := &Bot{...}`) so callbacks can safely close over `b`:

```
concurrency.New(concurrency.Config{
    InboxSize:         8,
    EvictionThreshold: 30 * time.Minute,
    SweepInterval:     60 * time.Second,
    OnEvict:           func(userID string) { ... logs eviction; b.sessionStore().Clear(userID) }
    OnOverflow:        func(userID string) { go func() { b.bot.Send(...) }() }
})
```

- `b.gate = userGate` -- wires gate field set in Plan 04
- `b.sessions = agentruntime.NewSessionStore(userGate)` -- gate-aware session store

### bot.go changes

**Stop() gate.Close() ordering (T-01-21):**

Added as the FIRST action in `Stop()`:
```go
if gate := b.userGate(); gate != nil {
    gate.Close()
}
```
This stops the InactivityTracker and cancels all actor goroutines before the Telegram bot and scheduler shut down (Pitfall 2 prevention from Plan 02).

## Phase 1 Integration Verification

All Phase 1 success criteria met:

| Criterion | Status |
|-----------|--------|
| Shared qdrant.Client created once in New() | DONE |
| WaitForReady 120s health gate before bot.Start() | DONE (inside New()) |
| UserGate created with OnEvict + OnOverflow | DONE |
| OnEvict persists session via sessionStore().Clear() | DONE |
| OnOverflow sends Telegram notice in goroutine | DONE |
| b.gate set; b.sessions = NewSessionStore(userGate) | DONE |
| Stop() calls gate.Close() first | DONE |
| go build ./internal/telegram/ | PASS |
| go vet ./internal/telegram/ | PASS |
| go test ./internal/telegram/ | PASS (2.27s) |
| go test ./internal/concurrency/ | PASS |
| go test ./internal/qdrant/ | PASS |
| go test ./internal/agentruntime/ | PASS |
| No Aura deps in internal/concurrency/ | PASS |
| No sync.Map in tracker.go | PASS |
| No duplicate Qdrant HTTP in tools | PASS |

## Deviations from Plan

### Task 2 (main.go) -- not needed

The plan had a Task 2 targeting main.go for the Qdrant health gate. Analysis showed no main.go changes are required:
- The health gate is placed inside `setup.go`'s `New()`, which is called from `main.go`'s `startAura()` BEFORE `go bot.Start()`
- `telegram.New()` returning an error already causes `startAura` to return with `fmt.Errorf("create telegram bot: %w", err)`
- The caller (`runHeadless`/`runWithTray`) already calls `activeLogger.Error("aura startup failed", ...)` and `os.Exit(1)`
- The full diagnostic message seen by the operator: `"create telegram bot: qdrant health gate failed: qdrant not ready after 2m0s at http://qdrant:6333 (elapsed: 2m0s)"`

This satisfies D-20 ("Aura exits with a clear diagnostic message indicating the Qdrant endpoint and elapsed wait time") without any main.go edits.

**Documentation deviation:** Task 2 became a no-op. No CLAUDE.md violations -- the change was correctly scoped by analyzing actual code flow rather than blindly following the plan's file list.

## Known Stubs

None. All callbacks are fully implemented with real behavior (session clear, Telegram send).

## Threat Flags

No new threat surface beyond what is declared in the plan's threat model. All STRIDE mitigations implemented:

| Threat ID | Status |
|-----------|--------|
| T-01-19 | Mitigated: WaitForReady called with 120s context + 120s timeout arg; returns fmt.Errorf on failure |
| T-01-20 | Mitigated: OnOverflow launches `go func(){}()` -- single Send call, no goroutine accumulation |
| T-01-21 | Mitigated: gate.Close() first in Stop(); InactivityTracker + actors fully stopped before bot |
| T-01-22 | Mitigated: OnEvict logs only userID (numeric Telegram ID), no conversation content |
| T-01-23 | Mitigated: sessionStore().Clear(userID) evicts only the specific user being evicted |

## Self-Check: PASSED

- [x] internal/telegram/setup.go modified (qdrant client + health gate + UserGate wiring)
- [x] internal/telegram/bot.go modified (gate.Close() in Stop())
- [x] Commit 1127f64a exists
- [x] go build ./internal/telegram/ passes
- [x] go vet ./internal/telegram/ passes
- [x] go test -count=1 -short ./internal/telegram/ passes (ok 2.27s)
- [x] go test -count=1 -short ./internal/qdrant/ ./internal/concurrency/ ./internal/agentruntime/ pass
- [x] No new deletions in commit (verified via git diff --diff-filter=D)
- [x] concurrency package has zero Aura imports
- [x] No sync.Map in tracker.go (0 matches)
- [x] No duplicate Qdrant HTTP methods in tools (0 matches)
