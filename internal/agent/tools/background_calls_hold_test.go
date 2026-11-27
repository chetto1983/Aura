package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/pausable"
)

// holdTool waits on the operator the way an MCP call with a server's form does
// (mcptools' askRun holds every clock above the call), then runs on unheld.
type holdTool struct{ held, after time.Duration }

func (h holdTool) Spec() Spec { return Spec{Name: "form"} }

func (h holdTool) Execute(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
	release := pausable.Hold(ctx)
	time.Sleep(h.held)
	release()
	select {
	case <-time.After(h.after):
		return ToolResult{Preview: "answered"}, nil
	case <-ctx.Done():
		return ToolResult{}, ctx.Err()
	}
}

// The window is long next to a goroutine's start, so the hold is taken before it can end.
const holdWindow = 200 * time.Millisecond

func TestBackgroundCallsHoldTheWindowWhileTheOperatorAnswers(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")

	res, err, promoted := calls.Execute(turn, holdTool{held: 3 * holdWindow}, nil, holdWindow, testCeiling, nil)
	if err != nil || promoted || res.Preview != "answered" {
		t.Fatalf("Execute = %q, %v, promoted %v; want the answer in its turn", res.Preview, err, promoted)
	}
}

func TestBackgroundCallsCountTheWindowAgainOnceTheHoldEnds(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")

	start := time.Now()
	_, err, promoted := calls.Execute(turn, holdTool{held: 3 * holdWindow, after: 2 * holdWindow}, nil, holdWindow, testCeiling, nil)
	if err != nil || !promoted {
		t.Fatalf("Execute: err %v, promoted %v; want the call moved once its unheld time passed the window", err, promoted)
	}
	if took := time.Since(start); took < 4*holdWindow {
		t.Fatalf("moved after %v, before the hold plus the window (%v)", took, 4*holdWindow)
	}
}
