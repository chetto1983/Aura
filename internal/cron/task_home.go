package cron

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"unicode/utf8"
)

// taskTitleRunes bounds a task conversation's title to one sidebar line.
const taskTitleRunes = 60

// TaskConversations tells a task whether the conversation it answers in still exists and
// opens one of its own when it does not. Consumer-declared like ConversationRecorder: the
// composition root adapts *conversations.Store, so cron imports no conversation package.
type TaskConversations interface {
	ConversationExists(ctx context.Context, conversationID, identityID string) (bool, error)
	CreateTaskConversation(ctx context.Context, identityID, title string) (string, error)
}

// TaskOriginSetter records the conversation a task answers in from now on. *Store
// satisfies it.
type TaskOriginSetter interface {
	SetTaskOrigin(ctx context.Context, taskID, conversationID string) error
}

// homeTask gives a task a conversation of its own when the one it answers in is gone or was
// never there. On the lab VM, 2026-10-14, the operator bulk-deleted their conversations, a daily
// goal's origin among them, and its next run lost every tool-ledger row to the conversation
// foreign key and its outcome to "conversation not found". On 2026-10-31 an agent_job scheduled
// with `aura task add` had no origin at all: its ledger key fell back to the job's non-UUID
// session, and a strict profile denied every tool it called. Either task moves into a
// conversation of its own and keeps it. While the origin exists nothing changes, a check that
// cannot be answered moves nothing, and a kind other than agent_job with no origin is left
// alone, because none of them calls tools through the gateway.
func (d *Dispatch) homeTask(ctx context.Context, task Task) Task {
	convs := d.deps.TaskConversations
	if convs == nil || d.deps.TaskOrigins == nil {
		return task
	}
	if task.OriginConversationID == "" {
		if task.Kind != KindAgentJob {
			return task
		}
		return d.openTaskConversation(ctx, task, "cron: the agent_job was scheduled outside a conversation; it answers in one of its own")
	}
	exists, err := convs.ConversationExists(ctx, task.OriginConversationID, task.IdentityID)
	if err != nil {
		slog.Warn("cron: could not check the task's conversation; the run answers there anyway",
			"task", task.ID, "conversation", task.OriginConversationID, "err", err)
		return task
	}
	if exists {
		return task
	}
	return d.openTaskConversation(ctx, task, "cron: the task's conversation is gone; it answers in a conversation of its own")
}

// openTaskConversation opens the task's own conversation, records it as the task's origin for
// its next runs, and returns the task answering there. A failed open leaves the task as it was.
func (d *Dispatch) openTaskConversation(ctx context.Context, task Task, moved string) Task {
	id, err := d.deps.TaskConversations.CreateTaskConversation(ctx, task.IdentityID, taskConversationTitle(task))
	if err != nil {
		slog.Warn("cron: the task needs a conversation of its own and none could be opened",
			"task", task.ID, "conversation", task.OriginConversationID, "err", err)
		return task
	}
	if err := d.deps.TaskOrigins.SetTaskOrigin(ctx, task.ID, id); err != nil {
		slog.Warn("cron: the task's new conversation was not recorded; this run answers there",
			"task", task.ID, "conversation", id, "err", err)
	}
	slog.Info(moved, "task", task.ID, "previous", task.OriginConversationID, "conversation", id)
	task.OriginConversationID = id
	return task
}

// taskConversationTitle names a task's own conversation after what the task does: an
// agent_job's goal or a reminder's text, else its kind.
func taskConversationTitle(task Task) string {
	var payload struct {
		Goal string `json:"goal"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(task.Payload, &payload)
	what := strings.Join(strings.Fields(payload.Goal+" "+payload.Text), " ")
	if what == "" {
		what = string(task.Kind)
	}
	title := "⏰ " + what
	if utf8.RuneCountInString(title) <= taskTitleRunes {
		return title
	}
	return string([]rune(title)[:taskTitleRunes-1]) + "…"
}
