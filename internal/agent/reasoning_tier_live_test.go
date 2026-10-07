//go:build reasoning_live

// Live validation of the SHIPPED agent wiring: a real LlmAgent with the granite
// embedder wired resolves its reasoning tier through the embedding classifier
// (readTurn). readTurn never asks the teacher (the runner does, in the background), and
// the client refuses every request all the same. Proves the production path — not a spike
// harness — uses the local classifier on the OpenRouter gate.
//
//	go test -tags reasoning_live -run TestAdaptiveReasoningTierLive ./internal/agent/
package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
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

	embedder := &embeddings.Client{
		BaseURL:    graniteBase(),
		Client:     &http.Client{Timeout: 30 * time.Second},
		Dimensions: config.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections() // goleak: drain keep-alive conns
	a := NewLlmAgent(LlmAgentConfig{
		Client:   refusingTeacher{},
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
		// Each case is the first message of a fresh conversation, as production dispatches it.
		TurnReading: TurnReading{Standalone: true},
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
		a.history = []llm.Message{{Role: llm.RoleSystem, Content: SystemPrompt}, {Role: llm.RoleUser, Content: tc.prompt}}
		decision, read := a.readTurn(context.Background())
		if decision.EffortSource != EffortSourceSeeds && decision.EffortSource != EffortSourceGreeting {
			t.Errorf("readTurn(%q) source = %q, want seeds or greeting (classifier path failed live)", tc.prompt, decision.EffortSource)
			continue
		}
		if decision.EffortRequested != tc.want.Effort() {
			t.Errorf("readTurn(%q) = %q, want %q", tc.prompt, decision.EffortRequested, tc.want.Effort())
			continue
		}
		t.Logf("ok: %q -> %s via %s (margin %.3f, ask teacher %v)", tc.prompt, decision.EffortRequested, decision.EffortSource, read.seedMargin, decision.AskTeacher)
	}
}

// refusingTeacher answers no request, so this test measures the seed bank alone.
type refusingTeacher struct{}

func (refusingTeacher) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, errors.New("no teacher in the seed-bank live test")
}
