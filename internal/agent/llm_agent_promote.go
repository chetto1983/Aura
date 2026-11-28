package agent

import (
	"slices"

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

// searchTool is the discovery hook. Its results are the only authority for a MANIFEST
// grant (deriveActivated); the gate's refusal also loads a tool, for the gate alone
// (deriveEverLoaded).
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
	searchCalls := tools.SearchCallIDs(hist)
	// calledOrder is least-recently-used first, so the tail is what the cap keeps. A
	// re-call moves the name to the end rather than duplicating it.
	var calledOrder []string
	var lastLoaded []string
	for _, m := range hist {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == searchTool {
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
		lastLoaded = reg.LoadedBy(searchTool, m.Content)
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
// any point in this conversation: every tool a tool_search result or a dispatch-gate refusal
// loaded (tools.Registry.References, which anchors each grant to the call that produced the
// result, so a tool cannot mint its own permission by echoing a spec block). It has no expiry
// and no use-promotes split.
//
// The two sets answer different questions and must not be merged. activated decides what
// goes in the MANIFEST, and it has to forget, or the manifest grows without bound (the
// measured 56-definition / ~20k-token conversation in deriveActivated's note). everLoaded
// decides whether a CALL is grounded, and forgetting there buys nothing: the schema is
// still sitting in the transcript the model is reading, so bouncing the call only spends an
// LLM round trip to tell it something it can already see.
//
// The gate refusal counts because it carries the schema and tells the model the tool "is now
// loaded". Before it counted, a tool loaded that way stayed callable after a reload only while
// it was among the last maxPromotedDeferredTools calls, through activated; past that the
// model was refused a tool it had been told was loaded.
func deriveEverLoaded(hist []llm.Message, reg *tools.Registry) map[string]struct{} {
	loaded := make(map[string]struct{})
	if reg == nil {
		return loaded
	}
	for _, names := range reg.References(hist) {
		for _, name := range names {
			loaded[name] = struct{}{}
		}
	}
	return loaded
}

// recordLoaded adds the deferred tools a just-appended tool result loaded to everLoaded,
// through the same rule deriveEverLoaded applies on reload, so the live set and the
// reloaded one are the same function of the history. Called only from the serial result
// loop, so the map write is race-free.
func (a *LlmAgent) recordLoaded(calledTool, content string) {
	if a.registry == nil {
		return
	}
	for _, name := range a.registry.LoadedBy(calledTool, content) {
		if a.everLoaded == nil {
			a.everLoaded = make(map[string]struct{})
		}
		a.everLoaded[name] = struct{}{}
	}
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
