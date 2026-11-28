package tools

import (
	"fmt"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// notLoadedPrefix opens the dispatch gate's refusal; the bytes are part of what the model
// has learned to read, so they do not change.
const notLoadedPrefix = "error: tool_not_loaded: "

// NotLoadedRefusal is the dispatch gate's answer to a call to a deferred tool whose schema
// was never read: the call did not run, and the schema follows so the retry can be written
// against it. The refusal therefore loads the tool exactly as a tool_search result does,
// and LoadedBy recognises it by these bytes.
func NotLoadedRefusal(s Spec) string {
	return fmt.Sprintf(
		notLoadedPrefix+"%q was called before its schema was read, so the arguments "+
			"could not be trusted and the call did NOT run. Its schema follows and the tool is "+
			"now loaded — call %s again using exactly these parameters. Do not call tool_search "+
			"for it.\n\n%s",
		s.Name, s.Name, RenderSpec(s))
}

// ResultCalls maps the index of each tool result in history to the name of the call it
// answers: the most recent call with its id BEFORE it. A call id is not unique across a
// conversation on every route, so one map over the whole history would let a later call
// that reuses an id re-label an earlier result, both un-granting a real tool_search result
// and granting an echo.
func ResultCalls(history []llm.Message) map[int]string {
	names := make(map[string]string)
	calls := make(map[int]string)
	for i, m := range history {
		for _, tc := range m.ToolCalls {
			names[tc.ID] = tc.Function.Name
		}
		if m.Role != llm.RoleTool {
			continue
		}
		if name, ok := names[m.ToolCallID]; ok {
			calls[i] = name
		}
	}
	return calls
}

// References maps the index of each tool result in history that loaded deferred tools to
// their names, deduplicated in order. It is a pure function of history and registry, so a
// reloaded conversation derives exactly what the live one did.
func (r *Registry) References(history []llm.Message) map[int][]string {
	refs := make(map[int][]string)
	for i, called := range ResultCalls(history) {
		if loaded := r.LoadedBy(called, history[i].Content); len(loaded) > 0 {
			refs[i] = loaded
		}
	}
	return refs
}

// LoadedBy returns the deferred tools that a result of a call to calledTool loaded.
//
// A grant is anchored twice. To the call that produced the result: only tool_search and
// the gate's refusal load tools, so no other tool can mint a permission by printing a spec.
// And to the tool's CURRENT schema, byte for byte: a tool is granted only where the result
// holds RenderSpec of its registered spec in full.
//   - tool_search loads each deferred tool whose full RenderSpec it carries. A "## " line
//     only nominates a candidate, because a spec's own description is markdown: reading
//     headings as specs lost every tool whose description has one, and let a description
//     forge a "## other\nParameters:" block for a different tool.
//   - The refusal of a call to X loads X alone, and only if the result opens with
//     NotLoadedRefusal(X's spec). Text appended after it (a steer message) adds nothing.
//
// The byte match is also what makes a grant decay with its schema: a select truncated by
// the preview cap grants only the specs it rendered whole, and a spec that changed since
// the result was written no longer grants.
func (r *Registry) LoadedBy(calledTool, content string) []string {
	if calledTool != toolSearchName {
		if spec, ok := r.deferredSpec(calledTool); ok && strings.HasPrefix(content, NotLoadedRefusal(spec)) {
			return []string{calledTool}
		}
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(content, "\n") {
		name, ok := strings.CutPrefix(line, "## ")
		if !ok || slices.Contains(out, name) {
			continue
		}
		if spec, ok := r.deferredSpec(name); ok && strings.Contains(content, RenderSpec(spec)) {
			out = append(out, name)
		}
	}
	return out
}

func (r *Registry) deferredSpec(name string) (Spec, bool) {
	tool, ok := r.Get(name)
	if !ok {
		return Spec{}, false
	}
	spec := tool.Spec()
	return spec, spec.Deferred
}
