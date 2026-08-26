package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/identityctx"
)

const (
	memoryContextTimeout = 2 * time.Second
	defaultPreloadTopK   = 5
)

type mountedMemoryContext struct {
	mu             sync.RWMutex
	client         *mcptools.MountedServer
	preloadTopK    int
	preloadTimeout time.Duration
}

func newMemoryContextProvider(client *mcptools.MountedServer, preloadTopK int, preloadTimeout time.Duration) *mountedMemoryContext {
	return &mountedMemoryContext{client: client, preloadTopK: preloadTopK, preloadTimeout: preloadTimeout}
}

func (m *mountedMemoryContext) setClient(client *mcptools.MountedServer) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.client = client
	m.mu.Unlock()
}

func (m *mountedMemoryContext) clearClient(client *mcptools.MountedServer) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.client == client {
		m.client = nil
	}
	m.mu.Unlock()
}

func (m *mountedMemoryContext) mountedClient() *mcptools.MountedServer {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

func (m *mountedMemoryContext) Context(ctx context.Context, identityID string) (string, error) {
	client := m.mountedClient()
	if client == nil {
		return "", fmt.Errorf("memory MCP is not mounted")
	}
	// The mounted OAuth pool selects the client session owned by this identity.
	callCtx, cancel := context.WithTimeout(identityctx.WithIdentityID(ctx, identityID), memoryContextTimeout)
	defer cancel()
	text, err := client.CallToolText(callCtx, "memory_digest", map[string]any{
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

// Search is the proactive per-message preload: a hybrid memory_search over the current
// user text, returning the top-k facts as bullet lines. An explicit abstention or an
// empty result yields "" (the caller then injects only the digest). Its own timeout
// (preloadTimeout, falling back to the digest timeout) keeps it off the critical path.
func (m *mountedMemoryContext) Search(ctx context.Context, identityID, query string) (string, error) {
	client := m.mountedClient()
	if client == nil {
		return "", fmt.Errorf("memory MCP is not mounted")
	}
	limit := m.preloadTopK
	if limit <= 0 {
		limit = defaultPreloadTopK
	}
	timeout := m.preloadTimeout
	if timeout <= 0 {
		timeout = memoryContextTimeout
	}
	// The identity-bound OAuth session, not a tool argument, selects the tenant.
	callCtx, cancel := context.WithTimeout(identityctx.WithIdentityID(ctx, identityID), timeout)
	defer cancel()
	text, err := client.CallToolText(callCtx, "memory_search", map[string]any{
		"query": query,
		"limit": limit,
	})
	if err != nil {
		return "", fmt.Errorf("memory search: %w", err)
	}
	var out struct {
		Facts []struct {
			Statement string `json:"statement"`
		} `json:"facts"`
		Retrieval struct {
			Abstained bool `json:"abstained"`
		} `json:"retrieval"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return "", fmt.Errorf("decode memory search: %w", err)
	}
	if out.Retrieval.Abstained || len(out.Facts) == 0 {
		return "", nil
	}
	var b strings.Builder
	for _, f := range out.Facts {
		s := strings.TrimSpace(f.Statement)
		if s == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(s)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}
