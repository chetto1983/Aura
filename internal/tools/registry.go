package tools

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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

// Register adds or replaces a tool.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns a registered tool by name.
func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
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

// BuildVectorIndex creates and populates a vector search index from the
// currently registered tools. When cfg.Backend is "fts" or the registry
// is nil, it is a no-op. Readiness and build errors are non-fatal: vector
// search degrades gracefully to FTS when the index is unavailable.
func (r *Registry) BuildVectorIndex(cfg ToolVectorConfig) {
	if r == nil || cfg.Backend == "fts" {
		return
	}
	idx := NewToolVectorIndex(cfg, r.logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := idx.Ready(ctx); err != nil {
		r.logger.Warn("tool vector index not ready, falling back to fts", "error", err)
		return
	}

	r.mu.RLock()
	docs := make([]toolVectorDoc, 0, len(r.tools))
	for _, t := range r.tools {
		def := definitionForTool(t)
		_ = toolCategories(t) // tags no longer used for embedding (D-24)
		docs = append(docs, toolVectorDoc{
			name: def.Name,
			text: searchableToolEmbeddingText(def), // D-24: narrow embedding text
		})
	}
	r.mu.RUnlock()

	if err := idx.Build(ctx, docs); err != nil {
		r.logger.Warn("tool vector index build failed, falling back to fts", "error", err)
		return
	}

	r.SetVectorIndex(idx)
	r.logger.Info("tool vector index ready", "backend", cfg.Backend, "docs", len(docs))
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
