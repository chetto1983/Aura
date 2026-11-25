package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// refuseSchedule words a schedule refusal for the model: what is missing, the argument names that
// arrived (never their values, which can be long or private) and where the fix goes. A provider
// that drops an argument gives the model no other way to see it was dropped (measured 2026-11-25).
func refuseSchedule(missing error, raw json.RawMessage) error {
	return fmt.Errorf("%w. Received: %s. Put it in this same call; ask the operator only if their request does not already say it. No task was created",
		missing, receivedArguments(raw))
}

var errScheduleAtMissing = errors.New("task schedule: schedule_kind=at needs `at`, the time to fire as an RFC-3339 string such as 2030-01-01T09:30:00Z")

const (
	maxReceivedKeys   = 20
	maxReceivedKeyLen = 40
)

func receivedArguments(raw json.RawMessage) string {
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	return fmt.Sprintf("%s; payload: %s", receivedKeys(top), receivedPayload(top["payload"]))
}

func receivedPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "(none)"
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return "(not an object)"
	}
	if len(fields) == 0 {
		return "(empty object)"
	}
	return receivedKeys(fields)
}

// receivedKeys lists names the model chose, so each is cut and stripped of control characters and
// the list is capped: none of it can grow the refusal or forge text in it.
func receivedKeys(m map[string]json.RawMessage) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "(none)"
	}
	more := 0
	if len(keys) > maxReceivedKeys {
		more = len(keys) - maxReceivedKeys
		keys = keys[:maxReceivedKeys]
	}
	for i, k := range keys {
		keys[i] = safeKey(k)
	}
	list := strings.Join(keys, ", ")
	if more > 0 {
		list += fmt.Sprintf(", ... and %d more", more)
	}
	return list
}

func safeKey(k string) string {
	k = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, k)
	if r := []rune(k); len(r) > maxReceivedKeyLen {
		return string(r[:maxReceivedKeyLen]) + "..."
	}
	return k
}
