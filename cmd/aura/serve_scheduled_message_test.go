package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
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
