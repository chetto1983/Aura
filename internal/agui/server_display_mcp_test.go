package agui

import (
	"context"
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
}
