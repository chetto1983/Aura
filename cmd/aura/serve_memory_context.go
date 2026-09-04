package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	// `total`, not `entities`: the latter counts the index LINES, which the limit
	// above sets to one. Read as the size of the memory it said "1 entity" in every
	// turn -- an agent told it knows one thing has no reason to open memory_entities.
	var digest struct {
		Total    int `json:"total"`
		Entities int `json:"entities"`
		Facts    int `json:"facts"`
	}
	if err := json.Unmarshal([]byte(text), &digest); err != nil {
		return "", fmt.Errorf("decode memory digest: %w", err)
	}
	if digest.Facts == 0 {
		return "", nil
	}
	return memoryPointer(digest.Facts, digestEntityCount(digest.Total, digest.Entities)), nil
}

// digestEntityCount never reports fewer entities than the digest already showed.
//
// This binary and the memory MCP restart independently, so a running server can
// predate `total` and omit it -- which decodes to zero, and zero is the one number
// that tells the model its memory is empty. Caught doing exactly that on
// 2026-09-04 by rebuilding one container and not the other.
func digestEntityCount(total, shown int) int {
	return max(total, shown)
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
//
// What it does NOT carry is anything the memory-aura skill teaches. The two overlap by
// construction -- this block is in every turn, the skill is loaded on demand -- so the
// line between them has to be deliberate or they drift apart and contradict each other.
// The rule: this block holds only what must be true BEFORE the skill is loaded, which is
// the three-way read routing, because the failure it prevents happens without any tool
// call at all. An agent that does not know `recent` exists answers "I recall nothing" to
// a question about the past and never opens anything. Everything with semantics --
// depth, cursors, classes, supersession -- belongs to the skill alone; a parenthetical
// explaining `depth 2` used to live here and was exactly the kind of detail that goes
// stale in one of the two places.
//
// It routes to the tools that ride in every manifest, not to the ones that read best on
// paper. memory_facts_about and memory_search are deferred (memoryManifestCore,
// internal/agent/mcptools/bridge_policy.go) and memory_recall answers both questions --
// `entity` takes the graph path, `query` the hybrid one -- so naming them here would have
// spent a tool_search to reach a tool the core already covers. They are named once, as
// part of what IS deferred, so the model knows they exist rather than guessing.
//
// The skill is also named for what it ACTUALLY covers. It used to be advertised as
// "writing and correcting", which sells short the half worth having: reading the record
// underneath the facts -- the conversation turns and the provider reasoning traces --
// lives there too, and an agent told the skill is for writes has no reason to load it
// when the question is why something was concluded.
func memoryPointer(facts, entities int) string {
	return fmt.Sprintf(
		"You have %d facts across %d entities in long-term memory. The content is NOT in "+
			"this context — read it when a question touches what you know: memory_recall, with "+
			"entity for a name you have, mode recent for what was said before, and query when "+
			"you have neither. memory_entities lists the names already in use, which a write "+
			"consults first. The rest of the memory surface — memory_search, "+
			"memory_facts_about, memory_digest, memory_forget, memory_merge_entities and the "+
			"maintenance tools — is deferred, one tool_search away. Load the memory-aura skill "+
			"to write, to correct, or to read what was said and thought underneath the facts.",
		facts, entities)
}

// Search is the proactive per-message preload: what the memory already knows about
// the current user text, injected so the turn ARRIVES with it instead of having to
// go and ask.
//
// It reads through memory_recall rather than memory_search. Both rank the same
// hybrid way, but recall also returns the ENTITIES the question reached and the
// facts hanging off them (internal/arcadedb/memory_recall_expand.go), which is the
// difference between injecting what the memory says about the wording and
// injecting the piece of graph the wording landed on. memory_search returns a flat
// statement list and nothing to expand from.
//
// An explicit abstention or an empty result yields "" (the caller then injects only
// the pointer). Its own timeout (preloadTimeout, falling back to the digest one)
// keeps it off the critical path.
//
// Whether the block landed is read from the runner's per-turn log line, never from
// the model. Asked on 2026-09-03 what it had been given, it answered that it had a
// <memory_recall> block "which contains a fact about this"; the block had not been
// built at all that turn. A model describing its own prompt is generating text
// about the prompt, and it was taken for a measurement here for a full day.
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
	text, err := client.CallToolText(callCtx, "memory_recall", map[string]any{
		"mode":  "semantic",
		"query": query,
		"limit": limit,
	})
	if err != nil {
		return "", fmt.Errorf("memory recall: %w", err)
	}
	var out memoryPreloadResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return "", fmt.Errorf("decode memory recall: %w", err)
	}
	if out.Retrieval.Abstained {
		return "", nil
	}
	return renderMemoryPreload(out), nil
}
