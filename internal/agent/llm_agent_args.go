package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/canonicaljson"
)

// parseTextResponse validates the terminal-tool arguments (D-13). A malformed or
// empty payload is an error the loop feeds back to the model, never a panic.
func parseTextResponse(rawArgs string) (string, error) {
	var a struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &a); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	if strings.TrimSpace(a.Text) == "" {
		return "", fmt.Errorf("validation error: text_response.text is empty")
	}
	return a.Text, nil
}

// canonicalArgs canonicalizes a tool call's JSON arguments for the dedup
// fingerprint (the budget's caller-canonicalizes contract, B2). A non-JSON or
// empty payload falls back to the raw bytes so a malformed-arg storm still dedups
// on identical raw input.
func canonicalArgs(rawArgs string) []byte {
	var v any
	if err := json.Unmarshal([]byte(rawArgs), &v); err != nil {
		return []byte(rawArgs)
	}
	canon, err := canonicaljson.Marshal(v)
	if err != nil {
		return []byte(rawArgs)
	}
	return canon
}
