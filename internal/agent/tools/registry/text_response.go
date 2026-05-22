package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// TextResponseTool delivers a direct, verbatim reply to the user without an
// extra LLM synthesis round. Calling it is a terminal signal: the agent loop
// exits immediately after Execute returns and the text argument is delivered
// as-is to the user.
type TextResponseTool struct{}

var _ Tool = (*TextResponseTool)(nil)

func (t *TextResponseTool) Name() string { return "text_response" }

func (t *TextResponseTool) Description() string {
	return "Reply directly to the user with the given text. Call when you have the final answer and no further tool calls are needed. The text argument is delivered verbatim. This terminates the turn."
}

func (t *TextResponseTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "The verbatim reply the user will receive. Must be non-empty.",
			},
		},
		"required": []string{"text"},
	}
}

func (t *TextResponseTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		VisibilityTier: VisibilityAlwaysOn,
		ReadOnlyHint:   true,
		IdempotentHint: true,
	}
}

// Execute returns text verbatim. Empty text is rejected so the model cannot
// silently close a turn without a meaningful reply.
func (t *TextResponseTool) Execute(_ context.Context, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("text_response: text is required and must be non-empty: %w", llm.ErrSchemaValidation)
	}
	return text, nil
}
