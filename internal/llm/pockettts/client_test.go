package pockettts_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm/pockettts"
)

// wavMagic is a minimal WAV header: RIFF + size(0) + WAVE.
var wavMagic = []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'A', 'V', 'E'}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSynthesizeHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tts" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wavMagic)
	}))
	defer srv.Close()

	c := pockettts.New(srv.URL, 5*time.Second, discardLogger())
	result, err := c.Synthesize(context.Background(), "Buongiorno", "")
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if !bytes.HasPrefix(result.Audio, []byte("RIFF")) {
		t.Fatalf("expected WAV magic header, got: %q", result.Audio)
	}
	if result.MimeType != "audio/wav" {
		t.Fatalf("MimeType = %q, want audio/wav", result.MimeType)
	}
	if result.ElapsedMs < 0 {
		t.Fatalf("ElapsedMs = %d, want >= 0", result.ElapsedMs)
	}
	if result.Voice != "giovanni" {
		t.Fatalf("Voice = %q, want giovanni", result.Voice)
	}
	if result.Language != "italian_24l" {
		t.Fatalf("Language = %q, want italian_24l", result.Language)
	}
}

func TestSynthesizeNon200WithBodyExcerpt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream busy", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := pockettts.New(srv.URL, 5*time.Second, discardLogger())
	_, err := c.Synthesize(context.Background(), "ciao", "giovanni")
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should contain 500, got: %v", err)
	}
	if !strings.Contains(err.Error(), "upstream busy") {
		t.Fatalf("error should contain body excerpt, got: %v", err)
	}
	// Body excerpt must be ≤200 chars in the error message (no full provider body leaked).
	if len(err.Error()) > 350 {
		t.Fatalf("error too verbose (%d chars), body excerpt must be ≤200", len(err.Error()))
	}
}

func TestSynthesizeEmptyTextRejectedPreHTTP(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wavMagic)
	}))
	defer srv.Close()

	c := pockettts.New(srv.URL, 5*time.Second, discardLogger())
	_, err := c.Synthesize(context.Background(), "", "giovanni")
	if !errors.Is(err, pockettts.ErrEmptyText) {
		t.Fatalf("expected ErrEmptyText, got: %v", err)
	}
	if callCount != 0 {
		t.Fatalf("HTTP server was called %d times, want 0 (empty text must be rejected before HTTP)", callCount)
	}
}

func TestSynthesizeCtxCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wavMagic)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := pockettts.New(srv.URL, 5*time.Second, discardLogger())
	_, err := c.Synthesize(ctx, "ciao", "giovanni")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestSynthesizeBodyCapTruncation(t *testing.T) {
	const cap8MB = 8 * 1024 * 1024
	// Serve 9 MB — LimitReader must truncate to 8 MB.
	bigBody := make([]byte, cap8MB+1024*1024)
	// Prefix with WAV magic so the client doesn't reject it as non-WAV.
	copy(bigBody, wavMagic)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(bigBody)
	}))
	defer srv.Close()

	c := pockettts.New(srv.URL, 30*time.Second, discardLogger())
	result, err := c.Synthesize(context.Background(), "ciao", "giovanni")
	if err != nil {
		t.Fatalf("Synthesize() unexpected error = %v", err)
	}
	if len(result.Audio) > cap8MB {
		t.Fatalf("audio size %d exceeds 8 MB cap %d — LimitReader not applied", len(result.Audio), cap8MB)
	}
	if len(result.Audio) == 0 {
		t.Fatal("audio size is 0, expected capped non-empty body")
	}
}

func TestSynthesizeMultipartFields(t *testing.T) {
	var capturedText, capturedVoice string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
			return
		}
		capturedText = r.FormValue("text")
		capturedVoice = r.FormValue("voice_url")
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wavMagic)
	}))
	defer srv.Close()

	c := pockettts.New(srv.URL, 5*time.Second, discardLogger())
	_, err := c.Synthesize(context.Background(), "test phrase", "giovanni")
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if capturedText != "test phrase" {
		t.Fatalf("text field = %q, want %q", capturedText, "test phrase")
	}
	if capturedVoice != "giovanni" {
		t.Fatalf("voice_url field = %q, want giovanni", capturedVoice)
	}
}

