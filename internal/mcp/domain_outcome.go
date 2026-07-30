package mcp

import (
	"encoding/json"
	"strings"
)

func explicitDomainFailure(structured json.RawMessage, text string) bool {
	if hasExplicitDomainFailure(structured) {
		return true
	}
	return hasExplicitDomainFailure(json.RawMessage(strings.TrimSpace(text)))
}

func hasExplicitDomainFailure(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var outcome struct {
		Success *bool `json:"success"`
		Error   any   `json:"error"`
	}
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return false
	}
	if outcome.Success != nil && !*outcome.Success {
		return true
	}
	switch value := outcome.Error.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	default:
		return true
	}
}
