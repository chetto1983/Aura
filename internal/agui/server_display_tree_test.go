package agui

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// linearTurns places a history on one branch, each turn continuing the one before it: the
// shape every conversation had before branches, and the one most snapshot tests need.
func linearTurns(hist []llm.Message) []conversations.DisplayTurn {
	out := make([]conversations.DisplayTurn, len(hist))
	for i, m := range hist {
		out[i] = conversations.DisplayTurn{Seq: i + 1, ParentSeq: i, Message: m}
	}
	return out
}

func turnAt(seq, parent int, role, content string) conversations.DisplayTurn {
	return conversations.DisplayTurn{Seq: seq, ParentSeq: parent, Message: llm.Message{Role: role, Content: content}}
}

type treeNode struct{ ID, Parent string }

func treeOf(snap displaySnapshotEvent) []treeNode {
	out := make([]treeNode, len(snap.Messages))
	for i, m := range snap.Messages {
		out[i].ID = m.ID
		if m.ParentID != nil {
			out[i].Parent = *m.ParentID
		}
	}
	return out
}

// TestSnapshotTree_IDsAreSeqsAndParentsTheStoredPointers is acceptance 4 (spec 2026-10-27):
// a rewritten root and the branch under it come back as their seqs, each naming the turn it
// continues.
func TestSnapshotTree_IDsAreSeqsAndParentsTheStoredPointers(t *testing.T) {
	snap := projectDisplaySnapshotWithFacts([]conversations.DisplayTurn{
		turnAt(1, 0, llm.RoleUser, "U1"),
		turnAt(2, 1, llm.RoleAssistant, "A1"),
		turnAt(3, 0, llm.RoleUser, "U1 rewritten"),
		turnAt(4, 3, llm.RoleAssistant, "A1 to the rewrite"),
		turnAt(5, 2, llm.RoleUser, "U2 on the first branch"),
	}, "", nil, nil)

	want := []treeNode{{"msg-1", ""}, {"msg-2", "msg-1"}, {"msg-3", ""}, {"msg-4", "msg-3"}, {"msg-5", "msg-2"}}
	if got := treeOf(snap); !reflect.DeepEqual(got, want) {
		t.Fatalf("tree = %+v, want %+v", got, want)
	}
}

// TestSnapshotTree_ARecoveredResultIsNamedAfterItsCall: a result the repair synthesized has
// no turn, so it takes the assistant's seq and its call's id, and hangs from the assistant.
func TestSnapshotTree_ARecoveredResultIsNamedAfterItsCall(t *testing.T) {
	call := llm.ToolCall{ID: "call-7", Type: "function"}
	call.Function.Name = "run_python"
	snap := projectDisplaySnapshotWithFacts([]conversations.DisplayTurn{
		turnAt(1, 0, llm.RoleUser, "compute"),
		{Seq: 2, ParentSeq: 1, Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}},
		{ParentSeq: 2, Message: llm.Message{Role: llm.RoleTool, ToolCallID: "call-7", Content: conversations.RecoveryToolResultContent(call)}},
	}, "", nil, nil)

	want := []treeNode{{"msg-1", ""}, {"msg-2", "msg-1"}, {"msg-2-call-7", "msg-2"}}
	if got := treeOf(snap); !reflect.DeepEqual(got, want) {
		t.Fatalf("tree = %+v, want %+v", got, want)
	}
	if !snap.Messages[2].IsError {
		t.Fatal("the recovered result replays as a success")
	}
}

// TestSnapshotTree_ReasoningLandsOnItsOwnTurn: on a tree the k-th answer is not the k-th
// answer turn, so the rows are matched by seq. Positional pairing put the rewrite's
// reasoning on the first answer.
func TestSnapshotTree_ReasoningLandsOnItsOwnTurn(t *testing.T) {
	snap := projectDisplaySnapshotWithFacts([]conversations.DisplayTurn{
		turnAt(1, 0, llm.RoleUser, "U1"),
		turnAt(2, 1, llm.RoleAssistant, "A1"),
		turnAt(3, 0, llm.RoleUser, "U1 rewritten"),
		turnAt(4, 3, llm.RoleAssistant, "A1 to the rewrite"),
	}, "", nil, nil)

	attachTurnReasoning(&snap, []conversations.TurnReasoning{{Seq: 4, Reasoning: "the rewrite asks for more", DurationMS: 30}})

	if snap.Messages[1].Reasoning != "" || snap.Messages[3].Reasoning != "the rewrite asks for more" {
		t.Fatalf("reasoning on A1 = %q, on A1' = %q", snap.Messages[1].Reasoning, snap.Messages[3].Reasoning)
	}
}

func TestSnapshotTree_AttachmentsLandOnTheirOwnTurn(t *testing.T) {
	snap := projectDisplaySnapshotWithFacts([]conversations.DisplayTurn{
		turnAt(1, 0, llm.RoleUser, "U1"),
		turnAt(2, 0, llm.RoleUser, "U1 rewritten, with a file"),
	}, "", nil, nil)

	attachTurnAttachments(&snap, []conversations.TurnAttachments{{Seq: 2, IDs: []string{"asset-a"}}})

	if len(snap.Messages[0].AttachmentIDs) != 0 || !reflect.DeepEqual(snap.Messages[1].AttachmentIDs, []string{"asset-a"}) {
		t.Fatalf("attachments = %v and %v, want only the rewrite's", snap.Messages[0].AttachmentIDs, snap.Messages[1].AttachmentIDs)
	}
}

// TestSnapshotTree_ADamagedPointerFallsBackToTheMessageBefore: a parent the snapshot does not
// draw before its child would leave the cockpit a node it cannot place.
func TestSnapshotTree_ADamagedPointerFallsBackToTheMessageBefore(t *testing.T) {
	snap := projectDisplaySnapshotWithFacts([]conversations.DisplayTurn{
		turnAt(1, 0, llm.RoleUser, "U1"),
		turnAt(2, 99, llm.RoleAssistant, "A1"),
	}, "", nil, nil)

	hideMessages(&snap, func(*displaySnapshotMessage) bool { return false })

	if got := treeOf(snap); !reflect.DeepEqual(got, []treeNode{{"msg-1", ""}, {"msg-2", "msg-1"}}) {
		t.Fatalf("tree = %+v, want A1 under U1", got)
	}
}

// TestServer_MessagesSnapshotHidesTheRegeneratePlaceholder is acceptance 4's second half over
// the route (D6): the empty turn a regenerate forks is not drawn, and the answer written after
// it hangs from the user turn, beside the answer it replaced. An empty answer that reasoned is
// not a placeholder.
func TestServer_MessagesSnapshotHidesTheRegeneratePlaceholder(t *testing.T) {
	const tid = "99999999-9999-9999-9999-999999999999"
	conv := &fakeConvStore{
		known: map[string]bool{tid: true},
		turns: map[string][]conversations.DisplayTurn{tid: {
			turnAt(1, 0, llm.RoleUser, "U1"),
			turnAt(2, 1, llm.RoleAssistant, "A1"),
			turnAt(3, 1, llm.RoleAssistant, ""),
			turnAt(4, 3, llm.RoleAssistant, "A1 regenerated"),
			turnAt(5, 4, llm.RoleUser, "U2"),
			turnAt(6, 5, llm.RoleAssistant, ""),
		}},
		reasonings: map[string][]conversations.TurnReasoning{tid: {{Seq: 6, Reasoning: "thought, said nothing"}}},
	}
	srv := newTestServer(t, &scriptedRunner{}, conv)

	resp, err := http.Get(srv.URL + "/threads/" + tid + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Messages []struct {
			ID       string  `json:"id"`
			ParentID *string `json:"parentId"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := make([]treeNode, len(body.Messages))
	for i, m := range body.Messages {
		got[i].ID = m.ID
		if m.ParentID != nil {
			got[i].Parent = *m.ParentID
		}
	}
	want := []treeNode{{"msg-1", ""}, {"msg-2", "msg-1"}, {"msg-4", "msg-1"}, {"msg-5", "msg-4"}, {"msg-6", "msg-5"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tree = %+v, want %+v", got, want)
	}
	if body.Messages[0].ParentID != nil {
		t.Fatalf("the root's parentId = %q, want null", *body.Messages[0].ParentID)
	}
}
