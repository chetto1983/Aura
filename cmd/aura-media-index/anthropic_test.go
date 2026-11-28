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
	"github.com/chetto1983/aura/internal/settings"
)

// emptySettings is a settings store with no rows that records which sealed keys were read.
type emptySettings struct {
	mu   sync.Mutex
	read []string
}

func (s *emptySettings) Secret(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.read = append(s.read, key)
	return "", nil
}

func claudeConfig(sidecar string) *config.Config {
	return &config.Config{
		LLM:                  llm.Config{Provider: llm.AnthropicProvider, Model: "claude-sonnet-5-5", BaseURL: llm.AnthropicBaseURL},
		MultimodalBaseURL:    sidecar,
		MultimodalModel:      "glm-ocr",
		MultimodalTimeoutSec: 5,
	}
}

// The request Claude is sent is pinned in internal/llm/anthropic (describe_test.go); here the
// indexer is followed to the Claude client, which reads the house key from the settings store
// and, with none set, hands the image to the local sidecar without dialing.
func TestMediaIndexerReadsImagesWithTheHouseKey(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"read by the sidecar"}}]}`)
	}))
	defer sidecar.Close()
	store := &emptySettings{}
	creds := settings.Credentials{Store: store}
	cfg := claudeConfig(sidecar.URL + "/v1")
	reader, err := anthropicRoute(cfg, creds)
	if _, ok := reader.(*anthropic.Client); err != nil || !ok {
		t.Fatalf("anthropicRoute = %T, %v; want the Claude client", reader, err)
	}
	path := filepath.Join(t.TempDir(), "receipt.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := newMediaRuntime(cfg, creds, nil, false, reader).derive(t.Context(), mediaKindImage, path, "receipt.png")
	if err != nil || text != "read by the sidecar" {
		t.Fatalf("derive = %q, %v; want the sidecar's text", text, err)
	}
	store.mu.Lock()
	read := slices.Clone(store.read)
	store.mu.Unlock()
	if !slices.Equal(read, []string{settings.AnthropicKeySetting}) {
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
