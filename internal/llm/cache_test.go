package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ptr returns a pointer to s, for building chatMessage.Content in tests.
func ptr(s string) *string { return &s }

// TestPromptCache_BreakpointPlacedAtSystemEnd verifies that injectCacheControl
// marks only the last system message and that the MarshalJSON emits the
// content-block array format with a cache_control field for that message.
func TestPromptCache_BreakpointPlacedAtSystemEnd(t *testing.T) {
	msgs := []chatMessage{
		{Role: "system", Content: ptr("First system.")},
		{Role: "system", Content: ptr("Second system.")},
		{Role: "user", Content: ptr("Hello")},
	}
	out := injectCacheControl(msgs)

	// Only the last system message (index 1) must carry a breakpoint.
	if !out[1].CacheBreakpoint {
		t.Error("expected last system message to have CacheBreakpoint=true")
	}
	if out[0].CacheBreakpoint {
		t.Error("expected first system message to have CacheBreakpoint=false")
	}
	if out[2].CacheBreakpoint {
		t.Error("expected user message to have CacheBreakpoint=false")
	}
	// Input slice must be unchanged (shallow copy contract).
	if msgs[1].CacheBreakpoint {
		t.Error("injectCacheControl must not mutate the input slice")
	}

	// The marked message must marshal to the content-block array format.
	b, err := json.Marshal(out[1])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "cache_control") {
		t.Errorf("marshaled breakpoint message missing cache_control: %s", b)
	}
	// Unmarked messages must use the plain string format.
	b0, _ := json.Marshal(out[0])
	if strings.Contains(string(b0), "cache_control") {
		t.Errorf("non-breakpoint message must not contain cache_control: %s", b0)
	}
}

// TestPromptCache_ProviderDeclinedFallback verifies that Send retries without
// cache markers when the provider returns 400, and that the first request
// contained the cache_control field.
func TestPromptCache_ProviderDeclinedFallback(t *testing.T) {
	callCount := 0
	var firstBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		callCount++
		if callCount == 1 {
			firstBody = body
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"cache_control format rejected"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "fallback reply"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer srv.Close()

	// Use a model name that triggers DetectCacheSupport (contains "claude").
	client := NewOpenAIClient(OpenAIConfig{
		APIKey:       "test",
		BaseURL:      srv.URL,
		Model:        "claude-3-haiku",
		CacheEnabled: true,
	})
	resp, err := client.Send(context.Background(), Request{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("expected success after fallback, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls (initial + retry), got %d", callCount)
	}
	if !strings.Contains(string(firstBody), "cache_control") {
		t.Errorf("expected first request to contain cache_control marker, body: %s", firstBody)
	}
	if resp.Content != "fallback reply" {
		t.Errorf("expected 'fallback reply', got %q", resp.Content)
	}
}

// TestPromptCache_BenchLogsCacheHit verifies that when the provider reports
// cached tokens in prompt_tokens_details, the value is surfaced in
// TokenUsage.CacheReadTokens so the bench can log cache_hit.
func TestPromptCache_BenchLogsCacheHit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "cached reply"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{
				"prompt_tokens":     100,
				"completion_tokens": 20,
				"total_tokens":      120,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 80,
				},
			},
		})
	}))
	defer srv.Close()

	client := NewOpenAIClient(OpenAIConfig{APIKey: "test", BaseURL: srv.URL, Model: "gpt-4o"})
	resp, err := client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage.CacheReadTokens != 80 {
		t.Errorf("expected CacheReadTokens=80, got %d", resp.Usage.CacheReadTokens)
	}
}

// TestPromptCache_ProviderNotSupportedExitsCleanly verifies that when
// CacheEnabled=true but the provider URL+model do not match any known
// cache-capable provider, no cache_control markers are injected and the
// request completes successfully.
func TestPromptCache_ProviderNotSupportedExitsCleanly(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		var chatReq struct {
			Messages []json.RawMessage `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &chatReq)
		// Fail the test if any message uses the content-block array format.
		for _, raw := range chatReq.Messages {
			var msg struct {
				Content json.RawMessage `json:"content"`
			}
			_ = json.Unmarshal(raw, &msg)
			if len(msg.Content) > 0 && msg.Content[0] == '[' {
				http.Error(w, "unexpected content-block format", http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
		})
	}))
	defer srv.Close()

	// srv.URL is 127.0.0.1:..., not openrouter.ai/anthropic.com.
	// Model "gpt-4o" does not match claude/deepseek heuristic.
	client := NewOpenAIClient(OpenAIConfig{
		APIKey:       "test",
		BaseURL:      srv.URL,
		Model:        "gpt-4o",
		CacheEnabled: true,
	})
	if client.cacheEnabled {
		t.Error("expected cacheEnabled=false for unsupported provider")
	}
	resp, err := client.Send(context.Background(), Request{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected HTTP server to be called")
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}
}
