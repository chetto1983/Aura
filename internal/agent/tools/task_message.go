package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// A message to someone else, scheduled for a time and reviewed by the operator when it is
// scheduled (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md,
// Part 2). The model's task call never runs: the agent parks it behind a host-authored review
// (HostReview), and the reviewed draft is sent once at its time with no model in between.

// ScheduledMessageGrace is how long a scheduled message's review outlives its send time. A
// reviewed draft lives 24 hours (internal/messagedrafts/store.go), so a message is scheduled at
// most 24h minus this ahead.
const ScheduledMessageGrace = 15 * time.Minute

const messageMaxAhead = 24*time.Hour - ScheduledMessageGrace

var errMessageNeedsReview = errors.New("task schedule: a message to someone else is scheduled only in a conversation where the operator reviews it, and no review could be prepared here. Nothing was scheduled; tell the operator")

// HostReviewer is a tool whose call the host parks for the operator's review before it runs.
// The agent calls it only with a live responder; (nil, false) lets the call run normally.
type HostReviewer interface {
	HostReview(ctx context.Context, conversationID, toolCallID string, raw json.RawMessage) (*ErrAwaitingUserInput, bool)
}

// ScheduledMessageDraft is what the composition root needs to store a scheduled message's draft
// against the mounted trusted tool.
type ScheduledMessageDraft struct {
	ConversationID, ToolCallID, TaskID string
	Channel                            string          // whatsapp | email
	Args                               json.RawMessage // the trusted recipe tool's arguments
	SendAt                             time.Time
}

// taskMessageDrafts is the drafts seam. The tools package cannot call messagedrafts, which
// imports it (decision 7), so cmd/aura implements it.
type taskMessageDrafts interface {
	CreateScheduledMessageDraft(ctx context.Context, in ScheduledMessageDraft) (draftID string, err error)
}

// messagePayload is a message task's payload (decision 6).
type messagePayload struct {
	Channel   string   `json:"channel"`
	Recipient string   `json:"recipient"`
	To        []string `json:"to"`
	Account   string   `json:"account"`
	Subject   string   `json:"subject"`
	Text      string   `json:"text"`
}

// recipeArgs are the arguments the trusted recipe tool takes. Email is sent as plain text,
// because the PIM's default body format is HTML.
func (p messagePayload) recipeArgs() (json.RawMessage, error) {
	switch p.Channel {
	case "whatsapp":
		return json.Marshal(map[string]string{"recipient": p.Recipient, "message": p.Text})
	case "email":
		args := map[string]any{"action": "send_email", "to": p.To, "subject": p.Subject, "body": p.Text, "bodyFormat": "text"}
		if p.Account != "" {
			args["accountId"] = p.Account
		}
		return json.Marshal(args)
	default:
		return nil, errMessagePayload
	}
}

var errMessagePayload = errors.New("task schedule: a message needs channel whatsapp or email, a recipient and a text. Nothing was scheduled")

// messageSchedule checks that a message is one send, ahead, within the review's lifetime, and
// that its payload names a channel, a recipient and a text.
func (t *TaskTool) messageSchedule(a taskArgs, now time.Time) (resolvedSpec, messagePayload, error) {
	if a.ScheduleKind != "at" {
		return resolvedSpec{}, messagePayload{}, errors.New("task schedule: kind=message is one-shot only (schedule_kind=at); recurring messages to someone else are not supported. Nothing was scheduled")
	}
	at, err := time.Parse(time.RFC3339, a.At)
	if err != nil || !at.After(now) {
		return resolvedSpec{}, messagePayload{}, errors.New("task schedule: kind=message needs a time in the future. Nothing was scheduled")
	}
	if at.After(now.Add(messageMaxAhead)) {
		return resolvedSpec{}, messagePayload{}, errors.New("task schedule: kind=message can be scheduled at most 23h45m ahead, because a reviewed message lives 24 hours. Nothing was scheduled")
	}
	var p messagePayload
	if err := json.Unmarshal(a.Payload, &p); err != nil {
		return resolvedSpec{}, messagePayload{}, errMessagePayload
	}
	p.Channel, p.Recipient, p.Text = strings.TrimSpace(p.Channel), strings.TrimSpace(p.Recipient), strings.TrimSpace(p.Text)
	reaches := (p.Channel == "whatsapp" && p.Recipient != "") || (p.Channel == "email" && len(p.To) > 0)
	if !reaches || p.Text == "" {
		return resolvedSpec{}, messagePayload{}, errMessagePayload
	}
	spec, _, err := t.resolveSchedule(a)
	if err != nil {
		return resolvedSpec{}, messagePayload{}, err
	}
	return spec, p, nil
}

// validateSchedule is the prefix both paths run (decision 32): a refusal here is Execute's to
// return, so a host review that fails it parks nothing.
func validateSchedule(a taskArgs) error {
	if a.Kind == "" {
		return errors.New("task schedule: kind is required")
	}
	return requireTaskPayload(a.Kind, a.Payload)
}

// HostReview parks a scheduling call for the operator before the task can act: a message for the
// review of its text, a task that needs approval for the approval naming what will run. Every
// other call runs normally.
func (t *TaskTool) HostReview(ctx context.Context, conversationID, toolCallID string, raw json.RawMessage) (*ErrAwaitingUserInput, bool) {
	var a taskArgs
	if err := json.Unmarshal(raw, &a); err != nil || a.Action != "schedule" {
		return nil, false
	}
	if a.Kind == "message" {
		return t.hostReviewMessage(ctx, conversationID, toolCallID, a)
	}
	return t.hostReviewApproval(ctx, conversationID, toolCallID, a)
}

// hostReviewMessage persists a message task pending its review, with its draft, and returns the
// review pause keyed to the model's own call. Any refusal returns (nil, false), so Execute gives
// the model the same refusal.
func (t *TaskTool) hostReviewMessage(ctx context.Context, conversationID, toolCallID string, a taskArgs) (*ErrAwaitingUserInput, bool) {
	if validateSchedule(a) != nil || t.MessageDrafts == nil || t.Store == nil {
		return nil, false
	}
	plan, directive, err := t.resolveDelivery(ctx, &a)
	if err != nil || directive != nil {
		return nil, false
	}
	spec, payload, err := t.messageSchedule(a, time.Now())
	if err != nil {
		return nil, false
	}
	args, err := payload.recipeArgs()
	if err != nil {
		return nil, false
	}
	task, err := t.Store.CreateScheduledTask(ctx, CreateTaskInput{
		Kind: "message", ScheduleKind: "at", RunAt: spec.RunAt, TZ: spec.TZ,
		Payload: payloadJSON(a.Payload), NotifyRoute: a.Notify, Status: "pending_approval",
		NextRunAt: spec.RunAt, OriginConversationID: conversationID, OriginToolCallID: toolCallID,
		TelegramRecipients: TelegramRecipientIDs(plan.Recipients),
	})
	if err != nil {
		slog.Warn("task: scheduled message task not created", "error_type", fmt.Sprintf("%T", err))
		return nil, false
	}
	draftID, err := t.MessageDrafts.CreateScheduledMessageDraft(ctx, ScheduledMessageDraft{
		ConversationID: conversationID, ToolCallID: toolCallID, TaskID: task.ID,
		Channel: payload.Channel, Args: args, SendAt: spec.RunAt,
	})
	if err != nil {
		slog.Warn("task: scheduled message draft not created", "task", task.ID, "error_type", fmt.Sprintf("%T", err))
		if cancelErr := t.Store.CancelScheduledTask(ctx, task.ID); cancelErr != nil {
			slog.Warn("task: scheduled message task not cancelled", "task", task.ID, "error_type", fmt.Sprintf("%T", cancelErr))
		}
		return nil, false
	}
	resume, err := json.Marshal(map[string]string{"type": "message_draft", "draft_id": draftID, "task_id": task.ID})
	if err != nil {
		return nil, false
	}
	return &ErrAwaitingUserInput{
		Question: "Review this scheduled message before it is sent", Kind: KindApproval, Priority: 90,
		ToolCallID: toolCallID, ResumeContext: resume,
	}, true
}

// scheduleMessage is Execute's path for kind=message: a delivery directive still comes back
// first, and otherwise the call is refused, because a message is only ever parked.
func (t *TaskTool) scheduleMessage(ctx context.Context, a taskArgs) (ToolResult, error) {
	_, directive, err := t.resolveDelivery(ctx, &a)
	if err != nil {
		return ToolResult{}, err
	}
	if directive != nil {
		return *directive, nil
	}
	if _, _, err := t.messageSchedule(a, time.Now()); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{}, errMessageNeedsReview
}
