package assets

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryAssetDBTX records the multi-row statement, which captureAssetDBTX cannot: its Query
// refuses before the SQL is seen, and NamesByKey is the store's only :many read that has to
// prove what it sends.
type queryAssetDBTX struct {
	query  string
	args   []any
	called bool
}

func (db *queryAssetDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errUnitAssetScan
}

func (db *queryAssetDBTX) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	db.query = query
	db.args = append([]any(nil), args...)
	db.called = true
	return nil, errUnitAssetScan
}

func (db *queryAssetDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return assetScanErrorRow{}
}

func TestNamesByKeyScopesTheLookupToOneIdentity(t *testing.T) {
	db := &queryAssetDBTX{}
	store := NewStore(db)

	_, err := store.NamesByKey(context.Background(), unitIdentityID, []string{"chat/a.png"})
	if !errors.Is(err, errUnitAssetScan) {
		t.Fatalf("NamesByKey error = %v, want sentinel scan error", err)
	}
	if !db.called {
		t.Fatal("NamesByKey did not execute a query")
	}
	query := compactSQL(db.query)
	if !strings.Contains(query, "where identity_id = $") {
		t.Errorf("NamesByKey SQL missing identity guard: %s", query)
	}
	if !strings.Contains(query, "and deleted_at is null") {
		t.Errorf("NamesByKey SQL missing deleted guard: %s", query)
	}
	if !uuidArgEquals(db.args, 0, unitIdentityID) {
		t.Errorf("NamesByKey arg $1 = %#v, want identity UUID %s", argAt(db.args, 0), unitIdentityID)
	}
}

func TestNamesByKeyAsksNothingWhenThereIsNothingToAsk(t *testing.T) {
	t.Run("no keys", func(t *testing.T) {
		db := &queryAssetDBTX{}
		names, err := NewStore(db).NamesByKey(context.Background(), unitIdentityID, nil)
		if err != nil || len(names) != 0 {
			t.Fatalf("NamesByKey(no keys) = %v, %v; want no names and no error", names, err)
		}
		if db.called {
			t.Fatal("an empty listing reached the database")
		}
	})

	// A malformed identity must fail rather than widen the read: the listing is the only
	// thing standing between one operator's file names and another's.
	t.Run("malformed identity", func(t *testing.T) {
		db := &queryAssetDBTX{}
		if _, err := NewStore(db).NamesByKey(context.Background(), "not-a-uuid", []string{"chat/a.png"}); err == nil {
			t.Fatal("NamesByKey accepted a malformed identity")
		}
		if db.called {
			t.Fatal("a malformed lookup reached the database")
		}
	})
}
