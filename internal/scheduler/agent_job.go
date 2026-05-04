package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/toolsets"
)

const (
	AgentJobWritePolicyProposeOnly = "propose_only"
)

var DefaultAgentJobTools = toolsets.SchedulerSafeTools()
var AgentJobAllowedTools = appendUniqueStrings(DefaultAgentJobTools, toolsets.MustResolveProfiles(toolsets.ProfileSkillsRead)...)

type AgentJobPayload struct {
	Goal            string   `json:"goal"`
	ToolAllowlist   []string `json:"tool_allowlist,omitempty"`
	EnabledToolsets []string `json:"enabled_toolsets,omitempty"`
	Skills          []string `json:"skills,omitempty"`
	ContextFrom     []string `json:"context_from,omitempty"`
	WakeIfChanged   []string `json:"wake_if_changed,omitempty"`
	WritePolicy     string   `json:"write_policy,omitempty"`
	Notify          *bool    `json:"notify,omitempty"`
}

func NormalizeAgentJobPayload(raw string) (AgentJobPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AgentJobPayload{}, errors.New("agent_job payload goal required")
	}
	var payload AgentJobPayload
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return AgentJobPayload{}, fmt.Errorf("parse agent_job payload: %w", err)
		}
	} else {
		payload.Goal = raw
	}
	payload.Goal = strings.TrimSpace(payload.Goal)
	if payload.Goal == "" {
		return AgentJobPayload{}, errors.New("agent_job payload goal required")
	}
	payload.EnabledToolsets = cleanUniqueStrings(payload.EnabledToolsets)
	payload.Skills = cleanUniqueStrings(payload.Skills)
	payload.ContextFrom = cleanUniqueStrings(payload.ContextFrom)
	payload.WakeIfChanged = cleanUniqueStrings(payload.WakeIfChanged)
	if len(payload.Skills) > 0 && !containsString(payload.EnabledToolsets, toolsets.ProfileSkillsRead) {
		payload.EnabledToolsets = append(payload.EnabledToolsets, toolsets.ProfileSkillsRead)
	}
	tools, err := ResolveAgentJobTools(payload.EnabledToolsets, payload.ToolAllowlist, len(payload.Skills) > 0)
	if err != nil {
		return AgentJobPayload{}, err
	}
	payload.ToolAllowlist = tools
	payload.WritePolicy = strings.TrimSpace(payload.WritePolicy)
	if payload.WritePolicy == "" {
		payload.WritePolicy = AgentJobWritePolicyProposeOnly
	}
	if payload.WritePolicy != AgentJobWritePolicyProposeOnly {
		return AgentJobPayload{}, fmt.Errorf("unsupported agent_job write_policy %q", payload.WritePolicy)
	}
	if payload.Notify == nil {
		notify := true
		payload.Notify = &notify
	}
	return payload, nil
}

func ResolveAgentJobTools(enabledToolsets []string, requestedTools []string, forceSkillsRead bool) ([]string, error) {
	enabledToolsets = cleanUniqueStrings(enabledToolsets)
	requestedTools = cleanUniqueStrings(requestedTools)

	var base []string
	var err error
	if len(enabledToolsets) > 0 {
		base, err = toolsets.ResolveProfiles(enabledToolsets...)
		if err != nil {
			return nil, fmt.Errorf("agent_job enabled_toolsets: %w", err)
		}
		base = toolsets.FilterAllowed(base, AgentJobAllowedTools)
		if len(base) == 0 {
			return nil, errors.New("agent_job enabled_toolsets have no tools allowed by scheduled-job perimeter")
		}
	} else {
		base = append([]string(nil), DefaultAgentJobTools...)
	}
	if forceSkillsRead {
		skillTools, err := toolsets.ResolveProfiles(toolsets.ProfileSkillsRead)
		if err != nil {
			return nil, fmt.Errorf("agent_job skills toolset: %w", err)
		}
		base = appendUniqueStrings(base, toolsets.FilterAllowed(skillTools, AgentJobAllowedTools)...)
	}

	if len(requestedTools) == 0 {
		return fallbackAgentJobTools(base), nil
	}
	filtered := toolsets.FilterAllowed(requestedTools, base)
	if forceSkillsRead {
		skillTools, err := toolsets.ResolveProfiles(toolsets.ProfileSkillsRead)
		if err != nil {
			return nil, fmt.Errorf("agent_job skills toolset: %w", err)
		}
		filtered = appendUniqueStrings(filtered, toolsets.FilterAllowed(skillTools, AgentJobAllowedTools)...)
	}
	if len(filtered) == 0 {
		if len(enabledToolsets) > 0 {
			return nil, errors.New("agent_job tool_allowlist has no tools allowed by enabled_toolsets")
		}
		return fallbackAgentJobTools(base), nil
	}
	return fallbackAgentJobTools(filtered), nil
}

func (p AgentJobPayload) JSON() (string, error) {
	normalized, err := NormalizeAgentJobPayload(mustMarshalPayload(p))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mustMarshalPayload(p AgentJobPayload) string {
	data, _ := json.Marshal(p)
	return string(data)
}

func cleanUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func appendUniqueStrings(out []string, values ...string) []string {
	seen := make(map[string]bool, len(out)+len(values))
	for _, value := range out {
		seen[value] = true
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func fallbackAgentJobTools(values []string) []string {
	if len(values) == 0 {
		return append([]string(nil), DefaultAgentJobTools...)
	}
	return append([]string(nil), values...)
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
