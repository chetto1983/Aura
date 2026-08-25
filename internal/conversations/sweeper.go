package conversations

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// stopJoinTimeout bounds the Stop() wg.Wait() drain so a hung sweep tick (a slow
// filesystem walk) cannot wedge the daemon shutdown. A tick already in flight when
// Stop fires runs to completion; this only bounds how long Stop waits to join it.
const stopJoinTimeout = 30 * time.Second

// SweeperConfig configures the periodic sidecar-sweep worker (audit M-06 part 2). The
// boot ScanOrphans is a one-shot at startup; in a long-running `serve` daemon sidecars
// accumulate between reboots, so this worker re-runs the SAME sweep on an interval.
type SweeperConfig struct {
	// Interval is the tick period. A non-positive interval DISABLES the periodic
	// worker (the boot sweep still runs); Start then launches no goroutine.
	Interval time.Duration
	// Sweep is the work each tick performs. The serve composition root wires this to
	// a closure over ScanOrphans (orphan dirs + tmp TTL + audit size WARN), reusing the
	// boot sweep's age/orphan rules — this worker adds NO new reclaim policy.
	Sweep func(context.Context)
}

// Sweeper is the interval-driven background worker that re-runs the boot sidecar sweep
// so aged/orphaned sidecars are reclaimed without a daemon restart (M-06). Its lifecycle
// mirrors the Runner's auto-title WaitGroup: Start launches one goroutine, Stop joins it
// under a bounded wait (goleak-clean), and a cancelled Start ctx winds it down too.
type Sweeper struct {
	interval time.Duration
	sweep    func(context.Context)

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

// NewSweeper builds a periodic sweeper from the config. It does NOT start the worker —
// the caller wires Start into the daemon boot and Stop into its shutdown.
func NewSweeper(cfg SweeperConfig) *Sweeper {
	return &Sweeper{
		interval: cfg.Interval,
		sweep:    cfg.Sweep,
		stop:     make(chan struct{}),
	}
}

// NewRunDirSweeper builds the production sweeper whose tick re-runs ScanOrphans over the
// run dir (the boot sweep) on the given interval. The pool is read-only inside the scan
// (existence lookups only). A non-positive interval yields a sweeper that Start no-ops.
func NewRunDirSweeper(pool *pgxpool.Pool, p ScanParams, interval time.Duration) *Sweeper {
	return NewSweeper(SweeperConfig{
		Interval: interval,
		Sweep: func(ctx context.Context) {
			// A scan error is a WARN-level degradation recovered on the next tick (the
			// same posture as the boot scan), never fatal to the daemon.
			_ = ScanOrphans(ctx, pool, p)
		},
	})
}

// Start launches the tick loop in its own goroutine. A non-positive interval or a nil
// sweep launches nothing (the worker is disabled). The goroutine exits on Stop() or on
// ctx cancellation, whichever comes first — Stop afterward is still a clean join.
func (s *Sweeper) Start(ctx context.Context) {
	if s == nil || s.interval <= 0 || s.sweep == nil {
		return
	}
	s.wg.Go(func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-ticker.C:
				s.sweep(ctx)
			}
		}
	})
}

// SweepNow runs the configured work synchronously. It shares Start's disabled
// semantics so a non-positive interval remains a complete off switch.
func (s *Sweeper) SweepNow(ctx context.Context) {
	if s == nil || s.interval <= 0 || s.sweep == nil {
		return
	}
	s.sweep(ctx)
}

// Stop signals the worker to exit and joins it under a bounded wait so a hung tick
// cannot wedge shutdown. It is idempotent (safe to call when Start was never invoked,
// e.g. a disabled interval) and safe to call multiple times.
func (s *Sweeper) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopJoinTimeout):
	}
}
