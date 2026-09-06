package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func isolateKeylessBootEnv(t *testing.T) {
	t.Helper()
	withTempHome(t)
	t.Chdir(t.TempDir())
	for _, k := range []string{
		"POSTGRES_PASSWORD",
		"AURA_DB_URL",
		"AURA_DB_MIGRATE_URL",
		"AURA_DB_BOOTSTRAP_URL",
		// The LLM pair is load-bearing HERE, and only became so when the empty-key gate
		// started reading the provider (amendment #219): with AURA_LLM_PROVIDER inherited
		// from the developer's shell as a local provider, no key is required and
		// TestChatBootStillRequiresAPIKey fails on the DB error instead — which is exactly
		// what happened on a machine that had exported it. Empty means the default provider,
		// which is the hosted one, which is the case this file exists to pin.
		"AURA_LLM_PROVIDER",
		"OPENROUTER_API_KEY",
	} {
		t.Setenv(k, "")
	}
}

func TestServeKeylessBootReachesInfraValidation(t *testing.T) {
	isolateKeylessBootEnv(t)

	_, err := bootServe(context.Background(), nil)
	if err == nil {
		t.Fatal("bootServe should fail on missing infra secrets in this test")
	}
	if errors.Is(err, llm.ErrMissingAPIKey) || strings.Contains(err.Error(), "API key is empty") {
		t.Fatalf("serve boot must tolerate an empty LLM key and fail later on infra validation, got %v", err)
	}
	// POSTGRES_PASSWORD is the whole required-secret gate.
	if !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatalf("bootServe err = %v, want infra validation error", err)
	}
}

func TestChatBootStillRequiresAPIKey(t *testing.T) {
	isolateKeylessBootEnv(t)

	_, err := bootChatEnv(context.Background())
	if !errors.Is(err, llm.ErrMissingAPIKey) {
		t.Fatalf("bootChatEnv err = %v, want ErrMissingAPIKey", err)
	}
}

func TestLLMNotConfiguredClientFailsClosed(t *testing.T) {
	client := newLLMClient(llm.Config{})

	ch, err := client.Stream(context.Background(), llm.Request{})
	if err == nil {
		t.Fatal("Stream with an empty API key should return llm_not_configured")
	}
	if ch != nil {
		t.Fatal("Stream with an empty API key should not return a stream channel")
	}

	var payload struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if unmarshalErr := json.Unmarshal([]byte(err.Error()), &payload); unmarshalErr != nil {
		t.Fatalf("llm_not_configured error should be JSON, got %q: %v", err.Error(), unmarshalErr)
	}
	if payload.Error != "llm_not_configured" {
		t.Fatalf("payload.error = %q, want llm_not_configured", payload.Error)
	}
	if !strings.Contains(payload.Hint, "OPENROUTER_API_KEY") {
		t.Fatalf("payload.hint = %q, want OPENROUTER_API_KEY guidance", payload.Hint)
	}
}

func TestLLMConfiguredClientBypassesKeylessGuard(t *testing.T) {
	client := newLLMClient(llm.Config{APIKey: "sk-test"})
	if _, guarded := client.(llmNotConfiguredClient); guarded {
		t.Fatal("configured LLM client should not use the keyless guard")
	}
}

func TestLLMLocalOpenAICompatClientAllowsEmptyCloudKey(t *testing.T) {
	for _, baseURL := range []string{
		"http://127.0.0.1:8080/v1",
		"http://aura-llm:8084/v1",
		"http://aura-vllm-chat:8000/v1",
		"http://192.168.1.40:11434/v1",
	} {
		client := newLLMClient(llm.Config{BaseURL: baseURL})
		if _, guarded := client.(llmNotConfiguredClient); guarded {
			t.Fatalf("local base URL %q should not require OPENROUTER_API_KEY", baseURL)
		}
	}
}
