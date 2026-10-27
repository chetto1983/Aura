package conversations

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func displayTurn(seq, parent int, m llm.Message) DisplayTurn {
	return DisplayTurn{Seq: seq, ParentSeq: parent, Message: m}
}

// TestRepairDisplayTurns_KeepsEachTurnsPlace pins the rule the history snapshot relies on
// (spec 2026-10-27, §4.3): the repair the model context gets keeps the seq of every turn it
// keeps, a result it synthesizes hangs from the assistant whose call it answers, and a turn
// whose parent it dropped hangs from that parent's parent.
func TestRepairDisplayTurns_KeepsEachTurnsPlace(t *testing.T) {
	t.Parallel()
	calls := []llm.ToolCall{{ID: "c1", Type: "function"}, {ID: "c2", Type: "function"}}
	calls[0].Function.Name = "web_search"
	calls[1].Function.Name = "web_fetch"
	in := []DisplayTurn{
		displayTurn(1, 0, llm.Message{Role: llm.RoleUser, Content: "U1"}),
		displayTurn(2, 1, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls}),
		displayTurn(3, 2, llm.Message{Role: llm.RoleTool, ToolCallID: "c2", Content: "fetched"}),
		displayTurn(4, 3, llm.Message{Role: llm.RoleTool, ToolCallID: "stray", Content: "orphan"}),
		displayTurn(5, 4, llm.Message{Role: llm.RoleUser, Content: "U2"}),
		displayTurn(6, 0, llm.Message{Role: llm.RoleUser, Content: "U1 rewritten"}),
	}

	got := repairDisplayTurns(in)

	want := []DisplayTurn{
		in[0],
		in[1],
		displayTurn(0, 2, llm.Message{Role: llm.RoleTool, ToolCallID: "c1", Content: RecoveryToolResultContent(calls[0])}),
		in[2],
		displayTurn(5, 3, in[4].Message),
		in[5],
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repaired turns:\n got %+v\nwant %+v", got, want)
	}
}

// TestRepairDisplayTurns_ADroppedChainClimbsToTheTurnKept: two orphans in a row both go, and
// the turn after them continues the turn before them.
func TestRepairDisplayTurns_ADroppedChainClimbsToTheTurnKept(t *testing.T) {
	t.Parallel()
	in := []DisplayTurn{
		displayTurn(1, 0, llm.Message{Role: llm.RoleUser, Content: "U1"}),
		displayTurn(2, 1, llm.Message{Role: llm.RoleTool, ToolCallID: "a", Content: "orphan"}),
		displayTurn(3, 2, llm.Message{Role: llm.RoleTool, ToolCallID: "b", Content: "orphan"}),
		displayTurn(4, 3, llm.Message{Role: llm.RoleAssistant, Content: "A1"}),
	}

	got := repairDisplayTurns(in)

	want := []DisplayTurn{in[0], displayTurn(4, 1, in[3].Message)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repaired turns:\n got %+v\nwant %+v", got, want)
	}
}

// TestRepairDisplayTurns_TheMessagesAreTheModelsRepair: the display and the model context
// run the same repair, so stripping the tree gives back LoadHistory's messages.
func TestRepairDisplayTurns_TheMessagesAreTheModelsRepair(t *testing.T) {
	t.Parallel()
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name = "run_python"
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "U1"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleUser, Content: "U2"},
		{Role: llm.RoleTool, ToolCallID: "late", Content: "orphan"},
	}
	in := make([]DisplayTurn, len(msgs))
	for i, m := range msgs {
		in[i] = displayTurn(i+1, i, m)
	}

	model, shown := repairToolMessagePairs(msgs), DisplayMessages(repairDisplayTurns(in))
	if !reflect.DeepEqual(shown, model) {
		t.Fatalf("display messages differ from the model's repair:\ndisplay %+v\n  model %+v", shown, model)
	}
}

func TestRepairDisplayTurns_Empty(t *testing.T) {
	t.Parallel()
	if got := repairDisplayTurns(nil); len(got) != 0 {
		t.Fatalf("repair(nil) = %+v, want none", got)
	}
}
