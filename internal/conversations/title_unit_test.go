// Unit tier (no build tag): the best-effort auto-title worker body, driven by the
// scripted agenttest.FakeClient (no network). Proves a success path produces a
// title and a stream-error path returns an error the caller treats as "leave NULL".
package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

func titleHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleSystem, Content: "you are aura"}, // skipped by the title renderer
		{Role: llm.RoleUser, Content: "help me refactor the budget loop"},
		{Role: llm.RoleAssistant, Content: "sure, let's start with the dispatch"},
	}
}

// TestGenerateTitle_Success: a scripted client streaming a title yields a sanitized
// non-empty title.
func TestGenerateTitle_Success(t *testing.T) {
	t.Parallel()
	client := agenttest.NewFakeClient(
		agenttest.TextChunks("stop", "  \"Refactor the budget loop\"  "),
	)
	got, err := generateTitle(context.Background(), client, "test-model", titleHistory())
	if err != nil {
		t.Fatalf("generateTitle: %v", err)
	}
	if got != "Refactor the budget loop" {
		t.Errorf("title sanitization: got %q", got)
	}
	// The request carried only the system+user title prompt, not the full history.
	req := client.LastRequest()
	if len(req.Messages) != 2 || req.Messages[0].Role != llm.RoleSystem {
		t.Errorf("title request shape wrong: %+v", req.Messages)
	}
	if !strings.Contains(req.Messages[1].Content, "refactor the budget loop") {
		t.Errorf("title prompt must include the user turn, got %q", req.Messages[1].Content)
	}
}

// TestGenerateTitle_StreamError: a stream error is returned (the caller leaves the
// title NULL, no crash).
func TestGenerateTitle_StreamError(t *testing.T) {
	t.Parallel()
	boom := errors.New("provider down")
	client := agenttest.NewFakeClient(agenttest.FakeTurn{Err: boom})
	_, err := generateTitle(context.Background(), client, "test-model", titleHistory())
	if err == nil {
		t.Fatal("generateTitle: want error on stream failure, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error must wrap the stream failure, got %v", err)
	}
}

// TestGenerateTitle_EmptyResult: a model that streams only whitespace yields an
// "empty result" error (not an empty title written to the DB).
func TestGenerateTitle_EmptyResult(t *testing.T) {
	t.Parallel()
	client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "   \n  "))
	if _, err := generateTitle(context.Background(), client, "test-model", titleHistory()); err == nil {
		t.Error("generateTitle: want error on empty result, got nil")
	}
}

// TestGenerateTitle_NilClient guards the nil-client path.
func TestGenerateTitle_NilClient(t *testing.T) {
	t.Parallel()
	if _, err := generateTitle(context.Background(), nil, "m", nil); err == nil {
		t.Error("generateTitle(nil client): want error")
	}
}

func TestSanitizeTitle(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"  hello  ":              "hello",
		"\"quoted\"":             "quoted",
		"'single'":               "single",
		"`backtick`":             "backtick",
		strings.Repeat("a", 100): strings.Repeat("a", 80),
	}
	for in, want := range cases {
		if got := sanitizeTitle(in); got != want {
			t.Errorf("sanitizeTitle(%q): got %q want %q", in, got, want)
		}
	}
}

func TestRenderHistoryForTitle_SkipsNonChatAndTruncates(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("z", 1000)
	h := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleTool, Content: "tool result"},
		{Role: llm.RoleUser, Content: long},
	}
	got := renderHistoryForTitle(h)
	if strings.Contains(got, "sys") || strings.Contains(got, "tool result") {
		t.Errorf("system/tool turns must be skipped, got %q", got)
	}
	if strings.Count(got, "z") > 500 {
		t.Errorf("per-turn content must be truncated, got %d z's", strings.Count(got, "z"))
	}
}
