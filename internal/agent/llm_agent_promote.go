package agent

import (
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// This file holds the full-promotion deferred-tool state helpers (Claude Code /
// OpenAI Agents parity): a deferred tool is not callable until tool_search loads
// its schema. a.activated is the promoted set — written here (from the serial
// dispatch result loop and from the rehydration below) and read by buildRequest and
// the dispatch gate.
//
// The grant is CONVERSATION-scoped, not turn-scoped. The runner builds a fresh
// LlmAgent per turn but seeds it with the rehydrated history, and a tool_search
// result carries the loaded schema in its text — so the schema outlives the turn
// that fetched it. deriveActivated re-reads those results at construction, keeping
// permission and schema in sync: without it the model saw a schema it was forbidden
// to use, and had to re-run tool_search every single turn to get the permission back.

// searchTool is the discovery hook. Its results are the ONLY authority for a
// deferred-tool grant — see deriveActivated.
const searchTool = "tool_search"

// deriveActivated rebuilds the promoted set from a rehydrated history so a deferred
// tool loaded in an EARLIER turn stays callable while its schema is still readable.
//
// A grant is anchored to the tool_call that PRODUCED the result, never to result text
// alone: only a RoleTool message whose ToolCallID belongs to a tool_search call is
// read. Otherwise any tool able to emit text could mint its own permissions (a bare
// `echo "## send_file"`) and then call that tool with fabricated arguments — the exact
// hallucination full-promotion exists to stop.
func deriveActivated(hist []llm.Message, reg *tools.Registry) map[string]struct{} {
	activated := make(map[string]struct{})
	if reg == nil {
		return activated
	}
	// Wire-valid history always places an assistant tool_call before its RoleTool
	// result, so one forward pass suffices to pair them.
	searchCalls := make(map[string]struct{})
	for _, m := range hist {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == searchTool {
				searchCalls[tc.ID] = struct{}{}
			}
		}
		if m.Role != llm.RoleTool {
			continue
		}
		if _, ok := searchCalls[m.ToolCallID]; !ok {
			continue
		}
		for _, name := range loadedSchemas(m.Content) {
			if tool, ok := reg.Get(name); ok && tool.Spec().Deferred {
				activated[name] = struct{}{}
			}
		}
	}
	return activated
}

// loadedSchemas returns the tool names whose FULL spec block is present in a
// tool_search result — a "## <name>" header followed by a non-empty "Parameters:"
// body before the next header — mirroring the layout ToolSearch.Execute writes.
//
// Requiring the Parameters body is what makes a grant decay together with its schema:
// when the preview cap truncates a batch select mid-block, the half-written tool is
// not granted, so the model is never left holding a permission for a tool whose
// arguments it can no longer read.
func loadedSchemas(content string) []string {
	var out []string
	var name string
	inBody := false
	for line := range strings.SplitSeq(content, "\n") {
		if header, ok := strings.CutPrefix(line, "## "); ok {
			name, inBody = strings.TrimSpace(header), false
			continue
		}
		if name == "" {
			continue
		}
		if line == "Parameters:" {
			inBody = true
			continue
		}
		if inBody && strings.TrimSpace(line) != "" {
			out = append(out, name)
			name, inBody = "", false
		}
	}
	return out
}

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
