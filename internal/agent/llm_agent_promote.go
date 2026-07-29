package agent

import (
	"slices"
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

// maxPromotedDeferredTools bounds how many USED deferred tools keep their full schema in
// the manifest. At the ~368 tokens an MCP definition averages here, 10 is ~3.7k tokens —
// the most a long session should pay to keep its working set pre-rendered. It is a
// manifest budget, not a permission: everLoaded still lets the model call anything whose
// schema it has read, so the eleventh tool is not blocked, only not pre-rendered.
const maxPromotedDeferredTools = 10

// lastCalled returns the final n entries of an LRU-ordered slice (all of it when shorter).
func lastCalled(order []string, n int) []string {
	if len(order) <= n {
		return order
	}
	return order[len(order)-n:]
}

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
	// calledOrder is least-recently-used first, so the tail is what the cap keeps. A
	// re-call moves the name to the end rather than duplicating it.
	var calledOrder []string
	var lastLoaded []string
	for _, m := range hist {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == searchTool {
				searchCalls[tc.ID] = struct{}{}
				continue
			}
			calledOrder = append(slices.DeleteFunc(calledOrder, func(n string) bool {
				return n == tc.Function.Name
			}), tc.Function.Name)
		}
		if m.Role != llm.RoleTool {
			continue
		}
		if _, ok := searchCalls[m.ToolCallID]; !ok {
			continue
		}
		lastLoaded = nil
		for _, name := range loadedSchemas(m.Content) {
			if tool, ok := reg.Get(name); ok && tool.Spec().Deferred {
				lastLoaded = append(lastLoaded, name)
			}
		}
	}
	// USE promotes, a lookup alone does not. Rebuilding the grant from every
	// tool_search result ever seen made the promoted set grow monotonically: a tool
	// looked up once and never called kept its full schema in the manifest for the
	// rest of the conversation. Measured on this deployment, an MCP tool definition
	// averages ~368 tokens, so a conversation that had accumulated 56 of them was
	// paying ~20k tokens of manifest EVERY turn — past the point where tool-choice
	// accuracy is known to collapse, which is what "the agent got worse the longer
	// we talked" actually was.
	//
	// Same split as earendil-works/pi's splitDeferredTools: a tool the model has
	// actually invoked stays loaded; one it merely searched does not.
	//
	// USE promotes, but no longer forever. The use-promotes rule closed the `searched`
	// door and left `called` wide open: every tool ever invoked kept its full schema in
	// the manifest for the rest of the conversation, so the same unbounded growth walked
	// back in through the other door — 30 tools used over a long session is ~11k tokens
	// of manifest on EVERY turn, including the ones that just say "ok". The cap keeps the
	// most recently used and drops the rest, which is affordable only because the dispatch
	// gate now consults everLoaded: a dropped tool is still callable, it just is not
	// pre-rendered. Before that, dropping one cost a whole wasted LLM round trip.
	for _, name := range lastCalled(calledOrder, maxPromotedDeferredTools) {
		if tool, ok := reg.Get(name); ok && tool.Spec().Deferred {
			activated[name] = struct{}{}
		}
	}
	// The most recent lookup is kept regardless, so searching at the end of a turn
	// and calling at the start of the next still works. Without this the model can
	// livelock: search, turn boundary, grant gone, search again.
	for _, name := range lastLoaded {
		activated[name] = struct{}{}
	}
	return activated
}

// deriveEverLoaded rebuilds the set of deferred tools whose schema the model has READ at
// any point in this conversation. Same anchoring as deriveActivated — only a RoleTool
// message answering a tool_search call counts, so a tool cannot mint its own permission by
// echoing a spec block — but with no expiry and no use-promotes split.
//
// The two sets answer different questions and must not be merged. activated decides what
// goes in the MANIFEST, and it has to forget, or the manifest grows without bound (the
// measured 56-definition / ~20k-token conversation in deriveActivated's note). everLoaded
// decides whether a CALL is grounded, and forgetting there buys nothing: the schema is
// still sitting in the transcript the model is reading, so bouncing the call only spends an
// LLM round trip to tell it something it can already see.
func deriveEverLoaded(hist []llm.Message, reg *tools.Registry) map[string]struct{} {
	loaded := make(map[string]struct{})
	if reg == nil {
		return loaded
	}
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
				loaded[name] = struct{}{}
			}
		}
	}
	return loaded
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
	if a.everLoaded == nil {
		a.everLoaded = make(map[string]struct{}, len(names))
	}
	for _, n := range names {
		a.activated[n] = struct{}{}
		// Within the turn that loaded it, too: the gate reads everLoaded, and a
		// load-then-call in the same turn must not depend on the next construction.
		a.everLoaded[n] = struct{}{}
	}
}

// isDeferredUnloaded reports whether name is a deferred tool whose schema the model has
// NEVER read in this conversation. That — not manifest membership — is the question the
// dispatch gate has to answer: it exists to stop a call whose arguments were invented,
// and arguments are invented exactly when no schema was ever seen.
//
// It used to consult `activated`, the manifest set, which forgets on purpose so the
// manifest stays small. The two questions got conflated, and the cost was a wasted LLM
// round trip every time the model called a tool it had legitimately looked up earlier:
// the transcript still showed the schema, so the model called in good faith and got
// bounced. everLoaded never forgets, and costs nothing to keep — the schema it attests to
// is already sitting in the history, read from cache.
func (a *LlmAgent) isDeferredUnloaded(name string) bool {
	tool, ok := a.registry.Get(name)
	if !ok || !tool.Spec().Deferred {
		return false
	}
	if _, seen := a.everLoaded[name]; seen {
		return false
	}
	_, promoted := a.activated[name]
	return !promoted
}
