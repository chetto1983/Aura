package mcptools

import (
	"slices"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestMCPRiskDefaultsAreConservativeWithoutOverridingReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		annotations     *sdkmcp.ToolAnnotations
		wantMutating    bool
		wantDestructive bool
	}{
		{
			name:        "read only",
			annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
		},
		{
			name:            "unannotated write defaults destructive",
			annotations:     nil,
			wantMutating:    true,
			wantDestructive: true,
		},
		{
			name:         "explicit additive write",
			annotations:  &sdkmcp.ToolAnnotations{DestructiveHint: new(bool)},
			wantMutating: true,
		},
		{
			name:            "contradictory read and destructive saturates upward",
			annotations:     &sdkmcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(true)},
			wantMutating:    true,
			wantDestructive: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := specFromToolDef("third_party", &sdkmcp.Tool{Name: "action", Annotations: tt.annotations})
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
		{name: "memory read", source: mcp.SourceRecipeMemory, tool: "memory_recall"},
		{name: "memory ordinary write", source: mcp.SourceRecipeMemory, tool: "memory_upsert_fact", wantMutating: true},
		{name: "memory hygiene", source: mcp.SourceRecipeMemory, tool: "memory_merge_entities", wantMutating: true},
		{name: "memory erase without approval loop", source: mcp.SourceRecipeMemory, tool: "memory_forget", wantMutating: true},
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
			spec := specFromToolDefWithPolicy("connector", &sdkmcp.Tool{Name: tt.tool}, policy)
			if spec.Mutating != tt.wantMutating || spec.Destructive != tt.wantDestructive {
				t.Fatalf("%s risk = mutating:%v destructive:%v, want %v/%v",
					tt.tool, spec.Mutating, spec.Destructive, tt.wantMutating, tt.wantDestructive)
			}
		})
	}
}

// TestMemoryRecipeCoversEveryServedTool is the tripwire the previous table
// lacked. A name that drifts out of this map does not fail loudly — it falls
// through to the unannotated default in mcpToolRisk, `return true, true`, and
// quietly becomes a Destructive action that stops the turn.
func TestMemoryRecipeCoversEveryServedTool(t *testing.T) {
	t.Parallel()
	served := []string{
		"graph_schema", "memory_digest", "memory_entities", "memory_facts_about", "memory_forget",
		"memory_merge_entities", "memory_recall", "memory_reembed", "memory_search", "memory_upsert_fact",
	}
	table := trustedRecipeActions[mcp.SourceRecipeMemory]
	for _, name := range served {
		if _, ok := table[name]; !ok {
			t.Errorf("%s is served but unclassified — it silently becomes Destructive and stops the turn", name)
		}
	}
	for name := range table {
		if !slices.Contains(served, name) {
			t.Errorf("%s is classified but no longer served — the table is describing a server that is gone", name)
		}
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
		&sdkmcp.Tool{Name: "get_emails"},
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
	spec := specFromToolDefWithPolicy("calendar", &sdkmcp.Tool{
		Name:        "update_event",
		Annotations: &sdkmcp.ToolAnnotations{DestructiveHint: new(true)},
	}, policy)
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("explicit destructive hint lowered by recipe policy: %+v", spec)
	}
}

// TestMCPToolRisk_NilAnnotationsFailsClosed pins T-45.1-09: a nil *ToolAnnotations
// (new — the deleted mcp.ToolDef.Annotations was a value, never nil) must take
// the SAME fail-closed branch an unannotated tool took before the swap.
func TestMCPToolRisk_NilAnnotationsFailsClosed(t *testing.T) {
	t.Parallel()
	mutating, destructive := mcpToolRisk(bridgePolicy{}, &sdkmcp.Tool{Name: "x", Annotations: nil})
	if !mutating || !destructive {
		t.Fatalf("nil Annotations must fail closed to mutating:true destructive:true, got %v/%v", mutating, destructive)
	}
}
