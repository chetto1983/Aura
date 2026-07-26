package adaptive

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultProjectorPollInterval    = time.Second
	defaultProjectorErrorBackoff    = 250 * time.Millisecond
	defaultProjectorMaxErrorBackoff = 30 * time.Second
)

// ErrProjectorWorkerRunning rejects duplicate goroutines over one worker.
var ErrProjectorWorkerRunning = errors.New("adaptive projector worker is already running")

type projectionProcessor interface {
	ProjectOne(context.Context) (bool, error)
}

// ProjectorWorkerConfig bounds idle polling and consecutive-error backoff.
type ProjectorWorkerConfig struct {
	PollInterval    time.Duration
	ErrorBackoff    time.Duration
	MaxErrorBackoff time.Duration
}

// ProjectorWorker owns one restartable ProjectOne polling lifecycle.
type ProjectorWorker struct {
	processor       projectionProcessor
	pollInterval    time.Duration
	errorBackoffMin time.Duration
	errorBackoffMax time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewProjectorWorker applies bounded polling defaults without starting work.
func NewProjectorWorker(
	processor projectionProcessor,
	cfg ProjectorWorkerConfig,
) *ProjectorWorker {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultProjectorPollInterval
	}
	errorBackoffMin := cfg.ErrorBackoff
	if errorBackoffMin <= 0 {
		errorBackoffMin = defaultProjectorErrorBackoff
	}
	errorBackoffMax := cfg.MaxErrorBackoff
	if errorBackoffMax <= 0 {
		errorBackoffMax = defaultProjectorMaxErrorBackoff
	}
	if errorBackoffMax < errorBackoffMin {
		errorBackoffMax = errorBackoffMin
	}
	return &ProjectorWorker{
		processor:       processor,
		pollInterval:    pollInterval,
		errorBackoffMin: errorBackoffMin,
		errorBackoffMax: errorBackoffMax,
	}
}

// Start launches one immediate poll followed by bounded waits.
func (w *ProjectorWorker) Start(ctx context.Context) error {
	if w == nil || w.processor == nil {
		return errors.New("adaptive projector worker is not configured")
	}
	if ctx == nil {
		return errors.New("adaptive projector worker requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	w.mu.Lock()
	if w.done != nil {
		select {
		case <-w.done:
			w.cancel = nil
			w.done = nil
		default:
			w.mu.Unlock()
			return ErrProjectorWorkerRunning
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	w.mu.Unlock()

	go w.run(runCtx, done)
	return nil
}

// Stop cancels and joins the active generation within the caller's deadline.
func (w *ProjectorWorker) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("adaptive projector worker requires a stop context")
	}

	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.mu.Unlock()
	if done == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}

	select {
	case <-done:
		w.mu.Lock()
		if w.done == done {
			w.cancel = nil
			w.done = nil
		}
		w.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *ProjectorWorker) run(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	failures := 0
	for {
		worked, err := w.processor.ProjectOne(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("adaptive projector: project outbox event", "err", err)
			if !waitProjector(ctx, w.errorBackoff(failures)) {
				return
			}
			failures++
			continue
		}
		failures = 0
		if worked {
			continue
		}
		if !waitProjector(ctx, w.pollInterval) {
			return
		}
	}
}

func (w *ProjectorWorker) errorBackoff(attempt int) time.Duration {
	delay := w.errorBackoffMin
	for step := 0; step < attempt && delay < w.errorBackoffMax; step++ {
		if delay > w.errorBackoffMax/2 {
			return w.errorBackoffMax
		}
		delay *= 2
	}
	if delay > w.errorBackoffMax {
		return w.errorBackoffMax
	}
	return delay
}

func waitProjector(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
