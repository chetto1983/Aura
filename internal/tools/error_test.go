package tools

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestFormatToolError_DefaultRetryable(t *testing.T) {
	result := FormatToolError(errors.New("schema validation failed: missing rows"))
	var te ToolError
	if err := json.Unmarshal([]byte(result), &te); err != nil {
		t.Fatalf("not valid JSON: %v (got %q)", err, result)
	}
	if te.OK {
		t.Error("OK should be false")
	}
	if !te.Retryable {
		t.Error("Retryable should be true by default")
	}
	if te.Error == "" {
		t.Error("Error should not be empty")
	}
	if te.Hint == "" {
		t.Error("Hint should not be empty")
	}
}

func TestFormatToolError_HintForMissing(t *testing.T) {
	result := FormatToolError(errors.New("missing required field 'rows'"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if te.Hint == "" {
		t.Fatal("expected a hint")
	}
	if !strings.Contains(te.Hint, "required field") {
		t.Errorf("hint should mention required field, got %q", te.Hint)
	}
}

func TestFormatToolError_HintForInvalid(t *testing.T) {
	result := FormatToolError(errors.New("invalid value for 'count'"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if !strings.Contains(te.Hint, "Fix the format") {
		t.Errorf("unexpected hint: %q", te.Hint)
	}
}

func TestFormatToolError_HintForNotFound(t *testing.T) {
	result := FormatToolError(errors.New("source not found"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if !strings.Contains(te.Hint, "exists") {
		t.Errorf("unexpected hint: %q", te.Hint)
	}
}

func TestFormatToolError_HintForTooLarge(t *testing.T) {
	result := FormatToolError(errors.New("too many rows"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if !strings.Contains(te.Hint, "Reduce") {
		t.Errorf("unexpected hint: %q", te.Hint)
	}
}

func TestFormatToolError_HintForTooManyTags(t *testing.T) {
	result := FormatToolError(errors.New("write_wiki: validation failed: wiki validation failed: too many tags (max 10)"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if te.Hint != "Retry with at most 10 short tags" {
		t.Fatalf("hint = %q", te.Hint)
	}
}

func TestFormatToolError_GenericHint(t *testing.T) {
	result := FormatToolError(errors.New("something unexpected happened"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if te.Hint == "" {
		t.Fatal("expected a generic hint")
	}
}

func TestFormatFatalToolError_NotRetryable(t *testing.T) {
	result := FormatFatalToolError(errors.New("permission denied"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if te.OK {
		t.Error("OK should be false")
	}
	if te.Retryable {
		t.Error("Retryable should be false for fatal errors")
	}
	if te.Error == "" {
		t.Error("Error should not be empty")
	}
	if te.Hint != "" {
		t.Error("Hint should be empty for fatal errors")
	}
}
