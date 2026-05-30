package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CurrentTime is a non-deferred builtin that returns the current instant as an
// RFC-3339 string. It is the ONLY path that reads the wall clock for the model
// (D-08): the live clock never enters the cached system prompt / messages[0], so
// the prompt prefix stays byte-stable and the KV cache is not poisoned.
type CurrentTime struct{}

type currentTimeArgs struct {
	Timezone string `json:"timezone"`
}

func (CurrentTime) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "timezone": {"type": "string", "description": "Optional IANA timezone name (e.g. 'Europe/Rome'). Defaults to 'UTC'."}
  }
}`)
	return Spec{
		Name:        "current_time",
		Summary:     "Get the current date and time as an RFC-3339 string.",
		Description: "Returns the current instant formatted as RFC-3339. With no argument it returns UTC; pass an IANA timezone name (e.g. 'Europe/Rome') to get the time in that zone with its UTC offset.",
		Parameters:  params,
		Deferred:    false,
	}
}

func (CurrentTime) Execute(_ context.Context, raw json.RawMessage) (ToolResult, error) {
	var a currentTimeArgs
	// An empty/absent body is valid (defaults to UTC); only a malformed JSON
	// object is an error.
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return ToolResult{}, fmt.Errorf("current_time args: %w", err)
		}
	}

	now := time.Now()
	if a.Timezone == "" || a.Timezone == "UTC" {
		s := now.UTC().Format(time.RFC3339)
		return ToolResult{Preview: s, Bytes: len(s)}, nil
	}

	loc, err := time.LoadLocation(a.Timezone)
	if err != nil {
		return ToolResult{}, fmt.Errorf("current_time: invalid timezone %q: %w", a.Timezone, err)
	}
	s := now.In(loc).Format(time.RFC3339)
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}
