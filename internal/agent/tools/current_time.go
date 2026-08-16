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
type CurrentTime struct {
	// Location is the zone an argument-free call answers in. Nil means UTC, which is what
	// this tool did unconditionally until 2026-08-16 -- see clock.go for what that cost.
	Location *time.Location
}

type currentTimeArgs struct {
	Timezone string `json:"timezone"`
}

func (CurrentTime) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "timezone": {"type": "string", "description": "Optional IANA timezone name (e.g. 'Europe/Rome'). Defaults to the operator's own timezone, already converted -- do NOT convert the result yourself."}
  }
}`)
	return Spec{
		Name:        "current_time",
		Summary:     "Get the current date and time as an RFC-3339 string.",
		Description: "Returns the current instant formatted as RFC-3339 IN THE OPERATOR'S TIMEZONE, with the UTC offset and the zone name (e.g. '2026-08-16T17:49:04+02:00 (Europe/Rome, CEST)'). The conversion is already done: report the time as given and never recompute an offset. Pass an IANA timezone name only to ask about a DIFFERENT zone.",
		Parameters:  params,
		// Deferred: barato per 103 token e usato di rado. Il modello lo trova per nome.
		Deferred: true,
	}
}

func (c CurrentTime) Execute(_ context.Context, raw json.RawMessage) (ToolResult, error) {
	var a currentTimeArgs
	// An empty/absent body is valid (defaults to UTC); only a malformed JSON
	// object is an error.
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return ToolResult{}, fmt.Errorf("current_time args: %w", err)
		}
	}

	now := time.Now()
	if a.Timezone == "" {
		s := FormatClock(now, c.Location)
		return ToolResult{Preview: s, Bytes: len(s)}, nil
	}

	// An explicitly named zone is a real question about elsewhere, so an unknown name is a
	// real error rather than a silent fallback: answering Tokyo's question with Rome's
	// clock would be worse than saying the name is wrong.
	loc, err := time.LoadLocation(a.Timezone)
	if err != nil {
		return ToolResult{}, fmt.Errorf("current_time: invalid timezone %q: %w", a.Timezone, err)
	}
	s := FormatClock(now, loc)
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}
