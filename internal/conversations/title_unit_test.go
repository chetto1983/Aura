// Unit tier (no build tag): the best-effort auto-title call, driven by the scripted
// local fake client (no network). Proves a success path produces a title and a
// stream-error path returns an error the caller answers with FallbackTitle.
package conversations

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/llm"
)

type titleTestTurn struct {
	chunks  []llm.Chunk
	openErr error
}

type titleTestClient struct {
	mu       sync.Mutex
	turns    []titleTestTurn
	requests []llm.Request
}

func (c *titleTestClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	turn := c.turns[0]
	c.turns = c.turns[1:]
	if turn.openErr != nil {
		return nil, turn.openErr
	}
	ch := make(chan llm.Chunk, len(turn.chunks))
	for _, chunk := range turn.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (c *titleTestClient) LastRequest() llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[len(c.requests)-1]
}

func titleTextClient(reason string, text ...string) *titleTestClient {
	chunks := make([]llm.Chunk, 0, len(text))
	for index, part := range text {
		chunk := llm.Chunk{Text: part}
		if index == len(text)-1 {
			chunk.FinishReason = reason
		}
		chunks = append(chunks, chunk)
	}
	return &titleTestClient{turns: []titleTestTurn{{chunks: chunks}}}
}

const titleUserMessage = "help me refactor the budget loop"

// TestGenerateTitle_Success: a scripted client streaming a title yields a sanitized
// non-empty title.
func TestGenerateTitle_Success(t *testing.T) {
	t.Parallel()
	client := titleTextClient("stop", "  \"Refactor the budget loop\"  ")
	got, err := GenerateTitle(context.Background(), client, "test-model", titleUserMessage)
	if err != nil {
		t.Fatalf("GenerateTitle: %v", err)
	}
	if got != "Refactor the budget loop" {
		t.Errorf("title sanitization: got %q", got)
	}
	// The request carried the title prompt and the person's message, nothing else.
	req := client.LastRequest()
	if len(req.Messages) != 2 || req.Messages[0].Role != llm.RoleSystem || req.Messages[1].Content != titleUserMessage {
		t.Errorf("title request shape wrong: %+v", req.Messages)
	}
	if req.ToolChoice != "none" {
		t.Errorf("title request tool choice = %q, want none", req.ToolChoice)
	}
	if req.Reasoning.Effort != llm.ReasoningEffortNone {
		t.Errorf("title request reasoning effort = %q, want none", req.Reasoning.Effort)
	}
}

// TestGenerateTitle_CapsTheUserMessage: a pasted document reaches the title model as
// its opening bytes, cut on a rune boundary.
func TestGenerateTitle_CapsTheUserMessage(t *testing.T) {
	t.Parallel()
	client := titleTextClient("stop", "Long paste")
	if _, err := GenerateTitle(context.Background(), client, "m", strings.Repeat("界", 400)); err != nil {
		t.Fatalf("GenerateTitle: %v", err)
	}
	got := client.LastRequest().Messages[1].Content
	if len(got) > titleInputCap || len(got) < titleInputCap-3 || !utf8.ValidString(got) {
		t.Fatalf("title input = %d bytes (valid UTF-8: %v), want the opening %d bytes on a rune boundary",
			len(got), utf8.ValidString(got), titleInputCap)
	}
}

// TestGenerateTitle_StreamError: a stream error is returned (the caller leaves the
// title NULL, no crash).
func TestGenerateTitle_StreamError(t *testing.T) {
	t.Parallel()
	boom := errors.New("provider down")
	client := &titleTestClient{turns: []titleTestTurn{{openErr: boom}}}
	_, err := GenerateTitle(context.Background(), client, "test-model", titleUserMessage)
	if err == nil {
		t.Fatal("GenerateTitle: want error on stream failure, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error must wrap the stream failure, got %v", err)
	}
}

func TestGenerateTitle_TerminalStreamError(t *testing.T) {
	t.Parallel()
	boom := errors.New("stream disconnected")
	client := &titleTestClient{turns: []titleTestTurn{{chunks: []llm.Chunk{
		{Text: "Partial title"},
		{Err: boom},
	}}}}
	_, err := GenerateTitle(context.Background(), client, "test-model", titleUserMessage)
	if !errors.Is(err, boom) {
		t.Fatalf("GenerateTitle: terminal stream error = %v, want wrapped %v", err, boom)
	}
}

func TestGenerateTitle_LengthIsIncomplete(t *testing.T) {
	t.Parallel()
	client := titleTextClient("length", "Partial title")
	if _, err := GenerateTitle(context.Background(), client, "test-model", titleUserMessage); err == nil {
		t.Fatal("GenerateTitle: length-truncated stream must not persist a partial title")
	}
}

// TestGenerateTitle_EmptyResult: a model that streams only whitespace yields an
// "empty result" error (not an empty title written to the DB).
func TestGenerateTitle_EmptyResult(t *testing.T) {
	t.Parallel()
	client := titleTextClient("stop", "   \n  ")
	if _, err := GenerateTitle(context.Background(), client, "test-model", titleUserMessage); err == nil {
		t.Error("GenerateTitle: want error on empty result, got nil")
	}
}

// TestGenerateTitle_NilClient guards the nil-client path.
func TestGenerateTitle_NilClient(t *testing.T) {
	t.Parallel()
	if _, err := GenerateTitle(context.Background(), nil, "m", titleUserMessage); err == nil {
		t.Error("GenerateTitle(nil client): want error")
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
		strings.Repeat("界", 100): strings.Repeat("界", 80),
	}
	for in, want := range cases {
		if got := sanitizeTitle(in); got != want {
			t.Errorf("sanitizeTitle(%q): got %q want %q", in, got, want)
		}
	}
}

func TestFallbackTitleFoldsWhitespace(t *testing.T) {
	t.Parallel()
	if title := FallbackTitle("  Pianifica\n il   rilascio  "); title != "Pianifica il rilascio" {
		t.Fatalf("title=%q", title)
	}
	if title := FallbackTitle(" \n\t "); title != "" {
		t.Fatalf("blank message: %q", title)
	}
}
