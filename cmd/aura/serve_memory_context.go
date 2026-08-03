package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
)

const memoryContextTimeout = 2 * time.Second

type mountedMemoryContext struct {
	client mcptools.HostClient
}

func newMemoryContextProvider(client mcptools.HostClient) *mountedMemoryContext {
	if client == nil {
		return nil
	}
	return &mountedMemoryContext{client: client}
}

func (m *mountedMemoryContext) Context(ctx context.Context, identityID string) (string, error) {
	if m == nil || m.client == nil {
		return "", fmt.Errorf("memory MCP is not mounted")
	}
	callCtx, cancel := context.WithTimeout(ctx, memoryContextTimeout)
	defer cancel()
	text, err := m.client.CallTool(callCtx, "memory_digest", map[string]any{
		"user_identifier":  identityID,
		"limit":            50,
		"facts_per_entity": 3,
	})
	if err != nil {
		return "", fmt.Errorf("memory digest: %w", err)
	}
	var digest struct {
		Text     string `json:"text"`
		Entities int    `json:"entities"`
		Facts    int    `json:"facts"`
		Covered  bool   `json:"covered"`
	}
	if err := json.Unmarshal([]byte(text), &digest); err != nil {
		return "", fmt.Errorf("decode memory digest: %w", err)
	}
	if strings.TrimSpace(digest.Text) == "" {
		return "", nil
	}
	return fmt.Sprintf("covered=%t entities=%d facts=%d\n%s",
		digest.Covered, digest.Entities, digest.Facts, strings.TrimSpace(digest.Text)), nil
}
