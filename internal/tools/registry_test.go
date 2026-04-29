package tools

import (
	"context"
	"testing"
)

type fakeTool struct{}

func (fakeTool) Name() string        { return "fake" }
func (fakeTool) Description() string { return "Fake tool" }
func (fakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (fakeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return "ok", nil
}

func TestRegistryDefinitionsAndExecute(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(fakeTool{})

	defs := reg.Definitions()
	if len(defs) != 1 {
		t.Fatalf("Definitions() length = %d, want 1", len(defs))
	}
	if defs[0].Name != "fake" {
		t.Errorf("definition name = %q, want fake", defs[0].Name)
	}
	if reg.Get("fake") == nil {
		t.Fatal("Get(fake) returned nil")
	}

	result, err := reg.Execute(context.Background(), "fake", map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result != "ok" {
		t.Errorf("Execute() = %q, want ok", result)
	}
}

func TestArgKeysSorted(t *testing.T) {
	got := argKeys(map[string]any{"z": 1, "a": 2})
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("argKeys() = %#v, want [a z]", got)
	}
}

func TestRegistryExecuteMissingTool(t *testing.T) {
	reg := NewRegistry(nil)
	if _, err := reg.Execute(context.Background(), "missing", nil); err == nil {
		t.Fatal("expected missing tool error")
	}
}
