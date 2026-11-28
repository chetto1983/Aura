package agui

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// foldedResult holds fullwidth letters and a ligature, which the frame's NFKC escaping folds
// ("ＲＯＭＥ" to "ROME", "ﬁ" to "fi"): the turn the model read no longer holds the bytes
// the live card showed.
const foldedResult = `{"results":[{"title":"ＲＯＭＥ ﬁeld guide","url":"https://w.test/rome","snippet":"sunny & warm"}]}`

func rawResultEnd(conversationID, callID, preview string) toolinvocations.Event {
	return toolinvocations.Event{
		ConversationID: conversationID, ToolCallID: callID, ToolName: "web_search",
		Event: toolinvocations.EventEnd, Status: "ok", ResultPreview: preview, PreviewBytes: len(preview),
	}
}

// shownResult projects a framed web_search turn with the given ledger rows and returns what
// the tool turn shows and the card derived for the call.
func shownResult(t *testing.T, facts []toolinvocations.Event) (string, *display.Payload) {
	t.Helper()
	hist := webSearchTurn("c1", nil)
	hist[1].Content = agent.FrameUntrustedPromptContent("web_search", foldedResult)
	snap := projectDisplaySnapshotWithFacts(linearTurns(hist), "conv-1", facts, nil)
	var shown string
	var card *display.Payload
	for _, m := range snap.Messages {
		if m.Role == llm.RoleTool {
			shown = m.Content
		}
		for _, tc := range m.ToolCalls {
			card = tc.Display
		}
	}
	return shown, card
}

func cardOf(t *testing.T, result string) string {
	t.Helper()
	live, ok := display.NormalizeToolPreview(display.PreviewInput{ToolCallID: "c1", ToolName: "web_search", Arguments: `{"query":"weather"}`, ResultPreview: result}, display.NewRegistry())
	if !ok {
		t.Fatalf("no card for %q", result)
	}
	b, _ := json.Marshal(live)
	return string(b)
}

// A row written before the ledger counted preview bytes (PreviewBytes 0) is still compared.
func TestAReloadedToolTurnShowsTheLedgersRawResult(t *testing.T) {
	t.Parallel()
	legacy := rawResultEnd("conv-1", "c1", foldedResult)
	legacy.PreviewBytes = 0
	for name, row := range map[string]toolinvocations.Event{
		"a counted row": rawResultEnd("conv-1", "c1", foldedResult),
		"a legacy row":  legacy,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shown, card := shownResult(t, []toolinvocations.Event{row})
			if shown != foldedResult {
				t.Fatalf("the tool turn shows %q, want the raw result %q", shown, foldedResult)
			}
			got, _ := json.Marshal(card)
			if string(got) != cardOf(t, foldedResult) {
				t.Fatalf("the card was not derived from the raw result: %s", got)
			}
		})
	}
}

// Without a ledger row that is this call's raw result, the turn shows the text the model
// read, unframed: folded, but whole.
func TestAReloadedToolTurnKeepsTheFoldedTextWithoutItsRawResult(t *testing.T) {
	t.Parallel()
	folded := agent.PlainToolOutput(agent.FrameUntrustedPromptContent("web_search", foldedResult))
	if folded == foldedResult {
		t.Fatal("the fixture does not fold")
	}
	// The ledger caps a preview over ResultPreviewCapBytes and keeps the original's length in
	// PreviewBytes. Such a row is skipped before any compare, even if its text matched.
	capped := rawResultEnd("conv-1", "c1", foldedResult[:40]+"…[capped]")
	capped.PreviewBytes = toolinvocations.ResultPreviewCapBytes + 1
	overCap := rawResultEnd("conv-1", "c1", foldedResult)
	overCap.PreviewBytes = toolinvocations.ResultPreviewCapBytes + 1
	foreign := rawResultEnd("conv-2", "c1", foldedResult)
	otherTool := rawResultEnd("conv-1", "c1", foldedResult)
	otherTool.ToolName = "web_fetch"
	for name, facts := range map[string][]toolinvocations.Event{
		"no row":                  nil,
		"a capped preview":        {capped},
		"a row over the cap":      {overCap},
		"two end rows for the id": {rawResultEnd("conv-1", "c1", foldedResult), rawResultEnd("conv-1", "c1", foldedResult)},
		"another conversation":    {foreign},
		"another tool":            {otherTool},
		"another result":          {rawResultEnd("conv-1", "c1", `{"results":[]}`)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shown, card := shownResult(t, facts)
			if shown != folded {
				t.Fatalf("the tool turn shows %q, want the unframed stored text %q", shown, folded)
			}
			got, _ := json.Marshal(card)
			if string(got) != cardOf(t, folded) {
				t.Fatalf("the card was not derived from the stored text: %s", got)
			}
		})
	}
}
