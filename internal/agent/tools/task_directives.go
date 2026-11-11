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

// awaitingApprovalResult is a task created pending_approval with nobody in the chat to ask: it
// waits, and the approval sweep reminds the operator on their channel. The model is told not to
// relay it, because the host already owns the question.
func awaitingApprovalResult(taskID, kind, schedule, tier, delivery string) ToolResult {
	s := fmt.Sprintf("scheduled task %s (kind=%s, %s, risk=%s, status=pending_approval). It needs the operator's approval and will not fire until they approve it; they are reminded on their channel. Do not call ask_user for it.", taskID, kind, schedule, tier)
	if delivery != "" {
		s += "\n" + delivery
	}
	return ToolResult{Preview: s, Bytes: len(s)}
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
