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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/idempotency"
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

// MetaActivatedTools is the ToolResult.Meta key tool_search sets to the list of tool names it loaded, so the runner can promote them into the callable set.
const MetaActivatedTools = "activated_tools"

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
	// Destructive marks a mutating operation whose effect is destructive or
	// externally irreversible and therefore must be withheld for operator policy.
	// It is runtime-only. The gateway saturates it upward to Mutating so a
	// contradictory descriptor cannot bypass the approval path.
	Destructive bool
	// Multiplexed marks a tool that fronts several sub-actions behind one
	// `action`-style discriminator (skill/task/swarm_spawn). It is a descriptor
	// HINT for the policy gateway, NOT policy itself: the gateway's boot-guard uses
	// it to require that every Mutating multiplexed tool resolves to a concrete risk
	// tier, so a newly-added multiplexed action can never silently under-gate. Like
	// Mutating it is runtime-only and never wire-encoded (not LLM-visible).
	Multiplexed bool
	// OperationScope, OperationNormalizer, and ReplayPolicy are Aura-owned
	// mutation metadata. They are runtime-only and never exposed to the model.
	OperationScope      idempotency.Scope
	OperationNormalizer string
	ReplayPolicy        ReplayPolicy
}

// ReplayPolicy is the finite way a completed mutation can be returned safely.
type ReplayPolicy string

// The replay policy, idempotency scopes, and argument normalizer a mutating tool
// declares in its Spec. ReplayToolResult is the only safe replay today: a repeated
// call returns the recorded ToolResult instead of re-running the mutation.
const (
	ReplayToolResult             ReplayPolicy      = "tool_result"
	OperationScopeAgent          idempotency.Scope = idempotency.ScopeAgentTool
	OperationScopeMCP            idempotency.Scope = idempotency.ScopeMCPTool
	OperationNormalizerCanonical                   = "canonical_tool_args_v1"
)

// OperationFingerprint hashes the typed tool name plus canonical JSON arguments.
// Parse errors are collapsed so model-controlled payload bytes never leak.
func OperationFingerprint(spec Spec, raw json.RawMessage) ([32]byte, error) {
	if !spec.Mutating || spec.OperationScope == "" || spec.OperationNormalizer == "" || spec.ReplayPolicy == "" {
		return [32]byte{}, errors.New("tool operation metadata is incomplete")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return [32]byte{}, errors.New("tool operation arguments are invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return [32]byte{}, errors.New("tool operation arguments are invalid")
	}
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Tool string `json:"tool"`
		Args any    `json:"args"`
	}{Tool: spec.Name, Args: normalized})
	if err != nil {
		return [32]byte{}, errors.New("tool operation arguments are invalid")
	}
	return fingerprint, nil
}

// TrustLevel classifies whether a tool result came from host/operator-trusted
// logic or from attacker-controllable external bytes. The zero value means the
// result did not explicitly declare provenance.
type TrustLevel string

const (
	// TrustUntrusted marks a result as attacker-controllable external bytes, the
	// posture every tool that reaches the network or the filesystem must declare.
	TrustUntrusted TrustLevel = "untrusted"
	// TrustTrusted marks a result as host/operator-trusted, short-circuiting the
	// name-based untrusted-by-default fallback (AG-052). Used by the MCP bridge:
	// a mounted MCP server is operator-configured infrastructure, so its output is
	// trusted content like a built-in, not attacker bytes (aligns with Claude
	// Code's MCP posture). Size caps still bound it; only the distrust framing is
	// dropped.
	TrustTrusted TrustLevel = "trusted"
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

// Registry holds the set of tools available to one agent.
//
// It is built at startup and then read on every turn, but it is no longer immutable: a
// remote MCP server that needs a human to authorize it cannot be mounted at process boot,
// because boot has no human and no identity. It is mounted when the authorization
// completes, which means tools arrive after the first read. LibreChat draws the same line —
// app-level connections at start-up, user-scoped connections created lazily
// (requiresUserScopedConnection, MCPManager.ts) — and a server behind OAuth is always the
// second kind.
//
// The mutex is therefore load-bearing rather than defensive: without it, mounting a newly
// authorized server races every in-flight turn's manifest render.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty Registry ready for boot-time Register calls.
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.tools[name]; dup {
		panic(fmt.Sprintf("tools.Registry.Register: duplicate tool name %q — a tool with this name is already registered", name))
	}
	r.tools[name] = t
}

// Adopt registers a set of tools that may already be present, replacing any it collides
// with. It is how a server is re-mounted: the same server yields the same tool names, and
// a remount that panicked on its own previous mount would be useless.
//
// Unlike Register, a collision here is expected rather than a wiring bug — which is why
// the two are separate methods instead of one with a flag. Boot must still fail loud.
func (r *Registry) Adopt(tools []Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range tools {
		r.tools[t.Spec().Name] = t
	}
}

// Forget removes every tool whose name begins with prefix, returning how many went. It is
// the unmount half: a server the operator disabled or removed must stop being offered to
// the model without waiting for a restart.
func (r *Registry) Forget(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
			n++
		}
	}
	return n
}

// HasPrefix reports whether any registered tool name begins with prefix. It answers "is
// this server mounted?" for the governance board, which namespaces every mount as
// <server>__<tool>.
func (r *Registry) HasPrefix(prefix string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// Get returns the tool registered under name, reporting whether it exists.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns the registered tools in map-iteration order. Callers that render a
// manifest must sort the result — the order is deliberately not stable here.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tools {
		s := t.Spec()
		if !s.Deferred && s.Name != toolSearchName {
			return nil
		}
	}
	return ErrNoNonDeferredTool
}
