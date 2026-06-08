package telegram

import (
	"context"
	"sync"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agui"
)

// docBot is a botSender double that records every *tele.Document it is asked to
// send and returns a populated *tele.Message (the live-API RESPONSE the spike
// asserts on, never getUpdates).
type docBot struct {
	mu   sync.Mutex
	docs []*tele.Document
}

func (b *docBot) Send(_ tele.Recipient, what any, _ ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d, ok := what.(*tele.Document); ok {
		b.docs = append(b.docs, d)
		// The Bot API echoes the uploaded document back in the message RESPONSE; the
		// double mirrors that so the test asserts on msg.Document, not getUpdates.
		return &tele.Message{ID: len(b.docs), Document: &tele.Document{
			FileName: d.FileName,
			Caption:  d.Caption,
			MIME:     "application/octet-stream",
		}}, nil
	}
	return &tele.Message{ID: len(b.docs)}, nil
}

func (b *docBot) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
	return &tele.Message{}, nil
}

func (b *docBot) recorded() []*tele.Document {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*tele.Document, len(b.docs))
	copy(out, b.docs)
	return out
}

// artifactCustom builds the AG-UI CUSTOM event the translator emits for an
// artifact (plan 13-02): name == agui.ArtifactEventName, value == the descriptor.
func artifactCustom(desc map[string]any) *events.CustomEvent {
	return events.NewCustomEvent(agui.ArtifactEventName, events.WithValue(desc))
}

// TestArtifactConsumeSendsDocument: an artifact CUSTOM event renders to a
// sendDocument whose RESPONSE carries a non-nil msg.Document.FileName (VALIDATION
// "send_file → artifact event → sendDocument", ground truth = the Send RESPONSE).
func TestArtifactConsumeSendsDocument(t *testing.T) {
	t.Parallel()
	bot := &docBot{}
	a := newArtifact(bot, tele.ChatID(42))

	msg, ok := a.consumeEvent(artifactCustom(map[string]any{
		"path": "/abs/results.xlsx", "filename": "results.xlsx", "caption": "results",
	}))
	if !ok {
		t.Fatal("an artifact CUSTOM event must be consumed")
	}
	if msg == nil || msg.Document == nil || msg.Document.FileName == "" {
		t.Fatalf("send RESPONSE must carry a non-nil Document.FileName, got %+v", msg)
	}
	if msg.Document.FileName != "results.xlsx" {
		t.Errorf("Document.FileName = %q, want results.xlsx", msg.Document.FileName)
	}

	docs := bot.recorded()
	if len(docs) != 1 {
		t.Fatalf("want 1 sendDocument, got %d", len(docs))
	}
	if docs[0].FileName != "results.xlsx" {
		t.Errorf("sent FileName = %q, want results.xlsx", docs[0].FileName)
	}
	if docs[0].Caption != "results" {
		t.Errorf("sent Caption = %q, want results", docs[0].Caption)
	}
}

// TestArtifactCaptionSanitizedASCII: a non-ASCII caption is folded to ASCII before
// it reaches the Bot API (Pitfall 4 / T-13-06-CaptionInject) — no byte > 0x7F.
func TestArtifactCaptionSanitizedASCII(t *testing.T) {
	t.Parallel()
	bot := &docBot{}
	a := newArtifact(bot, tele.ChatID(42))
	a.consumeEvent(artifactCustom(map[string]any{
		"path": "/abs/città.pdf", "filename": "città.pdf", "caption": "città è caffè — résumé",
	}))
	docs := bot.recorded()
	if len(docs) != 1 {
		t.Fatalf("want 1 sendDocument, got %d", len(docs))
	}
	for i, r := range docs[0].Caption {
		if r > 0x7F {
			t.Fatalf("caption[%d]=%q is non-ASCII; caption must be ASCII-sanitized: %q", i, r, docs[0].Caption)
		}
	}
}

// TestArtifactNonArtifactEventIgnored: a non-artifact event (a plain text frame or
// a CUSTOM with a different name) is NOT consumed as an artifact.
func TestArtifactNonArtifactEventIgnored(t *testing.T) {
	t.Parallel()
	bot := &docBot{}
	a := newArtifact(bot, tele.ChatID(42))

	if _, ok := a.consumeEvent(events.NewTextMessageContentEvent("m1", "hi")); ok {
		t.Error("a text event must not be consumed as an artifact")
	}
	if _, ok := a.consumeEvent(events.NewCustomEvent("other.event", events.WithValue(map[string]any{"path": "/x"}))); ok {
		t.Error("a differently-named CUSTOM event must not be consumed as an artifact")
	}
	if len(bot.recorded()) != 0 {
		t.Error("no document must be sent for a non-artifact event")
	}
}

// TestArtifactMissingPathIgnored: an artifact descriptor with no usable path is a
// no-op (no sendDocument) — never a panic.
func TestArtifactMissingPathIgnored(t *testing.T) {
	t.Parallel()
	bot := &docBot{}
	a := newArtifact(bot, tele.ChatID(42))
	if _, ok := a.consumeEvent(artifactCustom(map[string]any{"filename": "x.txt"})); ok {
		t.Error("an artifact with no path must not send a document")
	}
	if len(bot.recorded()) != 0 {
		t.Error("no document for a pathless artifact")
	}
}

// TestArtifactConsumeChannelDrainsAll proves the consume() drain over a channel of
// AG-UI events delivers every artifact event and ignores the rest, terminating
// when the producer closes the channel (goleak-clean).
func TestArtifactConsumeChannelDrainsAll(t *testing.T) {
	t.Parallel()
	bot := &docBot{}
	a := newArtifact(bot, tele.ChatID(42))

	ch := make(chan events.Event, 4)
	ch <- events.NewTextMessageContentEvent("m1", "ignore me")
	ch <- artifactCustom(map[string]any{"path": "/abs/a.pdf", "filename": "a.pdf", "caption": "one"})
	ch <- artifactCustom(map[string]any{"path": "/abs/b.csv", "filename": "b.csv", "caption": "two"})
	close(ch)

	a.consume(context.Background(), ch)
	if got := len(bot.recorded()); got != 2 {
		t.Fatalf("consume must send both artifact docs, got %d", got)
	}
}
