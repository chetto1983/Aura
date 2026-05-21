package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestAskUserClarification_Parameters(t *testing.T) {
	tool := &AskUserClarificationTool{}
	if tool.Name() != "ask_user_clarification" {
		t.Fatalf("Name = %q, want ask_user_clarification", tool.Name())
	}
	params := tool.Parameters()
	if params == nil {
		t.Fatal("Parameters is nil")
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("parameters.properties missing or wrong type: %T", params["properties"])
	}
	for _, field := range []string{"question", "options", "free_text_allowed"} {
		if _, ok := props[field]; !ok {
			t.Errorf("parameters.properties missing field %q", field)
		}
	}
	req, ok := params["required"].([]string)
	if !ok || len(req) != 1 || req[0] != "question" {
		t.Fatalf("required = %v, want [question]", params["required"])
	}
}

func TestAskUserClarification_ExecuteReturnsAwaitingUserInput(t *testing.T) {
	tool := &AskUserClarificationTool{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "Quale cliente cerchi?",
		"options": []any{
			map[string]any{"label": "Per nome", "value": "by_name"},
			map[string]any{"label": "Per codice", "value": "by_code"},
		},
	})
	if err == nil {
		t.Fatal("Execute returned nil error, want ErrAwaitingUserInput")
	}
	var awaitErr *ErrAwaitingUserInput
	if !errors.As(err, &awaitErr) {
		t.Fatalf("error is %T (%v), want *ErrAwaitingUserInput", err, err)
	}
	if awaitErr.Question != "Quale cliente cerchi?" {
		t.Errorf("Question = %q, want 'Quale cliente cerchi?'", awaitErr.Question)
	}
	if len(awaitErr.Options) != 2 || awaitErr.Options[0] != "Per nome" || awaitErr.Options[1] != "Per codice" {
		t.Errorf("Options = %v, want [Per nome Per codice]", awaitErr.Options)
	}
	if awaitErr.Kind != "clarification" {
		t.Errorf("Kind = %q, want clarification", awaitErr.Kind)
	}
}

func TestAskUserClarification_OptionsCapped(t *testing.T) {
	tool := &AskUserClarificationTool{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "Scegli",
		"options": []any{
			map[string]any{"label": "A", "value": "a"},
			map[string]any{"label": "B", "value": "b"},
			map[string]any{"label": "C", "value": "c"},
			map[string]any{"label": "D", "value": "d"},
			map[string]any{"label": "E", "value": "e"}, // 5th — must fail
		},
	})
	if err == nil {
		t.Fatal("Expected error for >4 options, got nil")
	}
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Errorf("error = %v (%T), want wrapping llm.ErrSchemaValidation", err, err)
	}
}
