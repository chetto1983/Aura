package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

func TestNoMountedWhatsAppRefusesTheScheduledDraft(t *testing.T) {
	t.Parallel()
	drafts := scheduledMessageDrafts{reg: tools.NewRegistry()}
	_, err := drafts.CreateScheduledMessageDraft(context.Background(), tools.ScheduledMessageDraft{
		ConversationID: "conv", ToolCallID: "call", TaskID: "task", Channel: "whatsapp",
		Args: json.RawMessage(`{"recipient":"10000000000","message":"hi"}`), SendAt: time.Now().Add(time.Hour),
	})
	if err == nil || !strings.Contains(err.Error(), "WhatsApp is not connected") {
		t.Fatalf("err = %v, want WhatsApp is not connected", err)
	}
}

// A scheduled email rides the PIM's send_email tool; the retired multiplexed calendar tool is
// not a mount it can be drafted against. A nil store refuses the draft before any query, which
// shows the call got past the route and the target.
func TestAScheduledEmailRidesTheSendEmailTool(t *testing.T) {
	t.Parallel()
	in := tools.ScheduledMessageDraft{
		ConversationID: "conv", ToolCallID: "call", TaskID: "task", Channel: "email",
		Args: json.RawMessage(`{"to":["someone@example.com"],"subject":"Hi","body":"Text"}`), SendAt: time.Now().Add(time.Hour),
	}
	multiplexed := tools.NewRegistry()
	multiplexed.Register(&recipeTool{name: "calendar__calendar", recipe: "recipe:calendar", tool: "calendar"})
	if _, err := (scheduledMessageDrafts{reg: multiplexed}).CreateScheduledMessageDraft(context.Background(), in); err == nil ||
		!strings.Contains(err.Error(), "email is not connected") {
		t.Fatalf("err = %v, want email is not connected", err)
	}
	split := tools.NewRegistry()
	split.Register(&recipeTool{name: "calendar__send_email", recipe: "recipe:calendar", tool: "send_email"})
	if _, err := (scheduledMessageDrafts{reg: split}).CreateScheduledMessageDraft(context.Background(), in); !errors.Is(err, messagedrafts.ErrInvalidArgs) {
		t.Fatalf("err = %v, want the store's own refusal past the route", err)
	}
}

// The agent parks only a registered tool that is a HostReviewer, and only the live tool has
// somewhere to put a draft.
func TestTheRegisteredTaskToolIsHostReviewed(t *testing.T) {
	t.Parallel()
	reg := tools.NewRegistry()
	live := newTaskTool(newCronTaskStore(nil, nil), nil, "", reg, nil)
	if live.MessageDrafts == nil {
		t.Fatal("the live task tool has no drafts seam")
	}
	reg.Register(live)
	registered, ok := reg.Get("task")
	if !ok {
		t.Fatal("task is not registered")
	}
	if _, ok := registered.(tools.HostReviewer); !ok {
		t.Fatal("the registered task tool is not a HostReviewer")
	}
	if got := newTaskTool(nil, nil, "", nil, nil); got.MessageDrafts != nil {
		t.Fatal("the pool-free task tool carries a drafts seam")
	}
}
