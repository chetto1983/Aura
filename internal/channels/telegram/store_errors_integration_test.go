//go:build db_integration

package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A store call that cannot reach its row reports it: the callers branch on the error (the
// HTTP answer, delivery's tri-state, the setup footer), so a swallowed one would read as
// "nothing there".
func TestStoreReportsWhatItCannotDo(t *testing.T) {
	s := New(migratedPool(t))
	ctx := ownerCtx()
	identityID := userIdentity(t, s.pool, "store-errors")

	if err := s.InsertPending(ctx, InsertPendingParams{OnboardingToken: uuid.NewString(), IdentityID: "local", ExpiresAt: time.Now().Add(time.Hour)}); err == nil {
		t.Error("InsertPending for a non-UUID identity succeeded")
	}
	if err := s.InsertPending(ctx, InsertPendingParams{OnboardingToken: uuid.NewString(), IdentityID: uuid.NewString(), ExpiresAt: time.Now().Add(time.Hour)}); err == nil {
		t.Error("InsertPending for an identity that does not exist succeeded")
	}
	if _, err := s.GetAccountByIdentity(ctx, identityID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetAccountByIdentity with no link = %v, want pgx.ErrNoRows", err)
	}
	if _, err := s.Unlink(ctx, "local", 9_000_100_001); err == nil {
		t.Error("Unlink for a non-UUID identity succeeded")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if n, err := s.CleanupExpired(cancelled); err == nil || n != 0 {
		t.Errorf("CleanupExpired on a cancelled context = %d, %v; want 0 and an error", n, err)
	}
	if n, err := s.CountAccounts(cancelled); err == nil || n != 0 {
		t.Errorf("CountAccounts on a cancelled context = %d, %v; want 0 and an error", n, err)
	}
	if err := s.TouchLastSeen(cancelled, identityID, 9_000_100_001); err == nil {
		t.Error("TouchLastSeen on a cancelled context succeeded")
	}
	if accounts, err := s.LinkedAccounts(cancelled, identityID); err == nil || accounts != nil {
		t.Errorf("LinkedAccounts on a cancelled context = %v, %v; want nil and an error", accounts, err)
	}
	if ok, err := s.Unlink(cancelled, identityID, 9_000_100_001); err == nil || ok {
		t.Errorf("Unlink on a cancelled context = %v, %v; want false and an error", ok, err)
	}
}
