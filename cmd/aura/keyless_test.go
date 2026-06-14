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
		"NEO4J_PASSWORD",
		"AURA_MCP_CONFIG",
		"AURA_MCP_SERVERS_JSON",
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
	if !strings.Contains(err.Error(), "POSTGRES_PASSWORD") || !strings.Contains(err.Error(), "NEO4J_PASSWORD") {
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
