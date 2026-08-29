package swarm

import (
	"context"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/llm"
)

// fakePauseAndPark is an in-memory PauseAndPark double: it records every
// (pause, park) pair OpenPauseAndPark is asked to write and returns a scripted
// (parked, err) result, so openPauseAndPark's success/lease-lost/error branches
// (delegation_run.go, exercised from delegation_queue_unit_test.go) are all testable
// without a database. Zero value = "always parks", matching every other fake-store
// default in this package's tests.
type fakePauseAndPark struct {
	mu     sync.Mutex
	calls  []fakePauseAndParkCall
	parked bool
	err    error
}

type fakePauseAndParkCall struct {
	pause askuser.InsertParams
	park  documents.ParkAwaitingInputRequest
}

func newFakePauseAndPark() *fakePauseAndPark {
	return &fakePauseAndPark{parked: true}
}

func (f *fakePauseAndPark) OpenPauseAndPark(_ context.Context, pause askuser.InsertParams, park documents.ParkAwaitingInputRequest) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakePauseAndParkCall{pause: pause, park: park})
	if f.err != nil {
		return false, f.err
	}
	return f.parked, nil
}

func (f *fakePauseAndPark) callsSnapshot() []fakePauseAndParkCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakePauseAndParkCall(nil), f.calls...)
}

// delegation_resume_test.go is the daemon-free (no build tag, no Postgres) half of
// 51-06b Task 1/2's own pause/park coverage: mintFreshUUID, openPauseAndPark (including
// its own defect-A RLS identity bind, live-check/d03/RESULTS.md), and buildResumeTurns'
// answer-substitution mechanism. DelegationResumeObserver.ProcessOnce and the end-to-end
// runChild resume/tool-promotion proofs live in delegation_resume_observer_test.go, split
// out to stay under CLAUDE.md's 600-LOC ceiling. The atomic pause-and-park transaction
// itself (PauseAndPark) is a consumer-declared interface satisfied at cmd/aura and proven
// live under db_integration (delegation_delivery_db_test.go's sibling coverage for the
// analogous Delivery seam).

// TestMintFreshUUIDProducesDistinctValidUUIDs pins the fence/token minting Task 1
// depends on for D-12/D-13: every call must succeed and no two calls may collide.
func TestMintFreshUUIDProducesDistinctValidUUIDs(t *testing.T) {
	a, err := mintFreshUUID()
	if err != nil {
		t.Fatalf("mintFreshUUID: %v", err)
	}
	b, err := mintFreshUUID()
	if err != nil {
		t.Fatalf("mintFreshUUID: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("mintFreshUUID returned an empty string")
	}
	if a == b {
		t.Fatal("two mintFreshUUID calls returned the same value")
	}
}

// TestOpenPauseAndParkBindsTheJobIdentityForRLS is openPauseAndPark's own leg of defect A
// (live-check/d03/RESULTS.md): the question it surfaces through l.Delivery.Deliver shares
// the SAME ConversationRecorder seam deliverSuccess uses (delegation_queue_unit_test.go's
// TestDeliverSuccessBindsTheJobIdentityForRLS), so it needs the identical identity bind --
// the claim loop's own ctx (ProcessOnce's daemon background loop) carries none.
func TestOpenPauseAndParkBindsTheJobIdentityForRLS(t *testing.T) {
	parker := newFakePauseAndPark()
	recorder := &fakeConversationRecorder{}
	l := &DelegationClaimLoop{
		PauseParker: parker,
		Delivery:    &DelegationDelivery{Recorder: recorder},
		IdentityID:  "identity-1",
	}
	job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-7"}
	report := ChildReport{Question: "which inbox?", ToolCallID: "call-1"}

	if err := l.openPauseAndPark(context.Background(), job, payload, report, nil); err != nil {
		t.Fatalf("openPauseAndPark = %v", err)
	}
	if len(recorder.appended) != 1 {
		t.Fatalf("appended = %+v, want exactly one recorded turn (the surfaced question)", recorder.appended)
	}
	if got := recorder.appended[0].identityID; got != job.IdentityID {
		t.Fatalf("recorder saw identity %q on ctx, want the job's identity %q -- a real "+
			"conversations.Store would have this write hidden by RLS", got, job.IdentityID)
	}
}

// TestBuildResumeTurnsAppendsTheAnswerAsTheToolResult is the entire "how the answer
// becomes the tool result" mechanism (Task 2): the persisted history followed by ONE
// RoleTool message whose ToolCallID matches the pending ask_user call and whose
// Content is the operator's answer -- nothing reordered, nothing dropped.
func TestBuildResumeTurnsAppendsTheAnswerAsTheToolResult(t *testing.T) {
	state := &DelegationResumeState{
		PendingToolCallID: "call-1",
		AnswerContent:     "the answer",
		History: []llm.Message{
			{Role: llm.RoleUser, Content: "brief"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-search"}}},
			{Role: llm.RoleTool, ToolCallID: "call-search", Content: "search result"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-1"}}},
		},
	}
	turns := buildResumeTurns(state)
	if len(turns) != len(state.History)+1 {
		t.Fatalf("buildResumeTurns returned %d turns, want %d (history + 1 answer)", len(turns), len(state.History)+1)
	}
	for i, h := range state.History {
		if turns[i].Role != h.Role || turns[i].Content != h.Content {
			t.Fatalf("turn[%d] = %+v, want the persisted history preserved verbatim, got %+v", i, h, turns[i])
		}
	}
	last := turns[len(turns)-1]
	if last.Role != llm.RoleTool {
		t.Fatalf("last turn role = %q, want RoleTool", last.Role)
	}
	if last.ToolCallID != "call-1" {
		t.Fatalf("last turn ToolCallID = %q, want the pending ask_user call id", last.ToolCallID)
	}
	if last.Content != "the answer" {
		t.Fatalf("last turn Content = %q, want the operator's answer", last.Content)
	}
}

// TestBuildResumeTurnsOnEmptyHistoryStillAppendsTheAnswer covers the degenerate case:
// even with no persisted history the answer turn is still appended (never silently
// dropped), so a malformed/empty DelegationResumeState fails loudly downstream
// (a lone RoleTool turn with no preceding assistant turn) rather than here.
func TestBuildResumeTurnsOnEmptyHistoryStillAppendsTheAnswer(t *testing.T) {
	turns := buildResumeTurns(&DelegationResumeState{PendingToolCallID: "call-x", AnswerContent: "yes"})
	if len(turns) != 1 {
		t.Fatalf("buildResumeTurns(empty history) = %d turns, want 1", len(turns))
	}
	if turns[0].ToolCallID != "call-x" || turns[0].Content != "yes" {
		t.Fatalf("turns[0] = %+v, want the answer turn", turns[0])
	}
}
