package agent

import "github.com/chetto1983/aura/internal/agent/tools"

// This file holds the full-promotion deferred-tool state helpers (Claude Code /
// OpenAI Agents parity): a deferred tool is not callable until tool_search loads
// its schema. a.activated is the per-run promoted set — written only here (from the
// serial dispatch result loop) and read by buildRequest and the dispatch gate.

// promoteFromMeta reads a tool_search result's Meta and promotes the loaded tool
// names into a.activated so the NEXT buildRequest includes them in the callable
// manifest with their full schema (schema-load-before-call parity). It is nil-safe
// and only ever called from the serial dispatch result loop, so the map write is
// race-free (the concurrent executeBatch only READS a.activated via the gate).
func (a *LlmAgent) promoteFromMeta(meta *tools.ToolResultMeta) {
	if meta == nil {
		return
	}
	names, ok := (*meta)[tools.MetaActivatedTools].([]string)
	if !ok {
		return
	}
	if a.activated == nil {
		a.activated = make(map[string]struct{}, len(names))
	}
	for _, n := range names {
		a.activated[n] = struct{}{}
	}
}

// isDeferredUnloaded reports whether name is a registered deferred tool whose
// schema has NOT yet been promoted into the callable set this run. It gates the
// dispatch path so a hallucinated call to a still-hidden deferred function is
// bounced back with load-it-first guidance instead of executing with fabricated
// arguments.
func (a *LlmAgent) isDeferredUnloaded(name string) bool {
	tool, ok := a.registry.Get(name)
	if !ok || !tool.Spec().Deferred {
		return false
	}
	_, promoted := a.activated[name]
	return !promoted
}
