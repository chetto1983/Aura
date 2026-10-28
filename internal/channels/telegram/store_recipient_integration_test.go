//go:build db_integration

package telegram

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// An identity push with several accounts linked reaches the one seen most recently (R6 of
// docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md), from #197's
// regression: zero links stay not-my-user, another identity's link does not interfere, a
// conversation-bound push still reaches its own chat, and a repair that leaves one link
// restores it. #197 refused the push as ambiguous instead; the operator chose R6.
func TestIdentityDeliveryWithSeveralLinksReachesTheOneSeenLast(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := ownerCtx()
	identityID := uuid.NewString()
	if _, err := pool.Exec(ctx, "INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')",
		identityID, "telegram-recipient-"+identityID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM aura.identities WHERE id = $1", identityID); err != nil {
			t.Error(err)
		}
	})

	const firstID int64 = 9_000_010_101
	link := func(id int64, identity string) {
		t.Helper()
		token := uuid.NewString()
		if err := s.InsertPending(ctx, InsertPendingParams{
			OnboardingToken: token, IdentityID: identity, ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, err := pool.Exec(ctx, "DELETE FROM aura.telegram_setup_pending WHERE onboarding_token = $1", token)
			if err != nil {
				t.Error(err)
			}
		})
		cleanupAccount(t, s, id)
		if _, err := s.ConsumeOnboarding(ctx, ConsumeParams{OnboardingToken: token, TelegramUserID: id}); err != nil {
			t.Fatal(err)
		}
	}
	// Another identity's link must not make this recipient absent or ambiguous.
	link(firstID+10, localID)

	// With no update since linking, the account seen last is the one linked last.
	check := func(t *testing.T, count int, seenLast int64) {
		t.Helper()
		account, err := s.GetAccountByIdentity(ctx, identityID)
		if count == 0 {
			if !errors.Is(err, pgx.ErrNoRows) || account != (Account{}) {
				t.Fatalf("no links: account=%+v err=%v", account, err)
			}
		} else if err != nil || account.TelegramUserID != seenLast || account.IdentityID != identityID {
			t.Fatalf("%d links: account=%+v err=%v, want %d", count, account, err, seenLast)
		}

		for _, route := range []string{"text", "conversation", "approval"} {
			t.Run(route, func(t *testing.T) {
				bot := &sendRecorder{}
				tg := NewChannel(Deps{Offline: true})
				tg.deliverBot, tg.deliverResolver = bot, s
				var sent bool
				var sendErr error
				want := seenLast
				switch route {
				case "text":
					sent, sendErr = tg.Deliver(ctx, identityID, "synthetic reminder")
				case "conversation":
					want = firstID
					sent, sendErr = tg.DeliverConversation(ctx, identityID, convID(firstID), "synthetic result")
				case "approval":
					sent, sendErr = tg.DeliverApproval(ctx, identityID, "synthetic-token", "synthetic-task", "reminder")
				}
				wantSent := count > 0
				if sent != wantSent || sendErr != nil {
					t.Fatalf("%d links: sent=%v err=%v", count, sent, sendErr)
				}
				sends := bot.recorded()
				if wantSent {
					if len(sends) != 1 || sends[0].to.Recipient() != fmt.Sprint(want) {
						t.Fatalf("%d links: sent to %+v, want %d", count, sends, want)
					}
				} else if len(sends) != 0 {
					t.Fatalf("unexpected sends: %+v", sends)
				}
			})
		}
	}
	for count := 0; count <= 3; count++ {
		if count > 0 {
			link(firstID+int64(count)-1, identityID)
		}
		t.Run(fmt.Sprintf("%d_links", count), func(t *testing.T) { check(t, count, firstID+int64(count)-1) })
	}
	// The repair is the operator's unlink, under the owning identity: since 0144 a DELETE
	// bound to another identity matches no row.
	for _, extra := range []int64{firstID + 1, firstID + 2} {
		if unlinked, err := s.Unlink(ctx, identityID, extra); err != nil || !unlinked {
			t.Fatalf("unlink %d = (%v, %v)", extra, unlinked, err)
		}
	}
	t.Run("after_operator_repair", func(t *testing.T) { check(t, 1, firstID) })
}
