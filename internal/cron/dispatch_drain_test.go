package cron

import (
	"context"
	"errors"
	"testing"
	"time"
)

type drainDeadlineHandler struct {
	initialErr chan error
}

func (h *drainDeadlineHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindAgentJob, MaxDuration: 50 * time.Millisecond}
}

func (h *drainDeadlineHandler) Run(ctx context.Context, _ Job) (string, error) {
	h.initialErr <- ctx.Err()
	<-ctx.Done()
	return "", ctx.Err()
}

func TestDispatchDetachesAdmittedJobAndEnforcesHandlerDeadline(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	cancel()

	handler := &drainDeadlineHandler{initialErr: make(chan error, 1)}
	dispatch := NewDispatch(
		map[TaskKind]Handler{KindAgentJob: handler},
		DispatchDeps{Store: &fakeCompleter{}},
	)
	done := make(chan error, 1)
	go func() {
		done <- dispatch.Dispatch(parent, Task{ID: "task-drain", Kind: KindAgentJob}, &Claim{RunID: "run-drain"})
	}()

	if err := <-handler.initialErr; err != nil {
		t.Fatalf("admitted handler started canceled by admission context: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Dispatch error = %v, want handler deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Dispatch did not enforce HandlerMeta.MaxDuration")
	}
}
