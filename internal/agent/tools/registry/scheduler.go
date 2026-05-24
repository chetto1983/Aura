package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/cron"
)

// schedulerToolMaxChars caps the LLM-facing output the same way other
// tools do. List output can grow with task count; truncate before
// dumping into context.
const schedulerToolMaxChars = 8000

// minScheduleEveryMinutes caps how often the LLM may schedule a recurring task.
// Anything tighter is a cost-bomb / self-DoS surface: a single prompt-injected
// schedule of every_minutes=1 would fire the agent loop every minute,
// exhausting LLM budget and writing archive rows on every fire.
const minScheduleEveryMinutes = 5

// RunTaskNowResult is the JSON envelope returned by the run_now action.
// Surfaces enough telemetry for the LLM to tell the user how the saved
// routine fared without a second tool call.
type RunTaskNowResult struct {
	OK               bool     `json:"ok"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Status           string   `json:"status"`
	Summary          string   `json:"summary,omitempty"`
	LastError        string   `json:"last_error,omitempty"`
	LLMCalls         int      `json:"llm_calls"`
	ToolCalls        int      `json:"tool_calls"`
	TokensPrompt     int      `json:"tokens_prompt"`
	TokensCompletion int      `json:"tokens_completion"`
	TokensTotal      int      `json:"tokens_total"`
	ElapsedMS        int64    `json:"elapsed_ms"`
	Notified         bool     `json:"notified"`
	Skipped          bool     `json:"skipped,omitempty"`
	WakeSignature    string   `json:"wake_signature,omitempty"`
	ToolAllowlist    []string `json:"tool_allowlist,omitempty"`
}

// ScheduledTaskRunner is the manual-fire surface the run_now action calls.
// In production this is implemented by *telegram.Bot.
type ScheduledTaskRunner interface {
	RunTaskNow(ctx context.Context, name string) (RunTaskNowResult, error)
}

// TaskTool consolidates the scheduler verb tools (schedule_task, list_tasks,
// cancel_task, run_task_now) into a single action-enum surface. Same
// picobot pattern as wiki_page and dev_tool: one tool, one action enum
// acting as the verb, so the LLM never has to discriminate between four
// near-identical schedule entry points.
//
//	action=schedule — persist a one-shot or recurring task.
//	action=list     — surface every task in the scheduler.
//	action=cancel   — flip an active task to status=cancelled.
//	action=run_now  — manually fire a saved task.
//
// runner may be nil at construction time and set later via SetRunner;
// the registration order in setup.go builds the *Bot (which implements
// ScheduledTaskRunner) AFTER the scheduler tools register, so we accept
// the deferred wiring rather than force a setup re-order.
type TaskTool struct {
	store  cron.Repository
	runner ScheduledTaskRunner
	loc    *time.Location
}

// NewTaskTool builds the unified scheduler tool. store is required;
// runner can be nil at construction (set later via SetRunner) so the
// schedule/list/cancel actions stay live even if the manual-fire path
// isn't wired yet. loc defaults to time.Local when nil.
func NewTaskTool(store cron.Repository, runner ScheduledTaskRunner, loc *time.Location) *TaskTool {
	if store == nil {
		return nil
	}
	if loc == nil {
		loc = time.Local
	}
	return &TaskTool{store: store, runner: runner, loc: loc}
}

// SetRunner wires the manual-fire path after the *Bot is constructed.
// Safe to call exactly once during setup; not concurrency-protected.
func (t *TaskTool) SetRunner(runner ScheduledTaskRunner) {
	t.runner = runner
}

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true, // cancel and run_now mutate or permanently remove tasks
		VisibilityTier:  VisibilityDeferred,
	}
}

func (t *TaskTool) Description() string {
	return `Manage scheduled tasks (reminders, agent jobs, wiki maintenance).

EXAMPLES — copy the shape exactly:

  task({"action":"list"})
  task({"action":"schedule","name":"morning-ping","kind":"reminder","payload":"Daily check-in","daily":"09:00"})
  task({"action":"schedule","name":"in-five","kind":"reminder","payload":"five minute reminder","in":"5m"})
  task({"action":"schedule","name":"weekly-recap","kind":"agent_job","payload":"summarise this week's notes","daily":"18:00","weekdays":["fri"]})
  task({"action":"cancel","name":"morning-ping"})
  task({"action":"run_now","name":"weekly-recap"})

action REQUIRED; valid: "schedule", "list", "cancel", "run_now".

Per-action required:
  • list     → nothing (optional status filter)
  • schedule → name AND kind AND exactly ONE of in / at_local / at / daily / every_minutes
  • cancel   → name
  • run_now  → name

Schedule fields are mutually exclusive: in (relative "5m"/"2h"/"1d"), at_local ("YYYY-MM-DDTHH:MM"), at (UTC ISO8601), daily ("HH:MM" + optional weekdays), every_minutes (>=5).`
}

func (t *TaskTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"schedule", "list", "cancel", "run_now"},
				"description": "Which operation to perform: schedule (persist new), list (enumerate), cancel (deactivate), run_now (fire saved task immediately).",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Task identifier. Required for schedule/cancel/run_now. Re-using a name on schedule UPDATES the existing task.",
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{"reminder", "wiki_maintenance", "agent_job"},
				"description": "schedule only, required: reminder | wiki_maintenance | agent_job.",
			},
			"payload": map[string]any{
				"type":        "string",
				"description": "schedule only, optional: task body. For reminder: message text. For wiki_maintenance: ignored. For agent_job: the goal to run, or a JSON object with goal/enabled_toolsets/skills/context_from/wake_if_changed/tool_allowlist/write_policy/notify. Prefer enabled_toolsets over raw tool_allowlist.",
			},
			"in": map[string]any{
				"type":        "string",
				"description": "schedule only: one-shot relative duration (\"60s\", \"5m\", \"2h\", \"1d\", \"2w\"). Server resolves to UTC.",
			},
			"at_local": map[string]any{
				"type":        "string",
				"description": "schedule only: one-shot wall-clock in user TZ. Accepts YYYY-MM-DDTHH:MM[:SS] or YYYY-MM-DD HH:MM[:SS].",
			},
			"at": map[string]any{
				"type":        "string",
				"description": "schedule only: one-shot absolute ISO8601 UTC.",
			},
			"daily": map[string]any{
				"type":        "string",
				"description": "schedule only: recurring local HH:MM (narrow with weekdays).",
			},
			"weekdays": map[string]any{
				"type":        "array",
				"description": "schedule only, optional with daily: filter to specific weekdays.",
				"items": map[string]any{
					"type": "string",
					"enum": []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"},
				},
			},
			"every_minutes": map[string]any{
				"type":        "integer",
				"description": "schedule only: recurring interval in minutes (>=5). 60 hourly, 1440 daily, 10080 weekly.",
				"minimum":     minScheduleEveryMinutes,
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"", "active", "done", "cancelled", "failed"},
				"description": "list only, optional: filter (active | done | cancelled | failed). Empty returns all.",
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "schedule", RequiredKeys: []string{"name", "kind"}},
			{Name: "list", RequiredKeys: nil},
			{Name: "cancel", RequiredKeys: []string{"name"}},
			{Name: "run_now", RequiredKeys: []string{"name"}},
		}),
		"examples": []any{
			map[string]any{"action": "list"},
			map[string]any{"action": "schedule", "name": "morning-ping", "kind": "reminder", "payload": "Daily check-in", "daily": "09:00"},
			map[string]any{"action": "schedule", "name": "in-five", "kind": "reminder", "payload": "five minute reminder", "in": "5m"},
			map[string]any{"action": "schedule", "name": "weekly-recap", "kind": "agent_job", "payload": "summarise this week's notes", "daily": "18:00", "weekdays": []string{"fri"}},
			map[string]any{"action": "cancel", "name": "morning-ping"},
			map[string]any{"action": "run_now", "name": "weekly-recap"},
		},
	}
}

func (t *TaskTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "schedule":
		return t.doSchedule(ctx, args)
	case "list":
		return t.doList(ctx, args)
	case "cancel":
		return t.doCancel(ctx, args)
	case "run_now":
		return t.doRunNow(ctx, args)
	case "":
		return "", ActionRequiredError("task", taskValidActions, args, taskActionHints, "list")
	default:
		return "", UnknownActionError("task", action, taskValidActions, args)
	}
}

var (
	taskValidActions = []string{"schedule", "list", "cancel", "run_now"}
	taskActionHints  = []ActionHint{
		// schedule is the most distinctive (needs kind + payload). cancel
		// and run_now both need only `name`; we default to cancel since
		// the model can re-clarify with run_now if needed.
		{Name: "schedule", RequiredKeys: []string{"kind", "payload"}},
		{Name: "cancel", RequiredKeys: []string{"name"}},
	}
)

func (t *TaskTool) doSchedule(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("task schedule: scheduler unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	kindStr, err := requiredString(args, "kind")
	if err != nil {
		return "", err
	}
	kind := cron.TaskKind(kindStr)
	switch kind {
	case cron.KindReminder, cron.KindWikiMaintenance, cron.KindAgentJob:
	default:
		return "", fmt.Errorf("task schedule: unknown kind %q", kindStr)
	}

	payload := stringArg(args, "payload")
	at := strings.TrimSpace(stringArg(args, "at"))
	atLocal := strings.TrimSpace(stringArg(args, "at_local"))
	in := strings.TrimSpace(stringArg(args, "in"))
	daily := strings.TrimSpace(stringArg(args, "daily"))
	weekdays := strings.TrimSpace(weekdayArg(args, "weekdays"))
	everyMinutes, hasEveryMinutes, err := positiveIntArg(args, "every_minutes")
	if err != nil {
		return "", err
	}

	scheduleFields := 0
	for _, v := range []string{at, atLocal, in, daily} {
		if v != "" {
			scheduleFields++
		}
	}
	if hasEveryMinutes {
		scheduleFields++
	}
	if scheduleFields == 0 {
		return "", errors.New("task schedule: provide one of in (relative), at_local (wall-clock), at (UTC), daily (HH:MM), or every_minutes (>=1)")
	}
	if scheduleFields > 1 {
		return "", errors.New("task schedule: in / at_local / at / daily / every_minutes are mutually exclusive; pick one")
	}
	if weekdays != "" && daily == "" {
		return "", errors.New("task schedule: weekdays can only be used with daily")
	}

	task := &cron.Task{Name: name, Kind: kind, Payload: payload}
	if kind == cron.KindReminder {
		uid := UserIDFromContext(ctx)
		if uid == "" {
			return "", errors.New("task schedule: reminder requires an authenticated user context")
		}
		task.RecipientID = uid
	}
	if kind == cron.KindAgentJob {
		agentPayload, err := cron.NormalizeAgentJobPayload(payload)
		if err != nil {
			return "", fmt.Errorf("task schedule: %w", err)
		}
		uid := UserIDFromContext(ctx)
		if uid == "" && agentPayload.Notify != nil && *agentPayload.Notify {
			return "", errors.New("task schedule: agent_job with notify=true requires an authenticated user context")
		}
		normalized, err := agentPayload.JSON()
		if err != nil {
			return "", fmt.Errorf("task schedule: %w", err)
		}
		task.Payload = normalized
		task.RecipientID = uid
	}
	now := time.Now().UTC()
	switch {
	case in != "":
		d, err := parseScheduleDuration(in)
		if err != nil {
			return "", fmt.Errorf("task schedule: parse in: %w", err)
		}
		if d <= 0 {
			return "", fmt.Errorf("task schedule: in %q must be positive", in)
		}
		ts := now.Add(d)
		task.ScheduleKind = cron.ScheduleAt
		task.ScheduleAt = ts
		task.NextRunAt = ts
	case atLocal != "":
		ts, err := parseLocalWallClock(atLocal, t.loc)
		if err != nil {
			return "", fmt.Errorf("task schedule: parse at_local: %w", err)
		}
		ts = ts.UTC()
		if !ts.After(now) {
			return "", fmt.Errorf("task schedule: at_local %s is not in the future (current local time: %s)", atLocal, now.In(t.loc).Format("2006-01-02 15:04:05"))
		}
		task.ScheduleKind = cron.ScheduleAt
		task.ScheduleAt = ts
		task.NextRunAt = ts
	case at != "":
		ts, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return "", fmt.Errorf("task schedule: parse at: %w", err)
		}
		ts = ts.UTC()
		if !ts.After(now) {
			return "", fmt.Errorf("task schedule: at %s is not in the future (current UTC: %s)", at, now.Format(time.RFC3339))
		}
		task.ScheduleKind = cron.ScheduleAt
		task.ScheduleAt = ts
		task.NextRunAt = ts
	case daily != "":
		normalizedWeekdays, err := cron.NormalizeWeekdays(weekdays)
		if err != nil {
			return "", fmt.Errorf("task schedule: %w", err)
		}
		next, err := cron.NextDailyRunOnWeekdays(daily, normalizedWeekdays, t.loc, now)
		if err != nil {
			return "", fmt.Errorf("task schedule: %w", err)
		}
		task.ScheduleKind = cron.ScheduleDaily
		task.ScheduleDaily = daily
		task.ScheduleWeekdays = normalizedWeekdays
		task.NextRunAt = next
	case hasEveryMinutes:
		task.ScheduleKind = cron.ScheduleEvery
		task.ScheduleEveryMinutes = everyMinutes
		task.NextRunAt = now.Add(time.Duration(everyMinutes) * time.Minute)
	}

	saved, err := t.store.Upsert(ctx, task)
	if err != nil {
		return "", fmt.Errorf("task schedule: %w", err)
	}

	when := saved.NextRunAt.Format(time.RFC3339)
	if saved.IsRecurring() {
		return fmt.Sprintf("Scheduled %s task %q %s.", saved.Kind, saved.Name, formatScheduleForUser(saved, when)), nil
	}
	return fmt.Sprintf("Scheduled %s task %q for %s.", saved.Kind, saved.Name, when), nil
}

func (t *TaskTool) doList(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("task list: scheduler unavailable")
	}
	statusFilter := cron.Status(strings.TrimSpace(stringArg(args, "status")))

	tasks, err := t.store.List(ctx, statusFilter)
	if err != nil {
		return "", fmt.Errorf("task list: %w", err)
	}
	if len(tasks) == 0 {
		if statusFilter == "" {
			return "No scheduled tasks.", nil
		}
		return fmt.Sprintf("No tasks with status %q.", statusFilter), nil
	}

	byStatus := make(map[cron.Status][]*cron.Task)
	for _, task := range tasks {
		byStatus[task.Status] = append(byStatus[task.Status], task)
	}
	statuses := make([]string, 0, len(byStatus))
	for st := range byStatus {
		statuses = append(statuses, string(st))
	}
	sort.Strings(statuses)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d task(s):\n\n", len(tasks))
	for _, st := range statuses {
		fmt.Fprintf(&sb, "## %s\n", st)
		for _, task := range byStatus[cron.Status(st)] {
			fmt.Fprintf(&sb, "- %s\n", formatTaskLine(task))
		}
	}
	return truncateForToolContext(sb.String(), schedulerToolMaxChars), nil
}

func (t *TaskTool) doCancel(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("task cancel: scheduler unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	ok, err := t.store.Cancel(ctx, name)
	if err != nil {
		return "", fmt.Errorf("task cancel: %w", err)
	}
	if !ok {
		// TaskWriter cannot distinguish "doesn't exist" from "already cancelled
		// or completed" without an extra Get; surface both possibilities so the
		// LLM doesn't waste a turn re-issuing cancel.
		return fmt.Sprintf("No active task named %q to cancel. The task may not exist, or it may already be cancelled or completed — call task(action=list) to verify.", name), nil
	}
	return fmt.Sprintf("Cancelled task %q.", name), nil
}

func (t *TaskTool) doRunNow(ctx context.Context, args map[string]any) (string, error) {
	if t.runner == nil {
		return "", errors.New("task run_now: runner unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", fmt.Errorf("task run_now: %w", err)
	}
	result, err := t.runner.RunTaskNow(ctx, name)
	if err != nil {
		return "", fmt.Errorf("task run_now: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("task run_now: marshal result: %w", err)
	}
	return string(data), nil
}

// formatTaskLine renders a single task entry for the list output.
func formatTaskLine(task *cron.Task) string {
	when := task.NextRunAt.Format(time.RFC3339)
	scheduleNote := when
	if task.IsRecurring() {
		scheduleNote = formatScheduleForUser(task, when)
	}
	parts := []string{
		fmt.Sprintf("`%s`", task.Name),
		string(task.Kind),
		scheduleNote,
	}
	if task.Payload != "" && task.Kind == cron.KindReminder {
		parts = append(parts, fmt.Sprintf("\"%s\"", truncateForToolContext(task.Payload, 80)))
	}
	if task.LastError != "" {
		parts = append(parts, fmt.Sprintf("last_error=%q", truncateForToolContext(task.LastError, 80)))
	}
	if task.Kind == cron.KindAgentJob && task.LastOutput != "" {
		parts = append(parts, fmt.Sprintf("last_output=%q", truncateForToolContext(task.LastOutput, 120)))
	}
	return strings.Join(parts, " · ")
}

func formatScheduleForUser(task *cron.Task, when string) string {
	switch task.ScheduleKind {
	case cron.ScheduleDaily:
		if task.ScheduleWeekdays != "" {
			return fmt.Sprintf("daily at %s on %s (next run %s)", task.ScheduleDaily, task.ScheduleWeekdays, when)
		}
		return fmt.Sprintf("daily at %s (next run %s)", task.ScheduleDaily, when)
	case cron.ScheduleEvery:
		return fmt.Sprintf("every %d minutes (next run %s)", task.ScheduleEveryMinutes, when)
	default:
		return when
	}
}

// parseLocalWallClock parses an ISO-ish wall-clock string in the given
// location. Accepts both "2006-01-02T15:04:05" and "2006-01-02 15:04",
// with or without seconds, so the LLM doesn't need to be picky about
// formatting. Times are interpreted as wall-clock in loc — never UTC.
func parseLocalWallClock(s string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if ts, err := time.ParseInLocation(layout, s, loc); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected YYYY-MM-DDTHH:MM[:SS] (no timezone), got %q", s)
}

func positiveIntArg(args map[string]any, key string) (int, bool, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	var n int
	switch x := v.(type) {
	case int:
		n = x
	case int64:
		n = int(x)
	case float64:
		if x != float64(int(x)) {
			return 0, true, fmt.Errorf("task schedule: %s must be an integer", key)
		}
		n = int(x)
	default:
		return 0, true, fmt.Errorf("task schedule: %s must be an integer", key)
	}
	if n < 1 {
		return 0, true, fmt.Errorf("task schedule: %s must be >= 1", key)
	}
	if key == "every_minutes" && n < minScheduleEveryMinutes {
		return 0, true, fmt.Errorf("task schedule: every_minutes must be >= %d", minScheduleEveryMinutes)
	}
	return n, true, nil
}

func weekdayArg(args map[string]any, key string) string {
	if v := strings.TrimSpace(stringArg(args, key)); v != "" {
		return v
	}
	values := stringSliceArg(args, key)
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ",")
}

// parseScheduleDuration accepts everything time.ParseDuration takes (s/m/h)
// plus "d" (days) and "w" (weeks) — units the tool description advertises but
// Go's stdlib does not. A bare integer falls back to time.ParseDuration which
// will reject it; this keeps existing diagnostics ("missing unit") intact.
func parseScheduleDuration(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	switch s[len(s)-1] {
	case 'd':
		n, err := strconv.Atoi(s[:len(s)-1])
		if err == nil {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	case 'w':
		n, err := strconv.Atoi(s[:len(s)-1])
		if err == nil {
			return time.Duration(n) * 7 * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}
