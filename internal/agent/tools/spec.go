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
	"errors"
	"fmt"
)

// ErrNoNonDeferredTool is returned by Registry.Validate when no actionable
// (non-deferred) capability tool is registered, excluding the always-on
// tool_search hook. It mirrors Anthropic's hard 400 ("At least one tool must be
// non-deferred"): a registry where every capability is deferred would let the
// model only search, never act.
var ErrNoNonDeferredTool = errors.New("registry: at least one non-deferred tool is required (excluding tool_search)")

// toolSearchName is the reserved name of the non-deferrable discovery hook; it is
// excluded from the Validate non-deferred count (it is never an actionable tool).
const toolSearchName = "tool_search"

// Spec is the LLM-visible metadata for a tool.
type Spec struct {
	Name        string
	Summary     string          // one line, always shown in the manifest
	Description string          // full description; only shown when not Deferred OR after a tool_search hit
	Parameters  json.RawMessage // JSON-schema for the tool arguments
	Deferred    bool            // true → full spec hidden until tool_search loads it
	// Mutating marks a tool that can change host state (write a file, run a
	// command, mutate the sandbox). The agent's completion gate (amendment #54 /
	// D-43) only runs its critic on a turn that dispatched at least one mutating
	// tool — a pure read/chat turn skips the gate at zero extra cost. It is NOT
	// LLM-visible (never wire-encoded); it is a runtime hint only. Conservative by
	// design: shell_exec is Mutating even though `ls` does not mutate, because the
	// agent cannot know statically whether a command writes.
	Mutating bool
}

// TrustLevel classifies whether a tool result came from host/operator-trusted
// logic or from attacker-controllable external bytes. The zero value means the
// result did not explicitly declare provenance.
type TrustLevel string

const (
	TrustUntrusted TrustLevel = "untrusted"
)

// ToolResultProvenance is runtime-only metadata consumed by the agent loop before
// a tool result is threaded back into the next LLM prompt. It is not rendered in
// the LLM-visible tool schema.
type ToolResultProvenance struct {
	Source string
	Trust  TrustLevel
}

// ToolResult is the value a tool's Execute returns. Preview is what the agent
// puts into the RoleTool history message (it is the full content for small
// outputs, or a truncated preview + a read_tool_output footer pointer for large
// ones). When an output exceeds the preview cap the full bytes are written to
// FullPath (the sidecar) and Truncated is true; Bytes is always the full
// (pre-truncation) length of the original content. See tools.NewResult (D-25).
type ToolResult struct {
	Preview    string // history-bound content: full content, or preview+footer when Truncated
	FullPath   string // sidecar path holding the full bytes; empty when not spilled
	Bytes      int    // full length of the original content in bytes
	Truncated  bool   // true when the output was spilled to a sidecar
	Meta       *ToolResultMeta
	Provenance *ToolResultProvenance
}

// ToolResultMeta carries tool-specific structured fields for audit. It is behind
// a pointer so the zero ToolResult remains comparable in existing tests.
type ToolResultMeta map[string]any

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

// Register adds a tool to the registry, keyed by its Spec().Name. Registration
// is a boot-time operation (the registry is immutable for the lifetime of a run),
// so a duplicate name is a programming error — two tools fighting for one name,
// where a silent overwrite would shadow the first tool and leave the model
// dispatching against whichever happened to register last. It therefore FAILS
// LOUD with a panic at registration time, the same way net/http panics on a
// duplicate route. Register has no error return because no caller can sensibly
// recover from a static wiring collision: fix the wiring.
func (r *Registry) Register(t Tool) {
	name := t.Spec().Name
	if _, dup := r.tools[name]; dup {
		panic(fmt.Sprintf("tools.Registry.Register: duplicate tool name %q — a tool with this name is already registered", name))
	}
	r.tools[name] = t
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

// Validate fails closed when no actionable (non-deferred) capability tool is
// registered, mirroring Anthropic's hard 400. tool_search is excluded from the
// count because it is a non-deferrable discovery hook, never an actionable tool —
// dropping that exclusion would let a search-only registry pass (RESEARCH Pitfall
// 5). Call it once at boot after all Register calls (the registry is empty at
// construction, so a constructor-time check is impossible).
func (r *Registry) Validate() error {
	for _, t := range r.tools {
		s := t.Spec()
		if !s.Deferred && s.Name != toolSearchName {
			return nil
		}
	}
	return ErrNoNonDeferredTool
}
