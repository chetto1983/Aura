// Package tools defines the tool interface the agent loop dispatches against,
// the deferred-tool flag that keeps big specs out of the default LLM manifest,
// and the built-in `tool_search` hook the model uses to fetch deferred specs.
//
// Tool design rule: every tool with a long Description, examples, or a complex
// Parameters schema MUST set Deferred = true. The default LLM manifest then
// shows only Name + the first sentence of Description for those tools. The
// model loads the full spec on demand by calling `tool_search`, which protects
// the prompt cache (no per-turn manifest bloat) and lets the registry scale to
// N tools without context cost.
package tools

import (
	"context"
	"encoding/json"
)

// Spec is the LLM-visible metadata for a tool.
type Spec struct {
	Name        string
	Summary     string          // one line, always shown in the manifest
	Description string          // full description; only shown when not Deferred OR after a tool_search hit
	Parameters  json.RawMessage // JSON-schema for the tool arguments
	Deferred    bool            // true → full spec hidden until tool_search loads it
}

// ToolResult is the value a tool's Execute returns. Preview is what the agent
// puts into the RoleTool history message (it is the full content for small
// outputs, or a truncated preview + a read_tool_output footer pointer for large
// ones). When an output exceeds the preview cap the full bytes are written to
// FullPath (the sidecar) and Truncated is true; Bytes is always the full
// (pre-truncation) length of the original content. See tools.NewResult (D-25).
type ToolResult struct {
	Preview   string // history-bound content: full content, or preview+footer when Truncated
	FullPath  string // sidecar path holding the full bytes; empty when not spilled
	Bytes     int    // full length of the original content in bytes
	Truncated bool   // true when the output was spilled to a sidecar
}

// Tool is what the agent loop dispatches against. Execute returns a ToolResult
// (whose Preview is the content threaded back to the LLM) or an error.
// Implementations live in `internal/agent/tools/<name>.go`.
type Tool interface {
	Spec() Spec
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// Registry holds the set of tools available to one agent. It is built at
// startup and stays immutable for the lifetime of an agent run (swarm-spawned
// children may receive a filtered copy).
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Spec().Name] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}
