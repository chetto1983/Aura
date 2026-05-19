package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm/whisper"
	source "github.com/aura/aura/internal/storage/sources/store"
	tele "gopkg.in/telebot.v4"
)

// fakeVoiceBytes is a minimal synthetic payload used as the voice memo body
// returned by the test API server. The handler doesn't decode audio bytes.
var fakeVoiceBytes = []byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00aura voice test")

func TestVoiceHandlerUnauthorizedUserRejected(t *testing.T) {
	sources := newDocumentTestSourceStore(t)
	h := newVoiceHandler(voiceHandlerConfig{
		Sources: sources,
		Allowlist: func(string) bool {
			return false
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)

	ctx := tele.NewContext(nil, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 999, Username: "guest"},
		Chat:   &tele.Chat{ID: 999},
		Voice: &tele.Voice{
			File:     tele.File{FileID: "voice-1", FileSize: int64(len(fakeVoiceBytes))},
			Duration: 3,
		},
	}})

	if err := h.onVoiceMessage(ctx); err != nil {
		t.Fatalf("onVoiceMessage() error = %v, want nil", err)
	}
	got, err := sources.List(source.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("sources = %d, want none for unauthorized user", len(got))
	}
	// handler must leave work counter intact (no leak)
	if !h.beginWork() {
		t.Fatal("beginWork returned false after unauthorized rejection")
	}
	h.finishWork()
}

func TestVoiceHandlerAuthorizedVoiceStoresKindAudio(t *testing.T) {
	var calls []telegramAPICall
	srv := newVoiceAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	sources := newDocumentTestSourceStore(t)
	h := newVoiceHandler(voiceHandlerConfig{
		Bot:     tb,
		Sources: sources,
		Allowlist: func(string) bool {
			return true
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)

	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Voice: &tele.Voice{
			File:     tele.File{FileID: "voice-1", FileSize: int64(len(fakeVoiceBytes))},
			Duration: 5,
		},
	}})

	if err := h.onVoiceMessage(ctx); err != nil {
		t.Fatalf("onVoiceMessage() error = %v, want nil", err)
	}

	var stored []*source.Source
	waitUntil(t, 2*time.Second, func() bool {
		var err error
		stored, err = sources.List(source.ListFilter{})
		return err == nil && len(stored) == 1
	})
	h.Stop()

	s := stored[0]

	// Kind and MimeType
	if s.Kind != source.KindAudio {
		t.Fatalf("source.Kind = %q, want %q", s.Kind, source.KindAudio)
	}
	if s.MimeType != "audio/ogg" {
		t.Fatalf("source.MimeType = %q, want audio/ogg", s.MimeType)
	}

	// Filename must match voice_<digits>.ogg
	filenameRe := regexp.MustCompile(`^voice_\d+\.ogg$`)
	if !filenameRe.MatchString(s.Filename) {
		t.Fatalf("source.Filename = %q, want voice_<unix>.ogg shape", s.Filename)
	}

	// Status must be stored (A01 does not call Update — Put sets StatusStored)
	if s.Status != source.StatusStored {
		t.Fatalf("source.Status = %q, want %q", s.Status, source.StatusStored)
	}

	// Initial progress: sendMessage with "saving voice memo"
	sendCalls := filterCalls(calls, "sendMessage")
	initialFound := false
	for _, c := range sendCalls {
		if text, _ := c.Body["text"].(string); strings.Contains(text, "saving voice memo") {
			initialFound = true
		}
	}
	if !initialFound {
		t.Fatalf("no sendMessage with initial 'saving voice memo' text found in calls: %v", calls)
	}

	// Final progress: editMessageText containing src_id + "✅ Stored"
	editCalls := filterCalls(calls, "editMessageText")
	if len(editCalls) < 1 {
		t.Fatalf("editMessageText calls = %d, want ≥ 1 (final progress edit)", len(editCalls))
	}
	lastEdit := editCalls[len(editCalls)-1]
	finalText, _ := lastEdit.Body["text"].(string)
	if !strings.Contains(finalText, s.ID) {
		t.Fatalf("final progress edit %q does not contain src ID %q", finalText, s.ID)
	}
	if !strings.Contains(finalText, "✅ Stored") {
		t.Fatalf("final progress edit %q does not contain '✅ Stored'", finalText)
	}
}

func TestVoiceHandlerTranscription(t *testing.T) {
	const expectedText = "ciao mondo prova"

	// Mock whisper-server: accepts POST /inference, returns canned transcript.
	whisperSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"text":%q}`, expectedText)
	}))
	defer whisperSrv.Close()

	var calls []telegramAPICall
	srv := newVoiceAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	sources := newDocumentTestSourceStore(t)
	wc := whisper.New(whisperSrv.URL, 10*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Tests stay codec-free: bypass the ffmpeg transcode step (no ffmpeg on the
	// dev / CI Windows runner) by treating the bytes as already-WAV.
	wc.Transcode = func(_ context.Context, b []byte, _ string) ([]byte, string, error) {
		return b, "audio/wav", nil
	}
	h := newVoiceHandler(voiceHandlerConfig{
		Bot:             tb,
		Sources:         sources,
		Whisper:         wc,
		WhisperLanguage: "it",
		Allowlist:       func(string) bool { return true },
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)

	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Voice: &tele.Voice{
			File:     tele.File{FileID: "voice-1", FileSize: int64(len(fakeVoiceBytes))},
			Duration: 5,
		},
	}})

	if err := h.onVoiceMessage(ctx); err != nil {
		t.Fatalf("onVoiceMessage() error = %v", err)
	}

	// Wait for transcription to complete (StatusTranscribeComplete).
	var stored []*source.Source
	waitUntil(t, 5*time.Second, func() bool {
		var err error
		stored, err = sources.List(source.ListFilter{Status: source.StatusTranscribeComplete})
		return err == nil && len(stored) == 1
	})
	h.Stop()

	s := stored[0]

	// Transcript metadata must be populated.
	if s.Transcript == nil {
		t.Fatal("source.Transcript is nil after transcription")
	}
	if s.Transcript.Text != expectedText {
		t.Fatalf("Transcript.Text = %q, want %q", s.Transcript.Text, expectedText)
	}
	if s.Transcript.Language != "it" {
		t.Fatalf("Transcript.Language = %q, want it", s.Transcript.Language)
	}
	if s.Transcript.DurationS != 5.0 {
		t.Fatalf("Transcript.DurationS = %.1f, want 5.0 (from voice.Duration)", s.Transcript.DurationS)
	}

	// OriginalDeletedAt must be set after audio purge.
	if s.OriginalDeletedAt == nil {
		t.Fatal("source.OriginalDeletedAt is nil; expected non-zero after audio purge")
	}

	// transcript.txt must exist with correct content.
	transcriptPath := sources.Path(s.ID, "transcript.txt")
	content, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("transcript.txt missing: %v", err)
	}
	if string(content) != expectedText {
		t.Fatalf("transcript.txt = %q, want %q", string(content), expectedText)
	}

	// original.ogg must be removed after successful transcription.
	originalPath := sources.Path(s.ID, "original.ogg")
	if _, statErr := os.Stat(originalPath); !os.IsNotExist(statErr) {
		t.Fatalf("original.ogg should be deleted after transcription, os.Stat returned: %v", statErr)
	}
}

// newVoiceAPIServer is a variant of newTelegramAPIServer that returns
// fakeVoiceBytes for /file/* requests (mimicking a Telegram voice download).
func newVoiceAPIServer(t *testing.T, calls *[]telegramAPICall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/file/") {
			w.Header().Set("Content-Type", "audio/ogg")
			_, _ = w.Write(fakeVoiceBytes)
			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := path.Base(r.URL.Path)
		*calls = append(*calls, telegramAPICall{Method: method, Body: body})
		w.Header().Set("Content-Type", "application/json")
		if method == "getFile" {
			sizeStr := strconv.Itoa(len(fakeVoiceBytes))
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"voice-1","file_path":"voice/test.ogg","file_size":` + sizeStr + `}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":123},"date":1760000000,"text":"ok"}}`))
	}))
}

func filterCalls(calls []telegramAPICall, method string) []telegramAPICall {
	var out []telegramAPICall
	for _, c := range calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}
