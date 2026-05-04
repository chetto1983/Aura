package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/skills"
)

const maxSkillProposalContentChars = 32000

var (
	skillProposalNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	toolNameRE          = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

// ProposeSkillChangeTool inserts a pending procedural-memory proposal into
// the same human review queue used by wiki proposals. It never writes skill
// files; installation remains an explicit reviewed/admin action.
type ProposeSkillChangeTool struct {
	store *summarizer.SummariesStore
}

func NewProposeSkillChangeTool(store *summarizer.SummariesStore) *ProposeSkillChangeTool {
	if store == nil {
		return nil
	}
	return &ProposeSkillChangeTool{store: store}
}

func (t *ProposeSkillChangeTool) Name() string { return "propose_skill_change" }

func (t *ProposeSkillChangeTool) Description() string {
	return "Propose a skill create/update/delete for human review. Use this when repeated work should become procedural memory. Requires a complete SKILL.md draft for create/update and one smoke prompt; never mutates local skill files directly."
}

func (t *ProposeSkillChangeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "update", "delete"},
				"description": "Skill proposal action. create/update require content and smoke_prompt; delete requires reason.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name. Use lowercase kebab/snake style when possible; only letters, numbers, underscore, and hyphen are accepted.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Short routing description. Optional if the SKILL.md frontmatter contains description.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Complete SKILL.md draft for create/update, including YAML frontmatter, trigger guidance, constraints, examples, and workflow. Max 32k chars.",
			},
			"allowed_tools": map[string]any{
				"type":        "array",
				"description": "Optional tool names the skill expects, used for review and future smoke tests.",
				"items":       map[string]any{"type": "string"},
			},
			"smoke_prompt": map[string]any{
				"type":        "string",
				"description": "One realistic prompt that should route to this skill after approval/install.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this procedural memory should be created, updated, or deleted.",
			},
			"source_turn_ids": map[string]any{
				"type":        "array",
				"description": "Optional archived conversation turn IDs that support this proposal.",
				"items":       map[string]any{"type": "integer"},
			},
			"evidence": map[string]any{
				"type":        "array",
				"description": "Optional compact evidence refs, usually copied from search_memory Evidence envelope.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":    map[string]any{"type": "string"},
						"id":      map[string]any{"type": "string"},
						"title":   map[string]any{"type": "string"},
						"page":    map[string]any{"type": "integer"},
						"snippet": map[string]any{"type": "string"},
					},
					"required": []string{"kind", "id"},
				},
			},
			"origin_tool": map[string]any{
				"type":        "string",
				"description": "Optional tool or routine that discovered the proposal, e.g. search_memory, daily_briefing, agent_job, aurabot_swarm.",
			},
			"origin_reason": map[string]any{
				"type":        "string",
				"description": "Optional short reason this should become procedural memory.",
			},
			"agent_job_id": map[string]any{
				"type":        "string",
				"description": "Optional scheduled agent job name/id that produced this proposal.",
			},
			"swarm_run_id": map[string]any{
				"type":        "string",
				"description": "Optional AuraBot swarm run ID that produced this proposal.",
			},
			"swarm_task_id": map[string]any{
				"type":        "string",
				"description": "Optional AuraBot swarm task ID that produced this proposal.",
			},
			"confidence": map[string]any{
				"type":        "number",
				"description": "Optional confidence from 0 to 1. Defaults to 1.",
			},
		},
		"required": []string{"action", "name"},
	}
}

func (t *ProposeSkillChangeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("propose_skill_change: review store unavailable")
	}
	action, err := requiredString(args, "action")
	if err != nil {
		return "", err
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if !skillProposalNameRE.MatchString(name) {
		return "", fmt.Errorf("propose_skill_change: invalid skill name %q", name)
	}

	normalizedAction, err := normalizeSkillProposalAction(action)
	if err != nil {
		return "", err
	}

	description := strings.TrimSpace(stringArg(args, "description"))
	content := strings.TrimSpace(stringArg(args, "content"))
	smokePrompt := strings.TrimSpace(stringArg(args, "smoke_prompt"))
	reason := strings.TrimSpace(stringArg(args, "reason"))
	allowedTools, err := cleanAllowedTools(stringSliceArg(args, "allowed_tools"))
	if err != nil {
		return "", err
	}

	if normalizedAction == summarizer.ActionSkillCreate || normalizedAction == summarizer.ActionSkillUpdate {
		if content == "" {
			return "", fmt.Errorf("propose_skill_change: content is required for %s", action)
		}
		if len(content) > maxSkillProposalContentChars {
			return "", fmt.Errorf("propose_skill_change: content exceeds %d chars", maxSkillProposalContentChars)
		}
		parsed, err := skills.ParseSkill([]byte(content))
		if err != nil {
			return "", fmt.Errorf("propose_skill_change: invalid SKILL.md: %w", err)
		}
		if parsed.Name != name {
			return "", fmt.Errorf("propose_skill_change: SKILL.md name %q does not match proposal name %q", parsed.Name, name)
		}
		if description == "" {
			description = parsed.Description
		}
		if description == "" {
			return "", fmt.Errorf("propose_skill_change: description is required")
		}
		if smokePrompt == "" {
			return "", fmt.Errorf("propose_skill_change: smoke_prompt is required for %s", action)
		}
	}
	if normalizedAction == summarizer.ActionSkillDelete && reason == "" {
		return "", fmt.Errorf("propose_skill_change: reason is required for delete")
	}

	originTool := strings.TrimSpace(stringArg(args, "origin_tool"))
	evidence := evidenceRefsArg(args, "evidence")
	if originTool == "search_memory" && len(evidence) == 0 {
		return "", fmt.Errorf("propose_skill_change: evidence refs are required when origin_tool is search_memory; copy compact refs from the search_memory Evidence envelope")
	}

	skillProposal := &summarizer.SkillProposal{
		Action:       strings.TrimPrefix(string(normalizedAction), "skill_"),
		Name:         name,
		Description:  description,
		AllowedTools: allowedTools,
		SmokePrompt:  smokePrompt,
		Content:      content,
		Reason:       reason,
	}
	proposal, err := t.store.Propose(ctx, summarizer.ProposalInput{
		ChatID:        userIDAsInt64(ctx),
		Fact:          formatSkillProposalFact(skillProposal),
		Action:        normalizedAction.String(),
		Similarity:    numberArg(args, "confidence"),
		SourceTurnIDs: int64SliceArg(args, "source_turn_ids"),
		Category:      "skill",
		Provenance: summarizer.Provenance{
			OriginTool:   originTool,
			OriginReason: strings.TrimSpace(stringArg(args, "origin_reason")),
			ProposalKind: "skill",
			Evidence:     evidence,
			Skill:        skillProposal,
			AgentJobID:   strings.TrimSpace(stringArg(args, "agent_job_id")),
			SwarmRunID:   strings.TrimSpace(stringArg(args, "swarm_run_id")),
			SwarmTaskID:  strings.TrimSpace(stringArg(args, "swarm_task_id")),
		},
	})
	if err != nil {
		return "", fmt.Errorf("propose_skill_change: %w", err)
	}
	resp := proposeSkillChangeResponse{
		OK:         true,
		ID:         proposal.ID,
		Status:     proposal.Status,
		Action:     proposal.Action,
		Name:       name,
		ReviewPath: "/summaries",
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("propose_skill_change: marshal response: %w", err)
	}
	return string(out), nil
}

type proposeSkillChangeResponse struct {
	OK         bool   `json:"ok"`
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Action     string `json:"action"`
	Name       string `json:"name"`
	ReviewPath string `json:"review_path"`
}

func normalizeSkillProposalAction(action string) (summarizer.Action, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "create":
		return summarizer.ActionSkillCreate, nil
	case "update":
		return summarizer.ActionSkillUpdate, nil
	case "delete":
		return summarizer.ActionSkillDelete, nil
	default:
		return "", fmt.Errorf("propose_skill_change: unsupported action %q", action)
	}
}

func cleanAllowedTools(values []string) ([]string, error) {
	values = cleanStrings(values)
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !toolNameRE.MatchString(value) {
			return nil, fmt.Errorf("propose_skill_change: invalid allowed tool %q", value)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		if len(out) >= 32 {
			return nil, fmt.Errorf("propose_skill_change: allowed_tools is limited to 32")
		}
		out = append(out, value)
	}
	return out, nil
}

func formatSkillProposalFact(p *summarizer.SkillProposal) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Skill proposal: %s `%s`", p.Action, p.Name)
	if p.Description != "" {
		fmt.Fprintf(&sb, "\n\nDescription: %s", p.Description)
	}
	if p.Reason != "" {
		fmt.Fprintf(&sb, "\n\nReason: %s", p.Reason)
	}
	if len(p.AllowedTools) > 0 {
		fmt.Fprintf(&sb, "\n\nAllowed tools: %s", strings.Join(p.AllowedTools, ", "))
	}
	if p.SmokePrompt != "" {
		fmt.Fprintf(&sb, "\n\nSmoke prompt: %s", p.SmokePrompt)
	}
	if p.Content != "" {
		fmt.Fprintf(&sb, "\n\n```markdown\n%s\n```", p.Content)
	}
	return sb.String()
}
