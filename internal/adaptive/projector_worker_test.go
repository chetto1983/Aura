package adaptive

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type projectionStep struct {
	worked             bool
	err                error
	block              <-chan struct{}
	ignoreCancellation bool
}

type scriptedProjectionProcessor struct {
	mu      sync.Mutex
	steps   []projectionStep
	calls   int
	reached chan int
}

func (p *scriptedProjectionProcessor) ProjectOne(ctx context.Context) (bool, error) {
	p.mu.Lock()
	index := p.calls
	p.calls++
	step := projectionStep{}
	if index < len(p.steps) {
		step = p.steps[index]
	}
	p.mu.Unlock()
	select {
	case p.reached <- index + 1:
	default:
	}
	if step.block != nil {
		if step.ignoreCancellation {
			<-step.block
			return step.worked, step.err
		}
		select {
		case <-step.block:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return step.worked, step.err
}

func (p *scriptedProjectionProcessor) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func stopProjectorWorker(t *testing.T, worker *ProjectorWorker) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := worker.Stop(ctx); err != nil {
			t.Errorf("Stop cleanup: %v", err)
		}
	})
}

func TestProjectorWorkerDrainsReadyWorkThenPolls(t *testing.T) {
	processor := &scriptedProjectionProcessor{
		steps:   []projectionStep{{worked: true}, {worked: true}},
		reached: make(chan int, 4),
	}
	worker := NewProjectorWorker(processor, ProjectorWorkerConfig{
		PollInterval: time.Hour,
	})
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopProjectorWorker(t, worker)

	for want := 1; want <= 3; want++ {
		select {
		case got := <-processor.reached:
			if got != want {
				t.Fatalf("ProjectOne call = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("ProjectOne call %d did not arrive", want)
		}
	}
	select {
	case got := <-processor.reached:
		t.Fatalf("idle worker polled again without waiting, call %d", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestProjectorWorkerRetryBackoffIsCapped(t *testing.T) {
	worker := NewProjectorWorker(&scriptedProjectionProcessor{}, ProjectorWorkerConfig{
		ErrorBackoff:    10 * time.Millisecond,
		MaxErrorBackoff: 25 * time.Millisecond,
	})
	want := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		25 * time.Millisecond,
		25 * time.Millisecond,
	}
	for attempt, expected := range want {
		if got := worker.errorBackoff(attempt); got != expected {
			t.Fatalf("errorBackoff(%d) = %s, want %s", attempt, got, expected)
		}
	}
}

func TestProjectorWorkerContinuesAfterRetryableProjectionError(t *testing.T) {
	retryable := errors.New("graph projection retry scheduled")
	processor := &scriptedProjectionProcessor{
		steps:   []projectionStep{{worked: true, err: retryable}},
		reached: make(chan int, 3),
	}
	worker := NewProjectorWorker(processor, ProjectorWorkerConfig{
		PollInterval:    time.Hour,
		ErrorBackoff:    time.Millisecond,
		MaxErrorBackoff: time.Millisecond,
	})
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopProjectorWorker(t, worker)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	for want := 1; want <= 2; want++ {
		select {
		case <-processor.reached:
		case <-ctx.Done():
			t.Fatalf("ProjectOne call %d after retryable error: %v", want, ctx.Err())
		}
	}
}

func TestProjectorWorkerCancellationRestartAndSingleStart(t *testing.T) {
	processor := &scriptedProjectionProcessor{
		reached: make(chan int, 4),
	}
	worker := NewProjectorWorker(processor, ProjectorWorkerConfig{
		PollInterval: time.Hour,
	})
	for generation := 1; generation <= 2; generation++ {
		if err := worker.Start(context.Background()); err != nil {
			t.Fatalf("Start generation %d: %v", generation, err)
		}
		if err := worker.Start(context.Background()); !errors.Is(err, ErrProjectorWorkerRunning) {
			t.Fatalf("second Start generation %d = %v, want running error", generation, err)
		}
		select {
		case <-processor.reached:
		case <-time.After(time.Second):
			t.Fatalf("generation %d did not poll", generation)
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := worker.Stop(stopCtx); err != nil {
			cancel()
			t.Fatalf("Stop generation %d: %v", generation, err)
		}
		cancel()
	}
	if got := processor.callCount(); got != 2 {
		t.Fatalf("ProjectOne calls = %d, want 2 restarts", got)
	}
}

func TestProjectorWorkerStopHasCallerBoundedDrain(t *testing.T) {
	release := make(chan struct{})
	processor := &scriptedProjectionProcessor{
		steps: []projectionStep{{
			block: release, ignoreCancellation: true,
		}},
		reached: make(chan int, 2),
	}
	worker := NewProjectorWorker(processor, ProjectorWorkerConfig{
		PollInterval: time.Hour,
	})
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-processor.reached

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := worker.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded Stop = %v, want deadline", err)
	}
	close(release)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := worker.Stop(drainCtx); err != nil {
		t.Fatalf("Stop after release: %v", err)
	}
}
