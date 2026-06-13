package runner

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
)

// fakeEvictorTool is a registry tool that records the session ids it was asked to
// evict, proving Runner.Stop reclaims per-session tool state (R-41 / AP-16).
type fakeEvictorTool struct {
	mu      sync.Mutex
	evicted []string
}

func (e *fakeEvictorTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "fake_evictor",
		Summary:     "Records eviction calls.",
		Description: "Records eviction calls.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (e *fakeEvictorTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Preview: "ok"}, nil
}

func (e *fakeEvictorTool) Evict(sessionID string) {
	e.mu.Lock()
	e.evicted = append(e.evicted, sessionID)
	e.mu.Unlock()
}

func (e *fakeEvictorTool) calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.evicted...)
}

// TestStop_EvictsSessionToolState proves Runner.Stop triggers eviction of the
// conversation's session-scoped tool state: every registry tool implementing
// SessionEvictor gets Evict(convID) called. A non-evictor tool is untouched (no
// panic), confirming the type-assertion gate.
func TestStop_EvictsSessionToolState(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	ev := &fakeEvictorTool{}
	r.registry.Register(ev)

	convID := newConvID(t)
	if err := r.Stop(context.Background(), convID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got := ev.calls()
	if len(got) != 1 || got[0] != convID {
		t.Fatalf("Evict calls = %v, want exactly [%s]", got, convID)
	}
}
