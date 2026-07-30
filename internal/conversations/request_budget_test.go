package conversations

import (
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestValidateFinalRequestBudgetCountsSystemMessagesAndTools(t *testing.T) {
	t.Parallel()

	cfg := llm.Config{
		Provider:        "openrouter",
		ContextWindow:   25_000,
		MaxOutputTokens: 20_000,
	}
	req := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: strings.Repeat("system contract ", 8_000)},
			{Role: llm.RoleUser, Content: "hello"},
		},
	}
	if _, err := ValidateFinalRequestBudget(req, cfg); !errors.Is(err, ErrContextWindowExceeded) {
		t.Fatalf("oversized system request error = %v, want ErrContextWindowExceeded", err)
	}

	req.Messages[0].Content = "small system"
	var tool llm.ToolDef
	tool.Type = "function"
	tool.Function.Name = "large_manifest"
	tool.Function.Description = strings.Repeat("industrial schema ", 8_000)
	req.Tools = []llm.ToolDef{tool}
	if _, err := ValidateFinalRequestBudget(req, cfg); !errors.Is(err, ErrContextWindowExceeded) {
		t.Fatalf("oversized tool manifest error = %v, want ErrContextWindowExceeded", err)
	}
}

func TestValidateFinalRequestBudgetUsesCompleteEnvelopeAndProviderReserve(t *testing.T) {
	t.Parallel()

	req := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "system"},
			{Role: llm.RoleUser, Content: "hello"},
		},
		MaxTokens: 64,
	}
	cfg := llm.Config{
		Provider:        "openrouter",
		ContextWindow:   128_000,
		MaxOutputTokens: 20_000,
	}
	estimate, err := ValidateFinalRequestBudget(req, cfg)
	if err != nil {
		t.Fatalf("small complete request: %v", err)
	}
	if estimate <= 0 {
		t.Fatalf("estimate = %d, want positive complete-envelope count", estimate)
	}

	cfg.ContextWindow = 0
	if disabled, err := ValidateFinalRequestBudget(req, cfg); err != nil || disabled != 0 {
		t.Fatalf("zero-window compatibility = (%d, %v), want (0, nil)", disabled, err)
	}
}
