package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func TestStreamChokepointRejectsOversizedFinalRequestBeforeProvider(t *testing.T) {
	t.Parallel()

	client := &retryClient{chunks: []llm.Chunk{{Text: "must not execute"}}}
	a := NewLlmAgent(LlmAgentConfig{
		Client: client,
		LLM: llm.Config{
			Model:           "m",
			Provider:        "openrouter",
			ContextWindow:   25_000,
			MaxOutputTokens: 20_000,
			TotalTimeoutSec: 30,
		},
		Registry: tools.NewRegistry(),
	})
	req := llm.Request{Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: strings.Repeat("final payload ", 8_000)},
		{Role: llm.RoleUser, Content: "hello"},
	}}

	_, err := a.streamWithOpenRetry(context.Background(), req, "preflight")
	if !errors.Is(err, conversations.ErrContextWindowExceeded) {
		t.Fatalf("stream error = %v, want ErrContextWindowExceeded", err)
	}
	if client.calls != 0 {
		t.Fatalf("provider calls = %d, want 0 for rejected final request", client.calls)
	}
}
