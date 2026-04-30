package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DefaultTickInterval is how often the scheduler wakes to check for due
// tasks in production. Override via Config.TickInterval in tests.
const DefaultTickInterval = 30 * time.Second

// Dispatcher is the user-supplied function that does the actual work
// when a task fires. The scheduler calls Dispatch once per due task,
// outside the tick loop's lock, so handlers can be slow.
type Dispatcher func(ctx context.Context, task *Task) error

// Config wires a scheduler to its store, dispatcher, and timing knobs.
type Config struct {
	Store        *Store
	Dispatcher   Dispatcher
	Logger       *slog.Logger
	Location     *time.Location // defaults to time.Local
	TickInterval time.Duration  // defaults to DefaultTickInterval
	// Now is overridable for tests; defaults to time.Now.
	Now func() time.Time
}

// Scheduler runs a tick loop that picks up due tasks from the store and
// hands them to the dispatcher. Tasks survive process restarts because
// state lives in SQLite — Start() picks up where the previous run left
// off.
type Scheduler struct {
	store      *Store
	dispatcher Dispatcher
	logger     *slog.Logger
	loc        *time.Location
	tick       time.Duration
	now        func() time.Time

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

func New(cfg Config) (*Scheduler, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("scheduler: store required")
	}
	if cfg.Dispatcher == nil {
		return nil, fmt.Errorf("scheduler: dispatcher required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	loc := cfg.Location
	if loc == nil {
		loc = time.Local
	}
	tick := cfg.TickInterval
	if tick <= 0 {
		tick = DefaultTickInterval
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Scheduler{
		store:      cfg.Store,
		dispatcher: cfg.Dispatcher,
		logger:     logger,
		loc:        loc,
		tick:       tick,
		now:        now,
	}, nil
}

// Start launches the tick loop in a new goroutine. Returns a cancel
// function the caller invokes during shutdown. Idempotent — calling
// Start twice is a no-op on the second call.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	go s.run(loopCtx)
}

// Stop cancels the tick loop. Returns immediately; in-flight dispatches
// continue until ctx hands them a cancellation.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
}

func (s *Scheduler) run(ctx context.Context) {
	s.logger.Info("scheduler started", "tick", s.tick)
	t := time.NewTicker(s.tick)
	defer t.Stop()

	// One immediate tick on startup so tasks that came due while the bot
	// was offline fire as soon as we boot.
	s.runTick(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-t.C:
			s.runTick(ctx)
		}
	}
}

func (s *Scheduler) runTick(ctx context.Context) {
	now := s.now().UTC()
	due, err := s.store.DueTasks(ctx, now)
	if err != nil {
		s.logger.Warn("scheduler: query due tasks failed", "err", err)
		return
	}
	for _, task := range due {
		s.fireOne(ctx, task, now)
	}
}

func (s *Scheduler) fireOne(ctx context.Context, task *Task, firedAt time.Time) {
	s.logger.Info("scheduler firing task",
		"id", task.ID, "name", task.Name, "kind", task.Kind,
		"scheduled_for", task.NextRunAt.Format(time.RFC3339),
	)

	dispatchErr := s.dispatcher(ctx, task)

	nextRun, nextStatus, lastErr := s.advance(task, firedAt, dispatchErr)
	if err := s.store.MarkFired(ctx, task.ID, firedAt, nextRun, nextStatus, lastErr); err != nil {
		s.logger.Warn("scheduler: persist fire result failed", "task", task.Name, "err", err)
		return
	}
	if dispatchErr != nil {
		s.logger.Warn("scheduler dispatch failed",
			"id", task.ID, "name", task.Name, "kind", task.Kind, "err", dispatchErr,
		)
	} else {
		s.logger.Info("scheduler task complete",
			"id", task.ID, "name", task.Name, "kind", task.Kind,
			"next_run", nextRun.Format(time.RFC3339), "status", nextStatus,
		)
	}
}

// advance computes the post-fire next_run_at, status, and last_error for
// a task. Pure function so it's easy to unit-test the state transitions.
func (s *Scheduler) advance(task *Task, firedAt time.Time, dispatchErr error) (nextRun time.Time, status Status, lastErr string) {
	if dispatchErr != nil {
		lastErr = dispatchErr.Error()
	}

	if task.IsRecurring() {
		next, err := NextDailyRun(task.ScheduleDaily, s.loc, firedAt)
		if err != nil {
			// Schedule string corrupted on disk — surface and keep the
			// task active so a future fix doesn't require a manual
			// re-schedule. Use a far-future next_run so we don't busy-
			// loop on the bad row.
			return firedAt.Add(24 * time.Hour), StatusFailed,
				fmt.Sprintf("compute next_run failed: %v", err)
		}
		return next, StatusActive, lastErr
	}

	// at-schedule: one-shot. Mark done (or failed) and freeze next_run
	// at the original schedule for audit history.
	if dispatchErr != nil {
		return task.NextRunAt, StatusFailed, lastErr
	}
	return task.NextRunAt, StatusDone, ""
}
