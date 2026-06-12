package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/google/uuid"
)

// fakeToolSelectObserver records every (request, usedTool) the runner passes to
// Observe — the proof the Open-Q #3 capture site is wired LIVE in the production turn
// path (not just exercised by the synthetic exemplar unit tests in
// internal/agent/tools/search_learn_test.go).
type fakeToolSelectObserver struct {
	mu       sync.Mutex
	observed [][2]string
	closed   bool
}

func (f *fakeToolSelectObserver) Observe(request, usedTool string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observed = append(f.observed, [2]string{request, usedTool})
}

func (f *fakeToolSelectObserver) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func (f *fakeToolSelectObserver) calls() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][2]string, len(f.observed))
	copy(out, f.observed)
	return out
}

// TestPersistEvent_ObservesToolSelectionOnRealTurn is the Open-Q #3 LIVE-WIRING proof:
// a real turn that EXECUTES a tool (a ToolInvocationEnd event flowing through
// persistEvent) must invoke the tool-select learner's Observe with the round's
// (userMsg, used-tool) — on the named off-hot-path, ToolName-bearing, best-effort
// branch in runner_persist.go. This resolves the "tested-but-not-wired" risk
// (T-08.2-20): the detection loop is reachable in production, not a silent dead loop.
func TestPersistEvent_ObservesToolSelectionOnRealTurn(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	obs := &fakeToolSelectObserver{}
	r.toolSelectLearner = obs

	convID := newConvID(t)
	requestID := uuid.Must(uuid.NewV7())
	startedAt := time.Now().UTC()
	endedAt := startedAt.Add(10 * time.Millisecond)
	// The tracker carries the round's user request (threaded in turnLocked) so the
	// capture site can pair it with the executed tool name.
	tr := &turnTracker{convID: convID, userMsg: "quanto costa il bitcoin adesso?"}

	end := &agent.Event{
		RequestID: requestID,
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event:         agent.ToolInvocationEnd,
			ToolCallID:    "call-1",
			ToolName:      "shell_exec", // the model improvised — the bitcoin mis-route
			StartedAt:     &startedAt,
			EndedAt:       &endedAt,
			Status:        "ok",
			ResultPreview: "...",
		}},
	}
	if err := r.persistEvent(context.Background(), tr, end); err != nil {
		t.Fatalf("persist end: %v", err)
	}

	got := obs.calls()
	if len(got) != 1 {
		t.Fatalf("Observe called %d times on a real tool-executing turn; want exactly 1 (Open-Q #3 wiring)", len(got))
	}
	if got[0][0] != "quanto costa il bitcoin adesso?" || got[0][1] != "shell_exec" {
		t.Errorf("Observe got (%q, %q); want the round's userMsg + the executed tool name", got[0][0], got[0][1])
	}
}

// A resume round (no userMsg) must NOT capture a signal — there is no fresh request to
// pair with, and the tool turns were already produced by the original request.
func TestPersistEvent_NoObserveWithoutUserMsg(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	obs := &fakeToolSelectObserver{}
	r.toolSelectLearner = obs

	convID := newConvID(t)
	startedAt := time.Now().UTC()
	endedAt := startedAt.Add(5 * time.Millisecond)
	tr := &turnTracker{convID: convID} // userMsg empty (resume round)

	end := &agent.Event{
		RequestID: uuid.Must(uuid.NewV7()),
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event:      agent.ToolInvocationEnd,
			ToolCallID: "call-1",
			ToolName:   "shell_exec",
			StartedAt:  &startedAt,
			EndedAt:    &endedAt,
			Status:     "ok",
		}},
	}
	if err := r.persistEvent(context.Background(), tr, end); err != nil {
		t.Fatalf("persist end: %v", err)
	}
	if len(obs.calls()) != 0 {
		t.Errorf("Observe was called on a resume round (no userMsg); want 0")
	}
}

// A nil tool-select learner is a no-op (the loop is off): the capture site must not
// panic when no learner is wired.
func TestPersistEvent_NilToolSelectLearnerIsNoOp(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.toolSelectLearner = nil

	convID := newConvID(t)
	startedAt := time.Now().UTC()
	endedAt := startedAt.Add(5 * time.Millisecond)
	tr := &turnTracker{convID: convID, userMsg: "find a restaurant"}

	end := &agent.Event{
		RequestID: uuid.Must(uuid.NewV7()),
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event:      agent.ToolInvocationEnd,
			ToolCallID: "call-1",
			ToolName:   "web_search",
			StartedAt:  &startedAt,
			EndedAt:    &endedAt,
			Status:     "ok",
		}},
	}
	if err := r.persistEvent(context.Background(), tr, end); err != nil {
		t.Fatalf("nil tool-select learner must be a no-op, got %v", err)
	}
}
