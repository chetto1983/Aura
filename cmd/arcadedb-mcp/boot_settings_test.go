package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

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

func TestApplyBootSettingsMakesPostgresWinAndKeepsSecretOutOfEnv(t *testing.T) {
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

	key, err := applyBootSettings(t.Context(), store)
	if err != nil {
		t.Fatalf("applyBootSettings: %v", err)
	}
	if got := os.Getenv("AURA_EMBED_BASE_URL"); got != "http://settings-embed:8081" {
		t.Fatalf("AURA_EMBED_BASE_URL = %q, want the Postgres value", got)
	}
	if got := os.Getenv("AURA_EMBED_MODEL"); got != "vendor/embed-v2" {
		t.Fatalf("AURA_EMBED_MODEL = %q, want the Postgres value", got)
	}
	if key != "stored-openrouter-key" || store.secretKey != "OPENROUTER_API_KEY" {
		t.Fatalf("secret = %q read as %q, want stored OPENROUTER_API_KEY", key, store.secretKey)
	}
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "inherited-but-not-authoritative" {
		t.Fatalf("OPENROUTER_API_KEY = %q: the stored secret must never enter the environment", got)
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
	key, err := loadBootSettingsWith(t.Context(), "postgres://db/aura", "authula-secret", func(context.Context, string, string) (bootSettingsStore, func(), error) {
		return store, func() { closed = true }, nil
	})
	if err != nil || key != "stored-key" {
		t.Fatalf("loadBootSettingsWith = (%q, %v)", key, err)
	}
	if !closed {
		t.Fatal("bootstrap Postgres pool was not closed")
	}
}

func TestEmbeddingRouteMatchesDaemonLocalAndCloudContract(t *testing.T) {
	for _, key := range []string{
		"AURA_EMBED_BASE_URL", "AURA_EMBED_MODEL", "AURA_EMBED_CLOUD_BASE_URL", "AURA_LLM_BASE_URL",
	} {
		t.Setenv(key, "")
	}

	t.Run("local default has no model or credential", func(t *testing.T) {
		_ = os.Unsetenv("AURA_EMBED_BASE_URL")
		route := embeddingRouteFromEnv("stored-key")
		if route.baseURL != "http://aura-llama-embed:8081" || route.model != "" || route.apiKey != "" {
			t.Fatalf("local route = %+v", route)
		}
	})

	t.Run("cloud ignores the chat LLM base", func(t *testing.T) {
		t.Setenv("AURA_EMBED_BASE_URL", "http://aura-llama-embed:8081")
		t.Setenv("AURA_EMBED_MODEL", "vendor/embed-v2")
		t.Setenv("AURA_LLM_BASE_URL", "http://host.docker.internal:11434/v1")
		route := embeddingRouteFromEnv("stored-key")
		if route.baseURL != "https://openrouter.ai/api" || route.model != "vendor/embed-v2" || route.apiKey != "stored-key" {
			t.Fatalf("cloud route = %+v, want OpenRouter whatever the chat LLM's base is", route)
		}
	})

	t.Run("explicit cloud endpoint wins", func(t *testing.T) {
		t.Setenv("AURA_EMBED_MODEL", "vendor/embed-v2")
		t.Setenv("AURA_EMBED_CLOUD_BASE_URL", "https://embed.example/v1")
		route := embeddingRouteFromEnv("stored-key")
		if route.baseURL != "https://embed.example" {
			t.Fatalf("explicit cloud base = %q", route.baseURL)
		}
	})

	t.Run("cloud defaults to the daemon OpenRouter route", func(t *testing.T) {
		t.Setenv("AURA_EMBED_MODEL", "vendor/embed-v2")
		route := embeddingRouteFromEnv("stored-key")
		if route.baseURL != "https://openrouter.ai/api" {
			t.Fatalf("default cloud base = %q", route.baseURL)
		}
	})
}
