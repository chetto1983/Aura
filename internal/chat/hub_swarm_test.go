package chat

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aura/aura/internal/identity"
)

// swarmFakeAuthorizer is a minimal Authorizer for swarm dispatch tests.
// allow lists the capabilities that are granted; everything else is denied.
type swarmFakeAuthorizer struct {
	allow map[identity.Capability]bool
}

func (a *swarmFakeAuthorizer) Authorize(_ context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	if a.allow[params.Capability] {
		return identity.AuthorizationDecision{Decision: identity.DecisionAllow}, nil
	}
	return identity.AuthorizationDecision{Decision: identity.DecisionDeny}, nil
}

// TestHubSwarm_DispatchAndComplete verifies ChannelSwarm dispatches a run and
// the run completes silently with RunStatusCompleted.
func TestHubSwarm_DispatchAndComplete(t *testing.T) {
	loop := &recordingLoop{}
	h := newHub(t, loop)
	h.RegisterOutbound(&fakeOutbound{channel: ChannelSwarm, mode: DeliveryModeSilent})

	run, err := h.ReceiveMessage(context.Background(), InboundMessage{
		Channel:     ChannelSwarm,
		Mode:        DeliveryModeSilent,
		PrincipalID: "user-1",
		Text:        "search for X",
	})
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if run.Status != RunStatusCompleted {
		t.Fatalf("run.Status = %s, want completed", run.Status)
	}

	loop.mu.Lock()
	calls := loop.calls
	loop.mu.Unlock()
	if calls != 1 {
		t.Fatalf("loop called %d times, want 1", calls)
	}
}

// TestHubSwarm_WaitForRunBlocksUntilDone verifies WaitForRun blocks while the
// run is in-flight and returns the correct Result once it completes.
func TestHubSwarm_WaitForRunBlocksUntilDone(t *testing.T) {
	loopReady := make(chan struct{})
	loopProceed := make(chan struct{})

	loop := blockingLoopFn(func(ctx context.Context, run *Run, _ InboundMessage, _ EmitFn) error {
		run.Metadata["final_text"] = "search results"
		close(loopReady)
		select {
		case <-loopProceed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	h := newHub(t, loop)

	var capturedRunID atomic.Value
	out := outboundFn(ChannelSwarm, DeliveryModeSilent, func(ev OutboundEvent) error {
		if ev.Type == EventRunStarted {
			capturedRunID.Store(ev.RunID)
		}
		return nil
	})
	h.RegisterOutbound(out)

	dispatchDone := make(chan error, 1)
	go func() {
		_, err := h.ReceiveMessage(context.Background(), InboundMessage{
			Channel:     ChannelSwarm,
			Mode:        DeliveryModeSilent,
			PrincipalID: "user-1",
			Text:        "search task",
		})
		dispatchDone <- err
	}()

	// Wait for the loop to have started.
	select {
	case <-loopReady:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not start within 2s")
	}
	runID, _ := capturedRunID.Load().(string)
	if runID == "" {
		t.Fatal("runID not captured from EventRunStarted")
	}

	// Signal loop completion after a short delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(loopProceed)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := h.WaitForRun(ctx, runID)
	if err != nil {
		t.Fatalf("WaitForRun: %v", err)
	}
	if result.ReplyText != "search results" {
		t.Fatalf("ReplyText = %q, want %q", result.ReplyText, "search results")
	}

	select {
	case dispatchErr := <-dispatchDone:
		if dispatchErr != nil {
			t.Fatalf("dispatch error: %v", dispatchErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch goroutine did not complete")
	}
}

// TestHubSwarm_GenericGrantCoversTools verifies that an actor holding the
// generic CapabilityToolExecute grant is allowed to dispatch a swarm child
// for any tool in the allowlist, even without per-tool granular grants.
// Owner-style principals carry the generic grant (TelegramOwnerCapabilities)
// — they should not be blocked by missing tool.execute.<name> rows.
func TestHubSwarm_GenericGrantCoversTools(t *testing.T) {
	loop := &recordingLoop{}
	h := newHub(t, loop)
	h.RegisterOutbound(&fakeOutbound{channel: ChannelSwarm, mode: DeliveryModeSilent})

	// Allow generic tool.execute only — NO granular tool.execute.web_search.
	authz := &swarmFakeAuthorizer{
		allow: map[identity.Capability]bool{
			identity.CapabilityToolExecute: true,
		},
	}
	ctx := identity.WithAuthorizer(context.Background(), authz)

	_, err := h.ReceiveMessage(ctx, InboundMessage{
		Channel:     ChannelSwarm,
		Mode:        DeliveryModeSilent,
		PrincipalID: "user-1",
		ChannelData: map[string]any{
			"tool_allowlist": []string{"web_search"},
		},
	})
	if err != nil {
		t.Fatalf("expected dispatch to succeed with generic grant, got error: %v", err)
	}

	loop.mu.Lock()
	calls := loop.calls
	loop.mu.Unlock()
	if calls != 1 {
		t.Fatalf("agent loop was called %d times, want 1 (dispatch should proceed)", calls)
	}
}

// TestHubSwarm_NoGenericGrantDenied verifies that an actor missing the generic
// CapabilityToolExecute grant is denied — even with no allowlist. This is the
// inverse of GenericGrantCoversTools and locks the no-grant deny path.
func TestHubSwarm_NoGenericGrantDenied(t *testing.T) {
	loop := &recordingLoop{}
	h := newHub(t, loop)
	h.RegisterOutbound(&fakeOutbound{channel: ChannelSwarm, mode: DeliveryModeSilent})

	authz := &swarmFakeAuthorizer{allow: map[identity.Capability]bool{}}
	ctx := identity.WithAuthorizer(context.Background(), authz)

	_, err := h.ReceiveMessage(ctx, InboundMessage{
		Channel:     ChannelSwarm,
		Mode:        DeliveryModeSilent,
		PrincipalID: "user-1",
		ChannelData: map[string]any{
			"tool_allowlist": []string{"web_search"},
		},
	})
	if err == nil {
		t.Fatal("expected error when generic tool.execute grant is missing")
	}

	loop.mu.Lock()
	calls := loop.calls
	loop.mu.Unlock()
	if calls != 0 {
		t.Fatalf("agent loop was called %d times, want 0 (auth denied before loop)", calls)
	}
}

// TestHubSwarm_BudgetExceededCancellation verifies that WaitForRun cancels the
// in-flight run when its context expires and returns context.DeadlineExceeded.
func TestHubSwarm_BudgetExceededCancellation(t *testing.T) {
	loop := blockingLoopFn(func(ctx context.Context, _ *Run, _ InboundMessage, _ EmitFn) error {
		<-ctx.Done()
		return ctx.Err()
	})

	h := newHub(t, loop)

	var capturedRunID atomic.Value
	out := outboundFn(ChannelSwarm, DeliveryModeSilent, func(ev OutboundEvent) error {
		if ev.Type == EventRunStarted {
			capturedRunID.Store(ev.RunID)
		}
		return nil
	})
	h.RegisterOutbound(out)

	go func() {
		_, _ = h.ReceiveMessage(context.Background(), InboundMessage{
			Channel:     ChannelSwarm,
			Mode:        DeliveryModeSilent,
			PrincipalID: "user-1",
			Text:        "blocking task",
		})
	}()

	// Wait for run_started to obtain the runID.
	var runID string
	for i := 0; i < 200; i++ {
		if id, _ := capturedRunID.Load().(string); id != "" {
			runID = id
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if runID == "" {
		t.Fatal("never observed run_started event")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := h.WaitForRun(ctx, runID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	// Allow the background goroutine to finish its cancellation cleanup.
	time.Sleep(300 * time.Millisecond)
}
