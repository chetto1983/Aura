package whisper_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm/whisper"
)

func TestTranscribeHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/inference" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ciao mondo prova"}`))
	}))
	defer srv.Close()

	c := whisper.New(srv.URL, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := c.Transcribe(context.Background(), []byte("fake ogg"), "audio/ogg", "it")
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if result.Text != "ciao mondo prova" {
		t.Fatalf("Text = %q, want %q", result.Text, "ciao mondo prova")
	}
	if result.ElapsedMs < 0 {
		t.Fatalf("ElapsedMs = %d, want >= 0", result.ElapsedMs)
	}
}

func TestTranscribeErrorPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failure", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := whisper.New(srv.URL, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := c.Transcribe(context.Background(), []byte("fake"), "audio/ogg", "it")
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should contain 500, got: %v", err)
	}
	// Body excerpt must be ≤200 chars in the error message (no full provider body leaked).
	if len(err.Error()) > 300 {
		t.Fatalf("error too verbose (%d chars), body excerpt must be ≤200", len(err.Error()))
	}
}

func TestTranscribeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // longer than client timeout below
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"late"}`))
	}))
	defer srv.Close()

	c := whisper.New(srv.URL, 20*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := c.Transcribe(context.Background(), []byte("fake"), "audio/ogg", "it")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestTranscribeMultipartFields(t *testing.T) {
	var capturedLang, capturedRespFmt, capturedTemp string
	var capturedFileBytes []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
			return
		}
		capturedLang = r.FormValue("language")
		capturedRespFmt = r.FormValue("response_format")
		capturedTemp = r.FormValue("temperature")

		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file: "+err.Error(), http.StatusBadRequest)
			return
		}
		capturedFileBytes, _ = io.ReadAll(f)
		f.Close()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	fakeAudio := []byte("synthetic ogg audio data")
	c := whisper.New(srv.URL, 5*time.Second, nil)
	_, err := c.Transcribe(context.Background(), fakeAudio, "audio/ogg", "fr")
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if capturedLang != "fr" {
		t.Fatalf("language field = %q, want fr", capturedLang)
	}
	if capturedRespFmt != "json" {
		t.Fatalf("response_format field = %q, want json", capturedRespFmt)
	}
	if capturedTemp != "0.0" {
		t.Fatalf("temperature field = %q, want 0.0", capturedTemp)
	}
	if !bytes.Equal(capturedFileBytes, fakeAudio) {
		t.Fatalf("file bytes mismatch: got %d bytes, want %d", len(capturedFileBytes), len(fakeAudio))
	}
}
