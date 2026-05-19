package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// ClassifyToolError maps a tool error to a low-cardinality class label suitable
// for logging without leaking LLM-controlled values (URLs, source IDs, file
// paths, hostnames). CLAUDE.md is explicit: "only tool names and argument
// *keys* are logged — never values"; tool error messages were the gap.
//
// The returned labels form the 10-label vocabulary consumed by BucketOf in
// observation.go to collapse into 5 Outcome buckets for the Phase-6 loop.
func ClassifyToolError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, llm.ErrSchemaValidation) {
		return "validation"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not found"), strings.Contains(msg, "does not exist"), strings.Contains(msg, "no such"):
		return "not_found"
	case strings.Contains(msg, "validation"), strings.Contains(msg, "invalid"), strings.Contains(msg, "must be"), strings.Contains(msg, "must not"), strings.Contains(msg, "is required"):
		return "validation"
	case strings.Contains(msg, "timed out"), strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "denied"), strings.Contains(msg, "not allowed"):
		return "permission"
	case strings.Contains(msg, "blocked"), strings.Contains(msg, "ssrf"), strings.Contains(msg, "refusing to dial"):
		return "blocked"
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"):
		return "rate_limited"
	case strings.Contains(msg, "i/o"),
		strings.Contains(msg, "io error"),
		strings.Contains(msg, "read"),
		strings.Contains(msg, "write"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "network unreachable"):
		return "io"
	}
	return "error"
}

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
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return appendRetryHintIfEnabled(validationErr.Error())
	}
	msg := err.Error()
	var out string
	if hint := specificHint(msg); hint != "" {
		out = "Error: " + msg + "\n\n" + hint
	} else {
		out = "Error: " + msg
	}
	return appendRetryHintIfEnabled(out)
}

// FormatFatalToolError exists for callsite intent only. Same wire format as
// FormatToolError; the loop no longer distinguishes the two. Kept so call
// sites stay readable when the author knows the failure is non-recoverable.
func FormatFatalToolError(err error) string {
	if err == nil {
		return ""
	}
	return appendRetryHintIfEnabled("Error: " + err.Error())
}

const retryHintSuffix = " [Analyze the error above and try a different approach.]"

func appendRetryHintIfEnabled(content string) string {
	if !op12RetryHintEnabled() {
		return content
	}
	return appendRetryHint(content)
}

func appendRetryHint(content string) string {
	if strings.TrimSpace(content) == "" || strings.Contains(content, retryHintSuffix) {
		return content
	}
	return strings.TrimRight(content, " \t\r\n") + retryHintSuffix
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
