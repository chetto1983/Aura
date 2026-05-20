package telegramadapter

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aura/aura/internal/audio"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/llm/pockettts"
	tele "gopkg.in/telebot.v4"
)

// mockVoiceStore is an in-memory audio.Store for tests.
type mockVoiceStore struct {
	mode audio.Mode
	err  error
}

func (m *mockVoiceStore) Get(_ context.Context, _ string) (audio.Mode, error) {
	return m.mode, m.err
}
func (m *mockVoiceStore) Set(_ context.Context, _ string, _ audio.Mode) error { return nil }

// newTTSServer returns a test TTS server that responds with minimal WAV bytes and counts calls.
func newTTSServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	wav := make([]byte, 1100)
	copy(wav, []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E'})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	return srv, &calls
}

// newFailingTTSServer returns a TTS server that always returns 500.
func newFailingTTSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream busy", http.StatusInternalServerError)
	}))
}

// buildOutboundContext creates a minimal tele.Context wired to botSrv for tests.
func buildOutboundContext(t *testing.T, botSrv *httptest.Server, chatID int64) tele.Context {
	t.Helper()
	tb, err := tele.NewBot(tele.Settings{URL: botSrv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatalf("tele.NewBot: %v", err)
	}
	return tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: chatID},
		Chat:   &tele.Chat{ID: chatID},
		Text:   "test",
	}})
}

// longContent is enough to cross streamingMinThreshold (30 chars) so streaming fires.
const longContent = "Ciao! Sono Aura, sempre pronta ad aiutarti con qualsiasi cosa tu abbia bisogno."

func streamTokens(content string) chan llm.Token {
	ch := make(chan llm.Token, 2)
	ch <- llm.Token{Content: content}
	ch <- llm.Token{Done: true}
	close(ch)
	return ch
}

// TestOutbound_VoiceMode_Off_NoSynth verifies that mode=off adds only the
// policy.Get call overhead — no TTS synthesis, no sendVoice.
func TestOutbound_VoiceMode_Off_NoSynth(t *testing.T) {
	ttsSrv, ttsCalls := newTTSServer(t)
	defer ttsSrv.Close()

	botCalls, botSrv := newFakeBotServer(t)
	defer botSrv.Close()

	ctx := buildOutboundContext(t, botSrv, 456)

	out := NewOutbound(slog.New(slog.NewTextHandler(io.Discard, nil)))
	out.SetTTS(pockettts.New(ttsSrv.URL, 5*time.Second, nil))
	out.SetVoicePolicy(&mockVoiceStore{mode: audio.ModeOff})

	_, _, err := out.ConsumeStream(ctx, streamTokens(longContent), "456", nil, nil)
	if err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}

	if n := ttsCalls.Load(); n != 0 {
		t.Errorf("TTS server called %d times, want 0 (mode=off should skip synthesis)", n)
	}
	voiceCalls := filterCalls(botCalls.snapshot(), "sendVoice")
	if len(voiceCalls) != 0 {
		t.Errorf("sendVoice called %d times, want 0", len(voiceCalls))
	}
	// Text should still be sent.
	textCalls := filterCalls(botCalls.snapshot(), "sendMessage")
	if len(textCalls) == 0 {
		t.Errorf("no sendMessage calls: text should be sent in off mode")
	}
}

// TestOutbound_VoiceMode_All_SendsBoth verifies that mode=all sends text AND voice.
func TestOutbound_VoiceMode_All_SendsBoth(t *testing.T) {
	ttsSrv, ttsCalls := newTTSServer(t)
	defer ttsSrv.Close()

	botCalls, botSrv := newFakeBotServer(t)
	defer botSrv.Close()

	ctx := buildOutboundContext(t, botSrv, 456)

	out := NewOutbound(slog.New(slog.NewTextHandler(io.Discard, nil)))
	out.SetTTS(pockettts.New(ttsSrv.URL, 5*time.Second, nil))
	out.SetVoicePolicy(&mockVoiceStore{mode: audio.ModeAll})

	_, _, err := out.ConsumeStream(ctx, streamTokens(longContent), "456", nil, nil)
	if err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}

	if n := ttsCalls.Load(); n != 1 {
		t.Errorf("TTS server called %d times, want 1", n)
	}
	voiceCalls := filterCalls(botCalls.snapshot(), "sendVoice")
	if len(voiceCalls) != 1 {
		t.Errorf("sendVoice calls = %d, want 1", len(voiceCalls))
	}
	// Text must also be sent (all mode is additive).
	snap := botCalls.snapshot()
	textSend := filterCalls(snap, "sendMessage")
	textEdit := filterCalls(snap, "editMessageText")
	if len(textSend)+len(textEdit) == 0 {
		t.Errorf("no text-send or edit calls: mode=all must also deliver text")
	}
}

// TestOutbound_VoiceMode_VoiceOnly_SuppressesText verifies that mode=voice_only
// on successful synthesis sends Voice but suppresses the final text edit.
func TestOutbound_VoiceMode_VoiceOnly_SuppressesText(t *testing.T) {
	ttsSrv, ttsCalls := newTTSServer(t)
	defer ttsSrv.Close()

	botCalls, botSrv := newFakeBotServer(t)
	defer botSrv.Close()

	ctx := buildOutboundContext(t, botSrv, 456)

	out := NewOutbound(slog.New(slog.NewTextHandler(io.Discard, nil)))
	out.SetTTS(pockettts.New(ttsSrv.URL, 5*time.Second, nil))
	out.SetVoicePolicy(&mockVoiceStore{mode: audio.ModeVoiceOnly})

	resp, _, err := out.ConsumeStream(ctx, streamTokens(longContent), "456", nil, nil)
	if err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}
	if resp.Content == "" {
		t.Fatalf("resp.Content empty: streaming should have captured the text internally")
	}

	if n := ttsCalls.Load(); n != 1 {
		t.Errorf("TTS server called %d times, want 1", n)
	}
	voiceCalls := filterCalls(botCalls.snapshot(), "sendVoice")
	if len(voiceCalls) != 1 {
		t.Errorf("sendVoice calls = %d, want 1", len(voiceCalls))
	}
	// The FINAL text edit (editMessageText after tok.Done) must NOT have happened.
	// The initial sendMessage from streaming IS allowed (placeholder flush).
	editCalls := filterCalls(botCalls.snapshot(), "editMessageText")
	if len(editCalls) != 0 {
		t.Errorf("editMessageText called %d times, want 0 (final text edit suppressed by voice_only)", len(editCalls))
	}
}

// TestOutbound_VoiceMode_VoiceOnly_FailureFallsBackToText verifies that when
// synthesis fails in voice_only mode, text is sent as degraded-mode fallback.
func TestOutbound_VoiceMode_VoiceOnly_FailureFallsBackToText(t *testing.T) {
	ttsSrv := newFailingTTSServer(t)
	defer ttsSrv.Close()

	botCalls, botSrv := newFakeBotServer(t)
	defer botSrv.Close()

	ctx := buildOutboundContext(t, botSrv, 456)

	out := NewOutbound(slog.New(slog.NewTextHandler(io.Discard, nil)))
	out.SetTTS(pockettts.New(ttsSrv.URL, 5*time.Second, nil))
	out.SetVoicePolicy(&mockVoiceStore{mode: audio.ModeVoiceOnly})

	_, _, err := out.ConsumeStream(ctx, streamTokens(longContent), "456", nil, nil)
	if err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}

	// No voice should be sent (synthesis failed).
	voiceCalls := filterCalls(botCalls.snapshot(), "sendVoice")
	if len(voiceCalls) != 0 {
		t.Errorf("sendVoice called %d times, want 0 (synthesis failed)", len(voiceCalls))
	}
	// Text MUST be sent as fallback (user must always get a reply).
	snap := botCalls.snapshot()
	textSend := filterCalls(snap, "sendMessage")
	textEdit := filterCalls(snap, "editMessageText")
	if len(textSend)+len(textEdit) == 0 {
		t.Errorf("no text-send or edit calls: voice_only fallback must send text when synthesis fails")
	}
}

// TestOutbound_TTSNotConfigured_OffByDefault verifies that with no TTS client
// configured, the outbound behaves identically to the pre-A07 baseline.
func TestOutbound_TTSNotConfigured_OffByDefault(t *testing.T) {
	botCalls, botSrv := newFakeBotServer(t)
	defer botSrv.Close()

	ctx := buildOutboundContext(t, botSrv, 456)

	// No SetTTS / SetVoicePolicy — TTS is nil.
	out := NewOutbound(slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, _, err := out.ConsumeStream(ctx, streamTokens(longContent), "456", nil, nil)
	if err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}

	voiceCalls := filterCalls(botCalls.snapshot(), "sendVoice")
	if len(voiceCalls) != 0 {
		t.Errorf("sendVoice called %d times, want 0 (TTS not configured)", len(voiceCalls))
	}
	// Text should be delivered normally.
	snap := botCalls.snapshot()
	textSend := filterCalls(snap, "sendMessage")
	textEdit := filterCalls(snap, "editMessageText")
	if len(textSend)+len(textEdit) == 0 {
		t.Errorf("no text delivery: text must be sent when TTS is not configured")
	}
}
