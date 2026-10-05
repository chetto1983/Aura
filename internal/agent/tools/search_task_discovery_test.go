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

// Measured on the lab VM, 2026-10-05: asked for a WhatsApp reminder in 10 minutes, the model
// loaded the WhatsApp tools, searched the contacts and sent the text at once with
// send_message. A reminder named after a channel is still the scheduler's job: the task
// delivers it there at fire time. The competing tool carries the WhatsApp server's own text.
func TestToolSearchFindsTaskForChannelReminders(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&TaskTool{})
	reg.Register(bm25Tool{name: "whatsapp__send_message", summary: "Send a WhatsApp message to a person or group. For group chats use the JID."})
	reg.Register(bm25Tool{name: "pim__calendar", summary: "Calendar, email and contacts: list events, send an email, search contacts."})
	ts := &ToolSearch{Registry: reg}
	for _, q := range []string{
		"remind me on WhatsApp in 10 minutes",
		"WhatsApp reminder in 10 minutes",
		"send me a WhatsApp message tomorrow at 9",
		"email me a reminder tomorrow morning",
		"message me on Telegram in an hour",
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
