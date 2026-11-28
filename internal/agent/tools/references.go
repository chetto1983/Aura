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

// SearchCallIDs returns the ids of every tool_search call in history.
func SearchCallIDs(history []llm.Message) map[string]struct{} {
	ids := make(map[string]struct{})
	for id, name := range callNames(history) {
		if name == toolSearchName {
			ids[id] = struct{}{}
		}
	}
	return ids
}

// References maps the index of each tool result in history that loaded deferred tools to
// their names, deduplicated in order. It is a pure function of history and registry, so a
// reloaded conversation derives exactly what the live one did.
func (r *Registry) References(history []llm.Message) map[int][]string {
	names := callNames(history)
	refs := make(map[int][]string)
	for i, m := range history {
		if m.Role != llm.RoleTool {
			continue
		}
		if loaded := r.LoadedBy(names[m.ToolCallID], m.Content); len(loaded) > 0 {
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

// callNames maps each tool_call id in history to the called tool's name.
func callNames(history []llm.Message) map[string]string {
	names := make(map[string]string)
	for _, m := range history {
		for _, tc := range m.ToolCalls {
			names[tc.ID] = tc.Function.Name
		}
	}
	return names
}
