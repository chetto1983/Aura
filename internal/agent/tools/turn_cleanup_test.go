package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTurnCleanupRunsEachKeyOnceNewestFirst(t *testing.T) {
	ctx, cleanup := WithTurnCleanup(context.Background())
	var order []string
	step := func(name string) func(context.Context) error {
		return func(context.Context) error { order = append(order, name); return nil }
	}
	TurnCleanupFromContext(ctx).Add("a", step("a"))
	TurnCleanupFromContext(ctx).Add("b", step("b"))
	TurnCleanupFromContext(ctx).Add("a", step("a-again"))

	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Equal(order, []string{"b", "a"}) {
		t.Fatalf("order = %v, want [b a]", order)
	}
	if err := cleanup.Run(context.Background()); err != nil || len(order) != 2 {
		t.Fatalf("a second Run repeated a step: %v, %v", order, err)
	}
}

func TestTurnCleanupRunsEveryStepAndJoinsTheFailures(t *testing.T) {
	_, cleanup := WithTurnCleanup(context.Background())
	first, second := errors.New("first"), errors.New("second")
	ran := 0
	cleanup.Add("1", func(context.Context) error { ran++; return first })
	cleanup.Add("2", func(context.Context) error { ran++; return second })

	err := cleanup.Run(context.Background())

	if ran != 2 || !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("ran %d steps, err = %v; want both run and both reported", ran, err)
	}
}

func TestTurnCleanupRunsEveryStepWhenOneStepPanics(t *testing.T) {
	_, cleanup := WithTurnCleanup(context.Background())
	ran := 0
	cleanup.Add("1", func(context.Context) error { ran++; return nil })
	// Registered after "1", so it runs FIRST (newest first) and its panic must not
	// stop "1" from running behind it.
	cleanup.Add("2", func(context.Context) error { panic("boom") })

	err := cleanup.Run(context.Background())

	if ran != 1 {
		t.Fatalf("step \"1\" ran %d times, want 1: a panicking step must not stop the rest", ran)
	}
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("Run error = %v, want it to mention the panic", err)
	}
}

func TestTurnCleanupOutsideARunIsNil(t *testing.T) {
	cleanup := TurnCleanupFromContext(context.Background())
	if cleanup != nil {
		t.Fatalf("a bare context carries a cleanup: %v", cleanup)
	}
	cleanup.Add("k", func(context.Context) error {
		t.Fatal("a nil cleanup must not keep a step")
		return nil
	})
	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("nil Run: %v", err)
	}
}

func TestTurnCleanupAcceptsConcurrentCalls(t *testing.T) {
	_, cleanup := WithTurnCleanup(context.Background())
	var ran atomic.Int32
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			cleanup.Add(fmt.Sprint(i%4), func(context.Context) error { ran.Add(1); return nil })
		})
	}
	wg.Wait()

	if err := cleanup.Run(context.Background()); err != nil || ran.Load() != 4 {
		t.Fatalf("ran %d steps (err %v), want one per distinct key", ran.Load(), err)
	}
}
