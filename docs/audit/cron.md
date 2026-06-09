# Audit: internal/cron

**Verdict:** needs-work — four not-wired methods accumulate technical debt; one structural latency hazard in the boot path; one duplicate Scheduler allocation leaks a minor design smell.

**Counts:** critical 0 / high 1 / medium 2 / low 2

---

## Findings

### [HIGH][NOT-WIRED] `Store.CreateRunAndAdvance` is never called in production

**Location:** `internal/cron/store.go:220`
**Confidence:** high

`Store.CreateRunAndAdvance` is documented as the "atomic claim-then-reschedule write" that opens a run row AND advances `next_run_at` in one transaction. However, the actual production claim path (`claim.go`) uses `insertRunOnConn` on the held advisory-lock connection, and `reschedule` (`scheduler.go:247`) calls `store.UpdateNextRunAt` through the pool — two separate operations protected by the advisory lock, not a DB transaction.

`CreateRunAndAdvance` only appears in tests (`store_test.go`). Critically, it opens a pool transaction (via `db.WithTx`) on an **ordinary pool connection**, not the held advisory-lock conn. Any future caller who reaches for this method as the "atomic" insert+advance will bypass the per-session advisory-lock invariant (Pitfall 2), because the transaction runs on a different connection than the one holding the lock. The method is a trap: its name and doc imply "the correct atomic pattern" but it does not satisfy the advisory-lock session constraint that the real production path requires.

**Suggested fix:** Either delete the method and add a comment pointing to the two-step production path (`insertRunOnConn` + `reschedule`), or rename it `CreateRunAndAdvanceForTests` with an explicit `//go:build db_integration` tag and a doc comment warning that it does NOT preserve the advisory-lock session.

---

### [MEDIUM][NOT-WIRED] `Store.InsertRun`, `Store.Heartbeat`, and `Store.GetRun` are test-only methods on the public API

**Location:** `internal/cron/store.go:200` (`InsertRun`), `store.go:245` (`Heartbeat`), `store_runs.go:59` (`GetRun`)
**Confidence:** high

All three exported `Store` methods have zero production callsites. Production uses:
- `insertRunOnConn` (unexported, held-conn) instead of `InsertRun`
- a raw `conn.Exec` UPDATE in `heartbeat.go` instead of `Heartbeat`
- no run-by-ID read in production flow (only diagnostic/test tooling)

These exported methods present the same advisory-lock session hazard as `CreateRunAndAdvance`: `InsertRun` and `Heartbeat` both use the pool, not the held conn. A caller who reaches for `store.InsertRun` inside a locked claim context would open the run row on a DIFFERENT connection than the advisory lock, violating the single-session invariant.

Grep evidence — all non-test, non-definition references:
- `InsertRun`: zero production callsites (all in `*_test.go`)
- `Heartbeat`: zero production callsites
- `GetRun`: zero production callsites

**Suggested fix:** Either unexport all three (making them `insertRun`, `heartbeat`, `getRun`) so the held-conn versions are the only visible surface, or add build-tag guards to keep them as explicit test helpers. At minimum, add a doc comment on each: "Test helper — uses the pool, not a held advisory-lock connection. Do not use inside a claimed run."

---

### [MEDIUM][BUG] Boot-phase missed dispatches run serially with no concurrency cap, blocking the ticker for N × MaxDuration

**Location:** `internal/cron/scheduler.go:126-128`
**Confidence:** high

`Start()` dispatches all missed catch-up tasks serially before starting the tick loop:

```go
for _, m := range missed {
    s.runMissed(ctx, m)  // each can block up to MaxDuration (default 120s per agent_job)
}
```

`runMissed` runs the handler synchronously. With the default `agentJobMaxDuration = 120s`, N missed `agent_job` tasks at boot block the ticker for up to `N × 120s` before the first normal tick fires. After a 2-hour outage with 10 pending agent jobs, the daemon is effectively unavailable for scheduling new tasks for up to 20 minutes post-restart.

The tick path (`scheduler.go:151`) uses a bounded semaphore (`make(chan struct{}, s.maxConcurrent)`) and a `sync.WaitGroup`, giving concurrent dispatch capped at `maxConcurrent`. The boot catch-up has no equivalent bound.

This is not catastrophic (the advisory lock still prevents double-firing; the context cancel still drains correctly), but it contradicts the concurrency model applied in `tick`.

**Suggested fix:** Apply the same semaphore pattern used in `tick` to the boot catch-up loop, OR dispatch each `runMissed` in a goroutine with a `sync.WaitGroup` and the same `sem` channel, waiting for all to finish before entering the tick loop. Cap at `s.maxConcurrent` to preserve the held-conn headroom invariant (Pitfall 2).

---

### [LOW][NOT-WIRED] Duplicate `NewScheduler` allocation in `buildDispatch` solely for `DuringQuietHours` method value

**Location:** `cmd/aura/serve.go:254`
**Confidence:** high

```go
QuietHours: cron.NewScheduler(chat.pool, store, cron.SchedulerConfig{}).DuringQuietHours,
```

A second `*Scheduler` instance is created with default `Now = time.Now` solely to bind `DuringQuietHours` as a function value. The live scheduler (line 143) also uses `time.Now` by default, so both schedulers agree on the clock in production. However:

1. The second scheduler is never `Start()`ed, yet it acquires a pool reference and a store reference — two allocations with no lifecycle cleanup.
2. If a future test or config path injects a synthetic `Now` into the live scheduler but not the quiet-hours scheduler, the two would diverge silently.
3. `DuringQuietHours` is a pure function of `Now()` and `AURA_SCHEDULER_QUIET_HOURS` env — it does not need to be a method on a stateful `Scheduler`.

**Suggested fix:** Extract `DuringQuietHours` as a package-level function `cron.DuringQuietHours(tz string, now func() time.Time) bool` (or a `QuietHoursChecker` type) so the caller supplies `time.Now` directly. This eliminates the phantom `Scheduler` and removes the clock-divergence footgun.

---

### [LOW][BUG] `pg_advisory_unlock` error silently discarded in `Claim.release`

**Location:** `internal/cron/claim.go:91`
**Confidence:** medium

```go
_, _ = c.conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", c.hash)
c.conn.Release()
```

The advisory unlock error is discarded. If `ctx` is already cancelled (graceful shutdown) before `release` is called — which is a realistic scenario since `claim.release` is deferred and the parent context may have been cancelled — the `Exec` call will fail with `context.Canceled`. The lock is then NOT explicitly released via SQL; however, Postgres auto-releases session advisory locks when the connection is returned to the pool and the underlying session is reused/reset.

The real risk: `pgxpool.Conn.Release()` does NOT close the underlying connection by default — it returns it to the pool. The session advisory lock MAY persist on the pooled connection until the pool decides to close or reset it, which can range from immediately (idle timeout) to never (the pool reuses the connection for another purpose, which inherits the lock). This is a subtle held-lock window.

**Suggested fix:** If the context is cancelled, pass `context.Background()` for the unlock exec to ensure the unlock actually runs regardless of cancellation state. Pattern:

```go
func (c *Claim) release(ctx context.Context) {
    if c == nil || c.conn == nil {
        return
    }
    // Use a fresh background context for the unlock so a cancelled parent ctx
    // does not leave the advisory lock held on the pooled connection.
    unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _, _ = c.conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", c.hash)
    c.conn.Release()
    c.conn = nil
}
```

---

## Clean sections (what was checked and found sound)

- **Schedule engine** (`schedule.go`): `ParseSchedule`, `NextRunAt`, `FirstFire` — DST-safe, correct `strings.Cut` on `-` separator (cuts at first `-`, preserving `HH:MM` tokens), proper zone-aware `gronx.NextTickAfter`, `ErrPastRunAt` gate for past one-shots.
- **Tick concurrency** (`scheduler.go:tick`): semaphore + `sync.WaitGroup` correctly bounds concurrent claims; loop-variable capture is safe (Go 1.26 loopvar fix; the `task` arg copy is explicit anyway).
- **Heartbeat** (`heartbeat.go`): goroutine-clean (`defer ticker.Stop()` + joinable `stop()` via `close(done)`); uses `hbCtx` (child of `ctx`) so parent cancel exits cleanly; goleak gate passes.
- **Advisory lock session invariant** (`claim.go`): `insertRunOnConn` runs on the held conn; heartbeat UPDATE runs on the held conn; `pg_advisory_unlock` runs on the held conn before `Release()`. Pitfall 1 is correctly addressed.
- **Race safety**: `Dispatch.handlers` and `Scheduler` fields are set once at construction; concurrent goroutine reads in `tick` are safe. No shared mutable state written post-construction.
- **Error classification**: `isUniqueViolation` uses `errors.As` + SQLSTATE `23505` (never string matching); `ErrAlreadyRunning` and `ErrTaskNotFound` are sentinel errors for clean `errors.Is` chains.
- **pgtype helpers**: `text`, `int4OrNull`, `tsOrNull`, `uuidOrNull` all correctly project zero/empty to `pgtype.{}` (NULL) and non-zero to valid typed values. `tsOrNull` always stores UTC.
- **DueTasks clamping** (`store_runs.go:81-84`): `limit <= 0 || limit > math.MaxInt32` clamped to 1 — WR-02 overflow guard is correct.
- **Quiet-hours wrap-around** (`scheduler.go:278-281`): midnight-crossing window (e.g. `23:00-07:30`) is handled by the `start > end` branch; boundary semantics (start inclusive, end exclusive) match the test suite.
- **Backup handler** (`handlers/backup.go`): FIXED argv (no payload interpolation, T-10-16); `docker` LookPath-gated; WR-04 bind-mount artifact check; retention sweep is best-effort (pruning failure does not fail the backup); `MissedBackupAlert` fires only past the 24h window.
- **Reminder handler** (`handlers/reminder.go`): pure text delivery, no LLM, no tools; empty payload degrades gracefully.
- **AgentJobHandler** (`handlers/agentjob.go`): `maxAutoRejects` loop bound prevents infinite ask_user loops (D-25); `childRegistry` drops `swarm_spawn` without importing `internal/swarm`; step budget inherited from row.
- **SkillTTLSweepHandler** (`handlers/skill_ttl.go`): nil sweeper and non-positive TTL are no-op successes; clock injectable for tests via unexported `now` field; `"auto"` actor for D-29 audit rows.
