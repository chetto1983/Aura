package cron

// claim.go holds the HA core of the scheduler (D-02/D-03/D-04): the held-conn
// session advisory lock that makes a due task a singleton across concurrent
// workers. The lifecycle is the only novel-to-this-repo wiring (RESEARCH key
// insight) — get it right and the chaos failover passes by construction.

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// taskHash maps a task UUID to the single int64 advisory key pg_try_advisory_lock
// takes. FNV-1a 64 over the UUID string gives a uniform 64-bit key with zero
// dependencies (stdlib hash/fnv). Collision posture (D-A / RESEARCH A1): the
// 64-bit namespace + per-task UUID input makes accidental collision negligible,
// and a collision only causes a benign singleton-skip+reschedule (D-04) — never a
// correctness break, since the loser logs and reschedules rather than double-firing.
func taskHash(id string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return int64(h.Sum64())
}

// Claim is a successful held-conn claim on a due task: the dedicated pooled conn
// that carries the session advisory lock for the run's whole lifetime (D-03), the
// opened run row, and the advisory key used to unlock. The conn MUST outlive the
// run — it is NOT returned to the pool until release.
type Claim struct {
	conn  *pgxpool.Conn
	RunID string
	hash  int64
}

// Conn exposes the held connection so the dispatcher (10-05) runs the job's writes
// (heartbeat included) on the SAME conn that owns the advisory lock (Pitfall 2).
func (c *Claim) Conn() *pgxpool.Conn { return c.conn }

// claim acquires a dedicated conn, takes the session advisory lock for the task,
// and — only if it won the lock — opens a run row. The held conn is the binding
// that makes the lock continuous-ownership (D-03): it is never released here on the
// success path. On a lost lock (another worker owns the task) the conn is released
// immediately and ErrAlreadyRunning is returned so the caller logs
// `skipped: previous run in progress` and reschedules via NextRunAt (D-04 singleton).
func (s *Scheduler) claim(ctx context.Context, task Task) (*Claim, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim acquire conn for task %q: %w", task.ID, err)
	}
	key := taskHash(task.ID)

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
		conn.Release()
		return nil, fmt.Errorf("claim try-lock task %q: %w", task.ID, err)
	}
	if !locked {
		conn.Release() // someone else owns it → D-04 skip+log+reschedule
		return nil, fmt.Errorf("claim task %q: %w", task.ID, ErrAlreadyRunning)
	}

	// Open the run row ON the held conn so the insert and the lock share one
	// session; a heartbeat on this same conn then needs no second pool slot.
	run, err := s.store.insertRunOnConn(ctx, conn, task.ID, task.StepBudget)
	if err != nil {
		// Unlock before releasing — the conn returns to the pool clean (Pitfall 1).
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", key)
		conn.Release()
		return nil, fmt.Errorf("claim insert run for task %q: %w", task.ID, err)
	}
	return &Claim{conn: conn, RunID: run.ID, hash: key}, nil
}

// release unlocks the session advisory lock and returns the held conn to the pool.
// It is idempotent-safe: a nil conn (already released) is a no-op. The unlock MUST
// run on the SAME conn that took the lock (Pitfall 1) — that is guaranteed because
// release uses c.conn, the held conn, never a fresh pool acquire.
func (c *Claim) release(ctx context.Context) {
	if c == nil || c.conn == nil {
		return
	}
	_, _ = c.conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", c.hash)
	c.conn.Release()
	c.conn = nil
}
