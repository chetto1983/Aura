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
	// limit 1: the counts are the whole payload now. The digest's TEXT is deliberately
	// discarded — see the pointer below for why — but memory_digest is still the call
	// that produces the totals, and asking for fewer entities makes it cheaper.
	text, err := client.CallToolText(callCtx, "memory_digest", map[string]any{
		"limit":            1,
		"facts_per_entity": 1,
	})
	if err != nil {
		return "", fmt.Errorf("memory digest: %w", err)
	}
	var digest struct {
		Entities int `json:"entities"`
		Facts    int `json:"facts"`
	}
	if err := json.Unmarshal([]byte(text), &digest); err != nil {
		return "", fmt.Errorf("decode memory digest: %w", err)
	}
	if digest.Facts == 0 {
		return "", nil
	}
	return memoryPointer(digest.Facts, digest.Entities), nil
}

// memoryPointer is what the turn carries instead of the memory itself.
//
// The block used to be the whole digest, one line per entity, injected before every user
// message. That removed the NEED to recall: the agent had the answer in front of it and
// paraphrased it, so the retrieval tools went uncalled — including the neighbourhood hop,
// which exists only behind a tool call and was therefore unreachable. It also does not
// scale: past the index's cap the agent is blind to the remainder while feeling informed,
// which is the worse of the two failures.
//
// So the turn carries the SHAPE of the memory and the way in. The counts stay because
// removing them entirely leaves no signal that a memory exists at all, and an agent that
// does not know it has one has no reason to open it. They are two integers whatever the
// corpus grows to.
func memoryPointer(facts, entities int) string {
	return fmt.Sprintf(
		"You have %d facts across %d entities in long-term memory. The content is NOT in "+
			"this context — read it when a question touches what you know: memory_facts_about "+
			"for a name you have (depth 2 to widen to what it connects to), memory_recall with "+
			"mode recent for what was said before, memory_search when you have neither. The "+
			"memory-aura skill covers writing and correcting.",
		facts, entities)
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
