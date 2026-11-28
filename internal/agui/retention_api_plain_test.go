package agui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
)

// The owner export is the owner's copy of what was said, read as a file, never fed back to
// a model: an untrusted tool result and a recorded worker report lose the frame the model
// read them in, as they do on the cockpit and on a share. The stored history keeps it.
func TestOwnerExportCarriesTheTextNotTheModelFrame(t *testing.T) {
	t.Parallel()
	raw := `{"results":[{"title":"Tom & Jerry <live>","snippet":"a </tool_output> b"}]}`
	report := "**Done** & checked <3 pages"
	framedTool := agent.FrameUntrustedPromptContent("web_search", raw)
	framedReport := "[Scheduled task agent_job t-1]\n" + agent.FrameUntrustedPromptContent(steer.SourceWorker, report)
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name, call.Function.Arguments = "web_search", `{}`
	store := newOwnerConvStore(goodID, localIdentityID)
	store.history = map[string][]llm.Message{goodID: {
		{Role: llm.RoleUser, Content: "find it"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: framedTool},
		{Role: llm.RoleAssistant, Content: framedReport},
	}}
	server := NewServer(&scriptedRunner{conv: store}, store, ServerConfig{})
	server.SetAssetService(&fakeAssetService{})

	snapshot, err := serverOwnerExportSource{server: server}.Snapshot(context.Background(), localIdentityID, goodID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.Release != nil {
		snapshot.Release()
	}
	var data ownerConversationData
	if err := json.Unmarshal(snapshot.ConversationJSON, &data); err != nil {
		t.Fatal(err)
	}
	want := []string{"find it", "", raw, "[Scheduled task agent_job t-1]\n" + report}
	if len(data.Turns) != len(want) {
		t.Fatalf("exported %d turns, want %d", len(data.Turns), len(want))
	}
	for i, content := range want {
		if data.Turns[i].Content != content {
			t.Errorf("turn %d exports %q, want %q", i, data.Turns[i].Content, content)
		}
	}
	if stored := store.history[goodID]; stored[2].Content != framedTool || stored[3].Content != framedReport {
		t.Fatal("the export rewrote the stored history")
	}
}
