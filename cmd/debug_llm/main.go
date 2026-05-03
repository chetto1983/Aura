package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/logging"
)

func main() {
	logger, cleanup := logging.Setup("debug", "./logs")
	defer cleanup()

	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")

	if apiKey == "" {
		logger.Error("LLM_API_KEY not set")
		os.Exit(1)
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4"
	}

	logger.Info("LLM debug config", "base_url", baseURL, "model", model, "api_key_set", apiKey != "")

	// Create OpenAI-compatible client
	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})

	// Wrap with retry
	retryClient := llm.NewRetryClient(client, llm.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  time.Second,
		MaxDelay:   10 * time.Second,
	})

	// Test Send (non-streaming)
	logger.Info("testing Send (non-streaming)...")
	resp, err := retryClient.Send(context.Background(), llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: "Say hello in one word."},
		},
		Model: model,
	})
	if err != nil {
		logger.Error("Send failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("Send response: %q\n", resp.Content)
	fmt.Printf("Usage: prompt=%d completion=%d total=%d\n", resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)

	// Test Stream
	logger.Info("testing Stream...")
	ch, err := retryClient.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: "Count from 1 to 5, one number per line."},
		},
		Model: model,
	})
	if err != nil {
		logger.Error("Stream failed", "error", err)
		os.Exit(1)
	}
	fmt.Print("Stream response: ")
	for token := range ch {
		if token.Err != nil {
			logger.Error("stream token error", "error", token.Err)
			break
		}
		if token.Done {
			break
		}
		fmt.Print(token.Content)
	}
	fmt.Println()

	logger.Info("LLM debug complete — all calls succeeded")
}
