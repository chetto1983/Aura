# Audit: internal/cron

**Verdict:** needs-work — two not-wired struct fields, one dead exported method, one misleading sentinel, one style inconsistency in raw SQL binding.

**Counts:** critical 0 / high 0 / medium 3 / low 2

---

## Findings

### [MEDIUM][NOT-WIRED] `HandlerMeta.MaxDuration` and `ReschedulesOnRecovery` in `cron.Dispatch` are never consumed

**Location:** `internal/cron/dispatch.go:31-32`, `internal/cron/dispatch.go:96-115`

**Confidence:** high

**Detail:**
`Dispatch.Dispatch` routes a task to its handler and calls `h.Run(ctx, job)` directly, passing the raw context without consulting `h.Meta().MaxDuration` to create a deadline. The dispatcher never calls `h.Meta()` at all. Both fields in the cron-local `HandlerMeta` struct are populated (via the `handlerAdapter` in `cmd/aura/serve.go:270-276`) but neither is read at dispatch time:

- `MaxDuration`: each handler self-enforces via its own `context.WithTimeout` call (agentjob.go:73, backup.go:97), making the cron-boundary field redundant dead state.
- `ReschedulesOnRecovery`: the boot catch-up (`recover.go:catchUpMissed`) fires ALL overdue tasks unconditionally regardless of this field. A reminder with `ReschedulesOnRecovery: false` is still dispatched at boot if overdue — correct per D-18 semantics, but the field is never the decision gate anywhere in the production path.

The risk: future code assuming the dispatcher enforces these fields will be surprised. The `ReschedulesOnRecovery` field is confirmed non-wired via grep (no call site reads it outside handler tests asserting its return value).

**Suggested fix:** Either (a) have the dispatcher read `h.Meta().MaxDuration` and enforce a per-kind `context.WithTimeout` wrapper before calling `h.Run`, removing the self-enforcement duplication in each handler; or (b) remove `MaxDuration` and `ReschedulesOnRecovery` from the cron-local `HandlerMeta` (they live correctly in the handlers-level `HandlerMeta`) and document that timeout enforcement is handler-local. Also add a `recoveryCandidates` filter in `catchUpMissed` or `runMissed` if per-kind recovery gating is intended.

---

### [MEDIUM][NOT-WIRED] `Store.CreateRunAndAdvance` is an exported method with no production caller

**Location:** `internal/cron/store.go:220-241`

**Confidence:** high

**Detail:**
`CreateRunAndAdvance` was designed as the atomic insert-run + advance-next_run_at write (in a single `db.WithTx`). The 10-03 held-conn approach superseded it: production claim flow uses `insertRunOnConn` (on the held advisory-lock conn) followed by a separate `reschedule` call (which calls `UpdateNextRunAt`). `CreateRunAndAdvance` is now called only from two integration tests in `store_test.go` (lines 242, 394). Confirmed via repo-wide grep — no non-test call site exists.

This is an exported method that was overtaken by a different architecture. It carries real pool-level transaction cost and its existence implies a public API contract that nothing actually satisfies.

**Suggested fix:** Unexport to `createRunAndAdvance` if needed for tests, or delete it entirely and have the two tests use the real claim path (`insertRunOnConn` + `reschedule`). The `db_integration`-tagged tests should prove the claim lifecycle, not a superseded code path.

---

### [MEDIUM][BUG] `ErrAlreadyRunning` sentinel documents one failure mode but is used for two semantically distinct errors

**Location:** `internal/cron/store.go:19-24`, `internal/cron/claim.go:68`

**Confidence:** high

**Detail:**
The doc comment for `ErrAlreadyRunning` reads:

> "the idempotency rejection when a duplicate completion hits the completed_with_hash UNIQUE constraint (the SC#2 swallow point)"

But the sentinel is also wrapped in `claim.go:68` for the advisory lock lost case ("another worker holds the task's session lock"). These are two semantically different failure modes:
1. Lock-lost path (`claim.go:68`): a concurrent worker is actively running this task right now — safe to skip + reschedule.
2. Duplicate completion (`store.go:282`): a completed run's hash was re-submitted — safe to swallow.

The error message (`"agent job already running for this completion hash"`) is misleading for the lock-lost path. A caller that wraps one of these errors and then does `errors.Is(err, ErrAlreadyRunning)` cannot distinguish them.

In the current code, `scheduler.go:179,220` treats both as "skip and continue," which happens to be correct for both cases — but only by coincidence. A future handler that needs to distinguish "task was already running (concurrent)" from "completion was already recorded (idempotency)" will get burned.

**Suggested fix:** Split into two sentinels: `ErrLockContended` (advisory lock lost, concurrent runner) and `ErrAlreadyCompleted` (duplicate completion hash). Update `claim.go:68` and `store.go:282` accordingly. Update `scheduler.go` to handle both.

---

### [LOW][BUG] Raw string `runID` passed to UUID column in `setMissedSinceOnConn` and `startHeartbeat` — inconsistent with the rest of the package

**Location:** `internal/cron/store_runs.go:50-51`, `internal/cron/heartbeat.go:38`

**Confidence:** medium

**Detail:**
Every other `conn.Exec` / `q.SomeQuery` call in this package converts the string UUID via `parseUUID(id)` before binding (see `store.go:150`, `store.go:245`, `store_runs.go:24`, `store.go:272`). Two sites pass the raw `string` directly:

- `store_runs.go:50`: `conn.Exec(ctx, "UPDATE ... WHERE id = $1", runID, ...)`
- `heartbeat.go:38`: `conn.Exec(hbCtx, "UPDATE ... WHERE id = $1", runID)`

pgx v5 sends a Go `string` as OID 0 (unspecified text), and PostgreSQL's implicit `text → uuid` cast makes this work at runtime. But it is relying on an undocumented implicit cast path rather than the explicit `pgtype.UUID` binding the rest of the codebase uses. A schema migration that removes the implicit cast, or a pgx version that sends `text` OID explicitly (PostgreSQL may reject explicit-text-to-uuid), would silently break only these two paths.

**Suggested fix:** Apply `parseUUID` to `runID` in both sites before binding, consistent with all other Store methods. Since `runID` is always a valid UUID (it was produced by `newUUID()` at claim time), `parseUUID` will never fail — the conversion is mechanical:

```go
// store_runs.go:setMissedSinceOnConn
ru, err := parseUUID(runID)
if err != nil {
    return fmt.Errorf("set missed_since: invalid run id: %w", err)
}
if _, err := conn.Exec(ctx, `UPDATE aura.agent_job_runs SET missed_since = $2 WHERE id = $1`, ru, missedSince.UTC()); err != nil {
```

```go
// heartbeat.go: pass pgtype.UUID instead of string
ru, _ := parseUUID(runID) // runID is always a valid UUID at this call site
_, _ = conn.Exec(hbCtx, "UPDATE aura.agent_job_runs SET last_heartbeat_at = now() WHERE id = $1", ru)
```

---

### [LOW][DEAD-CODE] `Store.GetTask`, `Store.GetRun`, and `Store.Heartbeat` have no production callers

**Location:** `internal/cron/store.go:146-158` (GetTask), `internal/cron/store_runs.go:59-72` (GetRun), `internal/cron/store.go:245-254` (Heartbeat)

**Confidence:** medium

**Detail:**
Confirmed via repo-wide grep that none of these three exported methods are called outside `internal/cron/*_test.go` files:

- `GetTask`: only in `store_test.go`, `scheduler_integration_test.go`, `recover_test.go`, `dispatch_integration_test.go`.
- `GetRun`: only in `store_runs.go` (definition) + `recover_test.go`, `heartbeat_test.go`, `dispatch_integration_test.go`.
- `Store.Heartbeat` (pool-based): only in `store_test.go`.

The production heartbeat path uses `startHeartbeat` (direct SQL on the held conn), never `Store.Heartbeat`. These methods are reasonable test scaffolding and may be future API surface, but they contribute to the exported footprint with no current production consumer. Unlike `CreateRunAndAdvance`, these are arguably natural convenience accessors — flagged only because they inflate the public surface.

**Suggested fix:** No action required unless the team wants to keep the exported API surface minimal. If retained, add a `// Test-only convenience method` comment so future auditors know the intent. If the package is intended to be a stable library with external callers, keep them and document them accordingly.
