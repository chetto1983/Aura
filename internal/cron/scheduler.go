package cron

// scheduler.go is the long-lived tick loop (D-02): every tick it selects due
// tasks (next_run_at<=Now) and claims each under a max-concurrent cap, driving the
// held-conn advisory-lock singleton (claim.go) + heartbeat (heartbeat.go). Boot
// runs the orphan scan + missed catch-up (recover.go) before the first tick.
//
// The clock is an injectable Now func (W8, budget.go precedent): a plain func
// field, NOT Go 1.26 synctest, so the loop's tests stay deterministic with zero
// background goroutines that would trip the goleak gate. The max-concurrent cap is
// sized strictly below the pool's MaxConns so the held conns never starve the tick
// query or a run's heartbeat (Pitfall 2).

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Default tick + concurrency knobs. The cap is deliberately below db.defaultMaxConns
// (10) so held run conns leave headroom for the tick DueTasks query (Pitfall 2).
const (
	defaultTickInterval      = 30 * time.Second
	defaultMaxConcurrentRuns = 4
	defaultHeartbeatInterval = 30 * time.Second
	staleRecoverySeconds     = 90 // last_heartbeat_at older than this → unknown_recovery (D-02)
)

// Dispatcher runs a claimed task's actual work (the per-TaskKind handlers land in
// 10-05). The tick loop owns the claim/heartbeat/release lifecycle and hands the
// dispatcher the held conn so its writes share the advisory-lock session. A nil
// dispatcher (tests / pre-10-05 wiring) makes the tick a claim-and-release probe.
type Dispatcher interface {
	Dispatch(ctx context.Context, task Task, c *Claim) error
}

// Scheduler is the in-process tick loop owning the due-task claim lifecycle.
type Scheduler struct {
	// Now is the injectable clock (W8). Defaults to time.Now; tests inject a frozen
	// clock so tick selection and reschedule are deterministic with no real sleep.
	Now func() time.Time

	store         *Store
	pool          *pgxpool.Pool
	dispatch      Dispatcher
	maxConcurrent int
	tickInterval  time.Duration
	hbInterval    time.Duration
}

// SchedulerConfig carries explicit overrides; zero fields fall through to the
// AURA_SCHEDULER_* env then the builtin defaults.
type SchedulerConfig struct {
	Dispatch      Dispatcher
	MaxConcurrent int
	TickInterval  time.Duration
	Now           func() time.Time
}

// NewScheduler builds a Scheduler over an open pool + Store. It resolves the
// concurrency cap and tick interval from the explicit config, then the
// AURA_SCHEDULER_* env, then the defaults — and defaults Now to time.Now.
func NewScheduler(pool *pgxpool.Pool, store *Store, cfg SchedulerConfig) *Scheduler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxC := cfg.MaxConcurrent
	if maxC <= 0 {
		maxC = envInt("AURA_SCHEDULER_MAX_CONCURRENT_RUNS", defaultMaxConcurrentRuns)
	}
	tick := cfg.TickInterval
	if tick <= 0 {
		tick = time.Duration(envInt("AURA_SCHEDULER_TICK_SECONDS", int(defaultTickInterval/time.Second))) * time.Second
	}
	return &Scheduler{
		Now:           now,
		store:         store,
		pool:          pool,
		dispatch:      cfg.Dispatch,
		maxConcurrent: maxC,
		tickInterval:  tick,
		hbInterval:    defaultHeartbeatInterval,
	}
}

// envInt reads a non-negative integer env var, falling back to def on unset or
// unparseable (a malformed scheduler knob is a misconfig, not a crash — the default
// is always a safe value).
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
