package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatToolErrorReturnsPlainText(t *testing.T) {
	got := FormatToolError(errors.New("schema validation failed: missing rows"))
	if strings.Contains(got, "{") || strings.Contains(got, `"ok"`) || strings.Contains(got, "retryable") {
		t.Fatalf("expected plain text, got JSON-looking output: %q", got)
	}
	if !strings.HasPrefix(got, "Error: ") {
		t.Fatalf("expected Error: prefix, got %q", got)
	}
	if !strings.Contains(got, "schema validation failed") {
		t.Fatalf("expected message body to be preserved, got %q", got)
	}
}

func TestFormatToolErrorPreservesUnderlyingMessage(t *testing.T) {
	for _, msg := range []string{
		"missing required field 'rows'",
		"invalid value for 'count'",
		"source not found",
		"too many rows",
		"something unexpected happened",
	} {
		got := FormatToolError(errors.New(msg))
		if !strings.Contains(got, msg) {
			t.Errorf("missing body for %q in %q", msg, got)
		}
	}
}

func TestFormatToolErrorDirectoryHint(t *testing.T) {
	got := FormatToolError(errors.New("read_file: workspace: wiki is a directory"))
	if !strings.Contains(got, "list_files") {
		t.Fatalf("expected list_files hint, got %q", got)
	}
}

func TestFormatToolErrorShellRedirectionHint(t *testing.T) {
	got := FormatToolError(errors.New("shell command failed (exit=2): /bin/sh: 26: Syntax error: redirection unexpected"))
	if !strings.Contains(got, "execute_code") {
		t.Fatalf("expected execute_code hint, got %q", got)
	}
}

func TestFormatToolErrorOmitsHintWhenNoneApplies(t *testing.T) {
	got := FormatToolError(errors.New("generic failure"))
	// Plain message, no double newline indicating an appended hint block.
	if strings.Contains(got, "\n\n") {
		t.Fatalf("expected no hint block, got %q", got)
	}
}

func TestFormatFatalToolErrorReturnsPlainText(t *testing.T) {
	got := FormatFatalToolError(errors.New("permission denied"))
	if strings.Contains(got, "{") || strings.Contains(got, "retryable") {
		t.Fatalf("expected plain text, got %q", got)
	}
	if !strings.Contains(got, "permission denied") {
		t.Fatalf("body lost, got %q", got)
	}
}

func TestFormatToolErrorRetryHintEnabled(t *testing.T) {
	t.Setenv("AURA_OP12B_RETRY_HINT_ENABLED", "true")

	got := FormatToolError(errors.New("backend unavailable"))
	if !strings.HasSuffix(got, retryHintSuffix) {
		t.Fatalf("expected retry hint suffix, got %q", got)
	}
}

func TestFormatToolErrorRetryHintOnValidationError(t *testing.T) {
	t.Setenv("AURA_OP12B_RETRY_HINT_ENABLED", "true")

	got := FormatToolError(&ValidationError{
		Tool: "fake",
		Result: ValidationResult{
			Valid:       false,
			Missing:     []string{"query"},
			InvalidType: []TypeMismatch{},
			InvalidEnum: []EnumMismatch{},
			Hint:        "provide query",
			Recoverable: true,
		},
	})
	if !strings.Contains(got, `"validation_error"`) || !strings.HasSuffix(got, retryHintSuffix) {
		t.Fatalf("expected structured validation payload with retry hint, got %q", got)
	}
}

func TestFormatToolErrorRetryHintIdempotent(t *testing.T) {
	t.Setenv("AURA_OP12B_RETRY_HINT_ENABLED", "true")

	got := FormatToolError(errors.New("backend unavailable" + retryHintSuffix))
	if strings.Count(got, retryHintSuffix) != 1 {
		t.Fatalf("expected one retry hint suffix, got %q", got)
	}
}

func TestFormatFatalToolErrorRetryHintEnabled(t *testing.T) {
	t.Setenv("AURA_OP12B_RETRY_HINT_ENABLED", "true")

	got := FormatFatalToolError(errors.New("permission denied"))
	if !strings.HasSuffix(got, retryHintSuffix) {
		t.Fatalf("expected retry hint suffix, got %q", got)
	}
}

func TestFormatToolErrorHandlesNil(t *testing.T) {
	if got := FormatToolError(nil); got != "" {
		t.Fatalf("nil error should produce empty string, got %q", got)
	}
	if got := FormatFatalToolError(nil); got != "" {
		t.Fatalf("nil error should produce empty string, got %q", got)
	}
}
