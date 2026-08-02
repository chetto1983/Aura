//go:build db_integration

// rls_seed_test.go holds this package's owner-scoped test context.
//
// Migration 0089 made aura.conversations, aura.conversation_turns, aura.paused_states and
// aura.shared_links fail closed, so a store call on a bare context sees no conversation and
// writes no turn — the same thing that happens in production to a caller with no principal.
// The tests carry an identity because the product does: agui.withPrincipal, bootCLIChat,
// telegram's scopeTurnToIdentity and runner.scopeContextToConversation all put one on the
// context before the store is reached.
package runner

import (
	"context"
	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// ownerCtx is context.Background() carrying the identity every fixture in this package is
// owned by.
func ownerCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), localIdentityID)
}

// asOwner runs one raw fixture statement inside a transaction scoped to identityID.
//
// ownerCtx() alone is NOT enough for a raw pool statement: it puts the identity on the GO
// context, which only the stores read — a bare pool.Exec sets no app.current_identity at all,
// so since migration 0089 it matches zero rows and reports success. That silence is the
// dangerous part: an UPDATE that ages a reservation, or a DELETE that should cascade, simply
// does nothing and the test fails somewhere else entirely.
func asOwner(t *testing.T, pool *pgxpool.Pool, identityID string, fn func(tx pgx.Tx) error) {
	t.Helper()
	if err := db.WithIdentityTxRaw(context.Background(), pool, identityID, fn); err != nil {
		t.Fatalf("as owner %s: %v", identityID, err)
	}
}
