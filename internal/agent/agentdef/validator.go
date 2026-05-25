package agentdef

import (
	"fmt"
	"log/slog"
	"strings"
)

type Validator struct {
	EnforceTier bool
	Logger      *slog.Logger
}

func (v *Validator) Validate(defs []AgentDefinition) error {
	seen := map[string]struct{}{}
	tiers := map[string]AgentTier{}
	for _, def := range defs {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			return fmt.Errorf("agentdef: definition %s has empty id", def.Source)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("agentdef: duplicate id %q", id)
		}
		seen[id] = struct{}{}
		tier, ok := normalizeTier(def.Tier)
		if !ok {
			return fmt.Errorf("agentdef: %s has invalid tier %q", id, def.Tier)
		}
		if strings.TrimSpace(def.WhenToUse) == "" {
			return fmt.Errorf("agentdef: %s missing when_to_use", id)
		}
		if strings.TrimSpace(def.Prompt) == "" {
			return fmt.Errorf("agentdef: %s missing prompt", id)
		}
		if tier == TierWorker && len(def.Subagents) > 0 {
			return fmt.Errorf("agentdef: worker %s cannot declare subagents", id)
		}
		tiers[id] = tier
	}
	for _, def := range defs {
		parentID := strings.TrimSpace(def.ID)
		parentTier := tiers[parentID]
		for _, sub := range def.Subagents {
			childID := strings.TrimSpace(sub.ID)
			if childID == "" {
				return fmt.Errorf("agentdef: %s has subagent with empty id", parentID)
			}
			childTier, ok := tiers[childID]
			if !ok {
				return fmt.Errorf("agentdef: %s references unknown subagent %q", parentID, childID)
			}
			if childTier == parentTier {
				return fmt.Errorf("agentdef: %s cannot delegate to same-tier subagent %s", parentID, childID)
			}
		}
	}
	return nil
}

func normalizeTier(tier AgentTier) (AgentTier, bool) {
	switch tier {
	case "":
		return TierWorker, true
	case TierChat, TierReasoning, TierWorker:
		return tier, true
	default:
		return "", false
	}
}
