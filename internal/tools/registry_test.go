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

func TestRegistryDefinitionsForFiltersAndKeepsRegistryOrder(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(namedFakeTool{name: "alpha"})
	reg.Register(namedFakeTool{name: "beta"})
	reg.Register(namedFakeTool{name: "gamma"})

	defs := reg.DefinitionsFor([]string{"gamma", "missing", "alpha"})
	if len(defs) != 2 {
		t.Fatalf("DefinitionsFor length = %d, want 2: %+v", len(defs), defs)
	}
	if defs[0].Name != "alpha" || defs[1].Name != "gamma" {
		t.Fatalf("DefinitionsFor names = %q,%q; want alpha,gamma", defs[0].Name, defs[1].Name)
	}
}

type namedFakeTool struct {
	name string
}

func (t namedFakeTool) Name() string        { return t.name }
func (t namedFakeTool) Description() string { return "Fake tool" }
func (t namedFakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (t namedFakeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.name, nil
}
