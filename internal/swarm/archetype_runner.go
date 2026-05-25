package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/agent/agentdef"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
)

type ArchetypeRunner struct {
	Manager  RunRunner
	Registry *agentdef.Registry
}

func (r *ArchetypeRunner) Run(ctx context.Context, archetype, prompt, modelOverride string, parentChain []string) (string, error) {
	if r == nil || r.Manager == nil {
		return "", errors.New("agentdef delegate: swarm manager unavailable")
	}
	if r.Registry == nil {
		return "", errors.New("agentdef delegate: registry unavailable")
	}
	archetype = strings.TrimSpace(archetype)
	prompt = strings.TrimSpace(prompt)
	if archetype == "" {
		return "", errors.New("agentdef delegate: archetype is required")
	}
	if prompt == "" {
		return "", errors.New("agentdef delegate: prompt is required")
	}
	def, ok := r.Registry.Get(archetype)
	if !ok {
		return "", fmt.Errorf("agentdef delegate: unknown archetype %q", archetype)
	}

	chain := append([]string(nil), parentChain...)
	depth := len(chain) - 1
	if depth < 0 {
		depth = 0
	}
	constraints, err := json.Marshal(map[string]any{
		"archetype":      def.ID,
		"parent_chain":   chain,
		"tool_allowlist": def.Tools.Named,
	})
	if err != nil {
		return "", fmt.Errorf("agentdef delegate constraints: %w", err)
	}

	result, err := r.Manager.Run(ctx, RunRequest{
		Goal:      "delegate " + def.ID,
		CreatedBy: tools.UserIDFromContext(ctx),
		Assignments: []Assignment{{
			Role:                      def.ID,
			Subject:                   "delegate",
			Prompt:                    prompt,
			SystemPrompt:              def.Prompt,
			ToolAllowlist:             def.Tools.Named,
			Depth:                     depth,
			UserID:                    tools.UserIDFromContext(ctx),
			Temperature:               def.Temperature,
			MaxIterations:             def.MaxIterations,
			MaxToolResultChars:        def.MaxResultChars,
			CompleteOnDeadline:        true,
			DelegatedCapabilities:     delegatedCapabilities(def),
			DelegationConstraintsJSON: string(constraints),
			Archetype:                 def.ID,
			ModelOverride:             strings.TrimSpace(modelOverride),
			ParentChain:               chain,
			MaxInputTokens:            def.MaxInputTokens,
			MaxOutputTokens:           def.MaxOutputTokens,
		}},
	})
	if err != nil {
		return "", err
	}
	for _, task := range result.Tasks {
		if strings.TrimSpace(task.Result) != "" {
			return task.Result, nil
		}
		if strings.TrimSpace(task.LastError) != "" {
			return "", errors.New(task.LastError)
		}
	}
	return SynthesizeRunResult(result).Summary, nil
}

func delegatedCapabilities(def agentdef.AgentDefinition) []identity.Capability {
	if len(def.Tools.Named) == 0 {
		return nil
	}
	return []identity.Capability{identity.CapabilityToolExecute}
}
