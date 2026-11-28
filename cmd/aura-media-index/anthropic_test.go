package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/anthropic"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/settings"
)

// recordedSettings is a settings store that records which sealed keys were read.
type recordedSettings struct {
	mu   sync.Mutex
	rows map[string]string
	read []string
}

func (s *recordedSettings) Secret(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.read = append(s.read, key)
	return s.rows[key], nil
}

func (s *recordedSettings) keysRead() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.read)
}

func writeImage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipt.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The client only dials api.anthropic.com, so the reader is replaced by one that reads the
// key source it was handed and answers with it: the run reads with the house key from the
// settings store, never with the sidecar. What Claude is sent is pinned in
// internal/llm/anthropic (describe_test.go).
func TestMediaIndexerReadsImagesWithTheHouseKey(t *testing.T) {
	store := &recordedSettings{rows: map[string]string{settings.AnthropicKeySetting: "sk-ant-house"}}
	creds := settings.Credentials{Store: store}
	var model string
	original := newClaudeReader
	t.Cleanup(func() { newClaudeReader = original })
	newClaudeReader = func(cfg llm.Config, key anthropic.KeySource) multimodal.ImageReader {
		model = cfg.Model
		return readerFunc(func(ctx context.Context, _ []byte, mimeType, _ string) (string, error) {
			got, err := key(ctx)
			return "read by Claude with " + got + " as " + mimeType, err
		})
	}
	cfg := claudeConfig("")
	reader, err := anthropicRoute(cfg, creds)
	if err != nil {
		t.Fatal(err)
	}
	text, err := newMediaRuntime(cfg, creds, nil, false, reader).derive(t.Context(), mediaKindImage, writeImage(t), "receipt.png")
	if err != nil || text != "read by Claude with sk-ant-house as image/png" || model != "claude-sonnet-5-5" {
		t.Fatalf("derive = %q, %v (model %q); want Claude's read with the house key", text, err, model)
	}
	if read := store.keysRead(); !slices.Equal(read, []string{settings.AnthropicKeySetting}) {
		t.Fatalf("the read asked the store for %v, want the house key once", read)
	}
}

func claudeConfig(sidecar string) *config.Config {
	return &config.Config{
		LLM:                  llm.Config{Provider: llm.AnthropicProvider, Model: "claude-sonnet-5-5", BaseURL: llm.AnthropicBaseURL},
		MultimodalBaseURL:    sidecar,
		MultimodalModel:      "glm-ocr",
		MultimodalTimeoutSec: 5,
	}
}

// The real Claude client, with no house key in the store, hands the image to the local
// sidecar without dialing.
func TestMediaIndexerFallsBackWithoutTheHouseKey(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"read by the sidecar"}}]}`)
	}))
	defer sidecar.Close()
	store := &recordedSettings{}
	creds := settings.Credentials{Store: store}
	cfg := claudeConfig(sidecar.URL + "/v1")
	reader, err := anthropicRoute(cfg, creds)
	if _, ok := reader.(*anthropic.Client); err != nil || !ok {
		t.Fatalf("anthropicRoute = %T, %v; want the Claude client", reader, err)
	}
	text, err := newMediaRuntime(cfg, creds, nil, false, reader).derive(t.Context(), mediaKindImage, writeImage(t), "receipt.png")
	if err != nil || text != "read by the sidecar" {
		t.Fatalf("derive = %q, %v; want the sidecar's text", text, err)
	}
	if read := store.keysRead(); !slices.Equal(read, []string{settings.AnthropicKeySetting}) {
		t.Fatalf("the Claude read asked the store for %v, want the house key once", read)
	}
}

func TestAnthropicRouteIsNilOffClaude(t *testing.T) {
	reader, err := anthropicRoute(&config.Config{LLM: llm.Config{Provider: "ollama", Model: "gemma4"}}, settings.Credentials{})
	if err != nil || reader != nil {
		t.Fatalf("anthropicRoute off Claude = %v, %v; want no reader", reader, err)
	}
}

func TestRunFailsOnAClaudeRouteWithoutTheSettingsStore(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("AURA_DB_URL", "")
	t.Setenv("AURA_LLM_PROVIDER", llm.AnthropicProvider)
	t.Setenv("AURA_LLM_MODEL", "claude-sonnet-5-5")
	t.Setenv("AURA_LLM_BASE_URL", llm.AnthropicBaseURL)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("MULTIMODAL_BASE_URL", "")
	t.Setenv("MULTIMODAL_MODEL", "")
	t.Setenv("AURA_AUTHULA_SECRET", testSecret)
	t.Setenv("AURA_SKILLS_DIR", filepath.Join(t.TempDir(), "skills"))
	path := filepath.Join(t.TempDir(), "scan.png")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"-kind", "image", "-name", "scan.png", path}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "AURA_DB_URL") {
		t.Fatalf("exit = %d (stderr %q), want 1 naming the settings store", code, stderr.String())
	}
}
