package telegram

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
)

// artifactAgentEvent builds an *agent.Event carrying an Actions.ArtifactDelta
// descriptor — the shape the translator maps to the AG-UI CUSTOM artifact event
// (the send_file Meta→ArtifactDelta lift, plan 13-02). handleTurn's fanout routes
// it to the artifact consumer → sendDocument.
func artifactAgentEvent(desc map[string]any) *agent.Event {
	return &agent.Event{Actions: agent.Actions{ArtifactDelta: desc}}
}

// TestHandleTurnSubscribesThreeBeforeRun proves handleTurn registers ALL THREE
// consumers (status + content + artifact) BEFORE Run: each recorder receives the
// lifecycle frames the producer guarantees. A Subscribe-after-Run would panic
// (fanout.go), and a never-subscribed consumer would see nothing — so all three
// seeing RUN_STARTED/RUN_FINISHED proves the Subscribe×3-before-Run wiring.
func TestHandleTurnSubscribesThreeBeforeRun(t *testing.T) {
	t.Parallel()

	stream := []*agent.Event{
		toolStartEvent("call-1", "web_search"),
		textEvent("Ciao"),
		artifactAgentEvent(map[string]any{"path": "/abs/r.xlsx", "filename": "r.xlsx", "caption": "r"}),
	}

	status := newRecordingConsumer()
	content := newRecordingConsumer()
	artifact := newRecordingConsumer()

	tg := NewChannel(Deps{
		Turn:    syntheticTurn(stream),
		Offline: true,
		consumerFactory: func(_ botSender, _ tele.Recipient) (eventConsumer, eventConsumer, eventConsumer) {
			return status, content, artifact
		},
	})

	userMsg := "hello"
	tg.handleTurn(context.Background(), nil, 909, &userMsg, false)

	for name, c := range map[string]*recordingConsumer{"status": status, "content": content, "artifact": artifact} {
		seen := c.seen()
		if !containsType(seen, events.EventTypeRunStarted) || !containsType(seen, events.EventTypeRunFinished) {
			t.Errorf("%s consumer missing the lifecycle frames (Subscribe×3-before-Run); saw %v", name, seen)
		}
	}
	// The artifact CUSTOM event must reach the artifact consumer (the 3rd subscriber
	// gets the SAME translated stream, proving the fanout fan-out, not a split).
	if !containsType(artifact.seen(), events.EventTypeCustom) {
		t.Errorf("artifact consumer missing the CUSTOM artifact event; saw %v", artifact.seen())
	}
}

// ttsDocBot records both the documents (artifact sendDocument) and the voices (the
// TTS-out sendVoice) so the Subscribe×3 + TTS-out tests assert on the live-API
// RESPONSE, never getUpdates.
type ttsDocBot struct {
	mu     sync.Mutex
	docs   []*tele.Document
	voices []*tele.Voice
}

func (b *ttsDocBot) Send(_ tele.Recipient, what any, _ ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch v := what.(type) {
	case *tele.Document:
		b.docs = append(b.docs, v)
		return &tele.Message{ID: len(b.docs), Document: &tele.Document{FileName: v.FileName}}, nil
	case *tele.Voice:
		b.voices = append(b.voices, v)
		return &tele.Message{ID: len(b.voices), Voice: &tele.Voice{Caption: v.Caption}}, nil
	}
	return &tele.Message{}, nil
}

func (b *ttsDocBot) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
	return &tele.Message{}, nil
}

func (b *ttsDocBot) sentDocs() []*tele.Document {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*tele.Document, len(b.docs))
	copy(out, b.docs)
	return out
}

func (b *ttsDocBot) sentVoices() []*tele.Voice {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*tele.Voice, len(b.voices))
	copy(out, b.voices)
	return out
}

// TestHandleTurnArtifactReachesSendDocument proves an artifact CUSTOM event flowing
// through the per-turn fanout reaches the chat as a real sendDocument (the default
// factory builds the production artifact consumer; the assertion is the recorded
// Document). This is the UX-02 "send_file → artifact event → sendDocument" leg, now
// wired into handleTurn (Subscribe×3) rather than only unit-tested in isolation.
func TestHandleTurnArtifactReachesSendDocument(t *testing.T) {
	t.Parallel()

	stream := []*agent.Event{
		textEvent("ecco il file"),
		artifactAgentEvent(map[string]any{
			"path": "/abs/results.xlsx", "filename": "results.xlsx", "caption": "results",
		}),
	}

	bot := &ttsDocBot{}
	tg := NewChannel(Deps{
		Turn:              syntheticTurn(stream),
		Offline:           true,
		StatusThrottleMS:  1,
		ContentThrottleMS: 1,
		ChatRateLimitMS:   1,
		// no consumerFactory → the production status_pane/renderer/artifact build,
		// so the artifact consumer really sends a document through the bot.
	})

	userMsg := "dammi il report"
	tg.handleTurn(context.Background(), bot, 1212, &userMsg, false)

	docs := bot.sentDocs()
	if len(docs) != 1 {
		t.Fatalf("an artifact event must reach the chat as exactly 1 sendDocument, got %d", len(docs))
	}
	if docs[0].FileName != "results.xlsx" {
		t.Errorf("sent document FileName = %q, want results.xlsx", docs[0].FileName)
	}
}

// TestHandleTurnTTSOutSpeaksWhenInboundVoice proves the echo-modality TTS-out:
// after the content render, an inbound-voice turn synthesizes the final answer to a
// voice note (ShouldSpeak true → ttsClient.Speak → sendVoice). The TTS sidecar is
// an httptest server returning canned opus.
func TestHandleTurnTTSOutSpeaksWhenInboundVoice(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(ttsHandler(t, []byte("OggS\x00opus")))
	defer srv.Close()

	stream := []*agent.Event{textEvent("Ecco la risposta finale")}

	bot := &ttsDocBot{}
	tg := NewChannel(Deps{
		Turn:              syntheticTurn(stream),
		Offline:           true,
		ContentThrottleMS: 1,
		ChatRateLimitMS:   1,
		Multimodal:        MultimodalConfig{TTSBaseURL: srv.URL, TTSVoice: "if_sara", TTSFormat: "opus"},
	})
	tg.buildDispatch() // build t.tts (Start would; the test drives handleTurn directly)

	userMsg := "(voice transcript)"
	tg.handleTurn(context.Background(), bot, 1313, &userMsg, true) // inboundWasVoice=true

	voices := bot.sentVoices()
	if len(voices) != 1 {
		t.Fatalf("an inbound-voice turn must speak the answer (1 sendVoice), got %d", len(voices))
	}
}

// TestHandleTurnNoTTSWhenTextInbound proves a text-inbound turn (no voice mode)
// does NOT synthesize a voice note — the TTS-out is gated on ShouldSpeak.
func TestHandleTurnNoTTSWhenTextInbound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(ttsHandler(t, []byte("opus")))
	defer srv.Close()

	stream := []*agent.Event{textEvent("Risposta testuale")}

	bot := &ttsDocBot{}
	tg := NewChannel(Deps{
		Turn:              syntheticTurn(stream),
		Offline:           true,
		ContentThrottleMS: 1,
		ChatRateLimitMS:   1,
		Multimodal:        MultimodalConfig{TTSBaseURL: srv.URL, TTSVoice: "if_sara", TTSFormat: "opus"},
	})
	tg.buildDispatch()

	userMsg := "ciao"
	tg.handleTurn(context.Background(), bot, 1414, &userMsg, false) // inboundWasVoice=false

	if got := len(bot.sentVoices()); got != 0 {
		t.Errorf("a text-inbound turn (no voice mode) must NOT speak, got %d voice notes", got)
	}
}

// TestHandleTurnTTSSkippedOnCancelledCtx proves a cancelled turn ctx (e.g. a
// /cancel mid-turn) suppresses the TTS-out — a cancelled turn must not still emit a
// voice note.
func TestHandleTurnTTSSkippedOnCancelledCtx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(ttsHandler(t, []byte("opus")))
	defer srv.Close()

	bot := &ttsDocBot{}
	tg := NewChannel(Deps{
		Turn:              syntheticTurn([]*agent.Event{textEvent("x")}),
		Offline:           true,
		ContentThrottleMS: 1,
		ChatRateLimitMS:   1,
		Multimodal:        MultimodalConfig{TTSBaseURL: srv.URL, TTSVoice: "if_sara", TTSFormat: "opus"},
	})
	tg.buildDispatch()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the turn drives — speakIfNeeded must no-op

	userMsg := "(voice)"
	tg.handleTurn(ctx, bot, 1515, &userMsg, true)

	if got := len(bot.sentVoices()); got != 0 {
		t.Errorf("a cancelled turn must NOT speak, got %d voice notes", got)
	}
}

// TestHandleTurnRendersPendingPauseAsKeyboard proves the RENDER half of HITL is wired:
// a turn that called ask_user (an AwaitingInput Event → the translator's
// RUN_FINISHED-interrupt) leaves a pending pause, and handleTurn renders it as an
// inline keyboard via hitl.prompt. This is the fix for the bug where the pause
// persisted but no buttons ever appeared. Drain consumers isolate the render so the
// only bot Send is the keyboard.
func TestHandleTurnRendersPendingPauseAsKeyboard(t *testing.T) {
	t.Parallel()
	pause := &agent.Event{Actions: agent.Actions{AwaitingInput: &agent.AwaitingInput{
		Question: "Confermi l'eliminazione?", Kind: "approval", ToolCallID: "tc-1",
	}}}
	rs := &fakeResume{pending: []askuser.Pending{{
		Token: "tok-1", Kind: "approval", Question: "Confermi l'eliminazione?", ToolCallID: "tc-1",
	}}}
	bot := &hitlBot{}
	tg := NewChannel(Deps{
		Turn:            syntheticTurn([]*agent.Event{pause}),
		Resume:          rs,
		Offline:         true,
		consumerFactory: recordingFactory(),
	})

	userMsg := "elimina tutti i miei dati"
	tg.handleTurn(context.Background(), bot, 42, &userMsg, false)

	var keyboards int
	for _, s := range bot.recorded() {
		if s.markup != nil && len(s.markup.InlineKeyboard) > 0 {
			keyboards++
		}
	}
	if keyboards != 1 {
		t.Fatalf("a paused turn must render exactly 1 inline keyboard (hitl.prompt), got %d of %d sends", keyboards, len(bot.recorded()))
	}
}
