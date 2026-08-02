//go:build db_integration

// The store's FAILURE contract, split out of store_test.go on 2026-08-03 when the
// identity-scoping work pushed it past the 600-LOC cap (CLAUDE.md, no god class).
//
// A natural seam rather than an arbitrary cut: everything next door asserts what the
// store DOES with a well-formed call, and everything here asserts what it does with a
// broken one — a dead pool, a malformed UUID, and (since 0089) a context carrying no
// identity at all, which must now return nothing instead of everything.

package conversations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreMethods_DBErrorWrapping(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)

	canceled, cancel := context.WithCancel(ownerCtx())
	cancel()

	newID := uuid.Must(uuid.NewV7()).String()
	if _, err := s.Create(canceled, CreateParams{ID: newID, IdentityID: localID, Model: "m"}); err == nil {
		t.Error("Create(canceled): want error")
	}
	if _, err := s.Get(canceled, convID); err == nil {
		t.Error("Get(canceled): want error")
	}
	if _, err := s.List(canceled, true); err == nil {
		t.Error("List(canceled): want error")
	}
	if err := s.UpdateStatus(canceled, convID, StatusArchived); err == nil {
		t.Error("UpdateStatus(canceled): want error")
	}
	if err := s.Rename(canceled, convID, "x"); err == nil {
		t.Error("Rename(canceled): want error")
	}
	if err := s.SetTitleIfNull(canceled, convID, "x"); err == nil {
		t.Error("SetTitleIfNull(canceled): want error")
	}
	if _, err := s.CountTurns(canceled, convID); err == nil {
		t.Error("CountTurns(canceled): want error")
	}
	if err := s.AppendTurn(canceled, AppendTurnParams{ConversationID: convID, Seq: 1, Role: llm.RoleUser, Content: "x"}); err == nil {
		t.Error("AppendTurn(canceled): want error")
	}
	if _, err := s.LoadHistory(canceled, convID); err == nil {
		t.Error("LoadHistory(canceled): want error")
	}
	if _, err := s.SearchConversationTurns(canceled, "q", 5); err == nil {
		t.Error("SearchConversationTurns(canceled): want error")
	}
	if err := s.Delete(canceled, convID); err == nil {
		t.Error("Delete(canceled): want error")
	}
}

// TestStoreMethods_InvalidUUIDWrapping exercises the parseUUID error branches.
func TestStoreMethods_InvalidUUIDWrapping(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	ctx := ownerCtx()
	const bad = "not-a-uuid"

	if _, err := s.Create(ctx, CreateParams{ID: bad, IdentityID: localID, Model: "m"}); err == nil {
		t.Error("Create(bad id): want error")
	}
	if _, err := s.Create(ctx, CreateParams{ID: uuid.Must(uuid.NewV7()).String(), IdentityID: bad, Model: "m"}); err == nil {
		t.Error("Create(bad identity): want error")
	}
	if _, err := s.Get(ctx, bad); err == nil {
		t.Error("Get(bad): want error")
	}
	if err := s.UpdateStatus(ctx, bad, StatusActive); err == nil {
		t.Error("UpdateStatus(bad): want error")
	}
	if err := s.Rename(ctx, bad, "x"); err == nil {
		t.Error("Rename(bad): want error")
	}
	if err := s.SetTitleIfNull(ctx, bad, "x"); err == nil {
		t.Error("SetTitleIfNull(bad): want error")
	}
	if _, err := s.CountTurns(ctx, bad); err == nil {
		t.Error("CountTurns(bad): want error")
	}
	if err := s.AppendTurn(ctx, AppendTurnParams{ConversationID: bad, Seq: 1, Role: llm.RoleUser}); err == nil {
		t.Error("AppendTurn(bad): want error")
	}
	if _, err := s.LoadHistory(ctx, bad); err == nil {
		t.Error("LoadHistory(bad): want error")
	}
	if err := s.Delete(ctx, bad); err == nil {
		t.Error("Delete(bad): want error")
	}
}

// TestStoreFailsClosedWithoutIdentityOnContext is the sharp end of migration 0089 for this
// package: the Store's own RLS carrier, measured on a pool with NO session identity, so
// nothing but db.WithCallerIdentityTx can be supplying it.
//
// It exists because the other integration tests in this file run on a session-scoped pool
// (sessionScopedAppURL) so their raw fixture SQL works. That convenience could mask a store
// method that dropped its transaction and read the pool directly — this test is the one that
// would not be fooled: with no principal on the context the store sees nothing and writes
// nothing, and with one it sees exactly its own.
func TestStoreFailsClosedWithoutIdentityOnContext(t *testing.T) {
	pool := unscopedPool(t)
	s := newStore(t, pool)
	scoped := ownerCtx()

	convID := uuid.Must(uuid.NewV7()).String()
	if _, err := s.Create(scoped, CreateParams{ID: convID, IdentityID: localID, Model: "m"}); err != nil {
		t.Fatalf("Create as the owner: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete(ownerCtx(), convID) })
	if err := s.AppendTurn(scoped, AppendTurnParams{ConversationID: convID, Role: llm.RoleUser, Content: "hello"}); err != nil {
		t.Fatalf("AppendTurn as the owner: %v", err)
	}

	// With a principal: the owner reads their own conversation and its history.
	if _, err := s.Get(scoped, convID); err != nil {
		t.Fatalf("owner cannot read their own conversation (self-lockout): %v", err)
	}
	if hist, err := s.LoadHistory(scoped, convID); err != nil || len(hist) != 1 {
		t.Fatalf("owner LoadHistory = %d turn(s), err=%v; want 1, nil", len(hist), err)
	}

	// Without one: invisible, not merely filtered. A read that returned rows here would mean
	// the store reached the pool without setting app.current_identity.
	bare := context.Background()
	if _, err := s.Get(bare, convID); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("Get with no principal = %v, want ErrConversationNotFound (RLS failing OPEN)", err)
	}
	if hist, err := s.LoadHistory(bare, convID); err != nil || len(hist) != 0 {
		t.Errorf("LoadHistory with no principal = %d turn(s), err=%v; want 0, nil", len(hist), err)
	}
	if list, err := s.List(bare, true); err != nil || len(list) != 0 {
		t.Errorf("List with no principal = %d conversation(s), err=%v; want 0, nil", len(list), err)
	}
	if err := s.AppendTurn(bare, AppendTurnParams{ConversationID: convID, Role: llm.RoleUser, Content: "forged"}); err == nil {
		t.Error("AppendTurn with no principal succeeded, want a refusal")
	}
}

// unscopedPool is migratedPool's sibling WITHOUT the session-level app.current_identity: the
// only thing that can scope a statement on it is the store's own transaction.
func unscopedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{URL: envOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("Open unscoped (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
