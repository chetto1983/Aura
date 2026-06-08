package telegram

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tele "gopkg.in/telebot.v4"
)

// fileBot is a botFiler/botReactor double: it returns canned OGG bytes for a
// File download and records every React it is asked to apply (the hard-fail UX
// asserts on the recorded 😵). It satisfies neither Send nor Edit — the STT path
// never sends through it (it produces a transcript string for the text path).
type fileBot struct {
	ogg []byte

	mu        sync.Mutex
	reactions []string
}

func (b *fileBot) File(_ *tele.File) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.ogg))), nil
}

func (b *fileBot) React(_ tele.Recipient, _ tele.Editable, r tele.Reactions) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, re := range r.Reactions {
		b.reactions = append(b.reactions, re.Emoji)
	}
	return nil
}

func (b *fileBot) recordedReactions() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.reactions))
	copy(out, b.reactions)
	return out
}

// sttHandler builds an httptest handler that asserts the multipart contract (the
// OGG/Opus bytes arrive in the `file` field, no ffmpeg pre-step) and returns the
// canned transcript with the given status.
func sttHandler(t *testing.T, wantOGG []byte, transcript string, status int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("STT method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("STT path = %s, want .../audio/transcriptions", r.URL.Path)
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("STT Content-Type = %q, want multipart/*", r.Header.Get("Content-Type"))
		}
		if status >= 400 {
			http.Error(w, "boom", status)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		var gotFile []byte
		var gotLang string
		for {
			part, perr := mr.NextPart()
			if errors.Is(perr, io.EOF) {
				break
			}
			if perr != nil {
				t.Fatalf("STT multipart read: %v", perr)
			}
			switch part.FormName() {
			case "file":
				gotFile, _ = io.ReadAll(part)
			case "language":
				b, _ := io.ReadAll(part)
				gotLang = string(b)
			}
		}
		if string(gotFile) != string(wantOGG) {
			t.Errorf("STT file field = %q, want the OGG bytes %q (no ffmpeg transcode)", gotFile, wantOGG)
		}
		// Regression guard (spike-027): the language MUST be pinned so whisper does
		// not auto-detect a short IT clip as the wrong language (observed 'ja' live).
		if gotLang != "it" {
			t.Errorf("STT language field = %q, want %q (pinned, not auto-detect)", gotLang, "it")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"`+transcript+`"}`)
	}
}

func sttTestConfig(base string) MultimodalConfig {
	return MultimodalConfig{STTBaseURL: base, STTModel: "large-v3-turbo", STTLanguage: "it"}
}

// TestVoiceTranscribeHappyPath proves the OGG/Opus bytes are POSTed directly
// (multipart, no ffmpeg) and the transcript text is returned for the text path.
func TestVoiceTranscribeHappyPath(t *testing.T) {
	t.Parallel()
	ogg := []byte("OggS\x00fake-opus-bytes")
	srv := httptest.NewServer(sttHandler(t, ogg, "ciao Aura", http.StatusOK))
	defer srv.Close()

	bot := &fileBot{ogg: ogg}
	vc := newVoiceClient(sttTestConfig(srv.URL))

	got, err := vc.Transcribe(context.Background(), bot, tele.ChatID(7), &tele.Voice{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "ciao Aura" {
		t.Errorf("transcript = %q, want %q", got, "ciao Aura")
	}
}

// TestVoiceTranscribeRetriesThenHardFail proves the 2-retry exp backoff (3 total
// attempts) and the hard-fail UX (😵 reaction) when the sidecar keeps 5xx'ing.
func TestVoiceTranscribeRetriesThenHardFail(t *testing.T) {
	t.Parallel()
	ogg := []byte("OggS\x00opus")
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	bot := &fileBot{ogg: ogg}
	cfg := sttTestConfig(srv.URL)
	cfg.RetryBackoff = []int{1, 1} // milliseconds: keep the test fast
	vc := newVoiceClient(cfg)

	_, err := vc.Transcribe(context.Background(), bot, tele.ChatID(7), &tele.Voice{})
	if err == nil {
		t.Fatal("a persistent 5xx must hard-fail with an error")
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3 (1 + 2 retries)", got)
	}

	if err := vc.HardFail(bot, tele.ChatID(7), &tele.Message{ID: 1}); err != nil {
		t.Fatalf("HardFail: %v", err)
	}
	react := bot.recordedReactions()
	if len(react) != 1 || react[0] != hardFailReaction {
		t.Errorf("hard-fail reaction = %v, want [%q]", react, hardFailReaction)
	}
}

// TestVoiceHardFailMessageIsItalian asserts the user-facing hard-fail copy is the
// locked IT string.
func TestVoiceHardFailMessageIsItalian(t *testing.T) {
	t.Parallel()
	if !strings.Contains(transcribeFailMessage, "Trascrizione non disponibile") {
		t.Errorf("hard-fail message = %q, want the IT copy", transcribeFailMessage)
	}
}
