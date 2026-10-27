package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestSingleRecipientAccount(t *testing.T) {
	identityID := uuid.New()
	added := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	seen := added.Add(time.Hour)
	first := sqlc.AuraTelegramAccounts{
		TelegramUserID: 101, IdentityID: pgtype.UUID{Bytes: identityID, Valid: true},
		Username:   pgtype.Text{String: "synthetic", Valid: true},
		FirstName:  pgtype.Text{String: "Recipient", Valid: true},
		AddedAt:    pgtype.Timestamptz{Time: added, Valid: true},
		LastSeenAt: pgtype.Timestamptz{Time: seen, Valid: true},
	}
	second := first
	second.TelegramUserID = 102
	second.LastSeenAt = pgtype.Timestamptz{Time: seen.Add(time.Hour), Valid: true}
	third := second
	third.TelegramUserID = 103
	want := Account{TelegramUserID: 101, IdentityID: identityID.String(), Username: "synthetic",
		FirstName: "Recipient", AddedAt: added, LastSeenAt: seen}
	for _, tc := range []struct {
		name string
		rows []sqlc.AuraTelegramAccounts
		want Account
		err  error
	}{
		{"none", nil, Account{}, pgx.ErrNoRows},
		{"one", []sqlc.AuraTelegramAccounts{first}, want, nil},
		{"two", []sqlc.AuraTelegramAccounts{first, second}, Account{}, ErrAmbiguousAccount},
		{"reversed", []sqlc.AuraTelegramAccounts{second, first}, Account{}, ErrAmbiguousAccount},
		{"three", []sqlc.AuraTelegramAccounts{first, second, third}, Account{}, ErrAmbiguousAccount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := singleRecipientAccount(tc.rows)
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Fatalf("account=%+v err=%v, want %+v / %v", got, err, tc.want, tc.err)
			}
			if tc.err == ErrAmbiguousAccount && errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("ambiguity must not trigger the not-my-user fallback")
			}
		})
	}
}

type recipientQueryFailure struct {
	sqlc.DBTX
	err error
}

func (f recipientQueryFailure) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, f.err
}

func TestGetAccountByIdentityPreservesQueryFailure(t *testing.T) {
	want := errors.New("database unavailable")
	s := &Store{q: sqlc.New(recipientQueryFailure{err: want})}
	account, err := s.GetAccountByIdentity(t.Context(), uuid.NewString())
	if !errors.Is(err, want) || account != (Account{}) {
		t.Fatalf("account=%+v err=%v, want original query failure", account, err)
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrAmbiguousAccount) {
		t.Fatal("database failure was misclassified as missing or ambiguous")
	}
}
