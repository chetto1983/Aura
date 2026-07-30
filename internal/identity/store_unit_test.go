// Unit tier (no build tag): pure validation logic that needs no database.
// Covers the wildcard rejection, capability-name grammar, and UUID parsing that
// every grant/revoke path runs BEFORE touching Postgres (the T-04-05/T-04-06
// mitigation surface). The DB round-trip paths are exercised under db_integration
// in store_test.go.
package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestValidateGrantInput(t *testing.T) {
	t.Parallel()
	var s Store // pool/q unused — validateGrantInput never touches them
	const id = "00000000-0000-0000-0000-000000000001"

	tests := []struct {
		name       string
		identityID string
		capability string
		wantErr    error // sentinel to errors.Is against; nil = success
	}{
		{"wildcard rejected", id, "*", ErrWildcardManaged},
		{"empty rejected", id, "", ErrInvalidCapability},
		{"uppercase rejected", id, "Foo", ErrInvalidCapability},
		{"leading digit rejected", id, "1foo", ErrInvalidCapability},
		{"leading dot rejected", id, ".foo", ErrInvalidCapability},
		{"space rejected", id, "foo bar", ErrInvalidCapability},
		{"too long rejected", id, "a" + strings.Repeat("b", 64), ErrInvalidCapability},
		{"bad uuid rejected", "not-a-uuid", "foo", nil}, // returns a parse error (not a sentinel)
		{"valid simple", id, "foo", nil},
		{"valid dotted", id, "memory.read", nil},
		{"valid with dashes", id, "web.fetch-url_v2", nil},
		{"valid max length", id, "a" + strings.Repeat("b", 63), nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := s.validateGrantInput(tc.identityID, tc.capability)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("validateGrantInput(%q,%q): want errors.Is %v, got %v", tc.identityID, tc.capability, tc.wantErr, err)
				}
			case tc.name == "bad uuid rejected":
				if err == nil {
					t.Fatalf("validateGrantInput(%q,%q): want a parse error, got nil", tc.identityID, tc.capability)
				}
			default:
				if err != nil {
					t.Fatalf("validateGrantInput(%q,%q): want nil, got %v", tc.identityID, tc.capability, err)
				}
			}
		})
	}
}

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"23505 matches", &pgconn.PgError{Code: "23505"}, true},
		{"23503 fk does not match", &pgconn.PgError{Code: "23503"}, false},
		{"42501 privilege does not match", &pgconn.PgError{Code: "42501"}, false},
		{"wrapped 23505 matches", errors.New("wrap: " + (&pgconn.PgError{Code: "23505"}).Error()), false},
		{"non-pg error does not match", errors.New("plain"), false},
		{"nil does not match", nil, false},
	}
	// The "wrapped 23505" case above is a plain errors.New (no Unwrap chain to a
	// *pgconn.PgError), so it must NOT match — proving we classify by type via
	// errors.As, not by message substring.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isUniqueViolation(tc.err); got != tc.want {
				t.Errorf("isUniqueViolation(%v): want %v, got %v", tc.err, tc.want, got)
			}
		})
	}
}

func TestParseUUID(t *testing.T) {
	t.Parallel()
	t.Run("valid round-trips", func(t *testing.T) {
		t.Parallel()
		const want = "00000000-0000-0000-0000-000000000001"
		u, err := db.ParseUUID("identity id", want)
		if err != nil {
			t.Fatalf("parseUUID: %v", err)
		}
		if !u.Valid {
			t.Fatal("parseUUID: want Valid=true")
		}
	})
	t.Run("invalid errors", func(t *testing.T) {
		t.Parallel()
		if _, err := db.ParseUUID("identity id", "nope"); err == nil {
			t.Fatal("parseUUID(nope): want error, got nil")
		}
	})
}
