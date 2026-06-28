package multimodal

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeTTSReq(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode tts request: %v", err)
	}
	return m
}

func TestTTSLocalNoModelNoAuth(t *testing.T) {
	var gotAuth, gotPath string
	var req map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		req = decodeTTSReq(t, r)
		_, _ = w.Write([]byte("OPUS"))
	}))
	defer srv.Close()

	c := NewTTSClient(TTSConfig{
		LocalBaseURL: srv.URL,
		Voice:        "if_sara",
		Format:       "opus",
		HTTPClient:   srv.Client(),
	})
	out, err := c.Synthesize(t.Context(), "ciao")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(out) != "OPUS" {
		t.Errorf("audio = %q, want OPUS", out)
	}
	if gotAuth != "" {
		t.Errorf("local TTS sent Authorization %q, want none", gotAuth)
	}
	if gotPath != "/audio/speech" {
		t.Errorf("path = %q, want /audio/speech", gotPath)
	}
	if _, hasModel := req["model"]; hasModel {
		t.Errorf("local TTS sent a model field %v, want omitempty (absent)", req["model"])
	}
	if req["voice"] != "if_sara" || req["response_format"] != "opus" {
		t.Errorf("voice/format = %v/%v", req["voice"], req["response_format"])
	}
}

func TestTTSCloudModelAndBearer(t *testing.T) {
	var gotAuth string
	var req map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		req = decodeTTSReq(t, r)
		_, _ = w.Write([]byte("MP3"))
	}))
	defer srv.Close()

	c := NewTTSClient(TTSConfig{
		CloudModel:        "hexgrad/kokoro-82m",
		Voice:             "if_sara",
		OpenRouterBaseURL: srv.URL,
		OpenRouterAPIKey:  "shared-key",
		HTTPClient:        srv.Client(),
	})
	if _, err := c.Synthesize(t.Context(), "hi"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if gotAuth != "Bearer shared-key" {
		t.Errorf("cloud TTS Authorization = %q, want Bearer shared-key", gotAuth)
	}
	if req["model"] != "hexgrad/kokoro-82m" {
		t.Errorf("model = %v, want hexgrad/kokoro-82m", req["model"])
	}
}

func TestTTSNon2xxStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := NewTTSClient(TTSConfig{LocalBaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.Synthesize(t.Context(), "x")
	var se *StatusError
	if err == nil || !errors.As(err, &se) || se.StatusCode != http.StatusBadGateway {
		t.Fatalf("err = %v, want *StatusError 502", err)
	}
}

func TestTTSAudioFormatDefault(t *testing.T) {
	c := NewTTSClient(TTSConfig{})
	if got := c.AudioFormat(); got != "opus" {
		t.Fatalf("AudioFormat() = %q, want opus default", got)
	}
}
