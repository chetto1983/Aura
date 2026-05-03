package tools

import (
	"encoding/json"
	"strings"
)

// ToolError is the structured format returned to the LLM when a tool call
// fails. The LLM reads retryable + hint to decide whether to self-correct.
type ToolError struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
	Hint      string `json:"hint,omitempty"`
}

// FormatToolError converts a Go error into a JSON tool-error result string.
// Default classification: retryable=true with a generic hint. Callers that
// know the error is fatal (permission denied, disk full) should use
// FormatFatalToolError instead.
func FormatToolError(err error) string {
	msg := err.Error()
	te := ToolError{
		OK:        false,
		Error:     msg,
		Retryable: true,
		Hint:      hintForError(msg),
	}
	b, _ := json.Marshal(te)
	return string(b)
}

// FormatFatalToolError converts a non-retryable Go error.
func FormatFatalToolError(err error) string {
	te := ToolError{
		OK:        false,
		Error:     err.Error(),
		Retryable: false,
	}
	b, _ := json.Marshal(te)
	return string(b)
}

// hintForError returns a short hint based on the error message content.
// If no pattern matches, returns a generic retry hint.
func hintForError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "too many tags"):
		return "Retry with at most 10 short tags"
	case strings.Contains(lower, "too many sources"):
		return "Retry with at most 10 source URLs or references"
	case strings.Contains(lower, "missing") || strings.Contains(lower, "required"):
		return "Provide the required field mentioned in the error"
	case strings.Contains(lower, "invalid") || strings.Contains(lower, "malformed"):
		return "Fix the format of the argument mentioned in the error"
	case strings.Contains(lower, "not found"):
		return "Check whether the referenced resource exists before retrying"
	case strings.Contains(lower, "too large") || strings.Contains(lower, "too many"):
		return "Reduce the size or count mentioned in the error"
	default:
		return "Correct your arguments and retry the tool call once"
	}
}
