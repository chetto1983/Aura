package handlers

import (
	"context"
	"strings"
	"testing"
	"time"
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
	// Behavior change, fix-plan 1.2 (restores the PRD Slice 6 contract): a missed
	// reminder is re-fired ONCE at boot catch-up (D-18 collapse) instead of being
	// silently dropped — re-notifying is idempotent, dropping is data loss.
	if !m.ReschedulesOnRecovery {
		t.Fatal("a missed reminder must be re-fired once at boot catch-up (fix-plan 1.2)")
	}
}

func TestReminderLateDeliveryMarker(t *testing.T) {
	t.Parallel()
	missed := time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC)
	got, err := ReminderHandler{}.Run(context.Background(), Job{
		Payload:     []byte(`{"text":"chiama Monica"}`),
		MissedSince: missed,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(got, "chiama Monica") {
		t.Fatalf("late delivery must keep the verbatim text, got %q", got)
	}
	if !strings.Contains(got, "2026-07-22 09:30 UTC") {
		t.Fatalf("late delivery must carry the original schedule (D-18 MissedSince), got %q", got)
	}
}
