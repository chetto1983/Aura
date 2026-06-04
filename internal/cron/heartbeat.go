package cron

// heartbeat.go is the liveness writer (D-02): a ticker that bumps
// last_heartbeat_at on the run's HELD conn every interval, so the boot orphan scan
// (recover.go) can tell a still-alive run from a crashed one. It runs on the same
// conn that owns the advisory lock (Pitfall 2) — no second pool slot — and is
// goleak-clean: defer ticker.Stop() + ctx-cancel shutdown + a joinable stop()
// (Pitfall 4).

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// startHeartbeat launches the liveness ticker on conn (the run's held conn) and
// returns a stop() that cancels the goroutine and BLOCKS until it has exited, so a
// caller's defer stop() leaves no leaked ticker goroutine (goleak gate). The
// UPDATE runs on conn — never a fresh pool.Acquire — so it shares the advisory-lock
// session and costs no extra pool slot.
func startHeartbeat(ctx context.Context, conn *pgxpool.Conn, runID string, interval time.Duration) (stop func()) {
	hbCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				// Best-effort: a failed heartbeat write is not fatal to the run — the
				// orphan scan's >90s window tolerates a few missed beats. A persistent
				// failure (dead conn) ends the run elsewhere; here we keep ticking.
				_, _ = conn.Exec(hbCtx, "UPDATE aura.agent_job_runs SET last_heartbeat_at = now() WHERE id = $1", runID)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}
