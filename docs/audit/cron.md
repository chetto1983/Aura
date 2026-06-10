# Audit: internal/cron

**Verdict:** needs-work — two dead methods in production code and one contract field (`HandlerMeta.MaxDuration`) declared by the dispatcher but never enforced by it; plus a misleading sentinel error doc.

**Counts:** critical 0 / high 0 / medium 2 / low 2

---

## Findings

### [MEDIUM][NOT-WIRED] `HandlerMeta.MaxDuration` is declared but never enforced by the dispatcher

**Location:** `internal/cron/dispatch.go:31` (field declaration), `dispatch.go:96-115` (`Dispatch` method)

**Confidence:** high

**Detail:**
`HandlerMeta` in the `cron` package (the dispatcher-local interface, distinct from `handlers.HandlerMeta`) carries a `MaxDuration time.Duration` field. The `Dispatch.Dispatch` method never reads it: it calls `h.Run(ctx, job)` with the unmodified tick-goroutine context, which carries no deadline. Two handlers self-enforce via their own `context.WithTimeout` (`AgentJobHandler` at `handlers/agentjob.go:73`, `BackupHandler` at `handlers/backup.go:97`). `ReminderHandler.Run` and `SkillTTLSweepHandler.Run` do not — they run unbounded from the dispatcher's perspective. If either blocked (e.g., a hung `SweepExpiredSnippets` FS call), the tick goroutine would be stuck for the lifetime of the parent context.

The field is contractually advertised in `HandlerMeta` as the "wall-clock budget" the dispatcher should enforce, but nothing in `Dispatch.Dispatch` or `runOne` / `runMissed` wraps the run call with a `context.WithTimeout(ctx, h.Meta().MaxDuration)`.

**Suggested fix:**
In `Dispatch.Dispatch`, after resolving the handler, wrap the run context:
```go
runCtx, cancel := context.WithTimeout(ctx, h.Meta().MaxDuration)
defer cancel()
summary, runErr := h.Run(runCtx, job)
```
Handlers that already apply their own internal timeout (backup, agent_job) are unaffected because their internal `context.WithTimeout` will fire first (or at the same time). A zero `MaxDuration` should fall through to no wrapping, so add a guard: `if d := h.Meta().MaxDuration; d > 0 { ... }`.

---

### [MEDIUM][DEAD-CODE] `Store.CreateRunAndAdvance` is unreachable in production

**Location:** `internal/cron/store.go:220-241`

**Confidence:** high

**Detail:**
`CreateRunAndAdvance` opens a run row and advances `next_run_at` atomically in one `db.WithTx` transaction. The intended use case is described in the comment: "claim-then-reschedule" atomicity. However, the actual claim lifecycle (`claim.go`) calls `insertRunOnConn` (on the held advisory-lock connection), and reschedule happens via `reschedule` → `Store.UpdateNextRunAt` on the normal pool. No production path calls `CreateRunAndAdvance`. Only `store_test.go:242` and `store_test.go:394` reference it.

Grep across the full repo (`D:/Aura/**/*.go`) confirms zero non-test, non-definition references.

**Suggested fix:**
Remove `CreateRunAndAdvance` from `store.go` and update the tests that exercise it to either test the actual atomic path or be dropped. If the method is intended for a future use (e.g., a simplified single-connection path), add a `//nolint:unused` annotation and a comment explaining when it will be wired.

---

### [LOW][DEAD-CODE] `Store.Heartbeat` is test-only and disconnected from the production heartbeat path

**Location:** `internal/cron/store.go:245-254`

**Confidence:** high

**Detail:**
`Store.Heartbeat` calls `s.q.UpdateHeartbeat(ctx, u)` through the pool. Production heartbeating is done entirely through `startHeartbeat` (`heartbeat.go:38`) which runs a raw `Exec` on the HELD advisory-lock connection — never through `Store.Heartbeat`. The only callers of `Store.Heartbeat` in the repo are `store_test.go:366` and `store_test.go:397`. The comment on the method acknowledges this: "this Store method serves the non-held path and tests."

The method is not dangerous but is an exported-looking API that leads a reader (or a future handler author) to believe `Store.Heartbeat` is a valid production heartbeat path — it is not, because it would break the advisory-lock session invariant (using a pool conn instead of the held conn).

**Suggested fix:**
Make `Heartbeat` unexported (`heartbeat`) or remove it entirely; keep only the `startHeartbeat` held-conn path. If it must remain for testing, rename to `heartbeatViaPool` and add a comment that it MUST NOT be called from a live run (advisory-lock session violation).

---

### [LOW][BUG] `ErrAlreadyRunning` sentinel is dual-use with a misleading doc comment

**Location:** `internal/cron/store.go:22-25`

**Confidence:** high

**Detail:**
`ErrAlreadyRunning` is returned in two entirely different situations:

1. `claim.go:68` — the advisory lock was lost to another worker (task is currently in-flight on another node/goroutine).
2. `store.go:282` — a duplicate run completion hit the `completed_with_hash` UNIQUE constraint (SC#2 idempotency guard).

The sentinel's declaration comment ("idempotency rejection when a duplicate completion hits the completed_with_hash UNIQUE constraint") only describes case 2. `runOne` / `runMissed` check `errors.Is(err, ErrAlreadyRunning)` from `claim` (case 1) and log "skipped: previous run in progress" — the message matches case 1, not the documented case 2. A caller using `errors.Is` on a `CompleteRun` error and getting `ErrAlreadyRunning` today cannot distinguish "task is live" from "completion is idempotent" without reading the call site.

The runtime behavior is correct because no single caller receives both case-1 and case-2 errors from the same call. However the shared sentinel is a readability and future-proofing hazard.

**Suggested fix:**
Split into two sentinels:
```go
ErrAlreadyRunning  = errors.New("scheduler: advisory lock held by another worker")
ErrDuplicateRun    = errors.New("scheduler: duplicate completion hash (idempotency guard)")
```
Update `store.go:282` to wrap `ErrDuplicateRun`; `claim.go:68` keeps `ErrAlreadyRunning`. Update `runOne`/`runMissed` to check both where appropriate.

---

## What was checked and found clean

- **Goroutine lifecycle**: `startHeartbeat` goroutine is correctly joined via the `stop()` return; `tick` blocks on `wg.Wait()`; `runMissed` calls are sequential (no leaked goroutines). `goleak` gate is correctly wired in `main_test.go`.
- **Advisory lock session invariant (Pitfall 1/2)**: `claim` acquires a dedicated conn, all subsequent operations (heartbeat, `insertRunOnConn`, `pg_advisory_unlock`) use `c.conn` — never a fresh pool acquire. Release calls `conn.Release()` after unlocking.
- **DST safety**: `NextRunAt` for `KindCron` converts `after` into the spec's IANA zone before calling `gronx.NextTickAfter`, then converts back to UTC — no fixed-offset stored.
- **Quiet-hours wraparound logic**: `DuringQuietHours` correctly handles `start > end` (midnight-spanning) windows with `nowM >= start || nowM < end`.
- **`DueTasks` limit clamping**: `store_runs.go:82-84` clamps non-positive and overflow limits to 1 before the `int32` cast — correct.
- **`isUniqueViolation`**: uses `errors.As` + SQLSTATE code, never string-matching.
- **`payloadOrEmpty`**: returns `{}` (not `nil`) for empty payloads — Postgres `jsonb` never gets a NULL default for non-null columns.
- **Context propagation**: `claim`, `reschedule`, `catchUpMissed`, `recoverOrphans`, all DB calls carry the passed `ctx` — no detached background contexts.
- **`int4OrNull` and `StepBudget=0`**: intentionally maps to NULL so the agent falls back to the env/builtin default — consistent with `newJobBudget`.
- **`taskHash` FNV-1a collision**: benign skip+reschedule on collision, not a correctness break — documented and acceptable.
- **`setMissedSinceOnConn` UUID string as `$1`**: pgx v5 coerces `string` → `uuid` on parameterized queries via OID inference; consistent with `heartbeat.go:38`. No runtime error.
- **`buildSend` JSON marshal error swallow**: `json.Marshal(map[string]string{...})` can never fail (no non-serializable values) — swallowing `_` is safe.
- **Race conditions**: no shared mutable state accessed without locks in the scheduler; `tick`'s semaphore is a buffered channel (correct), `wg` is stack-local per tick invocation.
