package multimodal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
)

func staticKey(key string) KeyFunc {
	return func(context.Context) (string, error) { return key, nil }
}

// rotatingKey answers each key in turn, the way a key replaced in the cockpit reaches a client
// built at boot.
func rotatingKey(keys ...string) KeyFunc {
	var mu sync.Mutex
	return func(context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		key := keys[0]
		if len(keys) > 1 {
			keys = keys[1:]
		}
		return key, nil
	}
}

// authServer answers every request with body and records each Authorization it was sent.
func authServer(t *testing.T, body string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func assertAuthorizations(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("authorization = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("authorization = %q, want %q", got, want)
		}
	}
}

func TestSTTCloudSendsTheKeyReadAtRequestTime(t *testing.T) {
	srv, seen := authServer(t, `{"text":"ok"}`)
	c := NewSTTClient(STTConfig{
		CloudModel: "openai/whisper-large-v3", OpenRouterBaseURL: srv.URL,
		OpenRouterKey: rotatingKey("k1", "k2"), HTTPClient: srv.Client(),
	})
	for range 2 {
		if _, err := c.Transcribe(t.Context(), []byte("audio"), "a.ogg", "ogg"); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
	}
	assertAuthorizations(t, seen(), "Bearer k1", "Bearer k2")
}

func TestTTSCloudSendsTheKeyReadAtRequestTime(t *testing.T) {
	srv, seen := authServer(t, "mp3")
	c := NewTTSClient(TTSConfig{
		CloudModel: "hexgrad/kokoro-82m", CloudVoice: "af_sarah", OpenRouterBaseURL: srv.URL,
		OpenRouterKey: rotatingKey("k1", "k2"), HTTPClient: srv.Client(),
	})
	for range 2 {
		if _, err := c.Synthesize(t.Context(), "ciao"); err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
	}
	assertAuthorizations(t, seen(), "Bearer k1", "Bearer k2")
}

// A key that cannot be read fails the request with the store's error, and nothing is sent:
// an unauthenticated request would come back 401 and name the wrong cause.
func TestSpeechKeyReadErrorFailsTheRequest(t *testing.T) {
	errBoom := errors.New("sealed row unreadable")
	failing := func(context.Context) (string, error) { return "", errBoom }
	srv, seen := authServer(t, `{"text":"ok"}`)
	stt := NewSTTClient(STTConfig{
		CloudModel: "openai/whisper-large-v3", OpenRouterBaseURL: srv.URL, OpenRouterKey: failing, HTTPClient: srv.Client(),
	})
	if _, err := stt.Transcribe(t.Context(), []byte("audio"), "a.ogg", "ogg"); !errors.Is(err, errBoom) {
		t.Fatalf("Transcribe: err = %v, want the read error", err)
	}
	tts := NewTTSClient(TTSConfig{
		CloudModel: "hexgrad/kokoro-82m", OpenRouterBaseURL: srv.URL, OpenRouterKey: failing, HTTPClient: srv.Client(),
	})
	if _, err := tts.Synthesize(t.Context(), "ciao"); !errors.Is(err, errBoom) {
		t.Fatalf("Synthesize: err = %v, want the read error", err)
	}
	if got := seen(); len(got) != 0 {
		t.Fatalf("%d request(s) reached the endpoint", len(got))
	}
}

// primaryKeyOf is the binding the daemon and aura-media-index give VisionConfigFrom.
func primaryKeyOf(cfg *config.Config, creds settings.Credentials) KeyFunc {
	return creds.PrimaryRouteKey(cfg.LLM.Provider, cfg.LLM.BaseURL)
}

func servicesKeyOnly() settings.Credentials {
	return settings.Credentials{LookupEnv: func(key string) (string, bool) {
		if key == settings.ServicesKeySetting {
			return "services", true
		}
		return "", false
	}}
}

// The services key is OpenRouter's: a primary chat route on Ollama reads the image without it,
// where the old projection handed it the chat profile's key, whatever host that was.
func TestVisionSendsNoKeyToANonOpenRouterPrimary(t *testing.T) {
	srv, seen := authServer(t, `{"choices":[{"message":{"content":"ok"}}]}`)
	cfg := &config.Config{LLM: llm.Config{Provider: "ollama", BaseURL: srv.URL, Model: "gemma4:31b-cloud"}}
	vcfg := VisionConfigFrom(cfg, true, primaryKeyOf(cfg, servicesKeyOnly()))
	vcfg.HTTPClient = srv.Client()
	if _, _, err := NewVisionClient(vcfg).Describe(t.Context(), []byte("img"), "image/png", "x"); err != nil {
		t.Fatalf("Describe: %v", err)
	}
	assertAuthorizations(t, seen(), "")
}

func TestVisionSendsTheServicesKeyToAnOpenRouterPrimary(t *testing.T) {
	cfg := &config.Config{LLM: llm.Config{Provider: "openrouter", BaseURL: llm.DefaultBaseURL, Model: "z-ai/glm-5.3-flash"}}
	vcfg := VisionConfigFrom(cfg, true, primaryKeyOf(cfg, servicesKeyOnly()))
	if key, err := vcfg.PrimaryKey(t.Context()); err != nil || key != "services" {
		t.Fatalf("PrimaryKey = %q, %v, want the services key", key, err)
	}
}

// The cloud speech legs run on OpenRouter itself, whatever host the chat runs on.
func TestSpeechConfigsRunOnOpenRouterWhateverTheChatBase(t *testing.T) {
	cfg := &config.Config{LLM: llm.Config{Provider: "ollama", BaseURL: "http://host.docker.internal:11434/v1"}}
	if got := STTConfigFrom(cfg, nil).OpenRouterBaseURL; got != llm.DefaultBaseURL {
		t.Fatalf("STT cloud base = %q, want %q", got, llm.DefaultBaseURL)
	}
	if got := TTSConfigFrom(cfg, "mp3", nil).OpenRouterBaseURL; got != llm.DefaultBaseURL {
		t.Fatalf("TTS cloud base = %q, want %q", got, llm.DefaultBaseURL)
	}
}
