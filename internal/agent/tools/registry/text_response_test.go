package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// TestTextResponse_ParametersValidated verifies empty text returns ErrSchemaValidation.
func TestTextResponse_ParametersValidated(t *testing.T) {
	tool := &TextResponseTool{}

	_, err := tool.Execute(context.Background(), map[string]any{"text": ""})
	if err == nil {
		t.Fatal("Execute with empty text returned nil error, want ErrSchemaValidation")
	}
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("error = %v (%T), want wrapping llm.ErrSchemaValidation", err, err)
	}

	_, err = tool.Execute(context.Background(), map[string]any{"text": "   "})
	if err == nil {
		t.Fatal("Execute with whitespace-only text returned nil error, want ErrSchemaValidation")
	}
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("whitespace error = %v (%T), want wrapping llm.ErrSchemaValidation", err, err)
	}
}

// TestTextResponse_ReturnsTextVerbatim verifies Execute returns the text argument as-is.
func TestTextResponse_ReturnsTextVerbatim(t *testing.T) {
	tool := &TextResponseTool{}

	cases := []string{
		"Hello, world!",
		"Galileo Galilei è nato il 15 febbraio 1564 a Pisa.",
		"Multi\nline\nreply",
	}
	for _, text := range cases {
		got, err := tool.Execute(context.Background(), map[string]any{"text": text})
		if err != nil {
			t.Fatalf("Execute(%q) error = %v, want nil", text, err)
		}
		// Execute trims leading/trailing whitespace; trimmed input must match output.
		if got != text {
			t.Errorf("Execute(%q) = %q, want verbatim text", text, got)
		}
	}
}

// TestTextResponse_MarksTerminal verifies the tool name matches the IsTerminalTool
// registration in internal/agent/toolexec.go. A rename would break terminal behavior;
// this test makes the dependency explicit.
func TestTextResponse_MarksTerminal(t *testing.T) {
	tool := &TextResponseTool{}
	const terminalName = "text_response"
	if tool.Name() != terminalName {
		t.Fatalf("Name() = %q, want %q — must match IsTerminalTool entry in agent/toolexec.go",
			tool.Name(), terminalName)
	}
}

// TestTextResponse_RegisteredInCatalog verifies TextResponseTool has been explicitly
// catalogued with MCP hints and VisibilityTier (not in the all-defaults state).
func TestTextResponse_RegisteredInCatalog(t *testing.T) {
	tool := &TextResponseTool{}
	def := definitionForTool(tool)

	if isDefaultState(def) {
		t.Fatalf("TextResponseTool has default (uncatalogued) metadata — add Definition() with explicit hints + VisibilityTier")
	}
	if def.VisibilityTier != VisibilityAlwaysOn {
		t.Errorf("VisibilityTier = %q, want %q", def.VisibilityTier, VisibilityAlwaysOn)
	}
	if !def.ReadOnlyHint {
		t.Error("ReadOnlyHint = false, want true (text_response has no side effects)")
	}
}
