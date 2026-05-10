# Phase 1: Fondamenta (Concurrency + Qdrant Readiness) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-10
**Phase:** 1-Fondamenta (Concurrency + Qdrant Readiness)
**Areas discussed:** Queue behavior & serialization, Notification paths, Eviction strategy, Gate API design, Qdrant readiness contract

---

## Queue Behavior & Serialization

| Option | Description | Selected |
|--------|-------------|----------|
| Stateful goroutine (actor) | One goroutine per user with channel inbox, private state, implicit serialization | ✓ |
| KeyedMutex (per-user lock) | Map of per-user sync.Mutex, simpler but no natural queuing | |
| Channel semaphore (capacity 1) | Channel-based gate, buffered channel of capacity 1 | |

**User's choice:** Stateful goroutine (actor pattern)
**Notes:** Researched best practices: actor pattern is the dominant 2025-2026 approach for per-entity state serialization. Go stdlib `TryLock` (since 1.18) was added specifically for the notification deadlock problem.

| Option | Description | Selected |
|--------|-------------|----------|
| Enqueue silently | Buffered channel, no immediate feedback, user sees typing indicator when processing starts | ✓ |
| Enqueue + acknowledge | Queue and immediately send Telegram acknowledgment | |
| Reject with feedback | No queuing, immediate rejection telling user to retry | |

**User's choice:** Enqueue silently

| Option | Description | Selected |
|--------|-------------|----------|
| Generous buffer, no timeout | Buffer 8, drop oldest when full with notice, no deadline | ✓ |
| Buffer + per-message deadline | Configurable deadline (30s), timed-out messages get notice to resend | |
| Tight buffer (no queueing) | Buffer of 1, no queuing at all | |

**User's choice:** Generous buffer (8), drop oldest, no timeout

| Option | Description | Selected |
|--------|-------------|----------|
| Spawn on first use, evict on idle | Goroutine created on first Acquire, destroyed on eviction | ✓ |
| Spawn/drain/terminate cycle | Goroutine self-terminates when inbox drained | |
| Spawn on use, idle drain N ticks | Self-terminates after N empty inbox polls | |

**User's choice:** Spawn on first use, evict on idle

---

## Notification Paths

| Option | Description | Selected |
|--------|-------------|----------|
| Drop + scheduler retry | TryAcquire fails → drop, scheduler retries next tick (30s) | ✓ |
| Try inbox, fallback to direct | Try inbox, fallback to direct Telegram delivery | |
| Dual-channel inbox | Separate notification channel alongside message inbox | |

**User's choice:** Drop + scheduler retry

| Option | Description | Selected |
|--------|-------------|----------|
| TryAcquire via inbox channel | Non-blocking send to same inbox channel | ✓ |
| Bypass gate — direct Telegram | Scheduler sends directly, no gate interaction | |
| Separate notification channel | Dedicated notification-only channel | |

**User's choice:** TryAcquire via inbox channel

| Option | Description | Selected |
|--------|-------------|----------|
| FIFO — no special handling | Inbox order determines delivery order, no behavioral difference | ✓ |
| Deliver inline, continue | Notification delivered immediately, conversation continues | |
| Wait for turn boundary | Notification waits for current turn to complete | |

**User's choice:** FIFO — no special handling

| Option | Description | Selected |
|--------|-------------|----------|
| Uniform — no kind needed | Same struct for messages and notifications | ✓ |
| Uniform + kind tag for logging | Kind field for minor behavioral differences | |
| Interface-based entries | Interface for different processing paths | |

**User's choice:** Uniform — no kind discriminator

---

## Eviction Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Last turn completion time | Clock resets when goroutine finishes processing inbox entry | ✓ |
| Last message received time | Clock resets when message is received (even if queued) | |
| Both — message + completion | Inactive only when both exceed threshold | |

**User's choice:** Last turn completion time

| Option | Description | Selected |
|--------|-------------|----------|
| 30 min — standard cleanup | Cancel goroutine, delete from maps, keep budget state and SQLite | ✓ |
| 5 min — aggressive | Fast cleanup for casual users | |
| 60 min — conservative | Long-lived goroutines for extended conversations | |

**User's choice:** 30 minutes, standard cleanup

| Option | Description | Selected |
|--------|-------------|----------|
| Standalone tracker + ticker | map[string]time.Time + sync.RWMutex + background ticker | ✓ |
| Self-terminating goroutines | Each goroutine tracks own idle time | |
| Embedded in UserGate | Tracker is part of UserGate struct | |

**User's choice:** Standalone InactivityTracker with onEvict callback

| Option | Description | Selected |
|--------|-------------|----------|
| 60s tick, persist + cleanup | Cancel, persist snapshot, clean maps | ✓ |
| 60s tick, discard context | Cancel and delete, no persistence | |
| 120s tick, same cleanup | Less frequent scanning | |

**User's choice:** 60s tick, persist + cleanup

---

## Gate API Design

| Option | Description | Selected |
|--------|-------------|----------|
| Acquire / TryAcquire / Evict | Three-method interface | ✓ |
| Acquire / TryAcquire only | Eviction handled by tracker directly | |
| Acquire / TryAcquire / Release / Shutdown | Explicit Release and graceful shutdown | |

**User's choice:** Acquire / TryAcquire / Evict

| Option | Description | Selected |
|--------|-------------|----------|
| New internal/concurrency/ package | Zero Aura dependencies, pure and testable | ✓ |
| Inside agentruntime package | Alongside SessionStore | |
| New internal/usergate/ package | Single-responsibility flat package | |

**User's choice:** internal/concurrency/

| Option | Description | Selected |
|--------|-------------|----------|
| Gate wraps SessionStore entry | onMessage spawns goroutine, calls Acquire | ✓ |
| Gate owns the call chain | UserGate.run calls handleConversation internally | |
| SessionStore mediates the gate | Begin/Finish call through to gate internally | |

**User's choice:** Gate wraps SessionStore entry

| Option | Description | Selected |
|--------|-------------|----------|
| Config struct — size + times | Single Config with InboxSize, thresholds, OnEvict | ✓ |
| Functional options | New(opts...) Go idiom | |
| Separate configs, caller wires | Caller creates and wires gate + tracker separately | |

**User's choice:** Single Config struct, UserGate creates InactivityTracker internally

---

## Qdrant Readiness Contract

| Option | Description | Selected |
|--------|-------------|----------|
| Shared interface + single client | internal/qdrant/ with Client interface, replaces duplicates | ✓ |
| Health gate only — defer dedup | Add startup check, unify clients in Phase 2 | |
| Shared transport, no interface | Shared HTTP transport, higher-level ops stay in packages | |

**User's choice:** Shared interface + single client

| Option | Description | Selected |
|--------|-------------|----------|
| Broad — all operations needed | Health, Search, Upsert, Delete, CreateCollection, DeleteCollection | ✓ |
| Narrow — Health + Search only | Minimal for Phase 2 needs | |
| Health + Search + CountPoints | Add count for Phase 4 warm cache detection | |

**User's choice:** Broad — all operations

| Option | Description | Selected |
|--------|-------------|----------|
| Blocking health gate at startup | Probe /readyz with 120s timeout, exit if unreachable | ✓ |
| Contract only — no startup gate | Package + interface only, gate in Phase 4 | |
| Non-blocking health probe | Warn but continue if Qdrant unavailable | |

**User's choice:** Blocking health gate at startup (120s default timeout)

| Option | Description | Selected |
|--------|-------------|----------|
| client.go + config.go + types.go | Three-file package layout | ✓ |
| + health.go | Separate health check file | |
| Single client.go file | Everything in one file | |

**User's choice:** client.go + config.go + types.go

---

## Claude's Discretion

No areas were left to Claude's discretion — all decisions were made explicitly by the user.

## Deferred Ideas

None — discussion stayed within phase scope.
