package tools

import (
	"context"
	"fmt"
	"strings"
)

// ErrAwaitingUserInput is the sentinel error returned by AskUserTool.Execute.
// The agent loop detects it and pauses the run with Status=waiting_for_user.
type ErrAwaitingUserInput struct {
	Question string
	Options  []string
	Kind     string // "clarification" | "approval"
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
	return `Pause the current task and ask the user a clarifying question or request approval before proceeding.

USE ask_user ONLY for these cardinal cases:
1. A required slot is missing and cannot be inferred (e.g. "schedule a meeting" — with whom? when?).
2. The request is genuinely ambiguous with two or more viable interpretations that lead to different actions.
3. You are about to take an irreversible or destructive action (e.g. delete data, cancel a task, overwrite a file).
4. The action requires permission escalation (e.g. accessing private data, spending money, installing software).
5. You are about to write a durable user-memory update without an explicit instruction to do so.
6. Three or more consecutive tool failures have occurred with recoverable errors and you need user guidance.

DO NOT use ask_user when:
- The instruction is clear and complete — just execute it.
- The action is low-risk and easily reversible.
- The answer can be found by reading the wiki, prior context, or using a search or read tool.
- Only phrasing or formatting is unclear (pick a reasonable default and proceed).

kind="clarification": provide 2–4 short options that reflect the most likely user intents. Free-text reply is always accepted.
kind="approval": omit options — canonical choices (approve_once / approve_session / approve_persist / deny / cancel) are supplied by the channel.`
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
