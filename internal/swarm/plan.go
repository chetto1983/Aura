package swarm

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aura/aura/internal/toolsets"
)

const (
	DefaultMaxPlanAssignments = 6
	resultPreviewLimit        = 240
)

var defaultPlanRoles = []string{"librarian", "critic", "researcher", "skillsmith", "synthesizer"}

type PlanOptions struct {
	Roles          []string
	UserID         string
	MaxAssignments int
}

type Plan struct {
	Goal        string
	Roles       []string
	Assignments []Assignment
}

type RunSynthesis struct {
	RunID   string              `json:"run_id,omitempty"`
	Goal    string              `json:"goal,omitempty"`
	Status  RunStatus           `json:"status,omitempty"`
	Summary string              `json:"summary,omitempty"`
	Metrics RunSynthesisMetrics `json:"metrics"`
	Tasks   []TaskSynthesis     `json:"tasks"`
}

type RunSynthesisMetrics struct {
	TotalTasks       int     `json:"total_tasks"`
	CompletedTasks   int     `json:"completed_tasks"`
	FailedTasks      int     `json:"failed_tasks"`
	RunningTasks     int     `json:"running_tasks"`
	PendingTasks     int     `json:"pending_tasks"`
	LLMCalls         int     `json:"llm_calls"`
	ToolCalls        int     `json:"tool_calls"`
	TokensPrompt     int     `json:"tokens_prompt"`
	TokensCompletion int     `json:"tokens_completion"`
	TokensTotal      int     `json:"tokens_total"`
	TaskElapsedMS    int64   `json:"task_elapsed_ms"`
	WallMS           int64   `json:"wall_ms"`
	Speedup          float64 `json:"speedup"`
}

type TaskSynthesis struct {
	ID            string     `json:"id"`
	Role          string     `json:"role"`
	Subject       string     `json:"subject,omitempty"`
	Status        TaskStatus `json:"status"`
	ResultPreview string     `json:"result_preview,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LLMCalls      int        `json:"llm_calls"`
	ToolCalls     int        `json:"tool_calls"`
	TokensTotal   int        `json:"tokens_total"`
	ElapsedMS     int64      `json:"elapsed_ms"`
}

func PlanAssignments(goal string, roles []string, userID string) ([]Assignment, error) {
	plan, err := BuildPlan(goal, PlanOptions{Roles: roles, UserID: userID})
	if err != nil {
		return nil, err
	}
	return plan.Assignments, nil
}

func BuildPlan(goal string, opts PlanOptions) (Plan, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Plan{}, errors.New("swarm plan: goal is required")
	}

	roles, err := normalizePlanRoles(opts.Roles, opts.MaxAssignments)
	if err != nil {
		return Plan{}, err
	}

	assignments := make([]Assignment, 0, len(roles))
	for _, role := range roles {
		assignments = append(assignments, Assignment{
			Role:               role,
			Subject:            roleSubject(role, goal),
			Prompt:             rolePrompt(role, goal),
			SystemPrompt:       rolePlanSystemPrompt(role),
			ToolAllowlist:      cloneStrings(roleReadOnlyTools(role)),
			Depth:              0,
			UserID:             opts.UserID,
			MaxToolCalls:       roleMaxToolCalls(role),
			MaxToolResultChars: roleMaxToolResultChars(role),
			CompleteOnDeadline: true,
		})
	}

	return Plan{
		Goal:        goal,
		Roles:       cloneStrings(roles),
		Assignments: assignments,
	}, nil
}

func SynthesizeRunResult(result RunResult) RunSynthesis {
	out := RunSynthesis{}
	if result.Run != nil {
		out.RunID = result.Run.ID
		out.Goal = result.Run.Goal
		out.Status = result.Run.Status
		if result.Run.CompletedAt != nil {
			out.Metrics.WallMS = result.Run.CompletedAt.Sub(result.Run.CreatedAt).Milliseconds()
		}
	}

	tasks := cloneTasks(result.Tasks)
	sort.SliceStable(tasks, func(i, j int) bool {
		if rankPlanRole(tasks[i].Role) != rankPlanRole(tasks[j].Role) {
			return rankPlanRole(tasks[i].Role) < rankPlanRole(tasks[j].Role)
		}
		if tasks[i].Subject != tasks[j].Subject {
			return tasks[i].Subject < tasks[j].Subject
		}
		return tasks[i].ID < tasks[j].ID
	})

	out.Tasks = make([]TaskSynthesis, 0, len(tasks))
	out.Metrics.TotalTasks = len(tasks)
	for _, task := range tasks {
		switch task.Status {
		case TaskCompleted:
			out.Metrics.CompletedTasks++
		case TaskFailed:
			out.Metrics.FailedTasks++
		case TaskRunning:
			out.Metrics.RunningTasks++
		case TaskPending:
			out.Metrics.PendingTasks++
		}
		out.Metrics.LLMCalls += task.LLMCalls
		out.Metrics.ToolCalls += task.ToolCalls
		out.Metrics.TokensPrompt += task.TokensPrompt
		out.Metrics.TokensCompletion += task.TokensCompletion
		out.Metrics.TokensTotal += task.TokensTotal
		out.Metrics.TaskElapsedMS += task.ElapsedMS
		out.Tasks = append(out.Tasks, TaskSynthesis{
			ID:            task.ID,
			Role:          task.Role,
			Subject:       task.Subject,
			Status:        task.Status,
			ResultPreview: oneLinePreview(task.Result, resultPreviewLimit),
			LastError:     oneLinePreview(task.LastError, resultPreviewLimit),
			LLMCalls:      task.LLMCalls,
			ToolCalls:     task.ToolCalls,
			TokensTotal:   task.TokensTotal,
			ElapsedMS:     task.ElapsedMS,
		})
	}
	if out.Metrics.WallMS > 0 {
		out.Metrics.Speedup = float64(out.Metrics.TaskElapsedMS) / float64(out.Metrics.WallMS)
	}
	out.Summary = buildRunSynthesisSummary(out)
	return out
}

func normalizePlanRoles(requested []string, maxAssignments int) ([]string, error) {
	if maxAssignments <= 0 {
		maxAssignments = DefaultMaxPlanAssignments
	}
	if len(requested) == 0 {
		requested = defaultPlanRoles
	}

	seen := make(map[string]bool, len(requested))
	roles := make([]string, 0, len(requested))
	for _, role := range requested {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" || seen[role] {
			continue
		}
		if !knownPlanRole(role) {
			return nil, fmt.Errorf("swarm plan: unknown role %q", role)
		}
		seen[role] = true
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		return nil, errors.New("swarm plan: at least one role required")
	}
	if len(roles) > maxAssignments {
		return nil, fmt.Errorf("swarm plan: %d roles exceeds max assignments %d", len(roles), maxAssignments)
	}
	return roles, nil
}

func knownPlanRole(role string) bool {
	return rankPlanRole(role) < len(defaultPlanRoles)
}

func rankPlanRole(role string) int {
	for i, known := range defaultPlanRoles {
		if role == known {
			return i
		}
	}
	return len(defaultPlanRoles)
}

func roleReadOnlyTools(role string) []string {
	tools, _ := toolsets.RoleTools(role)
	return tools
}

func roleSubject(role, goal string) string {
	return role + ": " + shortGoal(goal, 72)
}

func rolePlanSystemPrompt(role string) string {
	return "You are an AuraBot " + role + ". Complete only the assigned focused task. Use only available read-only tools. Do not mutate files, wiki pages, skills, settings, or external state. Keep tool use selective, avoid repeated equivalent searches, and always finish with a concise result containing evidence, gaps, and the next useful action."
}

func rolePrompt(role, goal string) string {
	switch role {
	case "librarian":
		return "Goal: " + goal + "\n\nFocus: inspect existing wiki pages and source inbox records that may answer or constrain the goal. Return relevant page/source references, gaps, and concise evidence. Do not write or modify anything."
	case "critic":
		return "Goal: " + goal + "\n\nFocus: look for contradictions, stale assumptions, missing evidence, and quality risks in existing wiki/source material. Return the strongest concerns first. Do not write or modify anything."
	case "researcher":
		return "Goal: " + goal + "\n\nFocus: gather current external context only when it directly helps the goal. Use at most two targeted searches before deciding whether one fetch is worth it. Return source URLs, dates when relevant, and a compact evidence summary. Do not perform actions beyond read-only search/fetch."
	case "skillsmith":
		return "Goal: " + goal + "\n\nFocus: inspect available skills and skill catalog entries that could help execute the goal. Return matching skill names, why they matter, and gaps. Do not install, delete, or edit skills."
	case "synthesizer":
		return "Goal: " + goal + "\n\nFocus: prepare a concise integration brief from available wiki/source context and likely worker findings. Identify the answer shape, unresolved questions, and final-response outline. Do not write or modify anything."
	default:
		return "Goal: " + goal + "\n\nFocus: complete the assigned read-only investigation and return concise evidence."
	}
}

func roleMaxToolCalls(role string) int {
	switch role {
	case "researcher":
		return 3
	case "librarian", "synthesizer":
		return 4
	case "critic", "skillsmith":
		return 3
	default:
		return 3
	}
}

func roleMaxToolResultChars(role string) int {
	switch role {
	case "researcher":
		return 1800
	default:
		return 2400
	}
}

func buildRunSynthesisSummary(s RunSynthesis) string {
	status := string(s.Status)
	if status == "" {
		status = "unknown"
	}
	roles := make([]string, 0, len(s.Tasks))
	for _, task := range s.Tasks {
		roles = append(roles, task.Role+"="+string(task.Status))
	}
	if len(roles) == 0 {
		roles = append(roles, "none")
	}
	return fmt.Sprintf(
		"Run %s (%s): %d/%d completed, %d failed, %d running, %d pending. Roles: %s. Metrics: llm=%d tools=%d tokens=%d task_elapsed_ms=%d wall_ms=%d speedup=%.2f.",
		emptyAs(s.RunID, "unknown"),
		status,
		s.Metrics.CompletedTasks,
		s.Metrics.TotalTasks,
		s.Metrics.FailedTasks,
		s.Metrics.RunningTasks,
		s.Metrics.PendingTasks,
		strings.Join(roles, ", "),
		s.Metrics.LLMCalls,
		s.Metrics.ToolCalls,
		s.Metrics.TokensTotal,
		s.Metrics.TaskElapsedMS,
		s.Metrics.WallMS,
		s.Metrics.Speedup,
	)
}

func shortGoal(goal string, limit int) string {
	return oneLinePreview(goal, limit)
}

func oneLinePreview(value string, limit int) string {
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	value = strings.Join(parts, " ")
	if limit > 0 && len(value) > limit {
		if limit <= 3 {
			return value[:limit]
		}
		return value[:limit-3] + "..."
	}
	return value
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func cloneTasks(tasks []Task) []Task {
	if len(tasks) == 0 {
		return nil
	}
	out := make([]Task, len(tasks))
	copy(out, tasks)
	return out
}
