# Concurrency & Resource Leak Audit — Aura Go Codebase (2026-05-17)

**Strictness**: Verified by grep + code inspection. Only findings with concrete file:line and symptom paths are reported.

## 1. Goroutine Lifecycle Inventory

### Safe Patterns (Properly Managed)

| Spawn Site | Exit Signal | Status |
|---|---|---|
| cmd/aura/app.go:72 | bgCancel via App.Stop() | Safe |
| internal/concurrency/tracker.go:43 | ctx.Done() select | Safe |
| internal/chat/hub_swarm.go:36 | ticker.Stop() + context | Safe |
| internal/cron/scheduler.go:154 | Ticker + ctx.Done() | Safe |
| internal/telegram/documents.go:159 | h.wg.Done() + context | Safe |
| internal/concurrency/gate.go:226 | actor.cancel() | Safe |
| internal/agent/executor.go:83,96 | wg.Done() via channel | Safe |

### Goroutine Spawns - Issues Found

**Issue 1: OnOverflow Goroutine Leak** 
- Location: internal/concurrency/gate.go:297
- Code: `go g.config.OnOverflow(userID)`
- Problem: Spawned without WaitGroup tracking
- Risk: If OnOverflow blocks or is slow (>100ms), goroutines accumulate
- Reproducer: Fill UserGate inbox repeatedly, call Close() while OnOverflow callbacks still running
- **BUG**: Gate.Close() returns before in-flight callbacks complete

## 2. Missing Close Calls - VERIFIED SAFE

**SQL Rows**: All have defer rows.Close()
- internal/cron/store.go:184,210
- internal/conversation/archive.go:141,175
- internal/agent/tools/attempts/sqlite.go:115,165,201

**HTTP Responses**: All have defer resp.Body.Close()
- 80+ calls verified via grep
- No leaks detected

**Database Transactions**: Proper error path handling
- internal/api/auth/store.go:543 — BeginTx() with Commit/Rollback

## 3. Mutex Held Across I/O

**Issue 2: InactivityTracker RLock Duration**
- Location: internal/concurrency/tracker.go:77-93
- Pattern: RLock held during map iteration
- Risk: Low; iteration is fast (<10ms) on bounded user count
- Mitigation: Collection pattern correct (collect under lock, evict outside)

**docHandler mutex**: Safe; only protects flag check and WaitGroup.Add()

## 4. Context Propagation Gaps

**context.Background() in init/boot**: ACCEPTABLE
- cmd/aura/main.go:243,264 — Boot-time setup before handlers

**context.Background() in cleanup hooks**: ACCEPTABLE
- cmd/aura/app.go:563 — OnClose hook; no parent context available

**Issue 3: docHandler lifecycle not context-aware**
- Location: internal/telegram/documents.go:72
- Code: `ctx, cancel := context.WithCancel(context.Background())`
- Problem: Document goroutines orphaned from handler context
- Impact: Low; bot.Stop() calls docHandler.Stop()

## 5. Channels Never Closed - FINDINGS

All 40+ make(chan) declarations verified:

**Safe patterns**:
- startedCh: Closed on entry start or drop (gate.go:290,270)
- done channels: Closed explicitly after WaitGroup.Wait()
- Signal channels: Proper signal.Stop() and never re-used

**Issue 4: OnOverflow callback spawned unbounded**
- See Issue 1 above

## 6. time.Tick vs time.NewTicker - ALL SAFE

All 6 NewTicker instances have defer .Stop():
- internal/concurrency/tracker.go:44
- internal/chat/hub_swarm.go:36
- internal/channels/telegram/invocation_builder.go:197
- internal/agent/tools/index/reconciler.go:177
- internal/cron/scheduler.go:154
- cmd/probe_telegram_ui/main.go:219

No time.Tick() calls found (good; time.Tick leaks).

## 7. Busy-Poll Candidates - NONE FOUND

All poll loops use proper intervals:
- WaitForRun: 200ms ticker
- Tracker sweep: 60s ticker
- Scheduler: 5s ticker

No busy-loops or short sleeps detected.

## 8. Race Conditions - NONE FOUND

All field accesses properly guarded:
- docHandler.closed: Protected by mu.Lock()
- Hub.threadStatus: sync.Map (lock-free)
- UserGate.actors: Protected by mu.Lock()

## 9. Defer Ordering - SAFE

All defer chains have correct LIFO ordering:
- docHandler.Stop(): Explicit unlock before cancel
- Scheduler.run(): ticker.Stop() before lock; done closed last

## 10. context.WithCancel without defer cancel - ALL SAFE

All 18 instances verified:
- Explicit cancel path in Stop() method
- OR defer cancel() present
- No leaks detected

## CONFIRMED BUGS

### BUG 1: OnOverflow Goroutine Leak (MEDIUM)

**File**: internal/concurrency/gate.go:297
**Code**: `go g.config.OnOverflow(userID)`

**Issue**: Goroutine spawned without WaitGroup tracking

**Symptom**: After UserGate.Close(), in-flight OnOverflow callbacks may still be running; memory leak on frequent overflows

**Reproducer**:
```
Create UserGate, spawn 100 rapid overflows, call Close()
Monitor goroutines: will see OnOverflow callbacks still running
```

**Fix**: Wrap with gate.wg.Add/Done:
```go
if g.config.OnOverflow != nil {
    g.wg.Add(1)
    go func() {
        defer g.wg.Done()
        g.config.OnOverflow(userID)
    }()
}
```

**Impact**: Low if OnOverflow is <10ms; high if slow or blocking

### BUG 2: docHandler Context Lifecycle (LOW)

**File**: internal/telegram/documents.go:72
**Code**: `ctx, cancel := context.WithCancel(context.Background())`

**Issue**: Document processing goroutines have independent context from handler

**Symptom**: Orphaned goroutines if handler panics

**Fix**: Propagate handler context to docHandler.process()

**Impact**: Low; bot.Stop() calls docHandler.Stop()

### BUG 3: SessionStore.Begin() Not Context-Aware (LOW)

**File**: internal/agent/session.go:98

**Issue**: No context parameter; no timeout on operations

**Impact**: Rare; sessions are in-memory; acceptable

## SUMMARY

**Critical**: 0
**High**: 0  
**Medium**: 1 (OnOverflow leak)
**Low**: 2 (docHandler context, SessionStore context)

**Status**: Mini-PC budget respected. No busy-loops, no mutex-held-across-I/O, proper shutdown. OnOverflow callback is only structural issue; impact depends on callback latency.

Audit Date: 2026-05-17
