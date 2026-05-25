package agentdef

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	registry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
)

const overDelegationPrefix = "Use only when direct response/direct tools are insufficient. "

type DelegateRunner interface {
	Run(ctx context.Context, archetype, prompt, modelOverride string, parentChain []string) (string, error)
}

type ToolRunner interface {
	Names() []string
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

func WithArchetypeDelegates(activeArchetype string, reg *Registry, runner DelegateRunner, parentChain []string, existingNames []string, logger *slog.Logger) []registry.Tool {
	activeArchetype = strings.TrimSpace(activeArchetype)
	if activeArchetype == "" || reg == nil || runner == nil {
		return nil
	}
	parent, ok := reg.Get(activeArchetype)
	if !ok || len(parent.Subagents) == 0 {
		return nil
	}

	used := nameSet(existingNames)
	out := make([]registry.Tool, 0, len(parent.Subagents))
	for _, ref := range parent.Subagents {
		targetID := strings.TrimSpace(ref.ID)
		target, ok := reg.Get(targetID)
		if targetID == "" || !ok {
			continue
		}
		if chainContains(parentChain, targetID) {
			logDelegateSkip(logger, "cycle", activeArchetype, delegateToolName(ref), targetID)
			continue
		}
		name := delegateToolName(ref)
		if name == "" {
			continue
		}
		if used[name] {
			logDelegateSkip(logger, "collision", activeArchetype, name, targetID)
			continue
		}
		used[name] = true
		out = append(out, &delegateTool{
			name:       name,
			target:     target,
			runner:     runner,
			parentPath: append([]string(nil), parentChain...),
		})
	}
	return out
}

func DelegateFullDefinitions(tools []registry.Tool) []registry.ToolDefinition {
	defs := make([]registry.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		if provider, ok := tool.(registry.ToolDefinitionProvider); ok {
			defs = append(defs, provider.Definition())
			continue
		}
		defs = append(defs, registry.ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
	}
	return defs
}

func DelegateLLMDefinitions(tools []registry.Tool) []llm.ToolDefinition {
	full := DelegateFullDefinitions(tools)
	defs := make([]llm.ToolDefinition, 0, len(full))
	for _, def := range full {
		defs = append(defs, def.LLMDefinition())
	}
	return defs
}

func ResolveDelegateDefinition(tools []registry.Tool, name string) (llm.ToolDefinition, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return llm.ToolDefinition{}, false
	}
	for _, tool := range tools {
		if tool != nil && tool.Name() == name {
			if provider, ok := tool.(registry.ToolDefinitionProvider); ok {
				return provider.Definition().LLMDefinition(), true
			}
			return registry.ToolDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Parameters(),
			}.LLMDefinition(), true
		}
	}
	return llm.ToolDefinition{}, false
}

func AppendUniqueLLMDefinitions(base, extra []llm.ToolDefinition) []llm.ToolDefinition {
	return appendUniqueByName(base, extra, func(def llm.ToolDefinition) string { return def.Name })
}

func AppendUniqueFullDefinitions(base, extra []registry.ToolDefinition) []registry.ToolDefinition {
	return appendUniqueByName(base, extra, func(def registry.ToolDefinition) string { return def.Name })
}

func appendUniqueByName[T any](base, extra []T, nameOf func(T) string) []T {
	out := append([]T(nil), base...)
	seen := make(map[string]bool, len(out)+len(extra))
	for _, def := range out {
		name := strings.TrimSpace(nameOf(def))
		if name != "" {
			seen[name] = true
		}
	}
	for _, def := range extra {
		name := strings.TrimSpace(nameOf(def))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, def)
	}
	return out
}

func NewToolOverlay(base ToolRunner, delegates []registry.Tool) ToolRunner {
	return toolOverlay{base: base, delegates: delegatesByName(delegates)}
}

type delegateTool struct {
	name       string
	target     AgentDefinition
	runner     DelegateRunner
	parentPath []string
}

func (t *delegateTool) Name() string { return t.name }

func (t *delegateTool) Description() string {
	return overDelegationPrefix + strings.TrimSpace(t.target.WhenToUse)
}

func (t *delegateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Bounded task prompt for the delegated agent.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional per-call model override for the delegated agent.",
			},
		},
		"required":             []string{"prompt"},
		"additionalProperties": false,
	}
}

func (t *delegateTool) Definition() registry.ToolDefinition {
	return registry.ToolDefinition{
		Name:               t.Name(),
		Description:        t.Description(),
		Parameters:         t.Parameters(),
		RequiredCapability: identity.CapabilitySwarmSpawn,
		ReadOnlyHint:       false,
		DestructiveHint:    false,
		IdempotentHint:     false,
		OpenWorldHint:      true,
		VisibilityTier:     registry.VisibilityActiveTurn,
	}
}

func (t *delegateTool) RequiredCapability() identity.Capability {
	return identity.CapabilitySwarmSpawn
}

func (t *delegateTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.runner == nil {
		return "", errors.New("delegate runner unavailable")
	}
	if err := authorizeDelegateTool(ctx, t.name); err != nil {
		return "", err
	}
	prompt := strings.TrimSpace(stringArg(args, "prompt"))
	if prompt == "" {
		return "", errors.New("prompt is required")
	}
	model := strings.TrimSpace(stringArg(args, "model"))
	chain := append(append([]string(nil), t.parentPath...), t.target.ID)
	return t.runner.Run(ctx, t.target.ID, prompt, model, chain)
}

type toolOverlay struct {
	base      ToolRunner
	delegates map[string]registry.Tool
}

func (o toolOverlay) Names() []string {
	seen := map[string]bool{}
	var names []string
	if o.base != nil {
		for _, name := range o.base.Names() {
			name = strings.TrimSpace(name)
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	for name := range o.delegates {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (o toolOverlay) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	name = strings.TrimSpace(name)
	if tool, ok := o.delegates[name]; ok {
		return tool.Execute(ctx, args)
	}
	if o.base == nil {
		return "", errors.New("tool registry unavailable")
	}
	return o.base.Execute(ctx, name, args)
}

func authorizeDelegateTool(ctx context.Context, name string) error {
	authorizer, ok := identity.AuthorizerFromContext(ctx)
	if !ok {
		return fmt.Errorf("%w: tool %q requires identity authorizer", identity.ErrUnauthorized, name)
	}
	decision, err := authorizer.Authorize(ctx, identity.AuthorizeParams{
		ActorID:    identity.ActorIDFromContext(ctx),
		Capability: identity.CapabilitySwarmSpawn,
		Resource:   identity.ResourceRef{Type: "tool", ID: name},
		RunID:      identity.RunIDFromContext(ctx),
		EventID:    identity.EventIDFromContext(ctx),
	})
	if err != nil {
		return fmt.Errorf("delegate authorization failed: %w", err)
	}
	if decision.Decision != identity.DecisionAllow {
		return fmt.Errorf("%w: tool %q is not authorized for capability %q", identity.ErrUnauthorized, name, identity.CapabilitySwarmSpawn)
	}
	return nil
}

func delegateToolName(ref SubagentRef) string {
	if name := strings.TrimSpace(ref.DelegateName); name != "" {
		return name
	}
	id := strings.TrimSpace(ref.ID)
	if id == "" {
		return ""
	}
	return "delegate_" + id
}

func delegatesByName(tools []registry.Tool) map[string]registry.Tool {
	out := make(map[string]registry.Tool, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name())
		if name != "" {
			out[name] = tool
		}
	}
	return out
}

func nameSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func chainContains(chain []string, id string) bool {
	for _, item := range chain {
		if strings.TrimSpace(item) == id {
			return true
		}
	}
	return false
}

func logDelegateSkip(logger *slog.Logger, reason, parent, name, target string) {
	if logger == nil {
		return
	}
	logger.Warn("agentdef delegate skipped", "reason", reason, "parent", parent, "tool", name, "target", target)
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch value := args[key].(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		return ""
	}
}
