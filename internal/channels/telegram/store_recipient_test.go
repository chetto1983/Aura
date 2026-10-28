package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type recipientQueryFailure struct {
	sqlc.DBTX
	err error
}

func (f recipientQueryFailure) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingRow{err: f.err}
}

type failingRow struct{ err error }

func (r failingRow) Scan(...any) error { return r.err }

// A lookup that fails stays an error (#197): read as "no account", it would let an
// identity push fall back to another channel.
func TestGetAccountByIdentityPreservesQueryFailure(t *testing.T) {
	want := errors.New("database unavailable")
	s := &Store{q: sqlc.New(recipientQueryFailure{err: want})}
	account, err := s.GetAccountByIdentity(t.Context(), uuid.NewString())
	if !errors.Is(err, want) || account != (Account{}) {
		t.Fatalf("account=%+v err=%v, want original query failure", account, err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("database failure was misclassified as missing")
	}
}
