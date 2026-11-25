package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// refuseSchedule words a schedule refusal for the model: what is missing, the argument names that
// arrived (never their values, which can be long or private) and where the fix goes. A provider
// that drops an argument gives the model no other way to see it was dropped (measured 2026-11-25).
func refuseSchedule(missing error, raw json.RawMessage) error {
	return fmt.Errorf("%w. Received: %s. Put it in this same call; ask the operator only if their request does not already say it. No task was created",
		missing, receivedArguments(raw))
}

var errScheduleAtMissing = errors.New("task schedule: schedule_kind=at needs `at`, the time to fire as an RFC-3339 string such as 2030-01-01T09:30:00Z")

func receivedArguments(raw json.RawMessage) string {
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	var payload map[string]json.RawMessage
	_ = json.Unmarshal(top["payload"], &payload)
	payloadKeys := "(none)"
	if payload != nil {
		payloadKeys = sortedKeys(payload)
	}
	return fmt.Sprintf("%s; payload: %s", sortedKeys(top), payloadKeys)
}

func sortedKeys(m map[string]json.RawMessage) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "(none)"
	}
	return strings.Join(keys, ", ")
}
