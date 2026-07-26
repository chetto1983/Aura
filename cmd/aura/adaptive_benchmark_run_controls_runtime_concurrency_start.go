package main

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const adaptiveBenchmarkConcurrencyBlockedObservation = 100 * time.Millisecond

func adaptiveBenchmarkStartConcurrencyClients(
	ctx context.Context,
	states map[string]*adaptiveBenchmarkConcurrencyClientState,
	start func(
		context.Context,
		*adaptiveBenchmarkConcurrencyClientState,
	) error,
	proveBlocked func(
		context.Context,
		*adaptiveBenchmarkConcurrencyClientState,
	) error,
) error {
	if start == nil || proveBlocked == nil {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark concurrency start dependencies are invalid",
		)
	}
	for _, clientID := range []string{"C0", "C1", "C2", "C3"} {
		if states[clientID] == nil {
			return adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency client is unavailable",
			)
		}
	}
	if err := start(ctx, states["C0"]); err != nil {
		return err
	}
	if err := adaptiveBenchmarkWaitForConcurrencyAssignment(
		ctx,
		states["C0"],
	); err != nil {
		return err
	}
	if err := start(ctx, states["C1"]); err != nil {
		return err
	}
	if err := adaptiveBenchmarkWaitSignal(
		ctx, states["C1"].started, "C1 start",
	); err != nil {
		return err
	}
	if err := proveBlocked(ctx, states["C1"]); err != nil {
		return err
	}
	if err := start(ctx, states["C2"]); err != nil {
		return err
	}
	if err := start(ctx, states["C3"]); err != nil {
		return err
	}
	return nil
}

func adaptiveBenchmarkWaitForConcurrencyAssignment(
	ctx context.Context,
	state *adaptiveBenchmarkConcurrencyClientState,
) error {
	if state == nil {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark concurrency client is unavailable",
		)
	}
	select {
	case <-state.assigned:
		return nil
	case <-state.done:
		state.mu.Lock()
		turnErr := state.turnErr
		state.mu.Unlock()
		return errors.Join(
			turnErr,
			adaptiveBenchmarkControlError(
				"adaptive benchmark "+state.spec.ClientID+
					" terminated before "+string(state.domain)+
					" assignment",
			),
		)
	case <-ctx.Done():
		return errors.Join(
			adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency wait failed: "+
					state.spec.ClientID+" assignment",
			),
			context.Cause(ctx),
		)
	}
}

func adaptiveBenchmarkProveConcurrencyClientBlocked(
	ctx context.Context,
	state *adaptiveBenchmarkConcurrencyClientState,
	observation time.Duration,
) error {
	if state == nil || observation <= 0 {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark concurrency blocked proof is invalid",
		)
	}
	timer := time.NewTimer(observation)
	defer timer.Stop()
	select {
	case <-state.assigned:
		return adaptiveBenchmarkControlError(
			"adaptive benchmark C1 reached assignment while C0 held the lock",
		)
	case <-timer.C:
	case <-ctx.Done():
		return errors.Join(
			adaptiveBenchmarkControlError(
				"adaptive benchmark C1 blocked proof timed out",
			),
			context.Cause(ctx),
		)
	}
	state.mu.Lock()
	assignmentID := state.assignmentID
	state.mu.Unlock()
	if assignmentID != uuid.Nil {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark C1 created an assignment while blocked",
		)
	}
	return nil
}

func adaptiveBenchmarkShutdownConcurrencyClients(
	cancel context.CancelFunc,
	started []*adaptiveBenchmarkConcurrencyClientState,
) {
	cancel()
	for _, state := range started {
		state.signalRelease()
	}
	for _, state := range started {
		<-state.done
	}
}

func adaptiveBenchmarkReleaseConcurrencyClient(
	ctx context.Context,
	state *adaptiveBenchmarkConcurrencyClientState,
) error {
	state.signalRelease()
	return adaptiveBenchmarkWaitSignal(
		ctx,
		state.released,
		state.spec.ClientID+" release",
	)
}

func (state *adaptiveBenchmarkConcurrencyClientState) signalRelease() {
	state.releaseOnce.Do(func() {
		close(state.release)
	})
}
