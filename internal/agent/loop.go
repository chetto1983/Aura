package agent

// Loop is the Phase-1 conversation skeleton (deleted in Plan 02-07): build the
// prompt for the LLM, parse streamed chunks back, dispatch tool calls, repeat up
// to MaxSteps. One Loop instance == one conversation == one goroutine. The
// cornerstone Agent interface that supersedes it lives in agent.go; the package
// doc moved there. KV-cache discipline (stable prefix, no mutation of
// Messages[0]) is the caller's responsibility; tool dispatch is the
// tools.Registry's; streaming wire is the llm.Client's.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

const defaultMaxSteps = 8

// Loop owns one conversation. Re-use across turns by calling Turn(ctx, userMsg)
// repeatedly. The Messages slice grows append-only; index 0 is the system
// message and MUST NOT change between turns.
type Loop struct {
	Model    string
	Messages []llm.Message
	Tools    *tools.Registry
	Client   llm.Client
	MaxSteps int
}

// NewLoop seeds a conversation with one stable system message. Anything that
// belongs in the prompt cache (overlay text, persona, MaxSteps doc) belongs
// in `systemPrompt` — once set, it never changes.
func NewLoop(client llm.Client, model, systemPrompt string, reg *tools.Registry) *Loop {
	return &Loop{
		Model:    model,
		Client:   client,
		Tools:    reg,
		MaxSteps: defaultMaxSteps,
		Messages: []llm.Message{{Role: llm.RoleSystem, Content: systemPrompt}},
	}
}

// Turn consumes one user message, runs up to MaxSteps tool-call rounds, and
// returns the final assistant text once `text_response` is invoked (or the
// loop terminates without a text_response, in which case the last assistant
// raw text is returned).
func (l *Loop) Turn(ctx context.Context, userText string) (string, error) {
	l.Messages = append(l.Messages, llm.Message{Role: llm.RoleUser, Content: userText})

	for step := 0; step < l.MaxSteps; step++ {
		req := llm.Request{
			Model:       l.Model,
			Messages:    l.Messages,
			Tools:       l.toolDefs(),
			Temperature: 0,
		}
		ch, err := l.Client.Stream(ctx, req)
		if err != nil {
			return "", fmt.Errorf("llm stream step %d: %w", step, err)
		}

		var (
			assistantText string
			toolCalls     []llm.ToolCall
		)
		for chunk := range ch {
			if chunk.Text != "" {
				assistantText += chunk.Text
			}
			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, *chunk.ToolCall)
			}
		}

		l.Messages = append(l.Messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   assistantText,
			ToolCalls: toolCalls,
		})

		if len(toolCalls) == 0 {
			return assistantText, nil
		}

		for _, tc := range toolCalls {
			result, err := l.runTool(ctx, tc)
			if err != nil {
				result = "error: " + err.Error()
			}
			l.Messages = append(l.Messages, llm.Message{
				Role:       llm.RoleTool,
				Name:       tc.Function.Name,
				Content:    result,
				ToolCallID: tc.ID,
			})
			if tc.Function.Name == "text_response" {
				return result, nil
			}
		}
	}
	return "", fmt.Errorf("MaxSteps=%d exceeded without text_response", l.MaxSteps)
}

func (l *Loop) runTool(ctx context.Context, tc llm.ToolCall) (string, error) {
	t, ok := l.Tools.Get(tc.Function.Name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", tc.Function.Name)
	}
	return t.Execute(ctx, json.RawMessage(tc.Function.Arguments))
}

func (l *Loop) toolDefs() []llm.ToolDef {
	entries := l.Tools.Render()
	out := make([]llm.ToolDef, 0, len(entries))
	for _, e := range entries {
		var def llm.ToolDef
		def.Type = "function"
		def.Function.Name = e.Name
		if e.Deferred {
			def.Function.Description = e.Summary + " (deferred — call tool_search to load full schema)"
		} else {
			def.Function.Description = e.Description
			def.Function.Parameters = e.Parameters
		}
		out = append(out, def)
	}
	return out
}
