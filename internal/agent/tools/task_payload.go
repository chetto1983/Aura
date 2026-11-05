package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// requireTaskPayload refuses a reminder with no text and an agent_job with no goal. A scheduled run
// starts in a fresh session where nobody can be asked, so the missing part must be asked for now:
// the reminder handler would otherwise deliver a generic line, and an empty job fails every run
// until the scheduler pauses it (measured 2026-11-05: payload={} paused after three failures).
func requireTaskPayload(kind string, payload json.RawMessage) error {
	var field, missing string
	switch kind {
	case "reminder":
		field, missing = "text", "a reminder needs payload.text, the words to deliver"
	case "agent_job":
		field, missing = "goal", "an agent_job needs payload.goal, a self-contained description of what to do when it runs"
	default:
		return nil
	}
	var fields map[string]any
	_ = json.Unmarshal(payload, &fields)
	if value, ok := fields[field].(string); ok && strings.TrimSpace(value) != "" {
		return nil
	}
	return fmt.Errorf("task schedule: %s. No task was created: ask the operator for it, then schedule again", missing)
}
