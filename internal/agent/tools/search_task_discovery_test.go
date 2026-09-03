package tools

import (
	"strings"
	"testing"
)

// Measured on Telegram, 2026-08-30: asked "hai un tool scheduler?", the model called
// tool_search("scheduling"), got "no matching tools", and answered that it cannot wake
// itself up — while the deferred `task` tool (agent_job wake-ups, reminders, cron) had
// fired jobs for it the day before. The roster line and the BM25 index are the only two
// ways a deferred tool is found, so every phrasing an operator uses for "schedule /
// wake up later / cron" must rank `task` first.
func TestToolSearchFindsTaskForSchedulingQueries(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&TaskTool{})
	reg.Register(bm25Tool{name: "todo_write", summary: "Track a multi-step plan as a checklist for this turn."})
	reg.Register(bm25Tool{name: "calendar__list_events", summary: "List calendar events in a date range."})
	reg.Register(bm25Tool{name: "shell_bg", summary: "Run a long command in the background and poll it."})
	ts := &ToolSearch{Registry: reg}
	for _, q := range []string{
		"scheduling", "scheduler", "schedule", "cron", "cron job",
		"wake me up later", "wake up in 10 minutes", "run this again tomorrow morning",
		"reminder", "periodic check", "recurring task", "timer",
	} {
		matches, _, _ := ts.match(q, 3)
		var names []string
		for _, m := range matches {
			names = append(names, m.Spec().Name)
		}
		if len(names) == 0 || names[0] != "task" {
			t.Errorf("tool_search(%q) = %s, want task first", q, strings.Join(names, ","))
		}
	}
}
