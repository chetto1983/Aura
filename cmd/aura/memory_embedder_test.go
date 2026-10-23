package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/settings"
)

// A manual endpoint is sent its own key, read at every request so a key replaced in the
// cockpit reaches the running daemon; the services key never goes there (spec, "Who pays").
func TestMemoryEmbedderReadsTheEndpointKeyOnEveryCall(t *testing.T) {
	server := newAuthorizationRecorder(t)
	creds, store := storedCredentials(map[string]string{
		settings.ServicesKeySetting: "services", settings.EmbedEndpointKeySetting: "k1",
	})
	cfg := &config.Config{Embed: config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL}}
	embedder := memoryEmbedder(cfg, creds)
	if _, err := embedder.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	store.set(settings.EmbedEndpointKeySetting, "k2")
	if _, err := embedder.Embed(t.Context(), []string{"b"}); err != nil {
		t.Fatalf("Embed after rotation: %v", err)
	}
	server.assertEmbeddedWith(t, "Bearer services", "Bearer k1", "Bearer k2")
}

// The daemon and arcadedb-mcp must name the same memory space, or each re-stamps the other's
// writes and memory stays lexical; MCP logs its space at boot, and so must the daemon, or the
// operator can see only one side (final review recommendation).
func TestLogMemorySpaceNamesTheDaemonsSpace(t *testing.T) {
	var out bytes.Buffer
	logEmbeddingSpace(slog.New(slog.NewTextHandler(&out, nil)), "memory", namedSpace("es1-daemon"), time.Second)
	if !strings.Contains(out.String(), "space=es1-daemon") {
		t.Fatalf("boot log = %q, want the daemon's memory space", out.String())
	}
}

type namedSpace string

func (s namedSpace) Embed(context.Context, []string) ([][]float64, error) { return nil, nil }

func (s namedSpace) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: string(s)}, nil
}

func TestMemoryEmbedderIsNilWithoutARoute(t *testing.T) {
	if embedder := memoryEmbedder(&config.Config{}, settings.Credentials{}); embedder != nil {
		t.Fatalf("got %#v, want nil with no embedding base", embedder)
	}
}

func TestLogEmbeddingSpaceNamesTheFamily(t *testing.T) {
	var out bytes.Buffer
	logEmbeddingSpace(slog.New(slog.NewTextHandler(&out, nil)), "documents", namedSpace("es1-docs"), time.Second)
	if !strings.Contains(out.String(), "family=documents") || !strings.Contains(out.String(), "space=es1-docs") {
		t.Fatalf("boot log = %q", out.String())
	}
}
