package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/chetto1983/aura/internal/cron"
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

// safeKey also strips U+2028 and U+2029, which are not control characters but end a line.
func safeKey(k string) string {
	k = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return '?'
		}
		return r
	}, k)
	return clip(k, maxReceivedKeyLen)
}

func clip(s string, maxRunes int) string {
	if r := []rune(s); len(r) > maxRunes {
		return string(r[:maxRunes]) + "..."
	}
	return s
}

// maxEngineReasonLen bounds the schedule engine's reason, which quotes the field's own value
// (cron, tz, at, schedule_kind): the model chose it and can make it as long as it likes.
const maxEngineReasonLen = 200

// engineReason is the engine's error cut to maxEngineReasonLen, still unwrapping to it so the
// refusal can tell which field it names.
type engineReason struct{ err error }

func (r engineReason) Error() string { return clip(r.err.Error(), maxEngineReasonLen) }
func (r engineReason) Unwrap() error { return r.err }

// refuseResolve words a resolveSchedule refusal from the arguments that arrived: a field that is
// absent gets the missing-field refusal, a field that arrived with a bad value is named as wrong
// so the model fixes the value instead of hunting for something to add.
func refuseResolve(a taskArgs, err error, raw json.RawMessage) error {
	field, present := wrongScheduleField(a, err)
	if !present {
		return refuseSchedule(err, raw)
	}
	return fmt.Errorf("%w. The value of `%s` is wrong, not missing; fix the value in this same call. No task was created", err, field)
}

func wrongScheduleField(a taskArgs, err error) (string, bool) {
	switch {
	case errors.Is(err, cron.ErrInvalidTimezone):
		return "tz", true
	case a.ScheduleKind == "at":
		return "at", true
	case a.ScheduleKind == "every":
		return "every_minutes", a.EveryMinutes != 0
	case a.ScheduleKind == "cron":
		return "cron", a.Cron != ""
	}
	return "schedule_kind", a.ScheduleKind != ""
}
