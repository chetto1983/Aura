//go:build db_integration

package telegram

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/db"
)

// A chat's conversation id is derived from the chat id, so it stays with the identity that
// first talked there. Linked to another identity, that chat's turns would fail at their first
// append (measured 2026-10-28): the consume refuses the link instead (spec D11).
func TestRelinkingAnotherIdentitysChatIsRefused(t *testing.T) {
	s := New(migratedPool(t))
	ctx := ownerCtx()
	idA, idB := userIdentity(t, s.pool, "relink-a"), userIdentity(t, s.pool, "relink-b")
	const chat int64 = 9_000_120_001
	linkThroughOnboarding(t, s, idA, chat)
	if err := db.WithIdentityTxRaw(ctx, s.pool, idA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO aura.conversations (id, identity_id) VALUES ($1, $2)`, convID(chat), idA)
		return err
	}); err != nil {
		t.Fatalf("A's chat conversation: %v", err)
	}
	if ok, err := s.Unlink(ctx, idA, chat); err != nil || !ok {
		t.Fatalf("A unlinks: %v, %v", ok, err)
	}

	token := uuid.NewString()
	if err := s.InsertPending(ctx, InsertPendingParams{OnboardingToken: token, IdentityID: idB, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: token, TelegramUserID: chat}); !errors.Is(err, ErrChatOwnedElsewhere) {
		t.Fatalf("B links A's chat: err = %v, want ErrChatOwnedElsewhere", err)
	}
	if _, err := s.GetAccountByTelegramID(ctx, chat); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the refused link was written: %v", err)
	}
	if consumed, err := s.PendingConsumed(ctx, token); err != nil || consumed {
		t.Fatalf("the refused link spent B's token: consumed=%v err=%v", consumed, err)
	}

	linkThroughOnboarding(t, s, idA, chat)
	if acct, err := s.GetAccountByTelegramID(ctx, chat); err != nil || acct.IdentityID != idA {
		t.Fatalf("A links its own chat again: %+v, %v", acct, err)
	}
}
