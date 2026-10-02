package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type chatGPTBootFixture struct {
	row           sqlc.AuraSettings
	getError      error
	operatorReads int
}

func (*chatGPTBootFixture) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("boot profile must not write")
}
func (*chatGPTBootFixture) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected boot profile list")
}
func (q *chatGPTBootFixture) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if args[0] == "AURA_LLM_MODEL" {
		return chatGPTBootRow{q: q, setting: true}
	}
	q.operatorReads++
	return chatGPTBootRow{q: q}
}

type chatGPTBootRow struct {
	q       *chatGPTBootFixture
	setting bool
}

func (r chatGPTBootRow) Scan(dest ...any) error {
	if r.setting {
		if r.q.getError != nil {
			return r.q.getError
		}
		row := r.q.row
		*dest[0].(*string), *dest[1].(*string), *dest[2].(*bool) = row.Key, row.Value, row.IsSecret
		*dest[3].(*pgtype.Timestamptz), *dest[4].(*pgtype.Text) = row.UpdatedAt, row.UpdatedBy
		return nil
	}
	*dest[0].(*int), *dest[1].(**string), *dest[2].(*bool) = 2, nil, false
	return nil
}

func TestChatGPTBootKeepsSavedProfileOwnerWithAdminAndMember(t *testing.T) {
	const owner = "448ddbe1-96ea-405d-8219-4a3d52a425c0"
	q := &chatGPTBootFixture{row: sqlc.AuraSettings{Key: "AURA_LLM_MODEL", Value: "account-model", UpdatedBy: pgtype.Text{String: owner, Valid: true}}}
	got, err := chatGPTBootOwner(context.Background(), "account-model", q)
	if err != nil || got != owner || q.operatorReads != 0 {
		t.Fatalf("owner %q, err %v, guessed operator %d times", got, err, q.operatorReads)
	}
	q.getError = pgx.ErrNoRows
	if _, err := chatGPTBootOwner(context.Background(), "account-model", q); !errors.Is(err, identityctx.ErrOperatorAmbiguous) {
		t.Fatalf("missing profile guessed an account: %v", err)
	}
}

func TestChatGPTBootRefusesUnattributedOrMismatchedProfile(t *testing.T) {
	for _, row := range []sqlc.AuraSettings{
		{Value: "account-model"},
		{Value: "different-model", UpdatedBy: pgtype.Text{String: "448ddbe1-96ea-405d-8219-4a3d52a425c0", Valid: true}},
		{Value: "account-model", IsSecret: true, UpdatedBy: pgtype.Text{String: "448ddbe1-96ea-405d-8219-4a3d52a425c0", Valid: true}},
	} {
		q := &chatGPTBootFixture{row: row}
		if _, err := chatGPTBootOwner(context.Background(), "account-model", q); err == nil || q.operatorReads != 0 {
			t.Fatal("invalid profile borrowed another account")
		}
	}
	q := &chatGPTBootFixture{getError: errors.New("database unavailable")}
	if _, err := chatGPTBootOwner(context.Background(), "account-model", q); err == nil || q.operatorReads != 0 {
		t.Fatal("database failure borrowed another account")
	}
}
