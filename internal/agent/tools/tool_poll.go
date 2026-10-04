package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ToolPoll reads, once, the result of a tool call that moved to the background
// (background_calls.go), or cancels one still running.
type ToolPoll struct {
	Calls *BackgroundCalls
}

type toolPollArgs struct {
	TaskID string `json:"task_id"`
	Cancel bool   `json:"cancel"`
}

func (p *ToolPoll) Spec() Spec {
	return Spec{
		Name:    "tool_poll",
		Summary: "Read the result of a tool call that moved to the background, or cancel it.",
		Description: "A tool call still running after its window continues in the background, and its result reads " +
			`{"status":"in_progress","task_id":...}. Aura notifies this conversation when it finishes; then call ` +
			"tool_poll once with that task_id: it returns the call's own result and forgets it. Before that it " +
			"reports the call as running, so do not poll in a loop. Pass \"cancel\": true to stop a call you no " +
			"longer need; nothing is delivered for it afterwards.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {"type": "string", "description": "The task_id the backgrounded call's result or its notification named."},
    "cancel": {"type": "boolean", "description": "Stop the call instead of reading it. Only a running call can be stopped."}
  },
  "required": ["task_id"]
}`),
		// Not deferred, for shell_poll's reason: a backgrounded result and its notification
		// name this tool, and a pointer handed to the model at runtime must be callable
		// when it is read.
		Deferred:   false,
		Foreground: true,
	}
}

// TerminateSession forwards a conversation delete to the registry, so a call the
// deleted conversation started does not outlive it (SessionJobTerminator).
func (p *ToolPoll) TerminateSession(ownerID, sessionID string) {
	if p == nil {
		return
	}
	p.Calls.TerminateSession(ownerID, sessionID)
}

func (p *ToolPoll) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if p.Calls == nil {
		return ToolResult{}, errors.New("tool_poll: background tool calls are not available in this context")
	}
	var a toolPollArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("tool_poll args: %w", err)
	}
	id := strings.TrimSpace(a.TaskID)
	if id == "" {
		return ToolResult{}, errors.New("tool_poll: task_id is required")
	}
	state, ok := p.Calls.poll(ctx, id, a.Cancel)
	if !ok {
		// A call of another conversation reads exactly like one that never existed.
		return ToolResult{}, fmt.Errorf("tool_poll: unknown task_id %q, or its result was already read", id)
	}
	switch {
	case state.status == BackgroundCallRunning:
		return toolPollStatus(ctx, id, state, "Still running. Aura notifies this conversation when it finishes; do not poll again before then.")
	case a.Cancel && state.status == BackgroundCallCancelled:
		return toolPollStatus(ctx, id, state, "Stopped. Nothing is delivered for it.")
	case state.err != nil:
		return ToolResult{}, fmt.Errorf("%s %s: %w", state.tool, state.status, state.err)
	default:
		return attributed(state.result, state.tool), nil
	}
}

// attributed keeps a result's provenance. One that carries none was judged trusted or not by
// its tool's name, which tool_poll's name would replace, so it is marked untrusted under the
// tool that produced it: never more trusted than it was.
func attributed(res ToolResult, tool string) ToolResult {
	if res.Provenance == nil {
		res.Provenance = &ToolResultProvenance{Source: tool, Trust: TrustUntrusted}
	}
	return res
}

func toolPollStatus(ctx context.Context, id string, state backgroundCallState, note string) (ToolResult, error) {
	body, err := json.Marshal(struct {
		TaskID    string `json:"task_id"`
		Tool      string `json:"tool"`
		Status    string `json:"status"`
		ElapsedMS int64  `json:"elapsed_ms"`
		Note      string `json:"note"`
	}{TaskID: id, Tool: state.tool, Status: state.status, ElapsedMS: state.elapsed.Milliseconds(), Note: note})
	if err != nil {
		return ToolResult{}, err
	}
	return runtimeResult(ctx, string(body))
}
