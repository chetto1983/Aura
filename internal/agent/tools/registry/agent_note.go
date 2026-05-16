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
	return `A per-conversation scratchpad for your own working memory (todo list, intermediate findings, plan). Use set to replace, append to add a new line, get to read, clear to delete. The note is scoped to the current conversation and garbage-collected at conversation end. NOT visible to the user; NOT promoted to wiki or user memory.

REQUIRED PARAMETERS BY ACTION (you MUST send all listed fields):
  • action="set":    content
  • action="append": line
  • action="get":    (none)
  • action="clear":  (none)

Actions (pick one via the "action" field):

  • set    — replace the entire note. Required: content (string).
             Returns "ok (N chars)" where N is the new byte length.

  • append — add a new line to the note. Required: line (string).
             If the note is empty, sets content=line with no leading
             newline. Returns "ok (note now N chars, M lines)".

  • get    — read the current note. Returns the full content in a
             fenced code block. Returns "(empty)" when no note exists.

  • clear  — delete the note. Returns "cleared".`
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
				"description": "Which action to perform on the scratchpad note.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full note content (action=set only).",
			},
			"line": map[string]any{
				"type":        "string",
				"description": "Single line to add to the note (action=append only).",
			},
		},
		"required": []string{"action"},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "set", RequiredKeys: []string{"content"}},
			{Name: "append", RequiredKeys: []string{"line"}},
			{Name: "get"},
			{Name: "clear"},
		}),
	}
}

func (t *AgentNoteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", errors.New("agent_note: tool unavailable")
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
		lineCount := 1
		if current == "" {
			lineCount = 0
		} else {
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
