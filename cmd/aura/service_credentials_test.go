package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/settings"
)

// sealedSettings is the settings store's sealed rows as settings.Credentials reads them; set
// stands in for the cockpit or the reconciler replacing a key under a running process.
type sealedSettings struct {
	mu   sync.Mutex
	rows map[string]string
}

func (s *sealedSettings) Secret(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[key], nil
}

func (s *sealedSettings) set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[key] = value
}

// storedCredentials reads rows and nothing from the environment, so a developer's .env cannot
// answer for a key the test left out.
func storedCredentials(rows map[string]string) (settings.Credentials, *sealedSettings) {
	store := &sealedSettings{rows: rows}
	return settings.Credentials{Store: store, LookupEnv: func(string) (string, bool) { return "", false }}, store
}

// authorizationRecorder is a hosted embedding endpoint serving vendor/embed at 768 values. It
// records the Authorization of every embedding request, and of every request at all.
type authorizationRecorder struct {
	*httptest.Server
	mu          sync.Mutex
	embeds, all []string
}

func newAuthorizationRecorder(t *testing.T) *authorizationRecorder {
	t.Helper()
	recorder := &authorizationRecorder{}
	recorder.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mu.Lock()
		recorder.all = append(recorder.all, r.Header.Get("Authorization"))
		recorder.mu.Unlock()
		// A hosted route reads the model's input limit from the catalogue first (fit.go).
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"vendor/embed","context_length":2048}]}`)
			return
		}
		recorder.mu.Lock()
		recorder.embeds = append(recorder.embeds, r.Header.Get("Authorization"))
		recorder.mu.Unlock()
		vector := make([]float64, 768)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
	}))
	t.Cleanup(recorder.Close)
	return recorder
}

// assertEmbeddedWith checks the embedding requests carried want, in order, and that no request
// of any kind carried forbidden.
func (r *authorizationRecorder) assertEmbeddedWith(t *testing.T, forbidden string, want ...string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.Join(r.embeds, ",") != strings.Join(want, ",") {
		t.Fatalf("embedding requests carried %q, want %q", r.embeds, want)
	}
	for _, got := range r.all {
		if got == forbidden {
			t.Fatalf("a request carried %q", forbidden)
		}
	}
}

// The daemon boots without a pool only on paths that never embed, but the helper must still
// answer from the environment there rather than fail.
func TestServiceCredentialsWithoutAPoolReadTheEnvironment(t *testing.T) {
	t.Setenv(settings.ServicesKeySetting, " env-services ")
	creds := serviceCredentials(&config.Config{}, nil)
	if key, err := creds.ServicesKey(t.Context()); err != nil || key != "env-services" {
		t.Fatalf("ServicesKey = %q, %v, want the environment's", key, err)
	}
}
