package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TodoTool is a lightweight working-memory scratchpad mirroring Claude Code's
// TodoWrite (parity P3): the model writes the FULL todo list each call (replacing
// the previous one) to plan and track multi-step work across a long turn — the
// coherence aid an autonomous swarm/cron run lacks today. The list is session-
// scoped and persists across turns. Non-deferred: like `task`, planning is a core
// verb the model should always see.
type TodoTool struct {
	mu   sync.Mutex
	byID map[string][]todoItem
}

type todoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm,omitempty"`
}

type todoWriteArgs struct {
	Todos []todoItem `json:"todos"`
}

// todoMarks renders each valid status; the map also doubles as the status
// allow-list (an unknown status is rejected). Statuses ride the field description
// (not a JSON-schema enum) so the OpenAI-compat wire stays DeepSeek-safe, matching
// the `task` tool's defensive schema shape.
var todoMarks = map[string]string{
	"pending":     "[ ]",
	"in_progress": "[~]",
	"completed":   "[x]",
}

func (t *TodoTool) Spec() Spec {
	return Spec{
		Name:    "todo_write",
		Summary: "Track multi-step work as a todo list (replace the full list each call).",
		Description: "Maintain a structured todo list to plan and track multi-step work. " +
			"Pass the FULL list each call — it replaces the previous one. Each item has content (imperative), status (pending | in_progress | completed), " +
			"and an optional activeForm (present-continuous form shown while in_progress). Keep exactly one item in_progress at a time. " +
			"Use it for non-trivial multi-step tasks; skip it for single-step work.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "todos": {
      "type": "array",
      "description": "The full todo list; replaces the previous list each call.",
      "items": {
        "type": "object",
        "properties": {
          "content": {"type": "string", "description": "The task in imperative form."},
          "status": {"type": "string", "description": "One of: pending, in_progress, completed."},
          "activeForm": {"type": "string", "description": "Optional present-continuous form shown while in_progress, e.g. \"Running tests\"."}
        },
        "required": ["content", "status"]
      }
    }
  },
  "required": ["todos"]
}`),
		// Deferred: 250 token per la lista di lavoro, che serve nei task lunghi e mai nelle
		// risposte brevi.
		Deferred: true,
	}
}

func (t *TodoTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a todoWriteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("todo_write args: %w", err)
	}
	inProgress := 0
	for i, it := range a.Todos {
		if strings.TrimSpace(it.Content) == "" {
			return ToolResult{}, fmt.Errorf("todo_write: todo %d has empty content", i)
		}
		if _, ok := todoMarks[it.Status]; !ok {
			return ToolResult{}, fmt.Errorf("todo_write: todo %d has invalid status %q (want pending|in_progress|completed)", i, it.Status)
		}
		if it.Status == "in_progress" {
			inProgress++
		}
	}
	if inProgress > 1 {
		return ToolResult{}, fmt.Errorf("todo_write: %d todos are in_progress; keep exactly one in_progress at a time", inProgress)
	}

	key := shellSessionKey(ctx) // reuse the per-session key (sessionID, "" for bare ctx)
	t.mu.Lock()
	if t.byID == nil {
		t.byID = map[string][]todoItem{}
	}
	t.byID[key] = a.Todos
	t.mu.Unlock()

	return NewResult(ctx, renderTodos(a.Todos))
}

// Evict reclaims a finished session's todo list (SessionEvictor, R-41). An unknown
// session id is a no-op; concurrency-safe under the same mutex that guards byID.
func (t *TodoTool) Evict(sessionID string) {
	t.mu.Lock()
	delete(t.byID, sessionID)
	t.mu.Unlock()
}

func renderTodos(todos []todoItem) string {
	if len(todos) == 0 {
		return "[todo list cleared]"
	}
	var b strings.Builder
	for _, it := range todos {
		b.WriteString(todoMarks[it.Status])
		b.WriteByte(' ')
		b.WriteString(it.Content)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
