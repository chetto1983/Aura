package tools

import (
	"strings"
)

// FormatToolError converts a Go error into the tool result string the LLM
// reads back. Plain text — no JSON envelope, no "retryable" flag that invites
// a retry loop. If the error message alone is not enough for the model to
// recover, an inline hint is appended; otherwise the message stands.
//
// Design: tools report what happened, the LLM decides what to do. The runtime
// neither suggests "retry" nor classifies fatality — both proved to inflate
// loop iterations against tools that simply needed different arguments.
func FormatToolError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if hint := specificHint(msg); hint != "" {
		return "Error: " + msg + "\n\n" + hint
	}
	return "Error: " + msg
}

// FormatFatalToolError exists for callsite intent only. Same wire format as
// FormatToolError; the loop no longer distinguishes the two. Kept so call
// sites stay readable when the author knows the failure is non-recoverable.
func FormatFatalToolError(err error) string {
	if err == nil {
		return ""
	}
	return "Error: " + err.Error()
}

// specificHint returns information the LLM cannot infer from the message
// alone — typically "use tool X instead". Returns empty when the error text
// already tells the model what to fix.
func specificHint(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "is a directory"):
		return "Hint: this path is a directory. Use list_files to enumerate it, then read individual files."
	case strings.Contains(lower, "shell command failed") &&
		(strings.Contains(lower, "syntax error") ||
			strings.Contains(lower, "redirection") ||
			strings.Contains(lower, "sh:")):
		return "Hint: /bin/sh is dash, not bash. For non-trivial shell logic use execute_code with Python."
	}
	return ""
}
