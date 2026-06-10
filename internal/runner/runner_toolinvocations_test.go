package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

func TestPersistEvent_PersistsToolInvocationEvents(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	ledger := newFakeToolInvocationStore()
	r.toolInvocations = ledger

	convID := newConvID(t)
	requestID := uuid.Must(uuid.NewV7())
	startedAt := time.Now().UTC()
	endedAt := startedAt.Add(37 * time.Millisecond)
	tr := &turnTracker{convID: convID}

	start := &agent.Event{
		RequestID: requestID,
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event:      agent.ToolInvocationStart,
			ToolCallID: "call-1",
			ToolName:   "shell_exec",
			Arguments:  `{"command":"echo hi"}`,
			ArgsBytes:  len(`{"command":"echo hi"}`),
			StartedAt:  &startedAt,
		}},
	}
	if err := r.persistEvent(context.Background(), tr, start); err != nil {
		t.Fatalf("persist start: %v", err)
	}

	exitCode := 0
	end := &agent.Event{
		RequestID: requestID,
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event:             agent.ToolInvocationEnd,
			ToolCallID:        "call-1",
			ToolName:          "shell_exec",
			Arguments:         `{"command":"echo hi"}`,
			ArgsBytes:         len(`{"command":"echo hi"}`),
			StartedAt:         &startedAt,
			EndedAt:           &endedAt,
			DurationMS:        37,
			Status:            "ok",
			ResultPreview:     "hi\n",
			PreviewBytes:      3,
			ResultBytes:       3,
			ResultTruncated:   false,
			ResultSidecarPath: "",
			ExitCode:          &exitCode,
			Meta:              map[string]any{"exit_code": 0},
		}},
	}
	if err := r.persistEvent(context.Background(), tr, end); err != nil {
		t.Fatalf("persist end: %v", err)
	}

	if len(ledger.events) != 2 {
		t.Fatalf("persisted %d tool invocation events, want 2", len(ledger.events))
	}
	if got := ledger.events[0]; got.ConversationID != convID || got.RequestID != requestID.String() ||
		got.Event != toolinvocations.EventStart || got.ToolCallID != "call-1" ||
		got.ToolName != "shell_exec" || got.Arguments != `{"command":"echo hi"}` {
		t.Fatalf("start ledger row = %+v", got)
	}
	if got := ledger.events[1]; got.Event != toolinvocations.EventEnd || got.Status != "ok" ||
		got.ResultBytes != 3 || got.PreviewBytes != 3 || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("end ledger row = %+v", got)
	}

	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("persisted history length = %d, want 2: %+v", len(hist), hist)
	}
	if got := hist[0]; got.Role != llm.RoleAssistant || len(got.ToolCalls) != 1 ||
		got.ToolCalls[0].ID != "call-1" ||
		got.ToolCalls[0].Function.Name != "shell_exec" ||
		got.ToolCalls[0].Function.Arguments != `{"command":"echo hi"}` {
		t.Fatalf("assistant tool_calls turn = %+v", got)
	}
	if got := hist[1]; got.Role != llm.RoleTool || got.ToolCallID != "call-1" || got.Content != "hi\n" {
		t.Fatalf("tool result turn = %+v", got)
	}
}

// TestPersistEvent_LedgerFailureDoesNotAbortTurn is the WR-01 regression: the ledger
// is observability, not a permission system (migration 0011), so a transient insert
// failure must be swallowed (logged) and the turn must continue — persistEvent returns
// nil, never the store error. A nil ledger is likewise a no-op, not a turn-killer.
func TestPersistEvent_LedgerFailureDoesNotAbortTurn(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	requestID := uuid.Must(uuid.NewV7())
	startedAt := time.Now().UTC()
	tr := &turnTracker{convID: convID}

	start := &agent.Event{
		RequestID: requestID,
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event:      agent.ToolInvocationStart,
			ToolCallID: "call-1",
			ToolName:   "shell_exec",
			StartedAt:  &startedAt,
		}},
	}

	// A failing ledger store must NOT abort the turn (persistEvent returns nil).
	failing := newFakeToolInvocationStore()
	failing.err = errors.New("transient pg hiccup")
	r.toolInvocations = failing
	if err := r.persistEvent(context.Background(), tr, start); err != nil {
		t.Fatalf("a ledger insert failure must not abort the turn, got %v", err)
	}
	if len(failing.events) != 0 {
		t.Fatalf("failing store should record nothing, got %d", len(failing.events))
	}

	// A nil ledger is likewise a logged no-op, never a turn-fatal error.
	r.toolInvocations = nil
	if err := r.persistEvent(context.Background(), tr, start); err != nil {
		t.Fatalf("a nil ledger must be a no-op, got %v", err)
	}
}

type fakeToolInvocationStore struct {
	events []toolinvocations.Event
	err    error
}

func newFakeToolInvocationStore() *fakeToolInvocationStore {
	return &fakeToolInvocationStore{}
}

func (f *fakeToolInvocationStore) Insert(_ context.Context, e toolinvocations.Event) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}
