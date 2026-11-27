package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestResolveModelProfileOnAnthropicNeverDials(t *testing.T) {
	srv, hits := countingServer(t)
	cfg := llm.Config{
		Provider: llm.AnthropicProvider, BaseURL: srv.URL, Model: "claude-sonnet-5-5",
		APIKey: "retained-key", Headers: map[string]string{"X-Leak": "1"},
		ContextWindow: 200000, MaxOutputTokens: 32768, MaxTokens: 4096, TotalTimeoutSec: 120,
	}
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatalf("ResolveModelProfile dialled %d times", hits.Load())
	}
	if cfg.APIKey != "" || len(cfg.Headers) != 0 {
		t.Fatalf("credentials kept: key %q headers %v", cfg.APIKey, cfg.Headers)
	}
	if cfg.CostStatus != llm.CostStatusUnknown {
		t.Fatalf("CostStatus = %q, want unknown", cfg.CostStatus)
	}
}

func TestSpendOnAnthropicNeverDials(t *testing.T) {
	srv, hits := countingServer(t)
	cfg := llm.Config{Provider: llm.AnthropicProvider, BaseURL: srv.URL, APIKey: "retained-key"}
	_, err := cfg.Spend(context.Background())
	if !errors.Is(err, llm.ErrSpendUnavailable) || hits.Load() != 0 {
		t.Fatalf("spend error=%v, outbound calls=%d", err, hits.Load())
	}
}
