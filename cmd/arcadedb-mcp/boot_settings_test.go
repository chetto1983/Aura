package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

type fakeBootSettings struct {
	rows      []sqlc.AuraSettings
	listErr   error
	secret    string
	secretErr error
	secretKey string
}

func (f *fakeBootSettings) List(context.Context) ([]sqlc.AuraSettings, error) {
	return f.rows, f.listErr
}

func (f *fakeBootSettings) Secret(_ context.Context, key string) (string, error) {
	f.secretKey = key
	return f.secret, f.secretErr
}

func TestApplyBootSettingsResolvesTheRouteFromRowsAndKeepsSecretOutOfEnv(t *testing.T) {
	t.Setenv("AURA_EMBED_BASE_URL", "http://stale-env:8081")
	t.Setenv("AURA_EMBED_MODEL", "")
	t.Setenv("OPENROUTER_API_KEY", "inherited-but-not-authoritative")
	store := &fakeBootSettings{
		rows: []sqlc.AuraSettings{
			{Key: "AURA_EMBED_BASE_URL", Value: "http://settings-embed:8081"},
			{Key: "AURA_EMBED_MODEL", Value: "vendor/embed-v2"},
		},
		secret: "stored-openrouter-key",
	}

	route, err := applyBootSettings(t.Context(), store)
	if err != nil {
		t.Fatalf("applyBootSettings: %v", err)
	}
	if route.baseURL != "https://openrouter.ai/api" || route.embed.CloudModel != "vendor/embed-v2" || route.apiKey != "stored-openrouter-key" {
		t.Fatalf("route = %+v, want the stored model on OpenRouter with the sealed key", route)
	}
	if store.secretKey != "OPENROUTER_API_KEY" {
		t.Fatalf("secret read as %q, want OPENROUTER_API_KEY", store.secretKey)
	}
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "inherited-but-not-authoritative" {
		t.Fatalf("OPENROUTER_API_KEY = %q: the stored secret must never enter the environment", got)
	}
}

func TestApplyBootSettingsKeepsTheLocalRouteWithoutAModel(t *testing.T) {
	for _, key := range []string{"AURA_EMBED_BASE_URL", "AURA_EMBED_MODEL", "AURA_EMBED_CLOUD_BASE_URL"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	route, err := applyBootSettings(t.Context(), &fakeBootSettings{secret: "stored-key"})
	if err != nil {
		t.Fatalf("applyBootSettings: %v", err)
	}
	if route.baseURL != "http://aura-llama-embed:8081" || route.embed.CloudModel != "" || route.apiKey != "" {
		t.Fatalf("local route = %+v, want the product default with no model or credential", route)
	}
}

func TestApplyBootSettingsFailsClosed(t *testing.T) {
	for name, store := range map[string]*fakeBootSettings{
		"settings overlay": {listErr: errors.New("postgres unavailable")},
		"secret read":      {secretErr: errors.New("sealed row unreadable")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := applyBootSettings(t.Context(), store); err == nil {
				t.Fatal("applyBootSettings succeeded; boot must not fall back to stale env")
			}
		})
	}
}

// The space is only logged, and the listener starts after it: a sidecar that accepts the
// connection and never answers must not hold boot for the embeddings client's full minute.
func TestBootSpaceGivesUpOnAStalledSidecar(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })

	done := make(chan error, 1)
	go func() {
		_, err := bootSpace(arcadedb.NewMemoryEmbedder(config.EmbedConfig{BaseURL: srv.URL}, nil), 50*time.Millisecond)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("bootSpace succeeded against a sidecar that never answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bootSpace still waiting after 5s: the boot deadline was not applied")
	}
}

func TestLoadBootSettingsRequiresPostgresDSNBeforeOpeningAnything(t *testing.T) {
	opened := false
	_, err := loadBootSettingsWith(t.Context(), "  ", "authula-secret", func(context.Context, string, string) (bootSettingsStore, func(), error) {
		opened = true
		return nil, nil, errors.New("unexpected open")
	})
	if err == nil || !strings.Contains(err.Error(), "AURA_DB_URL") {
		t.Fatalf("error = %v, want a named missing-DSN error", err)
	}
	if opened {
		t.Fatal("store opener called without a DSN")
	}
}

func TestLoadBootSettingsRequiresAuthulaSecretBeforeOpeningAnything(t *testing.T) {
	opened := false
	_, err := loadBootSettingsWith(t.Context(), "postgres://db/aura", "  ", func(context.Context, string, string) (bootSettingsStore, func(), error) {
		opened = true
		return nil, nil, errors.New("unexpected open")
	})
	if err == nil || !strings.Contains(err.Error(), "AURA_AUTHULA_SECRET") {
		t.Fatalf("error = %v, want a named missing-secret error", err)
	}
	if opened {
		t.Fatal("store opener called without the seal secret")
	}
}

func TestLoadBootSettingsPropagatesOpenFailureWithoutFallback(t *testing.T) {
	want := errors.New("postgres refused connection")
	_, err := loadBootSettingsWith(t.Context(), "postgres://db/aura", "authula-secret", func(_ context.Context, dsn, authulaSecret string) (bootSettingsStore, func(), error) {
		if dsn != "postgres://db/aura" || authulaSecret != "authula-secret" {
			t.Fatalf("opener inputs = (%q, %q)", dsn, authulaSecret)
		}
		return nil, nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped open failure", err)
	}
}

func TestLoadBootSettingsClosesTheBootstrapStore(t *testing.T) {
	closed := false
	store := &fakeBootSettings{secret: "stored-key"}
	_, err := loadBootSettingsWith(t.Context(), "postgres://db/aura", "authula-secret", func(context.Context, string, string) (bootSettingsStore, func(), error) {
		return store, func() { closed = true }, nil
	})
	if err != nil {
		t.Fatalf("loadBootSettingsWith: %v", err)
	}
	if !closed {
		t.Fatal("bootstrap Postgres pool was not closed")
	}
}
