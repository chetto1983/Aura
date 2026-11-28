package runner

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
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
	framed := agent.FrameUntrustedPromptContent("shell_exec", "hi\n")
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
			HistoryContent:    framed,
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
		got.ResultBytes != 3 || got.PreviewBytes != 3 || got.ExitCode == nil || *got.ExitCode != 0 ||
		got.ResultPreview != "hi\n" {
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
	// The ledger keeps the raw preview; the conversation keeps what the model read.
	if got := hist[1]; got.Role != llm.RoleTool || got.ToolCallID != "call-1" || got.Content != framed {
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

// A refused call's own end row says refused too (spec 2026-10-12, finding 4). recordDenyFact
// is best-effort and both writers insert with ON CONFLICT DO NOTHING, so this row can be the
// one a reload reads. The agent's meta map is copied, never written.
func TestPersistToolInvocationLedgerStampsARefusal(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	ledger := newFakeToolInvocationStore()
	r.toolInvocations = ledger
	tr := &turnTracker{convID: newConvID(t)}
	agentMeta := map[string]any{"exit_code": 1}
	for _, tc := range []struct {
		refused bool
		meta    map[string]any
		want    map[string]any
	}{
		{true, agentMeta, map[string]any{"exit_code": 1, "gateway_verdict": "deny"}},
		{true, nil, map[string]any{"gateway_verdict": "deny"}},
		{false, agentMeta, map[string]any{"exit_code": 1}},
	} {
		ledger.events = nil
		ev := &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationEnd, ToolCallID: "call-1", ToolName: "web_search",
			Status: "error", Error: "tool dispatch denied by policy", Refused: tc.refused, Meta: tc.meta,
		}}}
		if err := r.persistToolInvocationLedger(context.Background(), tr, ev); err != nil {
			t.Fatalf("persist: %v", err)
		}
		if got := ledger.events[0].Meta; !maps.Equal(got, tc.want) {
			t.Errorf("refused=%v: end row meta = %v, want %v", tc.refused, got, tc.want)
		}
	}
	if _, stamped := agentMeta["gateway_verdict"]; stamped {
		t.Fatal("the stamp wrote into the agent's own meta map")
	}
}

// ctxStrictConv and ctxStrictLedger refuse a write whose context is done, as Postgres does.
// The shared fakes ignore ctx, which hid the failure below.
type ctxStrictConv struct{ *fakeConvStore }

func (c ctxStrictConv) AppendTurn(ctx context.Context, p conversations.AppendTurnParams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.fakeConvStore.AppendTurn(ctx, p)
}

type ctxStrictLedger struct{ *fakeToolInvocationStore }

func (l ctxStrictLedger) Insert(ctx context.Context, e toolinvocations.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.fakeToolInvocationStore.Insert(ctx, e)
}

// MEASURED on the lab VM, 2026-10-13 (acceptance item 5, after-2): docker stop while
// browser__agent_browser_open ran. The shutdown cancelled the turn, as it must; the tool
// came back with "context canceled", and the runner wrote its result with that same context:
// "persist tool result turn …: append turn …: context canceled". The call's end row and its
// tool turn were never written, so the ledger kept a start without an end and the history an
// assistant tool_call without its answer. The end of a call that ran is a fact to record,
// whatever happened to the turn that was waiting for it.
func TestPersistEvent_RecordsAToolEndAfterTheTurnIsCancelled(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.Conv = ctxStrictConv{conv}
	ledger := newFakeToolInvocationStore()
	r.toolInvocations = ctxStrictLedger{ledger}
	convID := newConvID(t)
	tr := &turnTracker{convID: convID}
	requestID := uuid.Must(uuid.NewV7())
	startedAt := time.Now().UTC()
	call := func(event string) *agent.Event {
		return &agent.Event{RequestID: requestID, Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: event, ToolCallID: "call_86nz4f3i", ToolName: "browser__agent_browser_open",
			Arguments: `{"url":"https://arcadedb.com"}`, StartedAt: &startedAt,
		}}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := r.persistEvent(ctx, tr, call(agent.ToolInvocationStart)); err != nil {
		t.Fatalf("persist start: %v", err)
	}
	cancel()
	end := call(agent.ToolInvocationEnd)
	end.Actions.ToolInvocation.Status = "error"
	end.Actions.ToolInvocation.ResultPreview = "context canceled"
	end.Actions.ToolInvocation.HistoryContent = "context canceled"

	if err := r.persistEvent(ctx, tr, end); err != nil {
		t.Fatalf("persist end after the cancel: %v", err)
	}
	if len(ledger.events) != 2 || ledger.events[1].Event != toolinvocations.EventEnd {
		t.Fatalf("ledger = %+v, want the start and its end", ledger.events)
	}
	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(hist) != 2 || hist[1].Role != llm.RoleTool || hist[1].ToolCallID != "call_86nz4f3i" {
		t.Fatalf("history = %+v, want the call and its result", hist)
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
