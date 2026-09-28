package agui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

type recordingMCPFacts struct {
	calls int
	err   error
}

func (r *recordingMCPFacts) ListByConversation(context.Context, string) ([]toolinvocations.Event, error) {
	r.calls++
	return nil, r.err
}

func TestMCPDisplaySnapshotReadsFactsAfterOwnership(t *testing.T) {
	const owned = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const foreign = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	store := &fakeConvStore{known: map[string]bool{owned: true}}
	facts := &recordingMCPFacts{}
	server := NewServer(nil, store, ServerConfig{})
	server.SetToolInvocationReader(facts)
	httpServer := httptest.NewServer(server.Mux())
	defer httpServer.Close()
	get := func(id string) int {
		t.Helper()
		resp, err := http.Get(httpServer.URL + "/threads/" + id + "/messages")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if status := get(foreign); status != http.StatusNotFound || facts.calls != 0 {
		t.Fatalf("foreign status=%d fact reads=%d", status, facts.calls)
	}
	if status := get(owned); status != http.StatusOK || facts.calls != 1 {
		t.Fatalf("owned status=%d fact reads=%d", status, facts.calls)
	}
	facts.err = errors.New("ledger unavailable")
	if status := get(owned); status != http.StatusOK || facts.calls != 2 {
		t.Fatalf("ledger failure status=%d fact reads=%d", status, facts.calls)
	}
}

func TestMCPDisplaySnapshotRequiresMatchingOwnerFact(t *testing.T) {
	const convID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	var call llm.ToolCall
	call.ID = "m1"
	call.Type = "function"
	call.Function.Name = "alias__memory_search"
	call.Function.Arguments = `{"query":"x"}`
	history := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleTool, ToolCallID: call.ID, Content: `{"facts":[]}`},
	}
	fact := toolinvocations.Event{
		ConversationID: convID, ToolCallID: call.ID, ToolName: call.Function.Name,
		Event: toolinvocations.EventEnd, Status: "ok", ResultPreview: history[1].Content,
		Meta: map[string]any{"aura_display_source": map[string]any{"recipe": "recipe:memory", "tool": "memory_search"}},
	}
	if got := previewInputsByCallIDWithFacts(history, convID, nil)[call.ID].TrustedMCP; got != nil {
		t.Fatalf("no fact gained source %+v", got)
	}
	if got := previewInputsByCallIDWithFacts(history, convID, []toolinvocations.Event{fact})[call.ID].TrustedMCP; got == nil || got.Recipe != "recipe:memory" {
		t.Fatalf("matching fact source = %+v", got)
	}
	for _, tc := range []struct {
		name string
		edit func(*toolinvocations.Event)
	}{
		{"other conversation", func(e *toolinvocations.Event) { e.ConversationID = "other" }},
		{"wrong tool", func(e *toolinvocations.Event) { e.ToolName = "alias__memory_recall" }},
		{"wrong preview", func(e *toolinvocations.Event) { e.ResultPreview = "changed" }},
		{"failed call", func(e *toolinvocations.Event) { e.Status = "error" }},
		{"start fact", func(e *toolinvocations.Event) { e.Event = toolinvocations.EventStart }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := fact
			tc.edit(&changed)
			if got := previewInputsByCallIDWithFacts(history, convID, []toolinvocations.Event{changed})[call.ID].TrustedMCP; got != nil {
				t.Fatalf("mismatched fact gained source %+v", got)
			}
		})
	}
	if got := previewInputsByCallIDWithFacts(history, convID, []toolinvocations.Event{fact, fact})[call.ID].TrustedMCP; got != nil {
		t.Fatalf("duplicate facts gained source %+v", got)
	}
	large := fact
	large.ResultPreview = "redacted and capped in ledger"
	sum := sha256.Sum256([]byte(history[1].Content))
	large.Meta = map[string]any{
		"aura_display_source":         fact.Meta["aura_display_source"],
		"aura_display_preview_sha256": hex.EncodeToString(sum[:]),
	}
	if got := previewInputsByCallIDWithFacts(history, convID, []toolinvocations.Event{large})[call.ID].TrustedMCP; got == nil {
		t.Fatal("digest-verified preview lost trust after ledger redaction")
	}
	large.Meta["aura_display_preview_sha256"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got := previewInputsByCallIDWithFacts(history, convID, []toolinvocations.Event{large})[call.ID].TrustedMCP; got != nil {
		t.Fatal("incorrect preview digest gained trust")
	}
}

func TestMCPViewAndReadCardReplayTogether(t *testing.T) {
	const convID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	call := llm.ToolCall{ID: "cal-1", Type: "function"}
	call.Function.Name = "pim__calendar"
	call.Function.Arguments = `{"action":"list_accounts"}`
	preview := `{"accounts":[{"accountId":"a1","provider":"google","displayName":"Work"}]}`
	history := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleTool, ToolCallID: call.ID, Content: preview},
	}
	fact := toolinvocations.Event{
		ConversationID: convID, ToolCallID: call.ID, ToolName: call.Function.Name,
		Event: toolinvocations.EventEnd, Status: "ok", ResultPreview: preview,
		Meta: map[string]any{
			"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar", "action": "list_accounts"},
			"mcp_view":            map[string]any{"server": "pim", "resource_uri": "ui://calendar/view.html", "text_content": preview},
		},
	}
	snap := projectDisplaySnapshotWithFacts(history, convID, []toolinvocations.Event{fact})
	got := snap.Messages[0].ToolCalls[0]
	if got.Display == nil || got.Display.Table == nil || got.MCPView == nil {
		t.Fatalf("replay lost card or view: %+v", got)
	}
	if got.MCPView["tool_call_id"] != call.ID || got.MCPView["tool_name"] != call.Function.Name {
		t.Fatalf("view correlation mismatch: %+v", got.MCPView)
	}
	for _, bad := range [][]toolinvocations.Event{nil, {fact, fact}} {
		degraded := projectDisplaySnapshotWithFacts(history, convID, bad).Messages[0].ToolCalls[0]
		if degraded.Display != nil || degraded.MCPView != nil {
			t.Fatalf("unverified fact retained card or view: %+v", degraded)
		}
	}
}
