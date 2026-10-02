package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestChatGPTSpendDoesNotProbeRetainedAPIKey(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	t.Cleanup(server.Close)
	cfg := llm.Config{Provider: llm.ChatGPTProvider, BaseURL: server.URL, APIKey: "retained-key"}
	_, err := cfg.Spend(context.Background())
	if !errors.Is(err, llm.ErrSpendSubscriptionIncluded) || calls != 0 {
		t.Fatalf("spend error=%v, outbound calls=%d", err, calls)
	}
}
