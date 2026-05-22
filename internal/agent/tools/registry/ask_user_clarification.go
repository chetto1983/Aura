package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// AskUserClarificationTool is the structured-options variant of ask_user.
// Use it when the user's request is ambiguous and 2-4 concrete options cover the
// most likely intents. Options carry explicit label+value pairs so channel
// surfaces can render richer UI without string-parsing.
type AskUserClarificationTool struct{}

var _ Tool = (*AskUserClarificationTool)(nil)

func (t *AskUserClarificationTool) Name() string { return "ask_user_clarification" }

func (t *AskUserClarificationTool) Description() string {
	return "Read-only. Pause and ask a structured clarification with 2-4 concrete options. Required: question. Optional: options (label+value pairs, max 4), free_text_allowed (default true)."
}

func (t *AskUserClarificationTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "Specific clarification question. Include enough context for the user to answer without re-reading the conversation.",
			},
			"options": map[string]any{
				"type":     "array",
				"maxItems": 4,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label": map[string]any{"type": "string", "description": "Text displayed to the user."},
						"value": map[string]any{"type": "string", "description": "Machine-readable value returned when the user picks this option."},
					},
					"required": []string{"label", "value"},
				},
				"description": "2–4 answer choices, each with a human-readable label and a machine-readable value.",
			},
			"free_text_allowed": map[string]any{
				"type":        "boolean",
				"description": "When true (default) the user may reply with free text outside the listed options.",
			},
		},
		"required": []string{"question"},
	}
}

func (t *AskUserClarificationTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		VisibilityTier: VisibilityAlwaysOn,
		ReadOnlyHint:   true,
		IdempotentHint: true,
	}
}

// Execute validates options (max 4) and returns ErrAwaitingUserInput, reusing
// the ask_user pause-and-resume infrastructure.
func (t *AskUserClarificationTool) Execute(_ context.Context, args map[string]any) (string, error) {
	question, _ := args["question"].(string)
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("ask_user_clarification: question is required")
	}

	var labels []string
	if rawOpts, ok := args["options"]; ok {
		if opts, ok := rawOpts.([]any); ok {
			if len(opts) > 4 {
				return "", fmt.Errorf("ask_user_clarification: options must have at most 4 entries, got %d: %w",
					len(opts), llm.ErrSchemaValidation)
			}
			for _, item := range opts {
				if opt, ok := item.(map[string]any); ok {
					label, _ := opt["label"].(string)
					if label = strings.TrimSpace(label); label != "" {
						labels = append(labels, label)
					}
				}
			}
		}
	}

	return "", &ErrAwaitingUserInput{
		Question: question,
		Options:  labels,
		Kind:     "clarification",
	}
}
