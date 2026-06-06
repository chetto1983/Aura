package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

func TestPersistEvent_PersistsToolInvocationEvents(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
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
