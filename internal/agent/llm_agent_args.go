package agent

import (
	"encoding/json"
	"fmt"
	"strings"
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

// normalizeContentStopAnswer strips a text_response payload when a provider emits
// the terminal-tool arguments as plain content instead of a structured tool call.
func normalizeContentStopAnswer(raw string) string {
	answer, ok := parseTextResponsePayload(raw)
	if !ok {
		return raw
	}
	return answer
}

func parseTextResponsePayload(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return "", false
	}
	if len(obj) != 1 {
		return "", false
	}
	rawText, ok := obj["text"]
	if !ok {
		return "", false
	}
	var text string
	if err := json.Unmarshal(rawText, &text); err != nil {
		return "", false
	}
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}
