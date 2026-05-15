package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/identity"
)

// visibilityCountingTool implements Tool and ToolDefinitionProvider so
// VisibilityTier and RequiredCapability can be set explicitly, and records
// every Execute invocation so scenarios can assert the body was (or was not)
// called.
type visibilityCountingTool struct {
	name           string
	capability     identity.Capability
	visibilityTier VisibilityTier
	execCount      int
}

func (v *visibilityCountingTool) Name() string                                      { return v.name }
func (v *visibilityCountingTool) Description() string                               { return "visibility counting tool" }
func (v *visibilityCountingTool) Parameters() map[string]any                        { return map[string]any{"type": "object"} }
func (v *visibilityCountingTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	v.execCount++
	return "executed:" + v.name, nil
}
func (v *visibilityCountingTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:               v.name,
		VisibilityTier:     v.visibilityTier,
		RequiredCapability: v.capability,
	}
}

// TestVisibilityTierDoesNotBypassAuthorization verifies three invariants:
//
//  1. visible (VisibilityAlwaysOn) + authorized → tool executes
//  2. visible (VisibilityAlwaysOn) + UNAUTHORIZED → denial error, Execute NOT called
//  3. NOT visible (absent from registry pool) + authorized → tool-not-found, NOT an authz error
//
// Authorization lives in registry.Execute() → authorizeToolExecution(), which
// is the same path that agentExecutor.executeOneTool() calls in production.
// Exercising registry.Execute() directly is equivalent to exercising the
// agentExecutor authorization path without depending on agentExecutor internals.
func TestVisibilityTierDoesNotBypassAuthorization(t *testing.T) {
	_, identityStore, actor := newRegistryIdentityEnv(t)
	ctx := identity.WithActorID(identity.WithAuthorizer(context.Background(), identityStore), actor.ID)

	// narrowCap is intentionally distinct from CapabilityToolExecute so a
	// broad "execute all tools" grant never masks a missing narrow grant.
	const narrowCap = identity.CapabilityMemoryUserWrite

	t.Run("scenario1_visible_authorized_executes", func(t *testing.T) {
		reg := NewRegistry(nil)
		tool := &visibilityCountingTool{
			name:           "always_on_authorized",
			capability:     narrowCap,
			visibilityTier: VisibilityAlwaysOn,
		}
		reg.Register(tool)

		if _, err := identityStore.CreateGrant(context.Background(), identity.GrantParams{
			ID:          "grant:vis-authz:s1",
			SubjectType: identity.SubjectTypePrincipal,
			SubjectID:   actor.PrincipalID,
			Capability:  narrowCap,
			Resource:    identity.ResourceRef{Type: "tool", ID: "always_on_authorized"},
		}); err != nil {
			t.Fatalf("[scenario 1] create grant: %v", err)
		}

		_, err := reg.Execute(ctx, "always_on_authorized", nil)
		if err != nil {
			t.Fatalf("[scenario 1] expected success, got %v", err)
		}
		if tool.execCount != 1 {
			t.Fatalf("[scenario 1] execCount = %d, want 1 — tool must execute when visible+authorized", tool.execCount)
		}
	})

	t.Run("scenario2_visible_unauthorized_denied", func(t *testing.T) {
		reg := NewRegistry(nil)
		tool := &visibilityCountingTool{
			name:           "always_on_no_grant",
			capability:     narrowCap,
			visibilityTier: VisibilityAlwaysOn,
		}
		reg.Register(tool)

		// No grant created — actor lacks narrowCap for this tool.
		_, err := reg.Execute(ctx, "always_on_no_grant", nil)
		if err == nil {
			t.Fatal("[scenario 2] expected denial error, got nil — VisibilityAlwaysOn must NOT bypass authorization")
		}
		if tool.execCount != 0 {
			t.Fatalf("[scenario 2] execCount = %d, want 0 — Execute must not run when actor is unauthorized", tool.execCount)
		}
		if strings.Contains(err.Error(), "not found") {
			t.Fatalf("[scenario 2] got tool-not-found error, want authorization denial: %v", err)
		}
	})

	t.Run("scenario3_not_visible_tool_not_found", func(t *testing.T) {
		reg := NewRegistry(nil)
		// Tool absent from pool (not registered). Grant exists so authorization
		// would succeed IF the registry ever reached the auth check — it doesn't,
		// because the not-found return happens before authorizeToolExecution().
		if _, err := identityStore.CreateGrant(context.Background(), identity.GrantParams{
			ID:          "grant:vis-authz:s3",
			SubjectType: identity.SubjectTypePrincipal,
			SubjectID:   actor.PrincipalID,
			Capability:  identity.CapabilityToolExecute,
			Resource:    identity.ResourceRef{Type: "tool", ID: "deferred_absent"},
		}); err != nil {
			t.Fatalf("[scenario 3] create grant: %v", err)
		}

		_, err := reg.Execute(ctx, "deferred_absent", nil)
		if err == nil {
			t.Fatal("[scenario 3] expected tool-not-found error, got nil")
		}
		// Must be a distinct, non-authorization error.
		if errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("[scenario 3] got ErrUnauthorized, want tool-not-found (distinct error class): %v", err)
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("[scenario 3] error = %q, want 'not found' message (not authz error)", err.Error())
		}
	})
}
