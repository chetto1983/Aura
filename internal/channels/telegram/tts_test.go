package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"
)

// voiceBot is a botSender double that records every *tele.Voice it is asked to
// send and echoes it back in the message RESPONSE (the spike ground truth — never
// getUpdates).
type voiceBot struct {
	mu     sync.Mutex
	voices []*tele.Voice
}

func (b *voiceBot) Send(_ tele.Recipient, what any, _ ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if v, ok := what.(*tele.Voice); ok {
		b.voices = append(b.voices, v)
		return &tele.Message{ID: len(b.voices), Voice: &tele.Voice{Caption: v.Caption, MIME: "audio/ogg"}}, nil
	}
	return &tele.Message{ID: len(b.voices)}, nil
}

func (b *voiceBot) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
	return &tele.Message{}, nil
}

func (b *voiceBot) recordedVoices() []*tele.Voice {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*tele.Voice, len(b.voices))
	copy(out, b.voices)
	return out
}

// ttsHandler asserts the /audio/speech contract (response_format=opus, the voice
// id) and returns canned opus bytes.
func ttsHandler(t *testing.T, opus []byte) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("TTS method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/speech") {
			t.Errorf("TTS path = %s, want .../audio/speech", r.URL.Path)
		}
		var body struct {
			Model          string `json:"model"`
			Input          string `json:"input"`
			Voice          string `json:"voice"`
			ResponseFormat string `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("TTS decode body: %v", err)
		}
		if body.ResponseFormat != "opus" {
			t.Errorf("response_format = %q, want opus", body.ResponseFormat)
		}
		if body.Voice != "if_sara" {
			t.Errorf("voice = %q, want if_sara", body.Voice)
		}
		if body.Input == "" {
			t.Error("input must carry the reply text")
		}
		w.Header().Set("Content-Type", "audio/ogg")
		_, _ = w.Write(opus)
	}
}

func ttsTestConfig(base string) MultimodalConfig {
	return MultimodalConfig{TTSBaseURL: base, TTSVoice: "if_sara", TTSFormat: "opus"}
}

// TestTTSSpeakSendsVoice proves the opus bytes go out via sendVoice and the
// RESPONSE carries a non-nil msg.Voice (VALIDATION UX-04 "tts.go opus→sendVoice").
func TestTTSSpeakSendsVoice(t *testing.T) {
	t.Parallel()
	opus := []byte("OggS\x00opus-voice-note")
	srv := httptest.NewServer(ttsHandler(t, opus))
	defer srv.Close()

	bot := &voiceBot{}
	tc := newTTSClient(ttsTestConfig(srv.URL))

	msg, err := tc.Speak(context.Background(), bot, tele.ChatID(9), "Ecco la risposta")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if msg == nil || msg.Voice == nil {
		t.Fatalf("send RESPONSE must carry a non-nil Voice, got %+v", msg)
	}
	if got := len(bot.recordedVoices()); got != 1 {
		t.Fatalf("want 1 sendVoice, got %d", got)
	}
}

// TestTTSCaptionIsASCII proves the voice caption is ASCII-sanitized (Pitfall 4 —
// a non-ASCII byte triggers a sendVoice 400).
func TestTTSCaptionIsASCII(t *testing.T) {
	t.Parallel()
	opus := []byte("opus")
	srv := httptest.NewServer(ttsHandler(t, opus))
	defer srv.Close()

	bot := &voiceBot{}
	cfg := ttsTestConfig(srv.URL)
	cfg.TTSCaption = "Però è caffè — città"
	tc := newTTSClient(cfg)

	if _, err := tc.Speak(context.Background(), bot, tele.ChatID(9), "testo"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	voices := bot.recordedVoices()
	if len(voices) != 1 {
		t.Fatalf("want 1 sendVoice, got %d", len(voices))
	}
	for i, r := range voices[0].Caption {
		if r > 0x7F {
			t.Fatalf("caption[%d]=%q is non-ASCII; sendVoice caption must be ASCII-safe: %q", i, r, voices[0].Caption)
		}
	}
}

// TestTTSSpeakSidecarError proves a sidecar 5xx surfaces an error and sends no
// voice note.
func TestTTSSpeakSidecarError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	bot := &voiceBot{}
	tc := newTTSClient(ttsTestConfig(srv.URL))

	if _, err := tc.Speak(context.Background(), bot, tele.ChatID(9), "testo"); err == nil {
		t.Fatal("a sidecar 5xx must surface an error")
	}
	if got := len(bot.recordedVoices()); got != 0 {
		t.Errorf("no voice note must be sent on a sidecar error, got %d", got)
	}
}

// TestShouldSpeak proves the trigger predicate: voice_mode preference OR the
// inbound message was a voice note (echo modality). No explicit send_voice tool.
func TestShouldSpeak(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		voiceMode bool
		fromVoice bool
		want      bool
	}{
		{"text in, no voice mode", false, false, false},
		{"text in, voice mode on", true, false, true},
		{"voice in echoes voice", false, true, true},
		{"voice in + voice mode", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldSpeak(tc.voiceMode, tc.fromVoice); got != tc.want {
				t.Errorf("ShouldSpeak(%v,%v) = %v, want %v", tc.voiceMode, tc.fromVoice, got, tc.want)
			}
		})
	}
}

// TestVoiceModeDefaultFalse proves the Slice-10-preferences stub defaults to
// false until Phase 14 ships real preferences.
func TestVoiceModeDefaultFalse(t *testing.T) {
	t.Parallel()
	if VoiceModePref("any-conv") {
		t.Error("voice_mode must default false (Slice 10 preferences not shipped)")
	}
}
