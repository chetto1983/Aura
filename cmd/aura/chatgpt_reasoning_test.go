package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/llm"
)

func TestChatGPTCatalogProfileReachesComposerCapabilities(t *testing.T) {
	cfg := validFallbackLLMConfig()
	cfg.Provider, cfg.BaseURL, cfg.Model = llm.ChatGPTProvider, llm.ChatGPTBaseURL, "account-model"
	if err := cfg.ResolveChatGPTProfile(); err != nil {
		t.Fatal(err)
	}
	model := llm.ModelCatalogEntry{ID: cfg.Model, SupportedReasoningEfforts: []llm.ReasoningEffort{
		llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax,
	}, ReasoningMandatory: true}
	if err := applyChatGPTCatalogModel(&cfg, []llm.ModelCatalogEntry{model}); err != nil {
		t.Fatal(err)
	}
	server := agui.NewServer(nil, nil, agui.ServerConfig{})
	wireReasoningCapabilities(server, cfg)
	response := httptest.NewRecorder()
	server.Mux().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/composer/reasoning-capabilities", nil))
	var result struct {
		Levels   []string `json:"levels"`
		Backend  string   `json:"backend"`
		Detected bool     `json:"detected"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !result.Detected || result.Backend != llm.ChatGPTProvider || !slices.Equal(result.Levels, []string{"auto", "low", "mid", "high", "extra", "max"}) {
		t.Fatalf("capabilities %s", response.Body.String())
	}
}
