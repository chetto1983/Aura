package tools_test

import (
	"testing"

	"github.com/aura/aura/internal/tools"
)

func TestExecuteCodeTool_NilManager(t *testing.T) {
	tool := tools.NewExecuteCodeTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when manager is nil")
	}
}
