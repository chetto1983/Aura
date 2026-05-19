package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/llm/whisper"
	source "github.com/aura/aura/internal/storage/sources/store"
	tele "gopkg.in/telebot.v4"
)

// mockHubDispatcher is a minimal chat.Hub surface used in E2E voice handler tests.
// The interface is satisfied by *chat.Hub in production; this local struct captures
// dispatched InboundMessages for assertion.
type mockHubDispatcher struct {
	mu       sync.Mutex
	received []chat.InboundMessage
}

func (m *mockHubDispatcher) ReceiveMessage(_ context.Context, msg chat.InboundMessage) (*chat.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, msg)
	return &chat.Run{}, nil
}

func (m *mockHubDispatcher) first() (chat.InboundMessage, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.received) == 0 {
		return chat.InboundMessage{}, false
	}
	return m.received[0], true
}

func (m *mockHubDispatcher) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.received)
}

// TestVoiceHandlerE2EDispatchesToHub exercises the full voice ingestion pipeline
// with a mocked whisper server and a mock Hub. It asserts that after
// process() completes, the mock Hub received an InboundMessage with the expected
// shape: Text==transcript, Attachments[0].SourceID==stored src ID,
// Channel==ChannelTelegram, Mode==DeliveryModeStreaming, PrincipalID==senderID,
// and ChannelData contains the original tele.Context under "tele_context".
func TestVoiceHandlerE2EDispatchesToHub(t *testing.T) {
	const expectedTranscript = "benvenuto nel futuro"
	const senderID int64 = 123

	// Mock whisper-server: returns canned transcript.
	whisperSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"text":%q}`, expectedTranscript)
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

	hub := &mockHubDispatcher{}
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
		AfterTranscribe: func(ctx context.Context, teleCtx tele.Context, src *source.Source, transcript string) error {
			principalID := ""
			if s := teleCtx.Sender(); s != nil {
				principalID = strconv.FormatInt(s.ID, 10)
			}
			threadID := ""
			if c := teleCtx.Chat(); c != nil {
				threadID = strconv.FormatInt(c.ID, 10)
			}
			_, err := hub.ReceiveMessage(ctx, chat.InboundMessage{
				Channel:     chat.ChannelTelegram,
				PrincipalID: principalID,
				ThreadID:    threadID,
				Text:        transcript,
				Attachments: []chat.AttachmentRef{{
					SourceID: src.ID,
					MimeType: "audio/ogg",
					Status:   "transcribe_complete",
				}},
				Mode:      chat.DeliveryModeStreaming,
				CreatedAt: time.Now().UTC(),
				ChannelData: map[string]any{
					"tele_context": teleCtx,
				},
			})
			return err
		},
	})
	t.Cleanup(h.Stop)

	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: senderID, Username: "owner"},
		Chat:   &tele.Chat{ID: senderID},
		Voice: &tele.Voice{
			File:     tele.File{FileID: "voice-1", FileSize: int64(len(fakeVoiceBytes))},
			Duration: 5,
		},
	}})

	if err := h.onVoiceMessage(ctx); err != nil {
		t.Fatalf("onVoiceMessage() error = %v", err)
	}

	// Wait for the AfterTranscribe hook to fire (StatusTranscribeComplete + dispatch).
	waitUntil(t, 5*time.Second, func() bool {
		return hub.count() == 1
	})
	h.Stop()

	msg, ok := hub.first()
	if !ok {
		t.Fatal("mock hub received no InboundMessage")
	}

	// Channel
	if msg.Channel != chat.ChannelTelegram {
		t.Fatalf("InboundMessage.Channel = %q, want %q", msg.Channel, chat.ChannelTelegram)
	}

	// Transcript text
	if msg.Text != expectedTranscript {
		t.Fatalf("InboundMessage.Text = %q, want %q", msg.Text, expectedTranscript)
	}

	// Attachments
	if len(msg.Attachments) == 0 {
		t.Fatal("InboundMessage.Attachments is empty, want at least 1")
	}
	att := msg.Attachments[0]
	if att.SourceID == "" {
		t.Fatal("Attachments[0].SourceID is empty")
	}
	// Verify SourceID matches the persisted source
	stored, listErr := sources.List(source.ListFilter{Status: source.StatusTranscribeComplete})
	if listErr != nil {
		t.Fatalf("sources.List: %v", listErr)
	}
	if len(stored) == 0 {
		t.Fatal("no StatusTranscribeComplete source found after hook dispatch")
	}
	if att.SourceID != stored[0].ID {
		t.Fatalf("Attachments[0].SourceID = %q, want stored src ID %q", att.SourceID, stored[0].ID)
	}
	if att.MimeType != "audio/ogg" {
		t.Fatalf("Attachments[0].MimeType = %q, want audio/ogg", att.MimeType)
	}
	if att.Status != "transcribe_complete" {
		t.Fatalf("Attachments[0].Status = %q, want transcribe_complete", att.Status)
	}

	// DeliveryMode
	if msg.Mode != chat.DeliveryModeStreaming {
		t.Fatalf("InboundMessage.Mode = %q, want %q", msg.Mode, chat.DeliveryModeStreaming)
	}

	// PrincipalID matches sender
	wantPrincipal := strconv.FormatInt(senderID, 10)
	if msg.PrincipalID != wantPrincipal {
		t.Fatalf("InboundMessage.PrincipalID = %q, want %q", msg.PrincipalID, wantPrincipal)
	}

	// ChannelData carries the original tele.Context
	if msg.ChannelData == nil {
		t.Fatal("InboundMessage.ChannelData is nil, want tele_context entry")
	}
	if _, hasCtx := msg.ChannelData["tele_context"]; !hasCtx {
		t.Fatal("InboundMessage.ChannelData missing tele_context key")
	}
}

// TestVoiceHandlerE2EHookErrorPreservesTranscript verifies that a hook error does
// not roll back the transcript on disk and does not affect source status. The
// source remains StatusTranscribeComplete even when dispatch fails.
func TestVoiceHandlerE2EHookErrorPreservesTranscript(t *testing.T) {
	const expectedTranscript = "errore nel dispatch"

	whisperSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference" {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"text":%q}`, expectedTranscript)
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
		AfterTranscribe: func(_ context.Context, _ tele.Context, _ *source.Source, _ string) error {
			return fmt.Errorf("simulated dispatch failure")
		},
	})
	t.Cleanup(h.Stop)

	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Voice: &tele.Voice{
			File:     tele.File{FileID: "voice-1", FileSize: int64(len(fakeVoiceBytes))},
			Duration: 3,
		},
	}})

	if err := h.onVoiceMessage(ctx); err != nil {
		t.Fatalf("onVoiceMessage() error = %v", err)
	}

	// Wait for StatusTranscribeComplete (hook error doesn't change the source status).
	var stored []*source.Source
	waitUntil(t, 5*time.Second, func() bool {
		var err error
		stored, err = sources.List(source.ListFilter{Status: source.StatusTranscribeComplete})
		return err == nil && len(stored) == 1
	})
	h.Stop()

	// Source status must still be TranscribeComplete despite hook error.
	s := stored[0]
	if s.Status != source.StatusTranscribeComplete {
		t.Fatalf("source.Status = %q, want %q after hook error", s.Status, source.StatusTranscribeComplete)
	}
	// Transcript must be populated.
	if s.Transcript == nil || s.Transcript.Text != expectedTranscript {
		t.Fatalf("source.Transcript.Text = %q, want %q", func() string {
			if s.Transcript == nil {
				return "<nil>"
			}
			return s.Transcript.Text
		}(), expectedTranscript)
	}
}
