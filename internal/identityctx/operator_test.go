package identityctx

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeRow answers the single three-column query OperatorIdentity issues.
type fakeRow struct {
	enrolled   int
	firstID    *string
	seedExists bool
	err        error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int)) = r.enrolled
	*(dest[1].(**string)) = r.firstID
	*(dest[2].(*bool)) = r.seedExists
	return nil
}

type fakeQuerier struct {
	row     fakeRow
	gotArgs []any
}

func (q *fakeQuerier) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.gotArgs = args
	return q.row
}

func TestOperatorIdentity(t *testing.T) {
	enrolledID := "e3c8eb3b-7f0c-4bbc-bd6c-3311f9623c3e"

	t.Run("returns the enrolled operator, not the seed", func(t *testing.T) {
		// The regression this whole function exists for: after first login the seed is
		// deleted, so a caller defaulting to it addresses a tenant that is gone.
		q := &fakeQuerier{row: fakeRow{enrolled: 1, firstID: new(enrolledID), seedExists: false}}
		got, err := OperatorIdentity(context.Background(), q)
		if err != nil {
			t.Fatalf("OperatorIdentity: %v", err)
		}
		if got != enrolledID {
			t.Fatalf("got %q, want the enrolled identity %q", got, enrolledID)
		}
		if len(q.gotArgs) != 1 || q.gotArgs[0] != LocalOperatorIdentity {
			t.Fatalf("the seed must be passed as the parameter, got %#v", q.gotArgs)
		}
	})

	t.Run("falls back to the seed only while it is still real", func(t *testing.T) {
		q := &fakeQuerier{row: fakeRow{enrolled: 0, seedExists: true}}
		got, err := OperatorIdentity(context.Background(), q)
		if err != nil {
			t.Fatalf("OperatorIdentity: %v", err)
		}
		if got != LocalOperatorIdentity {
			t.Fatalf("got %q, want the seed %q", got, LocalOperatorIdentity)
		}
	})

	t.Run("refuses to guess when several operators are enrolled", func(t *testing.T) {
		q := &fakeQuerier{row: fakeRow{enrolled: 2, firstID: new(enrolledID), seedExists: false}}
		if _, err := OperatorIdentity(context.Background(), q); !errors.Is(err, ErrOperatorAmbiguous) {
			t.Fatalf("err = %v, want ErrOperatorAmbiguous", err)
		}
	})

	t.Run("reports having nobody rather than returning a dead identity", func(t *testing.T) {
		q := &fakeQuerier{row: fakeRow{enrolled: 0, seedExists: false}}
		if _, err := OperatorIdentity(context.Background(), q); !errors.Is(err, ErrNoOperator) {
			t.Fatalf("err = %v, want ErrNoOperator", err)
		}
	})

	t.Run("propagates a query failure instead of defaulting", func(t *testing.T) {
		q := &fakeQuerier{row: fakeRow{err: errors.New("connection refused")}}
		_, err := OperatorIdentity(context.Background(), q)
		if err == nil {
			t.Fatal("expected the query error to surface")
		}
	})

	t.Run("a nil querier is not an excuse to guess", func(t *testing.T) {
		if _, err := OperatorIdentity(context.Background(), nil); !errors.Is(err, ErrNoOperator) {
			t.Fatalf("err = %v, want ErrNoOperator", err)
		}
	})
}
