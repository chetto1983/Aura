package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/scheduler"
)

// schedulerToolMaxChars caps the LLM-facing output the same way other
// tools do. List output can grow with task count; truncate before
// dumping into context.
const schedulerToolMaxChars = 8000

// ScheduleTaskTool lets the LLM persist a one-shot or daily-recurring
// task. Recognized kinds: reminder (sends payload as a Telegram
// message at fire time), wiki_maintenance (runs the autonomous wiki
// pass).
type ScheduleTaskTool struct {
	store *scheduler.Store
	loc   *time.Location
}

func NewScheduleTaskTool(store *scheduler.Store, loc *time.Location) *ScheduleTaskTool {
	if loc == nil {
		loc = time.Local
	}
	return &ScheduleTaskTool{store: store, loc: loc}
}

func (t *ScheduleTaskTool) Name() string { return "schedule_task" }

func (t *ScheduleTaskTool) Description() string {
	return "Schedule a one-shot or daily task. Two kinds: \"reminder\" (sends payload to the user at fire time) and \"wiki_maintenance\" (runs the autonomous wiki pass). Pick one schedule field — prefer \"in\" for relative (\"60s\", \"5m\", \"2h\") and \"at_local\" for wall-clock times in the user's timezone."
}

func (t *ScheduleTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Unique task identifier (used for cancellation; re-using a name updates the existing task).",
			},
			"kind": map[string]any{
				"type":        "string",
				"description": "Either \"reminder\" or \"wiki_maintenance\".",
				"enum":        []string{"reminder", "wiki_maintenance"},
			},
			"payload": map[string]any{
				"type":        "string",
				"description": "Task body. For reminder: the message text. For wiki_maintenance: ignored.",
			},
			"in": map[string]any{
				"type":        "string",
				"description": "One-shot relative duration (e.g. \"60s\", \"5m\", \"2h\", \"1d\"). Server resolves to absolute UTC. Use this when the user says \"in N seconds/minutes/hours\".",
			},
			"at_local": map[string]any{
				"type":        "string",
				"description": "One-shot wall-clock time in the user's timezone, no offset (e.g. \"2026-04-30T17:00:00\" for 5pm local). Use this when the user names a specific clock time.",
			},
			"at": map[string]any{
				"type":        "string",
				"description": "One-shot absolute ISO8601 UTC (e.g. \"2026-04-30T15:00:00Z\"). Use only when the user is explicit about UTC.",
			},
			"daily": map[string]any{
				"type":        "string",
				"description": "Recurring local-time HH:MM (e.g. \"03:00\").",
			},
		},
		"required": []string{"name", "kind"},
	}
}

func (t *ScheduleTaskTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("schedule_task: scheduler unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	kindStr, err := requiredString(args, "kind")
	if err != nil {
		return "", err
	}
	kind := scheduler.TaskKind(kindStr)
	switch kind {
	case scheduler.KindReminder, scheduler.KindWikiMaintenance:
	default:
		return "", fmt.Errorf("schedule_task: unknown kind %q", kindStr)
	}

	payload := stringArg(args, "payload")
	at := strings.TrimSpace(stringArg(args, "at"))
	atLocal := strings.TrimSpace(stringArg(args, "at_local"))
	in := strings.TrimSpace(stringArg(args, "in"))
	daily := strings.TrimSpace(stringArg(args, "daily"))

	scheduleFields := 0
	for _, v := range []string{at, atLocal, in, daily} {
		if v != "" {
			scheduleFields++
		}
	}
	if scheduleFields == 0 {
		return "", errors.New("schedule_task: provide one of in (relative), at_local (wall-clock), at (UTC), or daily (HH:MM)")
	}
	if scheduleFields > 1 {
		return "", errors.New("schedule_task: in / at_local / at / daily are mutually exclusive — pick one")
	}

	task := &scheduler.Task{Name: name, Kind: kind, Payload: payload}
	// Reminders need a recipient — we capture it from the calling
	// conversation's user. Without it, the dispatcher has no chat to
	// send to, so reject the call up front rather than persisting a
	// task that will fail at fire time.
	if kind == scheduler.KindReminder {
		uid := UserIDFromContext(ctx)
		if uid == "" {
			return "", errors.New("schedule_task: reminder requires an authenticated user context")
		}
		task.RecipientID = uid
	}
	now := time.Now().UTC()
	switch {
	case in != "":
		d, err := time.ParseDuration(in)
		if err != nil {
			return "", fmt.Errorf("schedule_task: parse in: %w", err)
		}
		if d <= 0 {
			return "", fmt.Errorf("schedule_task: in %q must be positive", in)
		}
		ts := now.Add(d)
		task.ScheduleKind = scheduler.ScheduleAt
		task.ScheduleAt = ts
		task.NextRunAt = ts
	case atLocal != "":
		// Accept either "2026-04-30T17:00:00" or "2026-04-30 17:00" so
		// the LLM doesn't need to be picky about the separator.
		ts, err := parseLocalWallClock(atLocal, t.loc)
		if err != nil {
			return "", fmt.Errorf("schedule_task: parse at_local: %w", err)
		}
		ts = ts.UTC()
		if !ts.After(now) {
			return "", fmt.Errorf("schedule_task: at_local %s is not in the future (current local time: %s)", atLocal, now.In(t.loc).Format("2006-01-02 15:04:05"))
		}
		task.ScheduleKind = scheduler.ScheduleAt
		task.ScheduleAt = ts
		task.NextRunAt = ts
	case at != "":
		ts, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return "", fmt.Errorf("schedule_task: parse at: %w", err)
		}
		ts = ts.UTC()
		if !ts.After(now) {
			return "", fmt.Errorf("schedule_task: at %s is not in the future (current UTC: %s)", at, now.Format(time.RFC3339))
		}
		task.ScheduleKind = scheduler.ScheduleAt
		task.ScheduleAt = ts
		task.NextRunAt = ts
	case daily != "":
		next, err := scheduler.NextDailyRun(daily, t.loc, now)
		if err != nil {
			return "", fmt.Errorf("schedule_task: %w", err)
		}
		task.ScheduleKind = scheduler.ScheduleDaily
		task.ScheduleDaily = daily
		task.NextRunAt = next
	}

	saved, err := t.store.Upsert(ctx, task)
	if err != nil {
		return "", fmt.Errorf("schedule_task: %w", err)
	}

	when := saved.NextRunAt.Format(time.RFC3339)
	if saved.IsRecurring() {
		return fmt.Sprintf("Scheduled %s task %q daily at %s (next run %s).", saved.Kind, saved.Name, saved.ScheduleDaily, when), nil
	}
	return fmt.Sprintf("Scheduled %s task %q for %s.", saved.Kind, saved.Name, when), nil
}

// ListTasksTool surfaces every task in the scheduler. Output is sorted
// by next_run_at so the LLM sees the next-up entry first.
type ListTasksTool struct {
	store *scheduler.Store
}

func NewListTasksTool(store *scheduler.Store) *ListTasksTool {
	return &ListTasksTool{store: store}
}

func (t *ListTasksTool) Name() string { return "list_tasks" }

func (t *ListTasksTool) Description() string {
	return "List scheduled tasks. Optional status filter (active|done|cancelled|failed); omit to see everything."
}

func (t *ListTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "Optional filter (active|done|cancelled|failed). Empty returns all.",
				"enum":        []string{"", "active", "done", "cancelled", "failed"},
			},
		},
	}
}

func (t *ListTasksTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("list_tasks: scheduler unavailable")
	}
	statusFilter := scheduler.Status(strings.TrimSpace(stringArg(args, "status")))

	tasks, err := t.store.List(ctx, statusFilter)
	if err != nil {
		return "", fmt.Errorf("list_tasks: %w", err)
	}
	if len(tasks) == 0 {
		if statusFilter == "" {
			return "No scheduled tasks.", nil
		}
		return fmt.Sprintf("No tasks with status %q.", statusFilter), nil
	}

	// Group by status for a readable layout.
	byStatus := make(map[scheduler.Status][]*scheduler.Task)
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
		for _, task := range byStatus[scheduler.Status(st)] {
			fmt.Fprintf(&sb, "- %s\n", formatTaskLine(task))
		}
	}
	return truncateForToolContext(sb.String(), schedulerToolMaxChars), nil
}

func formatTaskLine(task *scheduler.Task) string {
	when := task.NextRunAt.Format(time.RFC3339)
	scheduleNote := when
	if task.IsRecurring() {
		scheduleNote = fmt.Sprintf("daily %s (next %s)", task.ScheduleDaily, when)
	}
	parts := []string{
		fmt.Sprintf("`%s`", task.Name),
		string(task.Kind),
		scheduleNote,
	}
	if task.Payload != "" && task.Kind == scheduler.KindReminder {
		parts = append(parts, fmt.Sprintf("\"%s\"", truncateForToolContext(task.Payload, 80)))
	}
	if task.LastError != "" {
		parts = append(parts, fmt.Sprintf("last_error=%q", truncateForToolContext(task.LastError, 80)))
	}
	return strings.Join(parts, " · ")
}

// CancelTaskTool flips an active task to status='cancelled' so the
// scheduler ignores it.
type CancelTaskTool struct {
	store *scheduler.Store
}

func NewCancelTaskTool(store *scheduler.Store) *CancelTaskTool {
	return &CancelTaskTool{store: store}
}

func (t *CancelTaskTool) Name() string { return "cancel_task" }

func (t *CancelTaskTool) Description() string {
	return "Cancel an active scheduled task by name. Returns whether the task existed and was active."
}

func (t *CancelTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the task to cancel.",
			},
		},
		"required": []string{"name"},
	}
}

func (t *CancelTaskTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("cancel_task: scheduler unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	ok, err := t.store.Cancel(ctx, name)
	if err != nil {
		return "", fmt.Errorf("cancel_task: %w", err)
	}
	if !ok {
		return fmt.Sprintf("No active task named %q.", name), nil
	}
	return fmt.Sprintf("Cancelled task %q.", name), nil
}

// parseLocalWallClock parses an ISO-ish wall-clock string in the given
// location. Accepts both "2006-01-02T15:04:05" and "2006-01-02 15:04",
// with or without seconds, so the LLM doesn't need to be precise about
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
