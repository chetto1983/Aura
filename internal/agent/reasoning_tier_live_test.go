//go:build reasoning_live

// Live validation of the SHIPPED agent wiring: a real LlmAgent with the granite
// embedder wired resolves its reasoning tier through the embedding classifier
// (adaptiveReasoningTier) WITHOUT calling the LLM router. Proves the production
// path — not a spike harness — uses the local classifier on the OpenRouter gate.
//
//	go test -tags reasoning_live -run TestAdaptiveReasoningTierLive ./internal/agent/
package agent

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/llm"
)

func graniteBase() string {
	if v := os.Getenv("AURA_EMBED_BASE_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8081"
}

func TestAdaptiveReasoningTierLive(t *testing.T) {
	if resp, err := http.Get(graniteBase() + "/health"); err != nil {
		t.Skipf("granite sidecar unreachable: %v", err)
	} else {
		_ = resp.Body.Close()
	}

	embedder := &documents.EmbeddingClient{
		BaseURL:    graniteBase(),
		Client:     &http.Client{Timeout: 30 * time.Second},
		Dimensions: config.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections() // goleak: drain keep-alive conns
	// Client is nil on purpose: a classifier hit must short-circuit BEFORE the LLM
	// router, so the agent must never dereference the client on these turns.
	a := NewLlmAgent(LlmAgentConfig{
		Registry: tools.NewRegistry(),
		LLM: llm.Config{
			Model:             "deepseek/deepseek-v4-flash",
			Provider:          "openrouter",
			BaseURL:           "https://openrouter.ai/api/v1",
			AdaptiveReasoning: true,
			MaxTokens:         4096,
			TotalTimeoutSec:   30,
		},
		Embedder: embedder,
	})

	cases := []struct {
		prompt string
		want   prompt.ReasoningTier
	}{
		{"ciao, come stai?", prompt.ReasoningTierNone},
		{"che tempo fa domani a Cuneo?", prompt.ReasoningTierLow},
		{"debugga questo segmentation fault nel mio codice C", prompt.ReasoningTierHigh},
	}
	for _, tc := range cases {
		a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: tc.prompt})
		tier, ok := a.adaptiveReasoningTier(context.Background())
		if !ok {
			t.Errorf("adaptiveReasoningTier(%q) not ok — classifier path failed live", tc.prompt)
			continue
		}
		if tier != tc.want {
			t.Errorf("adaptiveReasoningTier(%q) = %q, want %q", tc.prompt, tier, tc.want)
		} else {
			t.Logf("ok: %q -> %s (via embedding classifier, no LLM router)", tc.prompt, tier)
		}
	}
}
