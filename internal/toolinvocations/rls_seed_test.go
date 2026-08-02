//go:build db_integration

// rls_seed_test.go holds this package's fixture seed for the fail-closed tables.
//
// Migration 0089 made aura.conversations, aura.conversation_turns, aura.paused_states and
// aura.shared_links fail closed. A bare pool.Exec fixture is now refused 42501 on INSERT
// and - the quieter half - matches zero rows on DELETE while reporting success, so a
// cleanup can silently stop cleaning. Both halves of a fixture lifecycle therefore run
// inside a transaction scoped to the row owner. It is deliberately a per-package test
// helper rather than a shared one: a cross-package version would have to live in a
// non-test package and import "testing" there, which this codebase does not do (compare
// internal/agent/agenttest, which is testing-free).
package toolinvocations

import (
	"context"
	"github.com/chetto1983/aura/internal/identityctx"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedAsOwner executes one fixture statement inside a transaction scoped to identityID, so
// the fail-closed owner policies admit it. It t.Fatals on failure - a fixture that did not
// land is never something a test should carry on past.
func seedAsOwner(t *testing.T, pool *pgxpool.Pool, identityID, sql string, args ...any) {
	t.Helper()
	if err := db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	}); err != nil {
		t.Fatalf("seed as owner %s: %v", identityID, err)
	}
}

// ownerCtx is context.Background() carrying the identity every fixture in this package is
// owned by. Since migration 0089 the conversation plane is fail closed, so a store call on a
// bare context sees no conversation and can write no turn — the same thing that would happen
// in production to a caller with no principal. The tests carry an identity because the
// product does: agui.withPrincipal, bootCLIChat, telegram's scopeTurnToIdentity and
// runner.scopeContextToConversation all put one on the context before the store is reached.
func ownerCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), localIdentity)
}
