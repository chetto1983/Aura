package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
)

// Tool is callable by the agent through model tool calls.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, args map[string]any) (string, error)
}

const CategoryMCP = "mcp"
const CategoryAutonomous = "autonomous"

// CategorizedTool lets runtime orchestration select tools by capability class
// without coupling availability to user-text heuristics.
type CategorizedTool interface {
	Category() string
}

type MultiCategorizedTool interface {
	Categories() []string
}

type categorizedTool struct {
	Tool
	categories []string
}

func WithCategory(tool Tool, category string) Tool {
	if tool == nil {
		return nil
	}
	category = strings.TrimSpace(category)
	if category == "" {
		return tool
	}
	return categorizedTool{
		Tool:       tool,
		categories: registryUniqueStrings(append(toolCategories(tool), category)),
	}
}

func (t categorizedTool) Categories() []string {
	return append([]string(nil), t.categories...)
}

// Definition forwards the wrapped tool's curated definition when it provides
// one. Without this method the type-assertion at definitionForTool checks
// categorizedTool's own method set, which does NOT include the wrapped
// interface's Definition — examples were silently lost on every wrapped tool.
func (t categorizedTool) Definition() ToolDefinition {
	if provider, ok := t.Tool.(ToolDefinitionProvider); ok {
		return provider.Definition()
	}
	return ToolDefinition{}
}

func (t categorizedTool) RequiredCapability() identity.Capability {
	if provider, ok := t.Tool.(ToolCapabilityProvider); ok {
		return provider.RequiredCapability()
	}
	return ""
}

// Registry stores tools and dispatches tool calls by name.
type Registry struct {
	mu          sync.RWMutex
	tools       map[string]Tool
	vectorIndex *ToolVectorIndex
	logger      *slog.Logger
}

// NewRegistry constructs an empty tool registry.
func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		tools:  make(map[string]Tool),
		logger: logger,
	}
}

// Register adds a tool. If a tool with the same name is already registered, a
// warning is logged before the previous entry is replaced — callers that
// genuinely want override semantics get them, but a misbehaving MCP server (or
// a refactor that resurrects a retired tool) cannot silently shadow a
// built-in. Returns true when the registration replaced an existing entry.
func (r *Registry) Register(t Tool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	_, existed := r.tools[name]
	if existed && r.logger != nil {
		r.logger.Warn("tool registry: replacing existing registration", "tool", name)
	}
	r.tools[name] = t
	return existed
}

// Get returns a registered tool by name.
func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Unregister removes a tool by name. Returns true when a tool was actually
// removed, false when no such tool existed. Used by Wave 2.10.b's
// toolindex reconciler path to drop MCP tools whose server disappeared
// from mcp.json (today this is invoked at server-reload time; the
// reconciler then deletes the matching Qdrant point + state row).
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return false
	}
	delete(r.tools, name)
	return true
}

// Names returns all registered tool names in deterministic order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NamesByCategory returns registered tool names for a declared capability
// category in deterministic order.
func (r *Registry) NamesByCategory(category string) []string {
	category = strings.TrimSpace(category)
	if category == "" {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, tool := range r.tools {
		if !toolHasCategory(tool, category) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func toolHasCategory(tool Tool, category string) bool {
	for _, value := range toolCategories(tool) {
		if value == category {
			return true
		}
	}
	return false
}

func toolCategories(tool Tool) []string {
	if tool == nil {
		return nil
	}
	if categorized, ok := tool.(MultiCategorizedTool); ok {
		return registryUniqueStrings(categorized.Categories())
	}
	if categorized, ok := tool.(CategorizedTool); ok {
		return registryUniqueStrings([]string{categorized.Category()})
	}
	return nil
}

// FullDefinitions returns the registered tools with their full Aura metadata
// (hints + VisibilityTier). Used by the catalogue scan test and any runtime
// path that needs behavioural metadata beyond the LLM-facing schema.
func (r *Registry) FullDefinitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, definitionForTool(t))
	}
	return defs
}

// Definitions returns the registered tools in the LLM-facing format.
func (r *Registry) Definitions() []llm.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, definitionForTool(t).LLMDefinition())
	}
	return defs
}

// DefinitionFor returns the single LLM-facing definition for the named
// tool, or false when the tool is not registered. Used as the resolver
// for the agentloop's permissive tool pool (deferred-tools rollout):
// the loop calls this when the model invokes a tool by name that isn't
// yet in this turn's pool, then adds the returned def before dispatch.
func (r *Registry) DefinitionFor(name string) (llm.ToolDefinition, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return llm.ToolDefinition{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return llm.ToolDefinition{}, false
	}
	return definitionForTool(t).LLMDefinition(), true
}

// DefinitionsFor returns only definitions whose names are in allowlist.
func (r *Registry) DefinitionsFor(allowlist []string) []llm.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]llm.ToolDefinition, 0, len(allowlist))
	seen := make(map[string]bool, len(allowlist))
	for _, name := range allowlist {
		if seen[name] {
			continue
		}
		seen[name] = true
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		defs = append(defs, definitionForTool(t).LLMDefinition())
	}
	return defs
}

// defaultToolExecTimeout is the upper bound the registry imposes on every
// tool call as defense-in-depth. Individual tools (execute_code, web_fetch)
// often install tighter deadlines first; this only kicks in for the worst
// case where a tool ignores ctx entirely (notably misbehaving MCP servers).
const defaultToolExecTimeout = 5 * time.Minute

// Execute dispatches a tool call by name.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	if name == "" {
		return "", errors.New("tool name is required")
	}

	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", errors.New("tool not found")
	}
	decision, capability, err := authorizeToolExecution(ctx, name, t)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn(
				"tool authorization denied",
				"tool", name,
				"capability", string(capability),
				"decision", string(decision.Decision),
				"reason", decision.Reason,
			)
		}
		return "", err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultToolExecTimeout)
		defer cancel()
	}

	start := time.Now()
	if r.logger != nil {
		r.logger.Info("tool started", "tool", name, "arg_keys", argKeys(args))
	}

	result, err := t.Execute(ctx, args)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		if r.logger != nil {
			// Log the error CLASS, not the raw message. Tool error strings
			// often wrap LLM-controlled values (source IDs, hostnames, paths)
			// and CLAUDE.md forbids logging those values. The LLM still sees
			// the full err via the tool result; logs see a stable enum.
			r.logger.Warn("tool failed", "tool", name, "elapsed", elapsed, "error_class", classifyToolError(err))
		}
		return "", err
	}

	if r.logger != nil {
		r.logger.Info("tool completed", "tool", name, "elapsed", elapsed, "bytes", len(result))
	}
	return result, nil
}

func authorizeToolExecution(ctx context.Context, name string, t Tool) (identity.AuthorizationDecision, identity.Capability, error) {
	capability := requiredCapabilityForTool(t)
	authorizer, ok := identity.AuthorizerFromContext(ctx)
	if !ok {
		return identity.AuthorizationDecision{
			Capability: capability,
			Resource:   identity.ResourceRef{Type: "tool", ID: name},
			Decision:   identity.DecisionDeny,
			Reason:     "authorizer_required",
		}, capability, fmt.Errorf("%w: tool %q requires identity authorizer", identity.ErrUnauthorized, name)
	}
	decision, err := authorizer.Authorize(ctx, identity.AuthorizeParams{
		ActorID:    identity.ActorIDFromContext(ctx),
		Capability: capability,
		Resource:   identity.ResourceRef{Type: "tool", ID: name},
		RunID:      identity.RunIDFromContext(ctx),
		EventID:    identity.EventIDFromContext(ctx),
	})
	if err != nil {
		return decision, capability, fmt.Errorf("tool authorization failed: %w", err)
	}
	if decision.Decision != identity.DecisionAllow {
		return decision, capability, fmt.Errorf("tool %q is not authorized for capability %q", name, capability)
	}
	return decision, capability, nil
}

// PrepareVectorReader wires the *ToolVectorIndex onto the registry as the
// SEARCH path. Writes are owned by toolindex.Reconciler (Wave 2.10.b+);
// this method just hooks up the query-side client so Registry.Search can
// run vector cosine against the collection the Reconciler maintains.
// When cfg.Backend is "fts" or the registry is nil, it is a no-op.
// Readiness errors are non-fatal: vector search degrades to FTS when the
// reader cannot be wired.
func (r *Registry) PrepareVectorReader(cfg ToolVectorConfig) {
	if r == nil || cfg.Backend == "fts" {
		return
	}
	idx := NewToolVectorIndex(cfg, r.logger)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := idx.Ready(ctx); err != nil {
		r.logger.Warn("tool vector reader not ready, falling back to fts", "error", err)
		return
	}
	r.SetVectorIndex(idx)
	r.logger.Info("tool vector reader wired (writes via toolindex.Reconciler)", "backend", cfg.Backend)
}

func (r *Registry) SetVectorIndex(idx *ToolVectorIndex) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vectorIndex = idx
}

func (r *Registry) ToolVectorHealth() ToolVectorHealth {
	if r == nil {
		return ToolVectorHealth{Backend: "fts", Fallback: true}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.vectorIndex == nil {
		return ToolVectorHealth{Backend: "fts", Fallback: true}
	}
	return r.vectorIndex.Health()
}

// sensitiveArgKeyRe matches argument key names that hint at credentials. The
// LLM-controlled MCP layer can advertise arbitrary keys; CLAUDE.md's
// "names + keys, never values" rule still leaks when the KEY itself is the
// secret (e.g. {"api_key": "..."} — logging arg_keys=["api_key"] confirms the
// LLM is moving a credential). We redact the name to "<redacted>" so the log
// records "a sensitive arg was present" without naming it.
var sensitiveArgKeyRe = regexp.MustCompile(`(?i)(?:^|[._-])(password|passwd|secret|token|api[_-]?key|auth|credential|bearer|session[_-]?id|cookie)(?:$|[._-])`)

func argKeys(args map[string]any) []string {
	keys := make([]string, 0, len(args))
	for key := range args {
		if sensitiveArgKeyRe.MatchString(key) {
			keys = append(keys, "<redacted>")
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func registryUniqueStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
