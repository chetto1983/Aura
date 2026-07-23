package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// reminderMaxDuration bounds a reminder run — it is a verbatim text delivery, so the
// budget is tiny (the dispatcher's notify retry owns the real delivery latency).
const reminderMaxDuration = 30 * time.Second

// ReminderHandler is the simplest TaskKind (PRD ReminderAgent): it delivers the
// verbatim payload text as the run summary. No LLM, no tools — the dispatcher's
// Notifier carries the summary to the user's route (D-19).
type ReminderHandler struct{}

// reminderPayload is the reminder task payload shape: {"text": "..."}.
type reminderPayload struct {
	Text string `json:"text"`
}

// Meta declares the reminder contract: a missed reminder DOES reschedule on
// recovery (PRD Slice 6: re-notifying is idempotent, a silently dropped reminder is
// not — parity with agent_job/backup, fix-plan 1.2). D-18 collapses the missed
// windows upstream, so boot catch-up delivers exactly ONE late fire carrying
// MissedSince; a retired one-shot fires at most once.
func (ReminderHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindReminder, MaxDuration: reminderMaxDuration, ReschedulesOnRecovery: true}
}

// Run returns the verbatim payload text as the summary. An empty or malformed
// payload is not an error — a reminder with no text degrades to a generic line so the
// run still completes and the audit row is written (D-21 never silent-fails). A boot
// catch-up run (MissedSince non-zero, D-18) appends a late-delivery marker so the
// notification is honest about firing after its scheduled time.
func (ReminderHandler) Run(_ context.Context, job Job) (string, error) {
	text := reminderText(job.Payload)
	if text == "" {
		text = "reminder fired (no text in payload)"
	}
	if !job.MissedSince.IsZero() {
		return fmt.Sprintf("%s (late delivery — originally scheduled %s)",
			text, job.MissedSince.UTC().Format("2006-01-02 15:04 MST")), nil
	}
	return text, nil
}

// reminderText extracts the verbatim reminder text from the payload JSON, tolerating
// an absent/blank text field.
func reminderText(payload []byte) string {
	var p reminderPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.Text)
}
