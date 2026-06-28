package multimodal

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSTTLocalMultipart(t *testing.T) {
	var gotAuth, gotPath, gotModel, gotLang string
	var gotFile []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("content-type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "file":
				gotFile = data
			case "model":
				gotModel = string(data)
			case "language":
				gotLang = string(data)
			}
		}
		_, _ = io.WriteString(w, `{"text":"ciao"}`)
	}))
	defer srv.Close()

	c := NewSTTClient(STTConfig{
		LocalBaseURL: srv.URL,
		LocalModel:   "faster-whisper",
		Language:     "it",
		HTTPClient:   srv.Client(),
	})
	if c.Cloud() {
		t.Fatal("Cloud() = true for a local config")
	}
	out, err := c.Transcribe(t.Context(), []byte("OGGDATA"), "voice.ogg", "ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if out != "ciao" {
		t.Errorf("transcript = %q, want ciao", out)
	}
	if gotAuth != "" {
		t.Errorf("local STT sent Authorization %q, want none", gotAuth)
	}
	if gotPath != "/audio/transcriptions" {
		t.Errorf("path = %q, want /audio/transcriptions", gotPath)
	}
	if string(gotFile) != "OGGDATA" {
		t.Errorf("file part = %q, want OGGDATA", gotFile)
	}
	if gotModel != "faster-whisper" || gotLang != "it" {
		t.Errorf("model/lang = %q/%q, want faster-whisper/it", gotModel, gotLang)
	}
}

func TestSTTCloudJSONInputAudio(t *testing.T) {
	var gotAuth, gotPath string
	var body sttCloudRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, `{"text":"hello"}`)
	}))
	defer srv.Close()

	audio := []byte("OPUSBYTES")
	c := NewSTTClient(STTConfig{
		CloudModel:        "openai/whisper-large-v3",
		OpenRouterBaseURL: srv.URL,
		OpenRouterAPIKey:  "shared-key",
		HTTPClient:        srv.Client(),
	})
	if !c.Cloud() {
		t.Fatal("Cloud() = false for a cloud config")
	}
	out, err := c.Transcribe(t.Context(), audio, "voice.ogg", "ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if out != "hello" {
		t.Errorf("transcript = %q, want hello", out)
	}
	if gotAuth != "Bearer shared-key" {
		t.Errorf("cloud STT Authorization = %q, want Bearer shared-key", gotAuth)
	}
	if gotPath != "/audio/transcriptions" {
		t.Errorf("path = %q, want /audio/transcriptions", gotPath)
	}
	if body.Model != "openai/whisper-large-v3" {
		t.Errorf("model = %q", body.Model)
	}
	if body.InputAudio.Format != "ogg" {
		t.Errorf("format = %q, want ogg", body.InputAudio.Format)
	}
	if want := base64.StdEncoding.EncodeToString(audio); body.InputAudio.Data != want {
		t.Errorf("input_audio.data = %q, want base64(audio)", body.InputAudio.Data)
	}
}

func TestSTTNon2xxStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := NewSTTClient(STTConfig{LocalBaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.Transcribe(t.Context(), []byte("x"), "a.ogg", "ogg")
	var se *StatusError
	if err == nil || !errors.As(err, &se) || se.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, want *StatusError 503", err)
	}
}
