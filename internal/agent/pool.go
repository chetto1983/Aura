package agent

import (
	"strings"
	"sync"

	"github.com/aura/aura/internal/llm"
)

// toolPool is the per-turn set of tool definitions visible to the LLM.
//
// The pool starts from Options.ToolsProvider() (the always-on set) and
// grows monotonically during the turn via permissive-load: when the model
// emits a tool_call with a name not yet in the pool — seen in the system
// prompt manifest — the loop calls resolver(name) and adds the definition
// before dispatching. No vector retrieval, no absorption path.
//
// The pool is per-turn (one runLoop invocation). State does not leak across
// turns; the next turn starts fresh from ToolsProvider().
//
// Concurrency: a single tool batch may execute calls in parallel, but
// pool mutations happen in the loop's serial section (before/after
// ExecuteToolCalls), so a sync.RWMutex is sufficient.
type toolPool struct {
	mu       sync.RWMutex
	loaded   map[string]llm.ToolDefinition
	resolver func(name string) (llm.ToolDefinition, bool)
}

// newToolPool seeds the pool from `initial` and arms the permissive
// resolver. A nil resolver disables permissive-load (the loop still
// honors the seed but cannot expand). Callers that don't need permissive
// can pass nil — the deferred-tools rollout requires it.
func newToolPool(initial []llm.ToolDefinition, resolver func(string) (llm.ToolDefinition, bool)) *toolPool {
	pool := &toolPool{
		loaded:   make(map[string]llm.ToolDefinition, len(initial)),
		resolver: resolver,
	}
	for _, def := range initial {
		if def.Name == "" {
			continue
		}
		pool.loaded[def.Name] = def
	}
	return pool
}

// Defs returns a snapshot of the currently-loaded definitions, suitable
// to pass to ChatClient.Chat. Order is not stable across calls — the LLM
// API does not care, but tests that compare against fixtures should sort.
func (p *toolPool) Defs() []llm.ToolDefinition {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]llm.ToolDefinition, 0, len(p.loaded))
	for _, def := range p.loaded {
		out = append(out, def)
	}
	return out
}

// EnsureLoaded guarantees a definition for `name` is in the pool when
// possible. Returns true when the tool is now (or already was) in the
// pool. Returns false when the resolver is absent OR the resolver
// reports that the tool does not exist in the registry (the LLM
// hallucinated a name from the manifest — fail-cleanly so the model can
// recover on the next iteration).
func (p *toolPool) EnsureLoaded(name string) bool {
	if p == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	p.mu.RLock()
	_, present := p.loaded[name]
	p.mu.RUnlock()
	if present {
		return true
	}
	if p.resolver == nil {
		return false
	}
	def, ok := p.resolver(name)
	if !ok || def.Name == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Re-check under write lock in case a concurrent EnsureLoaded won.
	if _, present := p.loaded[def.Name]; present {
		return true
	}
	p.loaded[def.Name] = def
	return true
}
