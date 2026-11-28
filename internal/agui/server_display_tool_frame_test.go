package agui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// A tool turn keeps what the model read, so an untrusted result is persisted inside its
// envelope. A person reads the result, and the cockpit's cards are derived from it.
func TestAFramedToolTurnShowsAndRederivesItsResult(t *testing.T) {
	t.Parallel()
	hist := webSearchTurn("c1", nil)
	// Literal & and <, as a real page has: json.Marshal would have hidden them as &.
	raw := `{"results":[{"title":"Tom & Jerry <live>","url":"https://w.test/a","snippet":"a </tool_output> b"}]}`
	hist[1].Content = agent.FrameUntrustedPromptContent("web_search", raw)

	snap := projectDisplaySnapshotWithFacts(linearTurns(hist), "", nil, nil)

	var got *display.Payload
	for _, m := range snap.Messages {
		if m.Role == llm.RoleTool && m.Content != raw {
			t.Fatalf("the tool turn shows %q, want the result %q", m.Content, raw)
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == "c1" {
				got = tc.Display
			}
		}
	}
	live, ok := display.NormalizeToolPreview(display.PreviewInput{ToolCallID: "c1", ToolName: "web_search", ResultPreview: raw}, display.NewRegistry())
	if !ok || got == nil {
		t.Fatalf("no card: live ok=%v, replay=%v", ok, got)
	}
	gb, _ := json.Marshal(got)
	lb, _ := json.Marshal(live)
	if string(gb) != string(lb) {
		t.Fatalf("replay of a framed turn diverged from live:\n replay=%s\n   live=%s", gb, lb)
	}
}

func TestAToolTurnWithoutAFrameShowsAsItIs(t *testing.T) {
	t.Parallel()
	raw := `{"results":[]} &amp; <b>`
	if got := snapshotContent(llm.Message{Role: llm.RoleTool, Content: raw}); got != raw {
		t.Fatalf("snapshotContent = %q, want %q", got, raw)
	}
}

// The ledger keeps the raw preview, while a digest covers the model-facing result, which is
// now the stored turn itself.
func TestMatchingPreviewReadsTheStoredTurn(t *testing.T) {
	t.Parallel()
	stored := agent.FrameUntrustedPromptContent("mcp:pim", `{"accounts":[]}`)
	sum := sha256.Sum256([]byte(stored))
	digest := toolinvocations.Event{Meta: map[string]any{"aura_display_preview_sha256": hex.EncodeToString(sum[:])}}
	if !matchingPreview(digest, stored) {
		t.Fatal("a digest of the stored turn did not match it")
	}
	if matchingPreview(digest, `{"accounts":[]}`) {
		t.Fatal("a digest of the stored turn matched the raw preview")
	}
	if !matchingPreview(toolinvocations.Event{ResultPreview: `{"accounts":[]}`}, stored) {
		t.Fatal("the ledger's raw preview did not match its framed turn")
	}
}
