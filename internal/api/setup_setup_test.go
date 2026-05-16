package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/secrets"
)

// stubSettingsWriter is a minimal config.Writer for setup tests.
type stubSettingsWriter struct {
	data map[string]string
}

func (s *stubSettingsWriter) Set(_ context.Context, key, value string) error {
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
	return nil
}

func (s *stubSettingsWriter) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

// stubSecrets is an in-memory secrets.Store for setup tests.
type stubSecrets struct {
	data map[string]string
}

func (s *stubSecrets) Get(_ context.Context, key string) (string, error) {
	if v, ok := s.data[key]; ok {
		return v, nil
	}
	return "", secrets.ErrSecretNotFound
}

func (s *stubSecrets) Set(_ context.Context, key, value string) error {
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
	return nil
}

func (s *stubSecrets) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *stubSecrets) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestSetupProbeProviderConnectFailure(t *testing.T) {
	res := SetupProbeProvider(context.Background(), "http://127.0.0.1:1", "key", "/models")
	if res.OK {
		t.Errorf("expected OK=false on bad port, got %+v", res)
	}
	if res.Error == "" {
		t.Errorf("expected error message")
	}
}

func TestSetupProbeProvider401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	res := SetupProbeProvider(context.Background(), srv.URL, "wrong-key", "/v1/models")
	if res.OK {
		t.Errorf("expected OK=false on 401, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.Error), "auth") {
		t.Errorf("expected auth error, got %q", res.Error)
	}
}

func TestSetupProbeProviderHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing bearer header: %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o"}]}`))
	}))
	defer srv.Close()

	res := SetupProbeProvider(context.Background(), srv.URL, "sk-x", "/v1/models")
	if !res.OK {
		t.Errorf("expected OK=true, got %+v", res)
	}
	if len(res.Models) != 2 || res.Models[0] != "gpt-4o-mini" {
		t.Errorf("models = %v, want [gpt-4o-mini, gpt-4o]", res.Models)
	}
}

func TestSetupProbeProviderNoBaseURL(t *testing.T) {
	res := SetupProbeProvider(context.Background(), "  ", "k", "/v1/models")
	if res.OK || res.Error == "" {
		t.Errorf("expected error for blank base URL, got %+v", res)
	}
}

func TestSetupProbeProviderNonStandardResponseStillOK(t *testing.T) {
	// A provider that returns 200 but a body we can't parse as a model
	// list should still be reported OK — we connected; the user might
	// just be using a non-standard endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>not json</html>`))
	}))
	defer srv.Close()

	res := SetupProbeProvider(context.Background(), srv.URL, "k", "/v1/models")
	if !res.OK {
		t.Errorf("expected OK=true for connect-but-unparseable, got %+v", res)
	}
	if len(res.Models) != 0 {
		t.Errorf("expected empty model list, got %v", res.Models)
	}
}

func TestSetupPresetByID(t *testing.T) {
	if p, ok := SetupPresetByID("openai"); !ok || p.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("SetupPresetByID(openai) wrong: %+v", p)
	}
	if p, ok := SetupPresetByID("openrouter"); !ok || p.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("SetupPresetByID(openrouter) wrong: %+v", p)
	}
	if _, ok := SetupPresetByID("anthropic"); ok {
		t.Errorf("native Anthropic preset should not be exposed as OpenAI-compatible")
	}
	if _, ok := SetupPresetByID("does-not-exist"); ok {
		t.Errorf("SetupPresetByID returned ok for unknown id")
	}
}

func TestSetupLocaleDetectionAndPresets(t *testing.T) {
	if got := detectLocale("it-IT,it;q=0.9,en;q=0.8"); got != "it" {
		t.Fatalf("detectLocale Italian = %q", got)
	}
	if got := detectLocale("de-DE,de;q=0.9"); got != "en" {
		t.Fatalf("detectLocale fallback = %q", got)
	}
	text := setupText("it")
	if !strings.Contains(text["heading"], "Benvenuto") {
		t.Fatalf("Italian heading missing: %q", text["heading"])
	}
	presets := localizedPresets("it")
	var found bool
	for _, preset := range presets {
		if preset.ID == "openrouter" {
			found = true
			if strings.Contains(preset.Description, "router for many hosted") {
				t.Fatalf("OpenRouter description not localized: %q", preset.Description)
			}
		}
	}
	if !found {
		t.Fatal("OpenRouter preset missing")
	}
}

func TestPresetsUseModelsProbe(t *testing.T) {
	for _, preset := range SetupLLMPresets {
		if preset.ProbePath != "/models" {
			t.Fatalf("%s ProbePath = %q, want /models", preset.ID, preset.ProbePath)
		}
	}
}

func TestLoopbackOnly(t *testing.T) {
	tests := map[string]string{
		"":                 "127.0.0.1:8080",
		":8081":            "127.0.0.1:8081",
		"0.0.0.0:8081":     "127.0.0.1:8081",
		"127.0.0.1:8090":   "127.0.0.1:8090",
		"192.168.1.5:8081": "192.168.1.5:8081", // explicit non-loopback host preserved (operator's choice)
		"::":               "127.0.0.1:8080",
		"garbage-no-port":  "127.0.0.1:8080",
	}
	for in, want := range tests {
		got := loopbackOnly(in)
		if got != want {
			t.Errorf("loopbackOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListenAddressAllowsContainerBind(t *testing.T) {
	if got := listenAddress("0.0.0.0:8080", true); got != "0.0.0.0:8080" {
		t.Fatalf("listenAddress(headless) = %q, want 0.0.0.0:8080", got)
	}
	if got := listenAddress("0.0.0.0:8080", false); got != "127.0.0.1:8080" {
		t.Fatalf("listenAddress(desktop) = %q, want 127.0.0.1:8080", got)
	}
}

func TestWriteSecretNilStoreReturnsError(t *testing.T) {
	err := writeSecret(context.Background(), nil, secrets.KeyTelegramToken, "tok")
	if err == nil {
		t.Fatal("expected error with nil store, got nil")
	}
}

func TestWriteSecretEmptyValueCallsDelete(t *testing.T) {
	store := &stubSecrets{data: map[string]string{secrets.KeyLLMAPIKey: "existing"}}
	if err := writeSecret(context.Background(), store, secrets.KeyLLMAPIKey, ""); err != nil {
		t.Fatalf("writeSecret empty: %v", err)
	}
	if _, err := store.Get(context.Background(), secrets.KeyLLMAPIKey); err == nil {
		t.Error("expected key to be deleted after writeSecret with empty value")
	}
}

func TestWriteSecretRoundtrip(t *testing.T) {
	store := &stubSecrets{data: map[string]string{}}
	if err := writeSecret(context.Background(), store, secrets.KeyTelegramToken, "123:ABC"); err != nil {
		t.Fatalf("writeSecret: %v", err)
	}
	got, err := store.Get(context.Background(), secrets.KeyTelegramToken)
	if err != nil {
		t.Fatalf("Get after writeSecret: %v", err)
	}
	if got != "123:ABC" {
		t.Errorf("Get = %q, want 123:ABC", got)
	}
}

func TestSetupWizardSaveWritesSecretsToStore(t *testing.T) {
	// Find a free port before starting the server.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	secretsStore := &stubSecrets{data: map[string]string{}}
	settingsStore := &stubSettingsWriter{data: map[string]string{}}

	type result struct {
		token string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		tok, err := SetupRun(SetupConfig{
			Listen:          addr,
			AllowRemoteBind: true,
			SecretsStore:    secretsStore,
			SettingsStore:   settingsStore,
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		ch <- result{tok, err}
	}()

	// Retry until the server is up.
	payload, _ := json.Marshal(map[string]string{
		"telegram_token":   "123:ABC",
		"llm_api_key":      "sk-key",
		"embedding_api_key": "emb-key",
		"llm_base_url":     "https://api.openai.com/v1",
		"llm_model":        "gpt-4o",
	})
	var resp *http.Response
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Post("http://"+addr+"/save", "application/json", bytes.NewReader(payload))
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("POST /save: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /save status = %d, want 200", resp.StatusCode)
	}

	res := <-ch
	if res.err != nil {
		t.Fatalf("SetupRun: %v", res.err)
	}

	// Secrets store must have token + API keys.
	checkSecret := func(key, want string) {
		t.Helper()
		got, err := secretsStore.Get(context.Background(), key)
		if err != nil {
			t.Errorf("secretsStore.Get(%q): %v", key, err)
			return
		}
		if got != want {
			t.Errorf("secretsStore[%q] = %q, want %q", key, got, want)
		}
	}
	checkSecret(secrets.KeyTelegramToken, "123:ABC")
	checkSecret(secrets.KeyLLMAPIKey, "sk-key")
	checkSecret(secrets.KeyEmbeddingAPIKey, "emb-key")
}
