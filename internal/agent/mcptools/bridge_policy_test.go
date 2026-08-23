package mcptools

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// The policy a mounted server derives from its trust class decides two things no
// other code re-checks: whether the server's own document may be RENDERED, and
// whether it inherits a curated recipe's action table instead of failing closed.
// Both legs were mutation-silent — go-mutesting could delete
// `policy.views = mcp.TrustMayRenderViews(trust)` outright, and drop the guard
// that keeps a non-recipe server out of the action table, with every test still
// green (2026-08-23: bridge_risk.go 31/36, the three non-logging survivors all
// here). These tests kill the two that are genuinely reachable.
//
// Fixtures below pair a type with a trust class that is VALID for it.
// `checkTypeTrustConsistency` rejects e.g. stdio+remote_http, and a rejected pair
// leaves `Classify` returning an error and an EMPTY trust — a fixture built that
// way exercises the error path and proves nothing about the guard it was aimed at.

func TestManagedBridgePolicyGatesViewsOnTrustClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		server    mcp.ManagedServer
		wantViews bool
	}{
		{
			name:      "trusted recipe renders",
			server:    mcp.ManagedServer{Source: "recipe:whatsapp", Command: "bridge", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}},
			wantViews: true,
		},
		{
			name:      "trusted local renders",
			server:    mcp.ManagedServer{Source: "local:tool", Command: "tool", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}},
			wantViews: true,
		},
		{
			name:      "sandboxed local does not render",
			server:    mcp.ManagedServer{Source: "local:tool", Command: "tool", Trust: mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal}},
			wantViews: false,
		},
		{
			name:      "remote http does not render",
			server:    mcp.ManagedServer{Source: "remote:vendor", URL: "https://vendor.example/mcp", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
			wantViews: false,
		},
		{
			name:      "blocked does not render",
			server:    mcp.ManagedServer{Source: "local:tool", Command: "tool", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
			wantViews: false,
		},
		{
			// An inconsistent pair never reaches the views assignment at all. It
			// belongs here because "Classify failed" must read as "may not render",
			// never as "unset, so carry on with whatever the zero value is".
			name:      "an inconsistent type and trust does not render",
			server:    mcp.ManagedServer{Source: "local:tool", Command: "tool", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
			wantViews: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := managedBridgePolicy(tt.server).views; got != tt.wantViews {
				t.Fatalf("views = %v, want %v — this flag alone decides whether %q may render its own document",
					got, tt.wantViews, tt.server.Source)
			}
		})
	}
}

// A `recipe:` source names Aura's own curated catalog, and its action table is
// what lets a read stay a read instead of failing closed as Destructive. The
// TRUST class, not the source string, is what may hand that table out: an
// explicitly-classed server keeps its class (resolveTrust prefers a known class
// over the recipe: prefix), so a trusted_local server carrying a recipe: source
// must NOT inherit the recipe's tiers.
//
// The pre-existing TestRecipeOverridesRequireTrustedRecipeIdentity aims at this
// and cannot reach it: its stdio+remote_http fixture is an inconsistent pair, so
// Classify errors and the function returns before the guard is even evaluated.
func TestRecipeActionTableIsGatedOnTrustNotOnTheSourceString(t *testing.T) {
	t.Parallel()

	server := mcp.ManagedServer{
		Source:  "recipe:whatsapp",
		Command: "whatsapp-bridge",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
	}
	policy := managedBridgePolicy(server)
	if policy.recipeSource != "" {
		t.Fatalf("recipeSource = %q for a trusted_local server: a source string bought the curated tiers", policy.recipeSource)
	}

	// The consequence, stated in the terms the gateway sees: without the curated
	// table an unannotated tool fails closed, which is the whole point of gating.
	spec := specFromToolDefWithPolicy("whatsapp", &sdkmcp.Tool{Name: "list_chats"}, policy)
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("list_chats = mutating:%v destructive:%v on a non-recipe server; it must fail closed",
			spec.Mutating, spec.Destructive)
	}
}
