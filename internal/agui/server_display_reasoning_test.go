package agui

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

var errFakeReasoning = errors.New("reasoning read failed")

// TestAttachTurnReasoning_PositionalPairing pins the amendment #91 merge invariant:
// ListTurnReasoning returns one row per answer-shaped assistant turn (no tool
// calls) in seq order, and the snapshot preserves exactly those messages in the
// same order, so the k-th row decorates the k-th assistant no-tool-call message.
// Tool-call assistant messages and non-assistant roles are never decorated; a row
// with empty Reasoning (NULL column) decorates nothing.
func TestAttachTurnReasoning_PositionalPairing(t *testing.T) {
	var call llm.ToolCall
	call.ID = "c1"
	call.Type = "function"
	call.Function.Name = "shell_exec"
	call.Function.Arguments = `{"command":"ls"}`

	hist := []llm.Message{
		{Role: llm.RoleSystem, Content: "you are aura"},
		{Role: llm.RoleUser, Content: "list files then explain"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}, // tool-call turn: never decorated
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "file1\nfile2"},
		{Role: llm.RoleAssistant, Content: "two files here"}, // answer #1 → rows[0]
		{Role: llm.RoleUser, Content: "thanks"},
		{Role: llm.RoleAssistant, Content: "welcome"}, // answer #2 → rows[1] (NULL reasoning)
	}
	snap := projectDisplaySnapshot(hist)
	attachTurnReasoning(&snap, []conversations.TurnReasoning{
		{Seq: 5, Reasoning: "user wants a summary of ls output", DurationMS: 1200},
		{Seq: 7, Reasoning: "", DurationMS: 0},
	})

	if got := snap.Messages[4]; got.Reasoning != "user wants a summary of ls output" || got.ReasoningDurationMs != 1200 {
		t.Errorf("answer #1 reasoning = %q/%dms, want the persisted CoT + 1200", got.Reasoning, got.ReasoningDurationMs)
	}
	for i, m := range snap.Messages {
		if i == 4 {
			continue
		}
		if m.Reasoning != "" || m.ReasoningDurationMs != 0 {
			t.Errorf("message %d (%s) unexpectedly decorated: %q/%dms", i, m.Role, m.Reasoning, m.ReasoningDurationMs)
		}
	}
}

// TestAttachTurnReasoning_CountMismatchFailSoft: surplus rows are ignored and
// surplus messages stay bare — a defensive posture for histories the repair pass
// reshaped in ways the positional invariant does not cover.
func TestAttachTurnReasoning_CountMismatchFailSoft(t *testing.T) {
	hist := []llm.Message{
		{Role: llm.RoleUser, Content: "q"},
		{Role: llm.RoleAssistant, Content: "a"},
	}
	snap := projectDisplaySnapshot(hist)
	attachTurnReasoning(&snap, []conversations.TurnReasoning{
		{Seq: 2, Reasoning: "cot", DurationMS: 10},
		{Seq: 9, Reasoning: "surplus row", DurationMS: 20},
	})
	if got := snap.Messages[1]; got.Reasoning != "cot" {
		t.Errorf("answer reasoning = %q, want %q", got.Reasoning, "cot")
	}

	// More answer messages than rows: the extra answer stays bare.
	hist = append(hist, llm.Message{Role: llm.RoleUser, Content: "q2"}, llm.Message{Role: llm.RoleAssistant, Content: "a2"})
	snap = projectDisplaySnapshot(hist)
	attachTurnReasoning(&snap, []conversations.TurnReasoning{{Seq: 2, Reasoning: "cot", DurationMS: 10}})
	if got := snap.Messages[3]; got.Reasoning != "" {
		t.Errorf("row-less answer decorated with %q, want bare", got.Reasoning)
	}
}

// TestServer_MessagesSnapshotCarriesReasoning: the wire-level proof RS-07 consumes —
// GET /threads/{id}/messages decorates the assistant answer with the exact
// `reasoning` + `reasoningDurationMs` keys (camelCase, mirroring toolCallId).
func TestServer_MessagesSnapshotCarriesReasoning(t *testing.T) {
	const tid = "77777777-7777-7777-7777-777777777777"
	conv := &fakeConvStore{
		known: map[string]bool{tid: true},
		history: map[string][]llm.Message{tid: {
			{Role: llm.RoleUser, Content: "ciao"},
			{Role: llm.RoleAssistant, Content: "salve"},
		}},
		reasonings: map[string][]conversations.TurnReasoning{tid: {
			{Seq: 2, Reasoning: "greeting → reply in Italian", DurationMS: 850},
		}},
	}
	srv := newTestServer(t, &scriptedRunner{}, conv)

	resp, err := http.Get(srv.URL + "/threads/" + tid + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	for _, want := range []string{`"reasoning":"greeting → reply in Italian"`, `"reasoningDurationMs":850`} {
		if !strings.Contains(body, want) {
			t.Errorf("snapshot missing %s in %s", want, body)
		}
	}
}

// TestServer_MessagesSnapshotReasoningFailSoft: a ListTurnReasoning failure must
// degrade to a drawer-less 200 snapshot (reasoning is additive display data), never
// a 500 for the whole thread.
func TestServer_MessagesSnapshotReasoningFailSoft(t *testing.T) {
	const tid = "88888888-8888-8888-8888-888888888888"
	conv := &fakeConvStore{
		known: map[string]bool{tid: true},
		history: map[string][]llm.Message{tid: {
			{Role: llm.RoleUser, Content: "ciao"},
			{Role: llm.RoleAssistant, Content: "salve"},
		}},
		reasoningErr: errFakeReasoning,
	}
	srv := newTestServer(t, &scriptedRunner{}, conv)

	resp, err := http.Get(srv.URL + "/threads/" + tid + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fail-soft)", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(raw), "reasoning") {
		t.Errorf("failed reasoning read still decorated the snapshot: %s", raw)
	}
	if !strings.Contains(string(raw), "salve") {
		t.Errorf("snapshot lost the history on the fail-soft path: %s", raw)
	}
}
