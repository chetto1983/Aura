package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	AgentJobWritePolicyProposeOnly = "propose_only"
)

var DefaultAgentJobTools = []string{
	"list_wiki",
	"read_wiki",
	"search_memory",
	"search_wiki",
	"list_sources",
	"read_source",
	"lint_sources",
	"web_search",
	"web_fetch",
	"propose_wiki_change",
	"propose_skill_change",
}

type AgentJobPayload struct {
	Goal          string   `json:"goal"`
	ToolAllowlist []string `json:"tool_allowlist,omitempty"`
	WritePolicy   string   `json:"write_policy,omitempty"`
	Notify        *bool    `json:"notify,omitempty"`
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
	payload.ToolAllowlist = cleanUniqueStrings(payload.ToolAllowlist)
	if len(payload.ToolAllowlist) == 0 {
		payload.ToolAllowlist = append([]string(nil), DefaultAgentJobTools...)
	}
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
