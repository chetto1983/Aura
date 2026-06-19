package agui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/web"
)

// webSearchTurn builds the persisted (assistant tool-call, tool-result) pair for a
// web_search call whose result preview is the {"results":[…]} JSON the runner persists.
func webSearchTurn(callID string, results []web.Result) []llm.Message {
	preview, _ := json.Marshal(map[string]any{"results": results})
	var call llm.ToolCall
	call.ID = callID
	call.Type = "function"
	call.Function.Name = "web_search"
	call.Function.Arguments = `{"query":"weather"}`
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleTool, ToolCallID: callID, Content: string(preview)},
	}
}

// TestProjectMessagesDisplay (D-06): projectDisplaySnapshot re-derives the display for a
// persisted web_search tool turn and attaches it to the matching assistant tool-call
// entry — byte-identical to what the LIVE display normalizer produces from the same
// preview (live/replay parity by construction).
func TestProjectMessagesDisplay(t *testing.T) {
	results := []web.Result{
		{Title: "Rome", URL: "https://w.test/rome", Snippet: "sunny"},
		{Title: "Milan", URL: "https://w.test/milan", Snippet: "rain"},
	}
	hist := append([]llm.Message{{Role: llm.RoleUser, Content: "weather"}}, webSearchTurn("c1", results)...)

	snap := projectDisplaySnapshot(hist)

	// Find the assistant tool-call entry and assert its re-derived display.
	var got *display.Payload
	for _, m := range snap.Messages {
		for _, tc := range m.ToolCalls {
			if tc.ID == "c1" {
				got = tc.Display
			}
		}
	}
	if got == nil {
		t.Fatalf("web_search tool call carries no re-derived display")
	}
	if got.Type != display.KindWebResult || got.ToolCallID != "c1" || len(got.WebResults) != 2 {
		t.Fatalf("re-derived display mismatch: %+v", got)
	}

	// Parity: the replay payload must equal the live normalizer's payload over the SAME
	// preview shape (a fresh per-turn registry, as live builds).
	preview, _ := json.Marshal(map[string]any{"results": results})
	live, ok := display.NormalizeToolPreview("c1", "web_search", string(preview), display.NewRegistry())
	if !ok {
		t.Fatalf("live normalize failed")
	}
	gb, _ := json.Marshal(got)
	lb, _ := json.Marshal(live)
	if string(gb) != string(lb) {
		t.Fatalf("replay diverged from live:\n replay=%s\n   live=%s", gb, lb)
	}
}

// TestProjectMessagesDisplayUnrecognizedNoDisplay (D-06 / D-FALLBACK): a tool turn for
// a tool with no normalizer (shell_exec) produces NO display on the snapshot — the
// replay renders the raw card, identical to live.
func TestProjectMessagesDisplayUnrecognizedNoDisplay(t *testing.T) {
	// fs_read is not a display-recognized tool (the normalizer wires web_search/
	// web_fetch/swarm_spawn/shell_exec/sandbox_exec), so its result re-derives no
	// display — the D-FALLBACK raw card stands on replay exactly as live.
	var call llm.ToolCall
	call.ID = "c2"
	call.Type = "function"
	call.Function.Name = "fs_read"
	call.Function.Arguments = `{"path":"x"}`
	hist := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: "file1\nfile2"},
	}

	snap := projectDisplaySnapshot(hist)
	for _, m := range snap.Messages {
		for _, tc := range m.ToolCalls {
			if tc.ID == "c2" && tc.Display != nil {
				t.Fatalf("unrecognized tool produced a display: %+v", tc.Display)
			}
		}
	}
}

// TestProjectDisplaySnapshotWireShape: the envelope is byte-compatible with the SDK
// MESSAGES_SNAPSHOT (type + messages[].toolCalls[]) plus the additive per-tool-call
// `display` key the cockpit replay reads; a recognized turn carries `display`, an
// unrecognized one omits it (omitempty).
func TestProjectDisplaySnapshotWireShape(t *testing.T) {
	results := []web.Result{{Title: "A", URL: "https://a.test/x", Snippet: "s"}}
	hist := append(webSearchTurn("c1", results),
		llm.Message{Role: llm.RoleAssistant, Content: "Rome is sunny [1]."})

	raw, err := json.Marshal(projectDisplaySnapshot(hist))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		`"type":"MESSAGES_SNAPSHOT"`,
		`"toolCalls"`,
		`"display"`,
		`"web_result"`,
		`"src-1"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("snapshot wire shape missing %q in %q", want, body)
		}
	}
}

// TestRederiveDisplaysSharedRegistryStableRefIDs (D-06 / Pitfall 4): two web_search
// turns in one thread accumulate into ONE registry on replay, so a repeated URL keeps a
// stable RefID across turns — exactly as the live per-run registry does.
func TestRederiveDisplaysSharedRegistryStableRefIDs(t *testing.T) {
	hist := webSearchTurn("c1", []web.Result{{Title: "A", URL: "https://a.test/x"}})
	hist = append(hist, webSearchTurn("c2", []web.Result{
		{Title: "A", URL: "https://a.test/x"}, // same URL as turn 1 -> same RefID
		{Title: "B", URL: "https://b.test/y"},
	})...)

	displays := rederiveDisplays(hist)
	d1, d2 := displays["c1"], displays["c2"]
	if d1 == nil || d2 == nil {
		t.Fatalf("missing re-derived displays: c1=%v c2=%v", d1, d2)
	}
	if d1.WebResults[0].RefID != "src-1" {
		t.Fatalf("turn-1 RefID = %q, want src-1", d1.WebResults[0].RefID)
	}
	if d2.WebResults[0].RefID != "src-1" {
		t.Fatalf("repeated URL across turns got RefID %q, want stable src-1", d2.WebResults[0].RefID)
	}
	if d2.WebResults[1].RefID != "src-2" {
		t.Fatalf("new URL RefID = %q, want src-2", d2.WebResults[1].RefID)
	}
}
