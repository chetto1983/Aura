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
	Kind       string // "clarification" | "approval"
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
	return "Pause the agent and ask the user a clarifying question or request approval. Required: question. Optional: options (2-4 choices), kind (clarification|approval, default clarification)."
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
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"minItems":    2,
				"maxItems":    4,
				"description": "Optional 2–4 short answer choices for clarification questions. Omit for approval requests (canonical options are auto-supplied). Each choice should be a distinct, actionable intent.",
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{"clarification", "approval"},
				"description": "clarification = choosing between interpretations or supplying a missing slot; approval = confirming a risky or irreversible action. Default: clarification.",
			},
		},
		"required": []string{"question"},
		"examples": []any{
			map[string]any{"question": "Quale progetto vuoi che apra?", "options": []string{"Aura", "Gamma", "Mostrami tutti"}, "kind": "clarification"},
			map[string]any{"question": "Eliminare la pagina wiki 'old-contacts'? Operazione irreversibile.", "kind": "approval"},
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

	var opts []string
	if raw, ok := args["options"]; ok {
		switch v := raw.(type) {
		case []string:
			opts = append(opts, v...)
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					if s = strings.TrimSpace(s); s != "" {
						opts = append(opts, s)
					}
				}
			}
		}
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
