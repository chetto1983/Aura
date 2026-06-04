package handlers

import (
	"context"
	"testing"
)

func TestReminderVerbatim(t *testing.T) {
	t.Parallel()
	got, err := ReminderHandler{}.Run(context.Background(), Job{
		Payload: []byte(`{"text":"buy milk and call the dentist"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "buy milk and call the dentist" {
		t.Fatalf("reminder must deliver verbatim text, got %q", got)
	}
}

func TestReminderEmptyPayloadStillCompletes(t *testing.T) {
	t.Parallel()
	got, err := ReminderHandler{}.Run(context.Background(), Job{Payload: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got == "" {
		t.Fatal("an empty reminder must still produce a non-empty audit summary (D-21)")
	}
}

func TestReminderMalformedPayloadStillCompletes(t *testing.T) {
	t.Parallel()
	got, err := ReminderHandler{}.Run(context.Background(), Job{Payload: []byte(`not json`)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got == "" {
		t.Fatal("a malformed reminder payload must not fail the run")
	}
}

func TestReminderMeta(t *testing.T) {
	t.Parallel()
	m := ReminderHandler{}.Meta()
	if m.Kind != KindReminder {
		t.Fatalf("kind = %q, want reminder", m.Kind)
	}
	if m.ReschedulesOnRecovery {
		t.Fatal("a reminder should not reschedule on recovery")
	}
}
