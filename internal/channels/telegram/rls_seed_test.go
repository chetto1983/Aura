//go:build db_integration

// rls_seed_test.go holds this package's owner-scoped test context.
//
// Migration 0089 made aura.conversations, aura.conversation_turns, aura.paused_states and
// aura.shared_links fail closed, so a store call on a bare context sees no conversation and
// writes no turn — the same thing that happens in production to a caller with no principal.
// The tests carry an identity because the product does: agui.withPrincipal, bootCLIChat,
// telegram's scopeTurnToIdentity and runner.scopeContextToConversation all put one on the
// context before the store is reached.
package telegram

import (
	"context"

	"github.com/chetto1983/aura/internal/identityctx"
)

// ownerCtx is context.Background() carrying the identity every fixture in this package is
// owned by.
func ownerCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), localID)
}
