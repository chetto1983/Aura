package swarmtools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/swarm"
)

type ListSwarmTasksTool struct {
	store swarm.TaskLister
}

func NewListSwarmTasksTool(store swarm.TaskLister) *ListSwarmTasksTool {
	if store == nil {
		return nil
	}
	return &ListSwarmTasksTool{store: store}
}

func (t *ListSwarmTasksTool) Name() string { return "list_swarm_tasks" }

func (t *ListSwarmTasksTool) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: tools.VisibilityDeferred,
	}
}

func (t *ListSwarmTasksTool) Description() string {
	return "List tasks for an Aura delegated swarm run, including status and metrics."
}

func (t *ListSwarmTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"run_id": map[string]any{
				"type":        "string",
				"description": "Swarm run ID returned by an AGENTDEF delegate tool.",
			},
		},
		"required": []string{"run_id"},
	}
}

func (t *ListSwarmTasksTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	runID, err := requiredString(args, "run_id")
	if err != nil {
		return "", err
	}
	tasks, err := t.store.ListTasks(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("list_swarm_tasks: %w", err)
	}
	items := make([]taskSummary, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, summarizeTask(task, false))
	}
	return marshal(map[string]any{"run_id": runID, "tasks": items})
}

type ReadSwarmResultTool struct {
	store swarm.TaskGetter
}

func NewReadSwarmResultTool(store swarm.TaskGetter) *ReadSwarmResultTool {
	if store == nil {
		return nil
	}
	return &ReadSwarmResultTool{store: store}
}

func (t *ReadSwarmResultTool) Name() string { return "read_swarm_result" }

func (t *ReadSwarmResultTool) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: tools.VisibilityDeferred,
	}
}

func (t *ReadSwarmResultTool) Description() string {
	return "Read one Aura delegated swarm task result, including final content, errors, and metrics."
}

func (t *ReadSwarmResultTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "Task ID returned by list_swarm_tasks.",
			},
		},
		"required": []string{"task_id"},
	}
}

func (t *ReadSwarmResultTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	taskID, err := requiredString(args, "task_id")
	if err != nil {
		return "", err
	}
	task, err := t.store.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("read_swarm_result: %w", err)
	}
	return marshal(summarizeTask(*task, true))
}

type taskSummary struct {
	ID               string   `json:"id"`
	RunID            string   `json:"run_id"`
	Role             string   `json:"role"`
	Subject          string   `json:"subject,omitempty"`
	Status           string   `json:"status"`
	Depth            int      `json:"depth"`
	ToolAllowlist    []string `json:"tool_allowlist,omitempty"`
	Result           string   `json:"result,omitempty"`
	LastError        string   `json:"last_error,omitempty"`
	LLMCalls         int      `json:"llm_calls"`
	ToolCalls        int      `json:"tool_calls"`
	ElapsedMS        int64    `json:"elapsed_ms"`
	TokensPrompt     int      `json:"tokens_prompt"`
	TokensCompletion int      `json:"tokens_completion"`
	TokensTotal      int      `json:"tokens_total"`
	CreatedAt        string   `json:"created_at"`
	CompletedAt      string   `json:"completed_at,omitempty"`
}

func summarizeTask(task swarm.Task, includeResult bool) taskSummary {
	out := taskSummary{
		ID:               task.ID,
		RunID:            task.RunID,
		Role:             task.Role,
		Subject:          task.Subject,
		Status:           string(task.Status),
		Depth:            task.Depth,
		ToolAllowlist:    task.ToolAllowlist,
		LastError:        task.LastError,
		LLMCalls:         task.LLMCalls,
		ToolCalls:        task.ToolCalls,
		ElapsedMS:        task.ElapsedMS,
		TokensPrompt:     task.TokensPrompt,
		TokensCompletion: task.TokensCompletion,
		TokensTotal:      task.TokensTotal,
		CreatedAt:        task.CreatedAt.Format(time.RFC3339),
	}
	if includeResult {
		out.Result = task.Result
	}
	if task.CompletedAt != nil {
		out.CompletedAt = task.CompletedAt.Format(time.RFC3339)
	}
	return out
}

func requiredString(args map[string]any, key string) (string, error) {
	value := strings.TrimSpace(stringArg(args, key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func marshal(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
