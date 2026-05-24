package webadapter

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/chat"
)

func TestInboundNormalizeScopesThreadAndCopiesAttachments(t *testing.T) {
	createdAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	attachments := []chat.AttachmentRef{{SourceID: "src_123", Filename: "note.pdf"}}

	msg, err := New().Normalize(context.Background(), InboundRequest{
		ID:          " web-1 ",
		UserID:      " alice ",
		ThreadID:    " planning ",
		Message:     "  ciao  ",
		Attachments: attachments,
		CreatedAt:   createdAt,
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	attachments[0].SourceID = "mutated"

	if msg.ID != "web-1" {
		t.Fatalf("ID = %q", msg.ID)
	}
	if msg.Channel != chat.ChannelWeb || msg.Mode != chat.DeliveryModeDeferred {
		t.Fatalf("channel/mode = %s/%s", msg.Channel, msg.Mode)
	}
	if msg.PrincipalID != "alice" || msg.ThreadID != "web:alice:planning" {
		t.Fatalf("principal/thread = %q/%q", msg.PrincipalID, msg.ThreadID)
	}
	if msg.Text != "ciao" {
		t.Fatalf("Text = %q", msg.Text)
	}
	if msg.CreatedAt != createdAt {
		t.Fatalf("CreatedAt = %s", msg.CreatedAt)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].SourceID != "src_123" {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
}

func TestInboundNormalizeDefaultsAnonymousThread(t *testing.T) {
	msg, err := New().Normalize(context.Background(), &InboundRequest{
		ThreadID: "default",
		Message:  "hello",
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.PrincipalID != "anonymous" || msg.ThreadID != "web:anonymous:default" {
		t.Fatalf("principal/thread = %q/%q", msg.PrincipalID, msg.ThreadID)
	}
	if msg.CreatedAt.IsZero() {
		t.Fatal("CreatedAt was not defaulted")
	}
}

func TestInboundNormalizePreservesStreamingMode(t *testing.T) {
	msg, err := New().Normalize(context.Background(), InboundRequest{
		UserID:   "alice",
		ThreadID: "live",
		Message:  "stream this",
		Mode:     chat.DeliveryModeStreaming,
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.Mode != chat.DeliveryModeStreaming {
		t.Fatalf("Mode = %q, want streaming", msg.Mode)
	}
	if msg.ThreadID != "web:alice:live" {
		t.Fatalf("ThreadID = %q", msg.ThreadID)
	}
}

func TestInboundNormalizeRejectsWrongRawPayload(t *testing.T) {
	if _, err := New().Normalize(context.Background(), "bad"); err == nil {
		t.Fatal("expected error for wrong raw payload")
	}
}
