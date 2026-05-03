package swarmtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/tools"
)

type SpawnAuraBotTool struct {
	manager *swarm.Manager
}

type RunAuraBotSwarmTool struct {
	manager *swarm.Manager
}

func NewRunAuraBotSwarmTool(manager *swarm.Manager) *RunAuraBotSwarmTool {
	if manager == nil {
		return nil
	}
	return &RunAuraBotSwarmTool{manager: manager}
}

func (t *RunAuraBotSwarmTool) Name() string { return "run_aurabot_swarm" }

func (t *RunAuraBotSwarmTool) Description() string {
	return "Run a bounded read-only AuraBot team for a higher-level second-brain goal. Plans multiple roles, executes them in parallel, and returns a deterministic synthesis with metrics. MVP supports mode=wait only and cannot write wiki pages, sources, skills, settings, or files."
}

func (t *RunAuraBotSwarmTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"goal": map[string]any{
				"type":        "string",
				"description": "Higher-level read-only investigation goal for the AuraBot team.",
			},
			"roles": map[string]any{
				"type":        "array",
				"description": "Optional subset of read-only roles. Defaults to librarian, critic, researcher, skillsmith, synthesizer.",
				"items": map[string]any{
					"type": "string",
					"enum": []string{"librarian", "critic", "researcher", "skillsmith", "synthesizer"},
				},
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"wait"},
				"description": "Execution mode. MVP supports wait only.",
			},
		},
		"required": []string{"goal"},
	}
}

func (t *RunAuraBotSwarmTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.manager == nil {
		return "", errors.New("run_aurabot_swarm: swarm manager unavailable")
	}
	goal, err := requiredString(args, "goal")
	if err != nil {
		return "", err
	}
	mode := strings.TrimSpace(stringArg(args, "mode"))
	if mode == "" {
		mode = "wait"
	}
	if mode != "wait" {
		return "", fmt.Errorf("run_aurabot_swarm: unsupported mode %q (MVP supports wait)", mode)
	}

	plan, err := swarm.BuildPlan(goal, swarm.PlanOptions{
		Roles:  stringSliceArg(args, "roles"),
		UserID: tools.UserIDFromContext(ctx),
	})
	if err != nil {
		return "", err
	}
	run, runErr := t.manager.Run(ctx, swarm.RunRequest{
		Goal:        plan.Goal,
		CreatedBy:   tools.UserIDFromContext(ctx),
		Assignments: plan.Assignments,
	})
	synthesis := swarm.SynthesizeRunResult(run)
	resp := runSwarmResponse{
		OK:        runErr == nil,
		Goal:      plan.Goal,
		Roles:     plan.Roles,
		RunID:     synthesis.RunID,
		Status:    string(synthesis.Status),
		Summary:   synthesis.Summary,
		Metrics:   synthesis.Metrics,
		Tasks:     synthesis.Tasks,
		LastError: "",
	}
	if run.Run != nil {
		resp.LastError = run.Run.LastError
	}
	if runErr != nil && resp.LastError == "" {
		resp.LastError = runErr.Error()
	}
	return marshal(resp)
}

func NewSpawnAuraBotTool(manager *swarm.Manager) *SpawnAuraBotTool {
	if manager == nil {
		return nil
	}
	return &SpawnAuraBotTool{manager: manager}
}

func (t *SpawnAuraBotTool) Name() string { return "spawn_aurabot" }

func (t *SpawnAuraBotTool) Description() string {
	return "Run one bounded AuraBot worker for a focused background task. Roles have hardcoded read-only tool presets. Returns run/task metrics and result. MVP supports mode=wait only."
}

func (t *SpawnAuraBotTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Short task name used as the task subject.",
			},
			"role": map[string]any{
				"type":        "string",
				"enum":        []string{"librarian", "critic", "researcher", "synthesizer", "skillsmith"},
				"description": "Worker role. Controls the maximum allowed tool preset.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Focused worker prompt. Keep it small; do not paste full chat history.",
			},
			"tools": map[string]any{
				"type":        "array",
				"description": "Optional subset of the role's allowed tools. Empty uses the role preset.",
				"items":       map[string]any{"type": "string"},
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"wait"},
				"description": "Execution mode. MVP supports wait only.",
			},
		},
		"required": []string{"role", "task"},
	}
}

func (t *SpawnAuraBotTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.manager == nil {
		return "", errors.New("spawn_aurabot: swarm manager unavailable")
	}
	role, err := requiredString(args, "role")
	if err != nil {
		return "", err
	}
	prompt, err := requiredString(args, "task")
	if err != nil {
		return "", err
	}
	mode := strings.TrimSpace(stringArg(args, "mode"))
	if mode == "" {
		mode = "wait"
	}
	if mode != "wait" {
		return "", fmt.Errorf("spawn_aurabot: unsupported mode %q (MVP supports wait)", mode)
	}

	allowlist, err := resolveRoleTools(role, stringSliceArg(args, "tools"))
	if err != nil {
		return "", err
	}
	subject := strings.TrimSpace(stringArg(args, "name"))
	if subject == "" {
		subject = role + " task"
	}
	run, runErr := t.manager.Run(ctx, swarm.RunRequest{
		Goal:      subject,
		CreatedBy: tools.UserIDFromContext(ctx),
		Assignments: []swarm.Assignment{{
			Role:          role,
			Subject:       subject,
			Prompt:        prompt,
			SystemPrompt:  roleSystemPrompt(role),
			ToolAllowlist: allowlist,
			Depth:         0,
			UserID:        tools.UserIDFromContext(ctx),
		}},
	})
	resp := spawnResponse{OK: runErr == nil}
	if run.Run != nil {
		resp.RunID = run.Run.ID
		resp.Status = string(run.Run.Status)
		resp.Error = run.Run.LastError
	}
	if len(run.Tasks) > 0 {
		task := run.Tasks[0]
		resp.TaskID = task.ID
		resp.Role = task.Role
		resp.Result = task.Result
		resp.LLMCalls = task.LLMCalls
		resp.ToolCalls = task.ToolCalls
		resp.ElapsedMS = task.ElapsedMS
		resp.TokensPrompt = task.TokensPrompt
		resp.TokensCompletion = task.TokensCompletion
		resp.TokensTotal = task.TokensTotal
		if task.LastError != "" {
			resp.Error = task.LastError
		}
	}
	if runErr != nil && resp.Error == "" {
		resp.Error = runErr.Error()
	}
	return marshal(resp)
}

type ListSwarmTasksTool struct {
	store *swarm.Store
}

func NewListSwarmTasksTool(store *swarm.Store) *ListSwarmTasksTool {
	if store == nil {
		return nil
	}
	return &ListSwarmTasksTool{store: store}
}

func (t *ListSwarmTasksTool) Name() string { return "list_swarm_tasks" }

func (t *ListSwarmTasksTool) Description() string {
	return "List AuraBot swarm tasks for a run ID, including status and metrics."
}

func (t *ListSwarmTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"run_id": map[string]any{
				"type":        "string",
				"description": "Swarm run ID returned by spawn_aurabot.",
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
	store *swarm.Store
}

func NewReadSwarmResultTool(store *swarm.Store) *ReadSwarmResultTool {
	if store == nil {
		return nil
	}
	return &ReadSwarmResultTool{store: store}
}

func (t *ReadSwarmResultTool) Name() string { return "read_swarm_result" }

func (t *ReadSwarmResultTool) Description() string {
	return "Read one AuraBot task result, including final content, errors, and metrics."
}

func (t *ReadSwarmResultTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "Task ID returned by spawn_aurabot or list_swarm_tasks.",
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

type spawnResponse struct {
	OK               bool   `json:"ok"`
	RunID            string `json:"run_id,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	Status           string `json:"status,omitempty"`
	Role             string `json:"role,omitempty"`
	Result           string `json:"result,omitempty"`
	Error            string `json:"error,omitempty"`
	LLMCalls         int    `json:"llm_calls"`
	ToolCalls        int    `json:"tool_calls"`
	ElapsedMS        int64  `json:"elapsed_ms"`
	TokensPrompt     int    `json:"tokens_prompt"`
	TokensCompletion int    `json:"tokens_completion"`
	TokensTotal      int    `json:"tokens_total"`
}

type runSwarmResponse struct {
	OK        bool                      `json:"ok"`
	Goal      string                    `json:"goal"`
	Roles     []string                  `json:"roles"`
	RunID     string                    `json:"run_id,omitempty"`
	Status    string                    `json:"status,omitempty"`
	Summary   string                    `json:"summary,omitempty"`
	Metrics   swarm.RunSynthesisMetrics `json:"metrics"`
	Tasks     []swarm.TaskSynthesis     `json:"tasks"`
	LastError string                    `json:"last_error,omitempty"`
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

func resolveRoleTools(role string, requested []string) ([]string, error) {
	allowed, ok := roleToolPresets()[role]
	if !ok {
		return nil, fmt.Errorf("spawn_aurabot: unknown role %q", role)
	}
	if len(requested) == 0 {
		return allowed, nil
	}
	cleaned := cleanList(requested)
	for _, name := range cleaned {
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("spawn_aurabot: tool %q is not allowed for role %q", name, role)
		}
	}
	return cleaned, nil
}

func roleToolPresets() map[string][]string {
	return map[string][]string{
		"librarian":   []string{"list_wiki", "read_wiki", "search_wiki", "lint_wiki", "list_sources", "read_source", "lint_sources"},
		"critic":      []string{"lint_wiki", "list_wiki", "read_wiki", "lint_sources", "list_sources"},
		"researcher":  []string{"web_search", "web_fetch"},
		"synthesizer": []string{"list_wiki", "read_wiki", "search_wiki", "list_sources", "read_source"},
		"skillsmith":  []string{"list_skills", "read_skill", "search_skill_catalog"},
	}
}

func roleSystemPrompt(role string) string {
	return "You are an AuraBot " + role + ". Complete only the assigned focused task. Use only available allowed tools. Return a concise result with useful evidence and metrics-friendly structure."
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

func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return cleanList(x)
	case []any:
		values := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return cleanList(values)
	default:
		return nil
	}
}

func cleanList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func marshal(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
