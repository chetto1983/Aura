package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/cron"
)

// fakeDestinations is the scheduler notifier's schedule-time answer.
type fakeDestinations struct {
	recipient string
	err       error
	asked     []cron.NotifyRoute
}

func (f *fakeDestinations) Destination(_ context.Context, route cron.NotifyRoute) (string, error) {
	f.asked = append(f.asked, route)
	return f.recipient, f.err
}

// TestTaskScheduleRefusesARouteThatCannotDeliver pins the measured defect (2026-10-05): a
// notify=whatsapp reminder with no recipient was persisted, fired, and reached nobody.
// The refusal carries the reason so the model can tell the operator, and nothing is saved.
func TestTaskScheduleRefusesARouteThatCannotDeliver(t *testing.T) {
	store := &fakeTaskStore{}
	dest := &fakeDestinations{err: errors.New("no recipient for whatsapp: no WhatsApp account is linked for this identity")}
	tool := &TaskTool{Store: store, Destinations: dest}

	args := json.RawMessage(`{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"drink water"},"notify":"whatsapp"}`)
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("a route that cannot deliver was accepted")
	}
	for _, want := range []string{"notify=whatsapp cannot deliver", "no WhatsApp account is linked", "No task was created"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if store.created.Kind != "" {
		t.Fatalf("a refused route persisted a task: %+v", store.created)
	}
	if len(dest.asked) != 1 || dest.asked[0] != cron.RouteWhatsApp {
		t.Fatalf("destination asked for %v, want [whatsapp]", dest.asked)
	}
}

// TestTaskScheduleNamesTheRecipient: the model read only "scheduled task …" and told the
// operator the reminder would arrive "on your work number". The result names whom it reaches.
func TestTaskScheduleNamesTheRecipient(t *testing.T) {
	dest := &fakeDestinations{recipient: "393331112222"}
	tool := &TaskTool{Store: &fakeTaskStore{}, Destinations: dest}

	args := json.RawMessage(`{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"drink water"},"notify":"whatsapp"}`)
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if !strings.HasSuffix(res.Preview, "\nDelivers via whatsapp to 393331112222.") {
		t.Fatalf("preview %q does not name the recipient", res.Preview)
	}
}

func TestTaskScheduleRouteWithoutRecipientAddsNoDeliveryLine(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{}, Destinations: &fakeDestinations{}}

	args := json.RawMessage(`{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"drink water"},"notify":"stdout"}`)
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if strings.Contains(res.Preview, "Delivers via") {
		t.Fatalf("a route that addresses no one named a recipient: %q", res.Preview)
	}
}

// TestTaskScheduleApprovalNamesTheRecipient: an agent_job always waits for approval, and
// the operator approving it should see where its outcome will go.
func TestTaskScheduleApprovalNamesTheRecipient(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{}, Destinations: &fakeDestinations{recipient: "393331112222"}}

	args := json.RawMessage(`{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"agent_job","payload":{"goal":"summarize the news"},"notify":"whatsapp"}`)
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(res.Preview), &payload); err != nil {
		t.Fatalf("approval preview is not JSON: %v", err)
	}
	if payload["status"] != "pending_approval" || payload["delivery"] != "Delivers via whatsapp to 393331112222." {
		t.Fatalf("approval payload = %v, want pending_approval naming the recipient", payload)
	}
}
