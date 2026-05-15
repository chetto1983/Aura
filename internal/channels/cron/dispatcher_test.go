package cronadapter_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/aura/aura/internal/channels/cron"
	"github.com/aura/aura/internal/chat"
	ourcron "github.com/aura/aura/internal/cron"
)

// --- fakes -------------------------------------------------------------------

type stubLoop struct {
	calls atomic.Int32
	err   error
}

func (s *stubLoop) Run(_ context.Context, _ *chat.Run, _ chat.InboundMessage, _ chat.EmitFn) error {
	s.calls.Add(1)
	return s.err
}

// --- helpers -----------------------------------------------------------------

func newTestHub(t *testing.T, loop chat.AgentLoop) *chat.Hub {
	t.Helper()
	hub, err := chat.New(chat.Config{Loop: loop})
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	hub.RegisterInbound(cronadapter.New())
	return hub
}

// --- dispatcher routing ------------------------------------------------------

func TestHubDispatcher_AgentJob_RoutesViaHub(t *testing.T) {
	loop := &stubLoop{}
	hub := newTestHub(t, loop)

	fallbackCalled := false
	fallback := ourcron.Dispatcher(func(_ context.Context, _ *ourcron.Task) error {
		fallbackCalled = true
		return nil
	})

	d := cronadapter.NewHubDispatcher(hub, fallback)

	task := &ourcron.Task{
		ID:      1,
		Name:    "daily-check",
		Kind:    ourcron.KindAgentJob,
		Payload: `{"goal":"check memory"}`,
	}
	if err := d(context.Background(), task); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if loop.calls.Load() != 1 {
		t.Errorf("AgentLoop.Run called %d times, want 1", loop.calls.Load())
	}
	if fallbackCalled {
		t.Error("fallback should not be called for KindAgentJob")
	}
}

func TestHubDispatcher_Reminder_RoutesFallback(t *testing.T) {
	loop := &stubLoop{}
	hub := newTestHub(t, loop)

	fallbackCalled := false
	var fallbackTask *ourcron.Task
	fallback := ourcron.Dispatcher(func(_ context.Context, task *ourcron.Task) error {
		fallbackCalled = true
		fallbackTask = task
		return nil
	})

	d := cronadapter.NewHubDispatcher(hub, fallback)

	task := &ourcron.Task{
		ID:          2,
		Name:        "morning-reminder",
		Kind:        ourcron.KindReminder,
		RecipientID: "42",
		Payload:     "Time to stretch!",
	}
	if err := d(context.Background(), task); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if loop.calls.Load() != 0 {
		t.Errorf("AgentLoop.Run should not be called for KindReminder, got %d calls", loop.calls.Load())
	}
	if !fallbackCalled {
		t.Fatal("fallback should be called for KindReminder")
	}
	if fallbackTask != task {
		t.Error("fallback received wrong task pointer")
	}
}

func TestHubDispatcher_AgentJob_PropagatesLoopError(t *testing.T) {
	loop := &stubLoop{err: errors.New("llm down")}
	hub := newTestHub(t, loop)

	fallback := ourcron.Dispatcher(func(_ context.Context, _ *ourcron.Task) error { return nil })
	d := cronadapter.NewHubDispatcher(hub, fallback)

	task := &ourcron.Task{Kind: ourcron.KindAgentJob, Name: "x", Payload: `{"goal":"g"}`}
	err := d(context.Background(), task)
	if err == nil {
		t.Fatal("expected error from loop to propagate")
	}
}

// --- InboundAdapter ----------------------------------------------------------

func TestInboundAdapter_Normalize_MapsTaskFields(t *testing.T) {
	adapter := cronadapter.New()
	if adapter.Channel() != chat.ChannelCron {
		t.Fatalf("Channel() = %q, want %q", adapter.Channel(), chat.ChannelCron)
	}

	task := &ourcron.Task{
		ID:          99,
		Name:        "market-watch",
		Kind:        ourcron.KindAgentJob,
		Payload:     `{"goal":"check indices"}`,
		RecipientID: "777",
	}
	msg, err := adapter.Normalize(context.Background(), task)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.Channel != chat.ChannelCron {
		t.Errorf("Channel = %q", msg.Channel)
	}
	if msg.PrincipalID != "777" {
		t.Errorf("PrincipalID = %q", msg.PrincipalID)
	}
	if msg.ThreadID != "cron:market-watch" {
		t.Errorf("ThreadID = %q", msg.ThreadID)
	}
	if msg.Text != task.Payload {
		t.Errorf("Text = %q", msg.Text)
	}
	if msg.Mode != chat.DeliveryModeSilent {
		t.Errorf("Mode = %q", msg.Mode)
	}
	if msg.ChannelData["task_id"] != int64(99) {
		t.Errorf("ChannelData[task_id] = %v", msg.ChannelData["task_id"])
	}
	if msg.ChannelData["kind"] != "agent_job" {
		t.Errorf("ChannelData[kind] = %v", msg.ChannelData["kind"])
	}
}

func TestInboundAdapter_Normalize_EmptyRecipientDefaultsToCron(t *testing.T) {
	adapter := cronadapter.New()
	task := &ourcron.Task{Name: "anon-job", Kind: ourcron.KindAgentJob, Payload: `{"goal":"g"}`}
	msg, err := adapter.Normalize(context.Background(), task)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.PrincipalID != "cron" {
		t.Errorf("PrincipalID = %q, want %q", msg.PrincipalID, "cron")
	}
}

func TestInboundAdapter_Normalize_WrongTypeReturnsError(t *testing.T) {
	adapter := cronadapter.New()
	_, err := adapter.Normalize(context.Background(), "not-a-task")
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

// --- compile-time checks -----------------------------------------------------

var _ chat.InboundAdapter = (*cronadapter.InboundAdapter)(nil)
