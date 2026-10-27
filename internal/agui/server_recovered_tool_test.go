package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

func TestRecoveredToolResultIsNotProjectedAsSuccess(t *testing.T) {
	var call llm.ToolCall
	call.ID, call.Type, call.Function.Name = "recovered-call", "function", "shell_exec"
	unknown := `error: previous result unknown after crash recovery for tool "shell_exec"; verify before re-running this tool call.`
	for _, content := range []string{unknown, "9137", "error: ordinary command output"} {
		history := []llm.Message{
			{Role: llm.RoleUser, Content: unknown},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
			{Role: llm.RoleTool, ToolCallID: call.ID, Content: content},
		}
		encoded, err := json.Marshal(projectDisplaySnapshotWithFacts(linearTurns(history), "", nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Messages []struct {
				Content string `json:"content"`
				IsError bool   `json:"isError"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Messages[0].IsError || decoded.Messages[2].IsError != (content == unknown) {
			t.Fatalf("incorrect recovery outcome: %s", encoded)
		}
		if decoded.Messages[2].Content != content {
			t.Fatalf("result changed: %s", encoded)
		}
	}
}

// TestPendingPauseIsAwaitingNotFailed pins that a call whose turn waits on the person's
// answer replays as waiting: the recovery placeholder LoadHistory pairs with it is dropped
// and the call carries awaitingInput. A call with no pending pause keeps the error flag.
func TestPendingPauseIsAwaitingNotFailed(t *testing.T) {
	var call llm.ToolCall
	call.ID, call.Type, call.Function.Name = "pause-call", "function", "ask_user"
	cases := []struct {
		name         string
		pending      map[string]struct{}
		wantMessages int
		wantAwaiting bool
	}{
		{"pending pause", map[string]struct{}{call.ID: {}}, 2, true},
		{"no pending pause", nil, 3, false},
		{"another call's pause", map[string]struct{}{"other-call": {}}, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := projectDisplaySnapshotWithFacts(linearTurns([]llm.Message{
				{Role: llm.RoleUser, Content: "remind me a week before"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
				{Role: llm.RoleTool, ToolCallID: call.ID, Content: conversations.RecoveryToolResultContent(call)},
			}), "", nil, nil)
			markAwaitingInput(&snap, tc.pending)
			if len(snap.Messages) != tc.wantMessages {
				t.Fatalf("messages = %d, want %d", len(snap.Messages), tc.wantMessages)
			}
			if got := snap.Messages[1].ToolCalls[0].AwaitingInput; got != tc.wantAwaiting {
				t.Fatalf("awaitingInput = %v, want %v", got, tc.wantAwaiting)
			}
			if !tc.wantAwaiting && !snap.Messages[2].IsError {
				t.Fatal("a call with no pending pause lost its recovery error flag")
			}
		})
	}
}

// replayedMessage is a snapshot message as the cockpit reads it off the wire.
type replayedMessage struct {
	Role       string `json:"role"`
	ToolCallID string `json:"toolCallId"`
	IsError    bool   `json:"isError"`
	Refused    bool   `json:"refused"`
}

func replayed(t *testing.T, snap displaySnapshotEvent) []replayedMessage {
	t.Helper()
	encoded, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []replayedMessage `json:"messages"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded.Messages
}

// outcomeTurn is a user message, one assistant message calling each id, and one result per call.
func outcomeTurn(ids ...string) []llm.Message {
	calls := make([]llm.ToolCall, 0, len(ids))
	history := []llm.Message{{Role: llm.RoleUser, Content: "cerca il meteo"}, {Role: llm.RoleAssistant}}
	for _, id := range ids {
		var call llm.ToolCall
		call.ID, call.Type, call.Function.Name = id, "function", "web_search"
		calls = append(calls, call)
		history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: "result of " + id})
	}
	history[1].ToolCalls = calls
	return history
}

func endRow(convID, requestID, id, status string, meta map[string]any) toolinvocations.Event {
	return toolinvocations.Event{ConversationID: convID, RequestID: requestID, ToolCallID: id,
		ToolName: "web_search", Event: toolinvocations.EventEnd, Status: status, Meta: meta}
}

// A reload reads each call's outcome from its end row in aura.tool_invocations: status
// 'error' is a failure, and the gateway's deny verdict is a refusal, whether recordDenyFact
// wrote the row or the runner stamped its own. Before this only the crash-recovery
// placeholder replayed as failed (spec 2026-10-12, finding 4).
func TestSnapshotReplaysTheLedgerOutcome(t *testing.T) {
	const convID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	end := func(id, status string, meta map[string]any) toolinvocations.Event {
		return endRow(convID, "r1", id, status, meta)
	}
	facts := []toolinvocations.Event{
		end("failed", "error", nil),
		end("refused", "error", map[string]any{"gateway_verdict": "deny", "gateway_tier": "normal", "reason": "policy"}),
		end("stamped", "error", map[string]any{"gateway_verdict": "deny"}),
		end("ok", "ok", nil),
		endRow("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "r1", "ok", "error", nil),
		{ConversationID: convID, ToolCallID: "ok", Event: toolinvocations.EventStart, Status: "error"},
	}
	messages := replayed(t, projectDisplaySnapshotWithFacts(linearTurns(outcomeTurn("failed", "refused", "stamped", "ok")), convID, facts, nil))
	want := map[string][2]bool{"failed": {true, false}, "refused": {true, true}, "stamped": {true, true}, "ok": {false, false}}
	for _, m := range messages[2:] {
		if got := [2]bool{m.IsError, m.Refused}; got != want[m.ToolCallID] {
			t.Errorf("%s: isError, refused = %v, want %v", m.ToolCallID, got, want[m.ToolCallID])
		}
	}
	if messages[1].IsError || messages[1].Refused {
		t.Fatalf("the assistant message carries a call outcome: %+v", messages[1])
	}
}

// An id the snapshot cannot pin to one call gets no outcome, as matchingEndFacts gives it no
// display, and a call it can pin keeps its own. Ids do repeat: a blank provider id becomes the
// same call_<sha> in every turn that repeats the call (uniquifyToolCallIDs), and the ledger
// then holds one end row per turn.
func TestSnapshotDecoratesOnlyACallItCanPin(t *testing.T) {
	const convID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const id = "call_5e1f0c2ab4d7"
	once := outcomeTurn(id, "solo")
	twice := append(outcomeTurn(id, "solo"), outcomeTurn(id)...)
	solo := endRow(convID, "r1", "solo", "error", nil)
	for name, tc := range map[string]struct {
		history  []llm.Message
		facts    []toolinvocations.Event
		declined []string
	}{
		"end rows that disagree": {once, []toolinvocations.Event{solo,
			endRow(convID, "r1", id, "error", nil), endRow(convID, "r2", id, "ok", nil)}, nil},
		"refusals in two turns": {twice, []toolinvocations.Event{solo,
			endRow(convID, "r1", id, "error", map[string]any{"gateway_verdict": "deny"}),
			endRow(convID, "r2", id, "error", map[string]any{"gateway_verdict": "deny"})}, nil},
		"one end row for a call made twice": {twice, []toolinvocations.Event{solo,
			endRow(convID, "r1", id, "error", nil)}, nil},
		"a declined call made twice": {twice, []toolinvocations.Event{solo,
			endRow(convID, "r1", id, "ok", nil), endRow(convID, "r2", id, "ok", nil)}, []string{id}},
	} {
		for _, m := range replayed(t, projectDisplaySnapshotWithFacts(linearTurns(tc.history), convID, tc.facts, tc.declined)) {
			if m.ToolCallID == "solo" && !m.IsError {
				t.Errorf("%s: the call it can pin lost its failure: %+v", name, m)
			}
			if m.ToolCallID == id && (m.IsError || m.Refused) {
				t.Errorf("%s: %+v carries an outcome it cannot be pinned to", name, m)
			}
		}
	}
}

// markAwaitingInput drops only the crash-recovery placeholder of a pending call. A real
// result the ledger marks failed is a message the person must see.
func TestPendingPauseKeepsARealFailedResult(t *testing.T) {
	const convID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	history := outcomeTurn("pause-call")
	history[1].ToolCalls[0].Function.Name = "ask_user"
	snap := projectDisplaySnapshotWithFacts(linearTurns(history), convID, []toolinvocations.Event{
		endRow(convID, "r1", "pause-call", "error", nil)}, nil)
	markAwaitingInput(&snap, map[string]struct{}{"pause-call": {}})
	if len(snap.Messages) != 3 || !snap.Messages[2].IsError || !snap.Messages[1].ToolCalls[0].AwaitingInput {
		t.Fatalf("a pending call's real failed result was dropped: %+v", snap.Messages)
	}
}

type ledgerRows []toolinvocations.Event

func (l ledgerRows) ListByConversation(context.Context, string) ([]toolinvocations.Event, error) {
	return l, nil
}

// servedSnapshot is GET /threads/{id}/messages through the real handler, as the cockpit
// reads it.
func servedSnapshot(t *testing.T, server *Server, threadID string) []replayedMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	server.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads/"+threadID+"/messages", nil))
	var decoded struct {
		Messages []replayedMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d, decode %v: %s", rec.Code, err, rec.Body)
	}
	return decoded.Messages
}

// Postgres matches a uuid in any case and returns it lowercase, so a thread opened by an
// uppercase id must still meet its own ledger rows.
func TestSnapshotReadsTheLedgerForAnUppercaseThreadID(t *testing.T) {
	const convID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	upper := strings.ToUpper(convID)
	history := outcomeTurn("failed")
	server := NewServer(nil, &fakeConvStore{
		known:   map[string]bool{convID: true, upper: true},
		history: map[string][]llm.Message{convID: history, upper: history},
	}, ServerConfig{})
	server.SetToolInvocationReader(ledgerRows{endRow(convID, "r1", "failed", "error", nil)})
	if messages := servedSnapshot(t, server, upper); len(messages) != 3 || !messages[2].IsError {
		t.Fatalf("the failed call lost its ledger outcome: %+v", messages)
	}
}

// The operator's rejection is a refusal too (spec 2026-10-12). The withheld call's own end
// row is the approval request, status 'ok', so a reload learns of the decline from the pause:
// the snapshot reads the calls this thread's declined gateway approvals name. Without that
// read the snapshot is served as before.
func TestSnapshotReplaysTheOperatorsRejectionAsRefused(t *testing.T) {
	const convID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	approvals := &fakeApprovalStore{declined: []string{"withheld"}}
	server := NewServer(nil, &fakeConvStore{
		known:   map[string]bool{convID: true},
		history: map[string][]llm.Message{convID: outcomeTurn("withheld", "other")},
	}, ServerConfig{})
	server.SetToolInvocationReader(ledgerRows{
		endRow(convID, "r1", "withheld", "ok", nil), endRow(convID, "r1", "other", "ok", nil)})
	server.SetApprovalStore(approvals)
	messages := servedSnapshot(t, server, convID)
	if !messages[2].IsError || !messages[2].Refused || messages[3].IsError || messages[3].Refused {
		t.Fatalf("withheld %+v, other %+v: want only the withheld call refused", messages[2], messages[3])
	}
	if approvals.declinedFor != convID {
		t.Fatalf("declined pauses read for %q, want %q", approvals.declinedFor, convID)
	}
	approvals.err = errors.New("db down")
	if messages := servedSnapshot(t, server, convID); messages[2].IsError || messages[2].Refused {
		t.Fatalf("a failed pause read still marked the call: %+v", messages[2])
	}
}

func TestPendingToolCallsKeepsThisThreadsPauses(t *testing.T) {
	s := &Server{approvals: &fakeApprovalStore{pendings: []askuser.Pending{
		{ConversationID: "thread-1", ToolCallID: "call-a"},
		{ConversationID: "thread-1", ToolCallID: ""},
		{ConversationID: "thread-2", ToolCallID: "call-b"},
	}}}
	got := s.pendingToolCalls(context.Background(), "thread-1")
	if _, ok := got["call-a"]; !ok || len(got) != 1 {
		t.Fatalf("pending = %v, want only call-a", got)
	}
	if (&Server{}).pendingToolCalls(context.Background(), "thread-1") != nil {
		t.Fatal("no approval store must yield no pending calls")
	}
	failing := &Server{approvals: &fakeApprovalStore{err: errors.New("db down")}}
	if failing.pendingToolCalls(context.Background(), "thread-1") != nil {
		t.Fatal("a failed read must yield no pending calls")
	}
}
