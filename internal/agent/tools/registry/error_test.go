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
	if !strings.Contains(got, `file(action="list")`) {
		t.Fatalf("expected file(action=list) hint, got %q", got)
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

// Retry-hint tests retired 2026-05-24 with the AURA_OP12* feature suite:
// the dormant precall validator + coercer + retry-hint path was deleted
// after probe data showed zero observable benefit over the per-tool
// ActionRequiredError + RewriteVerbKeyAsAction paths that already cover
// the live bug patterns.

func TestFormatToolErrorHandlesNil(t *testing.T) {
	if got := FormatToolError(nil); got != "" {
		t.Fatalf("nil error should produce empty string, got %q", got)
	}
	if got := FormatFatalToolError(nil); got != "" {
		t.Fatalf("nil error should produce empty string, got %q", got)
	}
}
