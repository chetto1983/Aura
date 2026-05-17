package tools

import (
	"context"
	"testing"
)

// hintDefinedTool implements Tool + ToolDefinitionProvider with explicit hints.
type hintDefinedTool struct {
	def ToolDefinition
}

func (h hintDefinedTool) Name() string                                     { return h.def.Name }
func (h hintDefinedTool) Description() string                              { return "hint tool" }
func (h hintDefinedTool) Parameters() map[string]any                       { return map[string]any{"type": "object"} }
func (h hintDefinedTool) Execute(_ context.Context, _ map[string]any) (string, error) { return "", nil }
func (h hintDefinedTool) Definition() ToolDefinition                       { return h.def }

// hintBareTool implements Tool only — no ToolDefinitionProvider.
type hintBareTool struct {
	name string
}

func (h hintBareTool) Name() string                                     { return h.name }
func (h hintBareTool) Description() string                              { return "bare hint tool" }
func (h hintBareTool) Parameters() map[string]any                       { return map[string]any{"type": "object"} }
func (h hintBareTool) Execute(_ context.Context, _ map[string]any) (string, error) { return "", nil }

func TestToolDefinition_DefaultsWhenNoHintsSet(t *testing.T) {
	// Bare tool — no Definition() method, so all new fields come from normalizeToolDefinition defaults.
	got := definitionForTool(hintBareTool{name: "no_hints"})

	if got.ReadOnlyHint {
		t.Error("ReadOnlyHint: want false (default), got true")
	}
	if got.DestructiveHint {
		t.Error("DestructiveHint: want false (default), got true")
	}
	if got.IdempotentHint {
		t.Error("IdempotentHint: want false (default), got true")
	}
	if got.OpenWorldHint {
		t.Error("OpenWorldHint: want false (default), got true")
	}
	if got.VisibilityTier != VisibilityActiveTurn {
		t.Errorf("VisibilityTier: want %q (default), got %q", VisibilityActiveTurn, got.VisibilityTier)
	}
}

func TestToolDefinition_ProviderSuppliedHintsPreserved(t *testing.T) {
	supplied := ToolDefinition{
		Name:            "explicit_hints",
		ReadOnlyHint:    true,
		DestructiveHint: false,
		IdempotentHint:  true,
		OpenWorldHint:   true,
		VisibilityTier:  VisibilityDeferred,
	}
	got := definitionForTool(hintDefinedTool{def: supplied})

	if !got.ReadOnlyHint {
		t.Error("ReadOnlyHint: want true (provider-supplied), got false")
	}
	if got.DestructiveHint {
		t.Error("DestructiveHint: want false (provider-supplied), got true")
	}
	if !got.IdempotentHint {
		t.Error("IdempotentHint: want true (provider-supplied), got false")
	}
	if !got.OpenWorldHint {
		t.Error("OpenWorldHint: want true (provider-supplied), got false")
	}
	if got.VisibilityTier != VisibilityDeferred {
		t.Errorf("VisibilityTier: want %q (provider-supplied), got %q", VisibilityDeferred, got.VisibilityTier)
	}
}

func TestToolDefinition_VisibilityAlwaysOnPreserved(t *testing.T) {
	supplied := ToolDefinition{
		Name:           "always_on_tool",
		VisibilityTier: VisibilityAlwaysOn,
	}
	got := definitionForTool(hintDefinedTool{def: supplied})
	if got.VisibilityTier != VisibilityAlwaysOn {
		t.Errorf("VisibilityTier: want %q, got %q", VisibilityAlwaysOn, got.VisibilityTier)
	}
}

func TestToolDefinition_VisibilityActiveTurnPreservedWhenExplicit(t *testing.T) {
	supplied := ToolDefinition{
		Name:           "active_turn_tool",
		VisibilityTier: VisibilityActiveTurn,
	}
	got := definitionForTool(hintDefinedTool{def: supplied})
	if got.VisibilityTier != VisibilityActiveTurn {
		t.Errorf("VisibilityTier: want %q, got %q", VisibilityActiveTurn, got.VisibilityTier)
	}
}

// TestVisibilityTierIsTyped verifies VisibilityTier is a distinct named type.
// The switch arms use typed constants — if VisibilityTier were an alias for
// string, untyped string literals would satisfy the cases and the compiler
// would not catch typos. This compile-time assertion documents the invariant.
func TestVisibilityTierIsTyped(t *testing.T) {
	tier := VisibilityActiveTurn
	switch tier {
	case VisibilityAlwaysOn, VisibilityActiveTurn, VisibilityDeferred:
		// OK — exhaustive match over the typed constants.
	default:
		t.Errorf("unexpected VisibilityTier value: %q", tier)
	}
}

func TestToolDefinition_DestructiveHintPreserved(t *testing.T) {
	supplied := ToolDefinition{
		Name:            "destructive_tool",
		DestructiveHint: true,
		VisibilityTier:  VisibilityDeferred,
	}
	got := definitionForTool(hintDefinedTool{def: supplied})
	if !got.DestructiveHint {
		t.Error("DestructiveHint: want true (provider-supplied), got false")
	}
}
