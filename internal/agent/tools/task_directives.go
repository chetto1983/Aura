package tools

import (
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/cron"
)

// The directives actionSchedule hands the model instead of a created task, and the delivery line
// it appends to one. A directive persists nothing: the model relays it and calls again.

// deliveryNote names where a task delivers: the recipient an external route reaches, and
// "(your default channel)" when the route came from the operator's default rather than the
// request. "" for an explicit route that addresses no one.
func deliveryNote(route, recipient string, fromDefault bool) string {
	switch {
	case recipient == "" && !fromDefault:
		return ""
	case recipient == "":
		return fmt.Sprintf("Delivers via %s (your default channel).", route)
	case fromDefault:
		return fmt.Sprintf("Delivers via %s to %s (your default channel).", route, recipient)
	default:
		return fmt.Sprintf("Delivers via %s to %s.", route, recipient)
	}
}

func taskNotifyChoiceRequiredResult() ToolResult {
	payload := struct {
		Status   string              `json:"status"`
		Question string              `json:"question"`
		Options  []map[string]string `json:"options"`
		Message  string              `json:"message"`
	}{
		Status:   "delivery_choice_required",
		Question: "Where should the scheduled task deliver its result?",
		Options: []map[string]string{
			{"label": "No notification", "value": string(cron.RouteNone)},
			{"label": "This conversation", "value": string(cron.RouteStdout)},
			{"label": "Telegram", "value": string(cron.RouteTelegram)},
			{"label": "WhatsApp", "value": string(cron.RouteWhatsApp)},
			{"label": "Email", "value": string(cron.RouteEmail)},
		},
		Message: "No task was created. Call ask_user with kind=\"choice\", the question and options above. After the operator answers, call task action=\"schedule\" again with notify set to the selected option value, and tell them this choice can be saved as their default channel in the cockpit's Profile panel.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"status":"delivery_choice_required"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

// scheduledApprovalRequiredResult builds the pending_approval directive (the
// shell_exec/gateway approval-required precedent): a scheduled task never fires until
// the operator approves it on the channel it was scheduled from. The model MUST relay
// this as an ask_user approval pause carrying the resume_context the scheduled-task
// resume hook decodes; on accept the hook flips the task to active. The task_id +
// authenticated origin conversation are the authorization (the hook owner-scopes the
// UPDATE), so no question-match challenge is needed here.
func scheduledApprovalRequiredResult(taskID, kind, summary, tier, delivery string, immediate bool) ToolResult {
	// The human-facing question masks the UUID to its first 8 chars (kind + summary already
	// identify the task) so the Sì/No prompt reads friendly instead of dumping a 36-char id;
	// the full task_id stays in the machine-facing resume_context below (the resume hook keys
	// on it). Bounded id + kind only — never the payload.
	question := fmt.Sprintf("Approve scheduled %s task %s (%s, risk=%s)? It will not fire until you approve.", kind, shortScheduledTaskID(taskID), summary, tier)
	payload := map[string]string{
		"status":   "pending_approval",
		"task_id":  taskID,
		"question": question,
		"message": "This scheduled task requires operator approval before it can fire. " +
			"Call ask_user with kind=\"approval\", question exactly equal to the question field, and " +
			"resume_context={\"type\":\"scheduled_task_approval\",\"task_id\":\"" + taskID + "\"}. " +
			"The task activates only after the operator accepts on their channel.",
	}
	if immediate {
		payload["immediate_alert"] = "true"
	}
	if delivery != "" {
		payload["delivery"] = delivery
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"status":"pending_approval","task_id":"` + taskID + `"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

// shortScheduledTaskID renders the first 8 chars of a task id for the operator-facing approval
// prompt — a bounded, friendly handle instead of the raw 36-char UUID (the full id stays in the
// machine-facing resume_context). Parity with the sweep push's shortTelegramTaskID.
func shortScheduledTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
