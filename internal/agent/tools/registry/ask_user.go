package tools

import (
	"context"
	"fmt"
	"strings"
)

// ErrAwaitingUserInput is the sentinel error returned by AskUserTool.Execute.
// The agent loop detects it and pauses the run with Status=waiting_for_user.
type ErrAwaitingUserInput struct {
	Question   string
	Options    []string
	Kind       string // "clarification" | "approval" | "choice"
	ToolCallID string
}

func (e *ErrAwaitingUserInput) Error() string {
	return fmt.Sprintf("ask_user: awaiting user input: %s", e.Question)
}

// AskUserTool implements the ask_user tool — a standard tool that signals the
// agent loop to pause and await the user's reply (PRD §5.2.5).
//
// The tool is exclusive: if the LLM batches it with other tool calls in the
// same turn, ask_user is processed first and the other calls are discarded
// (they re-emit on resume).
type AskUserTool struct{}

var _ Tool = (*AskUserTool)(nil)

func (t *AskUserTool) Name() string { return "ask_user" }

func (t *AskUserTool) Description() string {
	return "Pause the agent and ask the user a clarification, approval, or structured choice. Required: question. Optional: options (2-4 strings or label/value choices), kind (clarification|approval|choice, default clarification)."
}

func (t *AskUserTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "The question to present to the user. Be specific — include enough context for the user to answer without re-reading the full conversation.",
			},
			"options": map[string]any{
				"type":     "array",
				"minItems": 2,
				"maxItems": 4,
				"items": map[string]any{
					"oneOf": []any{
						map[string]any{"type": "string"},
						map[string]any{
							"type": "object",
							"properties": map[string]any{
								"label": map[string]any{"type": "string", "description": "Text displayed to the user."},
								"value": map[string]any{"type": "string", "description": "Machine-readable value for the choice."},
							},
							"required": []string{"label", "value"},
						},
					},
				},
				"description": "Optional 2-4 choices. Use strings for simple choices or {label,value} objects when the displayed label should map to a stable value.",
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{"clarification", "approval", "choice"},
				"description": "clarification = missing information; approval = confirming a risky action; choice = choosing one structured option. Default: clarification.",
			},
		},
		"required": []string{"question"},
		"examples": []any{
			map[string]any{"question": "Which project should I open?", "options": []string{"Aura", "Gamma", "Show all"}, "kind": "clarification"},
			map[string]any{
				"question": "Which destination should I use?",
				"options": []any{
					map[string]any{"label": "Production", "value": "prod"},
					map[string]any{"label": "Staging", "value": "staging"},
				},
				"kind": "choice",
			},
			map[string]any{"question": "Delete wiki page 'old-contacts'? This cannot be undone.", "kind": "approval"},
		},
	}
}

func (t *AskUserTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		VisibilityTier: VisibilityAlwaysOn,
	}
}

// Execute returns ErrAwaitingUserInput — the agent loop converts this into a
// run pause rather than a tool-result string.
func (t *AskUserTool) Execute(_ context.Context, args map[string]any) (string, error) {
	question, _ := args["question"].(string)
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("ask_user: question is required")
	}

	opts, err := askUserOptionLabels(args["options"])
	if err != nil {
		return "", err
	}

	kind := "clarification"
	if k, ok := args["kind"].(string); ok && k != "" {
		kind = k
	}

	return "", &ErrAwaitingUserInput{
		Question: question,
		Options:  opts,
		Kind:     kind,
	}
}

func askUserOptionLabels(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	var opts []string
	switch v := raw.(type) {
	case []string:
		for _, item := range v {
			if s := strings.TrimSpace(item); s != "" {
				opts = append(opts, s)
			}
		}
	case []any:
		if len(v) > 4 {
			return nil, fmt.Errorf("ask_user: options must have at most 4 entries")
		}
		for _, item := range v {
			switch opt := item.(type) {
			case string:
				if s := strings.TrimSpace(opt); s != "" {
					opts = append(opts, s)
				}
			case map[string]any:
				label, _ := opt["label"].(string)
				value, _ := opt["value"].(string)
				label = strings.TrimSpace(label)
				value = strings.TrimSpace(value)
				switch {
				case label != "":
					opts = append(opts, label)
				case value != "":
					opts = append(opts, value)
				}
			}
		}
	default:
		return nil, fmt.Errorf("ask_user: options must be an array")
	}
	if len(opts) > 4 {
		return nil, fmt.Errorf("ask_user: options must have at most 4 entries")
	}
	return opts, nil
}
