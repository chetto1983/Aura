package tools

import (
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// NotLoadedPrefix opens the dispatch gate's refusal of a deferred tool called before its
// schema was read. The refusal carries that schema, so it loads the tool exactly as a
// tool_search result does, and References recognises it by this prefix.
const NotLoadedPrefix = "error: tool_not_loaded: "

// LoadedSchemas returns the tool names whose FULL spec block is present in a tool result
// — a "## <name>" header followed by a non-empty "Parameters:" body before the next
// header — mirroring the layout RenderSpec writes.
//
// Requiring the Parameters body is what makes a grant decay together with its schema:
// when the preview cap truncates a batch select mid-block, the half-written tool is not
// granted, so the model is never left holding a permission for a tool whose arguments it
// can no longer read.
func LoadedSchemas(content string) []string {
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
// A grant is anchored to the call that produced the result, never to its text alone:
// otherwise any tool able to print a spec block could mint its own permissions and then be
// called with fabricated arguments. Two calls load tools. tool_search loads every deferred
// tool whose full spec it rendered. The dispatch gate's refusal of a call to X loads X, and
// only X: it must start with NotLoadedPrefix and carry exactly X's spec.
func (r *Registry) LoadedBy(calledTool, content string) []string {
	var out []string
	switch {
	case calledTool == toolSearchName:
		for _, name := range LoadedSchemas(content) {
			if r.isDeferred(name) && !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	case strings.HasPrefix(content, NotLoadedPrefix):
		if slices.Equal(LoadedSchemas(content), []string{calledTool}) && r.isDeferred(calledTool) {
			out = []string{calledTool}
		}
	}
	return out
}

func (r *Registry) isDeferred(name string) bool {
	tool, ok := r.Get(name)
	return ok && tool.Spec().Deferred
}
