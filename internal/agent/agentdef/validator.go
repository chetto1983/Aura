package agentdef

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

type Validator struct {
	EnforceTier bool
	Logger      *slog.Logger
}

func (v *Validator) Validate(defs []AgentDefinition) error {
	fatal, policy := v.collectIssues(defs)
	if len(fatal) > 0 {
		return validationError(fatal)
	}
	if len(policy) == 0 {
		return nil
	}
	if v.EnforceTier {
		return validationError(policy)
	}
	v.logWarnings(policy)
	return nil
}

type validationIssue struct {
	rule    string
	id      string
	message string
}

func (v *Validator) collectIssues(defs []AgentDefinition) ([]validationIssue, []validationIssue) {
	seen := map[string]struct{}{}
	tiers := map[string]AgentTier{}
	refs := map[string][]string{}
	var fatal []validationIssue
	var policy []validationIssue

	for _, def := range defs {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			fatal = append(fatal, validationIssue{"empty_id", def.Source, "definition has empty id"})
			continue
		}
		if _, ok := seen[id]; ok {
			fatal = append(fatal, validationIssue{"duplicate_id", id, "duplicate id"})
			continue
		}
		seen[id] = struct{}{}
		tier, ok := normalizeTier(def.Tier)
		if def.Tier == "" {
			policy = append(policy, validationIssue{"missing_tier_default_worker", id, "missing tier defaults to worker"})
		}
		if !ok {
			policy = append(policy, validationIssue{"invalid_tier", id, fmt.Sprintf("invalid tier %q", def.Tier)})
			tier = TierWorker
		}
		if strings.TrimSpace(def.WhenToUse) == "" {
			policy = append(policy, validationIssue{"missing_when_to_use", id, "missing when_to_use"})
		}
		if strings.TrimSpace(def.Prompt) == "" {
			policy = append(policy, validationIssue{"missing_prompt", id, "missing prompt"})
		}
		if tier == TierWorker && len(def.Subagents) > 0 {
			policy = append(policy, validationIssue{"worker_has_subagents", id, "worker cannot declare subagents"})
		}
		tiers[id] = tier
		for _, sub := range def.Subagents {
			childID := strings.TrimSpace(sub.ID)
			if childID != "" {
				refs[id] = append(refs[id], childID)
			}
		}
	}
	for _, def := range defs {
		parentID := strings.TrimSpace(def.ID)
		parentTier := tiers[parentID]
		for _, sub := range def.Subagents {
			childID := strings.TrimSpace(sub.ID)
			if childID == "" {
				policy = append(policy, validationIssue{"empty_subagent_id", parentID, "subagent has empty id"})
				continue
			}
			childTier, ok := tiers[childID]
			if !ok {
				policy = append(policy, validationIssue{"unknown_subagent", parentID, fmt.Sprintf("references unknown subagent %q", childID)})
				continue
			}
			if childTier == parentTier {
				policy = append(policy, validationIssue{"same_tier_delegation", parentID, fmt.Sprintf("cannot delegate to same-tier subagent %s", childID)})
			}
		}
	}
	for root := range refs {
		if cycle := detectCycle(refs, root); len(cycle) > 0 {
			policy = append(policy, validationIssue{"cycle", root, "cycle detected: " + strings.Join(cycle, " -> ")})
		}
	}
	return fatal, policy
}

func (v *Validator) logWarnings(issues []validationIssue) {
	if v == nil || v.Logger == nil {
		return
	}
	for _, issue := range issues {
		v.Logger.Warn("agentdef validation warning", "rule", issue.rule, "id", issue.id, "message", issue.message)
	}
}

func validationError(issues []validationIssue) error {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("%s:%s: %s", issue.rule, issue.id, issue.message))
	}
	return fmt.Errorf("agentdef: %d validation errors: %s", len(issues), strings.Join(parts, "; "))
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

func detectCycle(refs map[string][]string, root string) []string {
	var stack []string
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string) []string
	walk = func(id string) []string {
		if visiting[id] {
			idx := slices.Index(stack, id)
			if idx < 0 {
				return append([]string{id}, id)
			}
			return append(append([]string(nil), stack[idx:]...), id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		stack = append(stack, id)
		for _, next := range refs[id] {
			if cycle := walk(next); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		visiting[id] = false
		visited[id] = true
		return nil
	}
	return walk(root)
}
