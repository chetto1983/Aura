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

func TestIdentityDeliveryRequiresUnambiguousAccount(t *testing.T) {
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

	check := func(t *testing.T, count int) {
		t.Helper()
		account, err := s.GetAccountByIdentity(ctx, identityID)
		switch count {
		case 0:
			if !errors.Is(err, pgx.ErrNoRows) || account != (Account{}) {
				t.Fatalf("no links: account=%+v err=%v", account, err)
			}
		case 1:
			if err != nil || account.TelegramUserID != firstID || account.IdentityID != identityID {
				t.Fatalf("one link: account=%+v err=%v", account, err)
			}
		default:
			if !errors.Is(err, ErrAmbiguousAccount) || errors.Is(err, pgx.ErrNoRows) || account != (Account{}) {
				t.Fatalf("%d links must refuse an ambiguous recipient: account=%+v err=%v", count, account, err)
			}
		}

		for _, route := range []string{"text", "conversation", "approval"} {
			t.Run(route, func(t *testing.T) {
				bot := &sendRecorder{}
				tg := NewChannel(Deps{Offline: true})
				tg.deliverBot, tg.deliverResolver = bot, s
				var sent bool
				var sendErr error
				switch route {
				case "text":
					sent, sendErr = tg.Deliver(ctx, identityID, "synthetic reminder")
				case "conversation":
					sent, sendErr = tg.DeliverConversation(ctx, identityID, convID(firstID), "synthetic result")
				case "approval":
					sent, sendErr = tg.DeliverApproval(ctx, identityID, "synthetic-token", "synthetic-task", "reminder")
				}
				wantSent := count == 1
				if sent != wantSent || (sendErr != nil) != (count > 1) {
					t.Fatalf("%d links: sent=%v err=%v", count, sent, sendErr)
				}
				if count > 1 && !errors.Is(sendErr, ErrAmbiguousAccount) {
					t.Fatalf("delivery lost the ambiguity error: %v", sendErr)
				}
				sends := bot.recorded()
				if wantSent {
					if len(sends) != 1 || sends[0].to.Recipient() != fmt.Sprint(firstID) {
						t.Fatalf("wrong recipient: %+v", sends)
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
		t.Run(fmt.Sprintf("%d_links", count), func(t *testing.T) { check(t, count) })
	}
	if _, err := pool.Exec(ctx,
		"DELETE FROM aura.telegram_accounts WHERE identity_id = $1 AND telegram_user_id <> $2", identityID, firstID); err != nil {
		t.Fatal(err)
	}
	t.Run("after_operator_repair", func(t *testing.T) { check(t, 1) })
}
