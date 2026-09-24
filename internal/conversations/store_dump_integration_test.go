//go:build db_integration

// Integration proof for the owner's raw export read (prd.md §7). Run against a THROWAWAY
// database — db_integration mutations are not isolated per run:
//
//	go test -tags db_integration -race -run TestLoadDump ./internal/conversations -count=1
package conversations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// TestLoadDumpReturnsEveryPersistedTurnUnrepaired seeds the shapes the redacted export lost
// on 2026-09-24: a tool call with its result, a result orphaned by an interrupted run, a
// spilled answer, reasoning, a fork and a compaction. LoadHistory drops the orphan; the dump
// must not.
func TestLoadDumpReturnsEveryPersistedTurnUnrepaired(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool, Config{RunDir: t.TempDir(), TurnCapBytes: 64})
	convID := newConversation(t, s)
	ctx := ownerCtx()

	var call llm.ToolCall
	call.ID, call.Type = "call_1", "function"
	call.Function.Name = "calendar__calendar"
	call.Function.Arguments = `{"action":"create"}`
	toolCalls, err := json.Marshal([]llm.ToolCall{call})
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	attachment := uuid.NewString()
	spilled := strings.Repeat("long answer ", 20)
	for _, p := range []AppendTurnParams{
		{Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{Seq: 2, Role: llm.RoleUser, Content: "noon lunch", AttachmentIDs: []string{attachment}},
		{Seq: 3, Role: llm.RoleAssistant, ToolCalls: toolCalls, Reasoning: "create the event",
			ReasoningDurationMS: 1500, InputTokens: 1200, OutputTokens: 40, CachedTokens: 900, ContextTokens: 20000},
		{Seq: 4, Role: llm.RoleTool, Content: "created", ToolCallID: "call_1"},
		{Seq: 5, Role: llm.RoleTool, Content: "orphaned", ToolCallID: "call_lost"},
		{Seq: 6, Role: llm.RoleAssistant, Content: spilled, DeliveryKey: "job-1:terminal"},
	} {
		p.ConversationID = convID
		if err := s.AppendTurn(ctx, p); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", p.Seq, err)
		}
	}
	forkSeq, forkBranch, err := s.ForkBranch(ctx, convID, 2, llm.RoleUser, "edited lunch")
	if err != nil {
		t.Fatalf("ForkBranch: %v", err)
	}
	if err := s.SaveCompaction(ctx, convID, uuid.Nil.String(), Compaction{
		Summary: "user booked lunch", Model: "test-model", CoversThroughSeq: 4, SourceTurns: 4,
	}); err != nil {
		t.Fatalf("SaveCompaction: %v", err)
	}

	d, err := s.LoadDump(ctx, convID)
	if err != nil {
		t.Fatalf("LoadDump: %v", err)
	}
	if len(d.Turns) != 7 {
		t.Fatalf("LoadDump returned %d turns, want all 7 persisted rows: %+v", len(d.Turns), d.Turns)
	}
	for i, turn := range d.Turns {
		if turn.Seq != i+1 || turn.CreatedAt.IsZero() {
			t.Errorf("turn %d = seq %d created %v, want seq order with a timestamp", i, turn.Seq, turn.CreatedAt)
		}
	}
	if got := d.Turns[1].AttachmentIDs; len(got) != 1 || got[0] != attachment {
		t.Errorf("attachments = %v, want [%s]", got, attachment)
	}
	answer := d.Turns[2]
	if answer.Reasoning != "create the event" || answer.ReasoningDurationMS != 1500 ||
		answer.InputTokens != 1200 || answer.ContextTokens != 20000 || !strings.Contains(string(answer.ToolCalls), `"calendar__calendar"`) {
		t.Errorf("tool-call turn lost a column: %+v", answer)
	}
	if orphan := d.Turns[4]; orphan.ToolCallID != "call_lost" || orphan.Content != "orphaned" {
		t.Errorf("orphaned result = %+v, want it kept verbatim", orphan)
	}
	if last := d.Turns[5]; last.Content != spilled || last.DeliveryKey != "job-1:terminal" {
		t.Errorf("spilled turn = %q / %q, want the sidecar rehydrated and the delivery key", last.Content, last.DeliveryKey)
	}
	if fork := d.Turns[6]; fork.Seq != forkSeq || fork.BranchID != forkBranch.String() || fork.ParentSeq != 1 {
		t.Errorf("fork = seq %d branch %s parent %d, want seq %d on %s under #1", fork.Seq, fork.BranchID, fork.ParentSeq, forkSeq, forkBranch)
	}
	if d.Turns[0].ParentSeq != 0 || d.Turns[0].BranchID != uuid.Nil.String() {
		t.Errorf("root = %+v, want the canonical branch with no parent", d.Turns[0])
	}
	if len(d.Compactions) != 1 || d.Compactions[0].Summary != "user booked lunch" || d.Compactions[0].CoversThroughSeq != 4 {
		t.Errorf("compactions = %+v, want the saved summary", d.Compactions)
	}

	history, err := s.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	for _, m := range history {
		if m.ToolCallID == "call_lost" {
			t.Fatal("precondition: LoadHistory kept the orphan, so this test no longer proves the dump differs")
		}
	}

	foreign, err := s.LoadDump(identityctx.WithIdentityID(context.Background(), uuid.NewString()), convID)
	if err != nil {
		t.Fatalf("LoadDump as a foreign identity: %v", err)
	}
	if len(foreign.Turns) != 0 || len(foreign.Compactions) != 0 {
		t.Fatalf("a foreign identity read %d turns and %d compactions through RLS", len(foreign.Turns), len(foreign.Compactions))
	}
}

func TestLoadDumpRejectsAMalformedConversationID(t *testing.T) {
	s := newStore(t, migratedPool(t))
	if _, err := s.LoadDump(ownerCtx(), "not-a-uuid"); err == nil {
		t.Fatal("LoadDump accepted a malformed conversation id")
	}
}
