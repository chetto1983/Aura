// Unit-safe Go-logic tests for internal/db — no build tag, no Postgres container.
// These cover the pure classification/parsing/iteration helpers that do NOT need a
// live pool. The pool-bound paths (Status/Ping/WithTx query bodies, EnsureRoles DDL,
// Migrate/Reset up/down) are owned by the //go:build db_integration tier.

package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUndefinedTable_ClassifiesSQLSTATE(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"exact 42P01", &pgconn.PgError{Code: "42P01"}, true},
		{
			"wrapped 42P01",
			fmt.Errorf("status query: %w", &pgconn.PgError{Code: "42P01"}),
			true,
		},
		{"42501 insufficient_privilege is NOT undefined-table", &pgconn.PgError{Code: "42501"}, false},
		{"23505 unique_violation is NOT undefined-table", &pgconn.PgError{Code: "23505"}, false},
		{"plain non-pg error", errors.New("connection refused"), false},
		{
			// A non-PgError that merely carries the 42P01 text must NOT match — the
			// classifier keys on the typed SQLSTATE, never on the message string.
			"text-only 42P01 string is not a PgError",
			errors.New("ERROR: relation does not exist (SQLSTATE 42P01)"),
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUndefinedTable(tc.err); got != tc.want {
				t.Errorf("isUndefinedTable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestUndefinedTableConstant(t *testing.T) {
	// The 42P01 SQLSTATE is load-bearing for Status's first-boot empty contract;
	// pin the constant so a typo can't silently break the missing-table mapping.
	if undefinedTable != "42P01" {
		t.Errorf("undefinedTable = %q, want 42P01 (relation does not exist)", undefinedTable)
	}
}

func TestMigrateAllCountingSteps_PropagatesRealError(t *testing.T) {
	// A genuine migration failure (NOT ErrNoChange / ErrNotExist) must surface with
	// the count of steps applied before the failure — this is the error-return branch
	// migrateAllCountingSteps takes when Steps returns something other than the two
	// stop-sentinels.
	boom := errors.New("syntax error at or near \"CRAETE\"")
	f := &fakeStepMigrator{errs: []error{nil, nil, boom}}

	n, err := migrateAllCountingSteps(f)
	if err == nil {
		t.Fatal("migrateAllCountingSteps: want the real error to propagate, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("migrateAllCountingSteps error = %v, want it to wrap %v", err, boom)
	}
	if n != 2 {
		t.Errorf("applied steps before failure = %d, want 2", n)
	}
	if f.calls != 3 {
		t.Errorf("Steps calls = %d, want 3 (two OK + one failing)", f.calls)
	}
}

func TestMigrateAllCountingSteps_FirstStepNoChangeIsZero(t *testing.T) {
	// A database already at the latest version: the very first Steps probe returns
	// ErrNoChange, so the helper reports zero applied without error.
	f := &fakeStepMigrator{errs: []error{migrate.ErrNoChange}}
	n, err := migrateAllCountingSteps(f)
	if err != nil {
		t.Fatalf("migrateAllCountingSteps: %v", err)
	}
	if n != 0 {
		t.Errorf("applied steps = %d, want 0 (already at latest)", n)
	}
	if f.calls != 1 {
		t.Errorf("Steps calls = %d, want 1 (single no-change probe)", f.calls)
	}
}

func TestDatabaseNameFromDSN(t *testing.T) {
	cases := []struct {
		name     string
		dsn      string
		want     string
		wantErr  bool
		errMatch string
	}{
		{
			name: "standard dsn",
			dsn:  "postgres://aura:pw@127.0.0.1:5432/aura?sslmode=disable",
			want: "aura",
		},
		{
			name: "dsn without query string",
			dsn:  "postgres://aura@host:5432/custom_db",
			want: "custom_db",
		},
		{
			name:     "empty database path",
			dsn:      "postgres://aura:pw@127.0.0.1:5432/?sslmode=disable",
			wantErr:  true,
			errMatch: "database name is empty",
		},
		{
			name:     "no path at all",
			dsn:      "postgres://aura@127.0.0.1:5432",
			wantErr:  true,
			errMatch: "database name is empty",
		},
		{
			name:    "unparseable dsn",
			dsn:     "://%zz",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := databaseNameFromDSN(tc.dsn)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("databaseNameFromDSN(%q): want error, got name %q", tc.dsn, got)
				}
				if tc.errMatch != "" && err.Error() != tc.errMatch {
					t.Errorf("databaseNameFromDSN(%q) error = %q, want %q", tc.dsn, err.Error(), tc.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("databaseNameFromDSN(%q): %v", tc.dsn, err)
			}
			if got != tc.want {
				t.Errorf("databaseNameFromDSN(%q) = %q, want %q", tc.dsn, got, tc.want)
			}
		})
	}
}

func TestQuoteIdent_EscapesEmbeddedQuotes(t *testing.T) {
	// quoteIdent doubles internal double-quotes so a database name containing one
	// cannot break out of the quoted identifier in grantCreateDatabaseSQL.
	cases := []struct {
		in   string
		want string
	}{
		{"aura", `"aura"`},
		{"with space", `"with space"`},
		{`a"b`, `"a""b"`},
		{`"`, `""""`},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := quoteIdent(tc.in); got != tc.want {
				t.Errorf("quoteIdent(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGrantCreateDatabaseSQL_QuotesAdversarialName(t *testing.T) {
	// A database name carrying a double-quote must be neutralised inside the
	// GRANT statement (defense-in-depth on the identifier-injection surface).
	got := grantCreateDatabaseSQL(`db"; DROP`)
	want := `GRANT CREATE ON DATABASE "db""; DROP" TO aura_migrate`
	if got != want {
		t.Errorf("grantCreateDatabaseSQL = %q, want %q", got, want)
	}
}
