package telegram

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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
