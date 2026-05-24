package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/agentnote"
)

// agentNoteStore is the subset of agentnote.Store used by AgentNoteTool.
type agentNoteStore interface {
	Get(ctx context.Context, conversationID string) (content string, exists bool, err error)
	Set(ctx context.Context, conversationID, content string) error
	Append(ctx context.Context, conversationID, line string) error
	Clear(ctx context.Context, conversationID string) error
}

// AgentNoteTool provides a per-conversation scratchpad for the agent's
// working memory. Content is scoped to the current conversation and must be
// garbage-collected when the conversation ends.
type AgentNoteTool struct {
	store                  agentNoteStore
	conversationIDProvider func(ctx context.Context) (string, error)
}

// NewAgentNoteTool returns an AgentNoteTool backed by the given store.
// The conversationIDProvider closure resolves the current conversation ID
// at Execute time — wired by the caller (agent loop). Returns nil when store is nil.
func NewAgentNoteTool(store *agentnote.Store, conversationIDProvider func(ctx context.Context) (string, error)) *AgentNoteTool {
	if store == nil {
		return nil
	}
	return &AgentNoteTool{store: store, conversationIDProvider: conversationIDProvider}
}

func (t *AgentNoteTool) Name() string { return "agent_note" }

func (t *AgentNoteTool) Description() string {
	return `Per-conversation scratchpad for your own working memory (todo list, plan, intermediate findings). Private to this conversation, not visible to the user, not promoted to wiki or user memory.

EXAMPLES — copy the shape exactly:

  agent_note({"action":"set","content":"todo:\n- fetch wiki page\n- summarise"})
  agent_note({"action":"append","line":"step done at 14:32"})
  agent_note({"action":"get"})
  agent_note({"action":"clear"})

The "action" field is REQUIRED on every call. Valid values: "set", "append", "get", "clear".

Per-action required fields:
  • set    → also send "content" (string, full new note body)
  • append → also send "line"    (string, single line appended)
  • get    → no other fields
  • clear  → no other fields

Returns:
  • set    → "ok (N chars)"
  • append → "ok (note now N chars, M lines)"
  • get    → the note inside a fenced code block, or "(empty)"
  • clear  → "cleared"`
}

var agentNoteActions = []string{"set", "append", "get", "clear"}

var agentNoteHints = []ActionHint{
	{Name: "set", RequiredKeys: []string{"content"}},
	{Name: "append", RequiredKeys: []string{"line"}},
}

func (t *AgentNoteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        agentNoteActions,
				"description": `REQUIRED. Pick one of: "set" (replace, also send content), "append" (add line, also send line), "get" (read), "clear" (delete).`,
			},
			"content": map[string]any{
				"type":        "string",
				"description": `Required when action="set". The full new note body.`,
			},
			"line": map[string]any{
				"type":        "string",
				"description": `Required when action="append". A single line to add.`,
			},
		},
		"required": []string{"action"},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "set", RequiredKeys: []string{"content"}},
			{Name: "append", RequiredKeys: []string{"line"}},
			{Name: "get"},
			{Name: "clear"},
		}),
		// JSON Schema "examples" — top-level concrete calls. Models that
		// honour the field (Gemma, Claude with tool-use) read these before
		// the human-readable description, which fixes the "missing action"
		// failure observed on Telegram turn #143-144 (2026-05-23 dump).
		"examples": []any{
			map[string]any{"action": "set", "content": "todo:\n- step 1\n- step 2"},
			map[string]any{"action": "append", "line": "step 1 done"},
			map[string]any{"action": "get"},
			map[string]any{"action": "clear"},
		},
	}
}

func (t *AgentNoteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("agent_note: tool unavailable")
	}
	if rewritten, ok := RewriteVerbKeyAsAction(args, agentNoteActions, agentNoteHints); ok {
		args = rewritten
	}
	action := stringArg(args, "action")
	if action == "" {
		return "", ActionRequiredError("agent_note", agentNoteActions, args, agentNoteHints, "get")
	}

	convID, err := t.conversationIDProvider(ctx)
	if err != nil {
		return "", fmt.Errorf("agent_note: %w", err)
	}
	if convID == "" {
		return "", errors.New("agent_note: conversation ID not available")
	}

	switch action {
	case "set":
		content := stringArg(args, "content")
		if err := t.store.Set(ctx, convID, content); err != nil {
			return "", fmt.Errorf("agent_note: %w", err)
		}
		return fmt.Sprintf("ok (%d chars)", len(content)), nil

	case "append":
		line := stringArg(args, "line")
		if err := t.store.Append(ctx, convID, line); err != nil {
			return "", fmt.Errorf("agent_note: %w", err)
		}
		current, _, err := t.store.Get(ctx, convID)
		if err != nil {
			return "ok (appended)", nil
		}
		lineCount := 0
		if current != "" {
			lineCount = strings.Count(current, "\n") + 1
		}
		return fmt.Sprintf("ok (note now %d chars, %d lines)", len(current), lineCount), nil

	case "get":
		content, exists, err := t.store.Get(ctx, convID)
		if err != nil {
			return "", fmt.Errorf("agent_note: %w", err)
		}
		if !exists || content == "" {
			return "(empty)", nil
		}
		return "\n```\n" + content + "\n```\n", nil

	case "clear":
		if err := t.store.Clear(ctx, convID); err != nil {
			return "", fmt.Errorf("agent_note: %w", err)
		}
		return "cleared", nil

	default:
		return "", UnknownActionError("agent_note", action, agentNoteActions, args)
	}
}
