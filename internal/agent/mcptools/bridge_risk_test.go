package mcptools

import (
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestMCPRiskDefaultsAreConservativeWithoutOverridingReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		annotations     mcp.ToolAnnotations
		wantMutating    bool
		wantDestructive bool
	}{
		{
			name:        "read only",
			annotations: mcp.ToolAnnotations{ReadOnlyHint: true},
		},
		{
			name:            "unannotated write defaults destructive",
			wantMutating:    true,
			wantDestructive: true,
		},
		{
			name:         "explicit additive write",
			annotations:  mcp.ToolAnnotations{DestructiveHint: new(false)},
			wantMutating: true,
		},
		{
			name:            "contradictory read and destructive saturates upward",
			annotations:     mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(true)},
			wantMutating:    true,
			wantDestructive: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := specFromToolDef("third_party", mcp.ToolDef{Name: "action", Annotations: tt.annotations})
			if spec.Mutating != tt.wantMutating || spec.Destructive != tt.wantDestructive {
				t.Fatalf("risk = mutating:%v destructive:%v, want %v/%v",
					spec.Mutating, spec.Destructive, tt.wantMutating, tt.wantDestructive)
			}
		})
	}
}

func TestTrustedRecipeRiskPolicyIsGraduated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		source          string
		tool            string
		wantMutating    bool
		wantDestructive bool
	}{
		{name: "calendar read", source: "recipe:calendar", tool: "get_emails"},
		{name: "calendar reversible write", source: "recipe:calendar", tool: "update_event", wantMutating: true},
		{name: "calendar external send", source: "recipe:calendar", tool: "send_email", wantMutating: true, wantDestructive: true},
		{name: "calendar external response", source: "recipe:calendar", tool: "respond_to_event", wantMutating: true, wantDestructive: true},
		{name: "memory read", source: mcp.SourceRecipeMemory, tool: "memory_search"},
		{name: "memory ordinary write", source: mcp.SourceRecipeMemory, tool: "memory_update", wantMutating: true},
		{name: "memory erase", source: mcp.SourceRecipeMemory, tool: "memory_forget", wantMutating: true, wantDestructive: true},
		{name: "whatsapp read", source: "recipe:whatsapp", tool: "list_messages"},
		{name: "whatsapp local download", source: "recipe:whatsapp", tool: "download_media", wantMutating: true},
		{name: "whatsapp external send", source: "recipe:whatsapp", tool: "send_message", wantMutating: true, wantDestructive: true},
		{name: "unknown recipe tool fails closed", source: "recipe:calendar", tool: "new_side_effect", wantMutating: true, wantDestructive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy := managedBridgePolicy(mcp.ManagedServer{
				Source: tt.source,
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			})
			spec := specFromToolDefWithPolicy("connector", mcp.ToolDef{Name: tt.tool}, policy)
			if spec.Mutating != tt.wantMutating || spec.Destructive != tt.wantDestructive {
				t.Fatalf("%s risk = mutating:%v destructive:%v, want %v/%v",
					tt.tool, spec.Mutating, spec.Destructive, tt.wantMutating, tt.wantDestructive)
			}
		})
	}
}

func TestRecipeOverridesRequireTrustedRecipeIdentity(t *testing.T) {
	t.Parallel()

	server := mcp.ManagedServer{
		Source: "recipe:calendar",
		Trust:  mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	spec := specFromToolDefWithPolicy(
		"calendar",
		mcp.ToolDef{Name: "get_emails"},
		managedBridgePolicy(server),
	)
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("untrusted server spoofed recipe policy: %+v", spec)
	}
}

func TestExplicitDestructiveHintCanOnlyRaiseTrustedRecipeRisk(t *testing.T) {
	t.Parallel()

	policy := managedBridgePolicy(mcp.ManagedServer{
		Source: "recipe:calendar",
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	})
	spec := specFromToolDefWithPolicy("calendar", mcp.ToolDef{
		Name: "update_event",
		Annotations: mcp.ToolAnnotations{
			DestructiveHint: new(true),
		},
	}, policy)
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("explicit destructive hint lowered by recipe policy: %+v", spec)
	}
}
