package db

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// The classification is by type through errors.As, never by message: a 23505 reached through
// an Unwrap chain matches, and the same text in a plain error does not.
func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"23505 matches", &pgconn.PgError{Code: "23505"}, true},
		{"wrapped 23505 matches", errors.Join(errors.New("ctx"), &pgconn.PgError{Code: "23505"}), true},
		{"23505 only in the message does not", errors.New("wrap: " + (&pgconn.PgError{Code: "23505"}).Error()), false},
		{"23503 fk does not", &pgconn.PgError{Code: "23503"}, false},
		{"42501 privilege does not", &pgconn.PgError{Code: "42501"}, false},
		{"plain error does not", errors.New("boom"), false},
		{"nil does not", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
