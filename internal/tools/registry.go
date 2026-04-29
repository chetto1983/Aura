package tools

import (
	"context"
	"errors"
	"log/slog"
	"sort"
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

// Registry stores tools and dispatches tool calls by name.
type Registry struct {
	mu     sync.RWMutex
	tools  map[string]Tool
	logger *slog.Logger
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

// Definitions returns the registered tools in the LLM-facing format.
func (r *Registry) Definitions() []llm.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, llm.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return defs
}

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

	start := time.Now()
	if r.logger != nil {
		r.logger.Info("tool started", "tool", name, "arg_keys", argKeys(args))
	}

	result, err := t.Execute(ctx, args)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("tool failed", "tool", name, "elapsed", elapsed, "error", err)
		}
		return "", err
	}

	if r.logger != nil {
		r.logger.Info("tool completed", "tool", name, "elapsed", elapsed, "bytes", len(result))
	}
	return result, nil
}

func argKeys(args map[string]any) []string {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
