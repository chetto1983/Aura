package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIClientSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		resp := chatResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{
				{Message: chatMessage{Role: "assistant", Content: "Hello from AI"}},
			},
		}
		resp.Usage.PromptTokens = 10
		resp.Usage.CompletionTokens = 5
		resp.Usage.TotalTokens = 15

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	result, err := client.Send(context.Background(), Request{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	})

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.Content != "Hello from AI" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello from AI")
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
}

func TestOpenAIClientSendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	_, err := client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
}

func TestOpenAIClientSendModelOverride(t *testing.T) {
	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedModel = req.Model

		resp := chatResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{
				{Message: chatMessage{Role: "assistant", Content: "ok"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	// Request with model override
	_, err := client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
		Model:    "gpt-3.5-turbo",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if receivedModel != "gpt-3.5-turbo" {
		t.Errorf("model = %q, want %q", receivedModel, "gpt-3.5-turbo")
	}

	// Request without model override (should use default)
	_, err = client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if receivedModel != "gpt-4" {
		t.Errorf("model = %q, want %q", receivedModel, "gpt-4")
	}
}

func TestOpenAIClientStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send SSE events
		flusher, _ := w.(http.Flusher)

		chunks := []struct {
			content string
			done    bool
		}{
			{"Hello", false},
			{" world", false},
			{"", true},
		}

		for _, chunk := range chunks {
			if chunk.done {
				w.Write([]byte("data: [DONE]\n\n"))
			} else {
				data, _ := json.Marshal(streamChunk{
					Choices: []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
						FinishReason *string `json:"finish_reason"`
					}{
						{Delta: struct {
							Content string `json:"content"`
						}{Content: chunk.content}},
					},
				})
				w.Write([]byte("data: " + string(data) + "\n\n"))
			}
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	ch, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var result string
	for token := range ch {
		if token.Err != nil {
			t.Fatalf("stream error: %v", token.Err)
		}
		if token.Done {
			break
		}
		result += token.Content
	}

	if result != "Hello world" {
		t.Errorf("stream result = %q, want %q", result, "Hello world")
	}
}

func TestOpenAIClientStreamAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error": "overloaded"}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	_, err := client.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 503 response")
	}
}

func TestOpenAIClientSendNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	_, err := client.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestRetryClientWithOpenAIClient(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := chatResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{
				{Message: chatMessage{Role: "assistant", Content: "Success"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	openai := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4",
	})

	retry := NewRetryClient(openai, RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	})

	result, err := retry.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.Content != "Success" {
		t.Errorf("Content = %q, want %q", result.Content, "Success")
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}
