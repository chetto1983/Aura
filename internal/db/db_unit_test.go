// Unit-safe tests for internal/db — no build tag, no Postgres container required.
// Container-gated suite lives in db_test.go (//go:build db_integration).

package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRedactDSN_StripsPassword(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "postgres://aura:secret@127.0.0.1:5432/aura?sslmode=disable",
			want: "postgres://aura:***@127.0.0.1:5432/aura?sslmode=disable",
		},
		{
			in:   "postgres://aura_migrate:s3cr3tP@ss@127.0.0.1:5432/aura",
			want: "postgres://aura_migrate:***@127.0.0.1:5432/aura",
		},
		{
			in:   "postgres://aura@127.0.0.1:5432/aura", // no password
			want: "postgres://aura@127.0.0.1:5432/aura",
		},
		{
			in:   "",
			want: "<empty-dsn>",
		},
		{
			in:   "://not-a-url",
			want: "<unparseable-dsn>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := redactDSN(tc.in)
			if got != tc.want {
				t.Errorf("redactDSN(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// Defense-in-depth: assert no plaintext "secret" leaks through.
			if strings.Contains(got, "secret") || strings.Contains(got, "s3cr3tP@ss") {
				t.Errorf("redactDSN leaked plaintext: %q", got)
			}
		})
	}
}

func TestMigrate_MissingURLFailsFast(t *testing.T) {
	// D-07 literal error string. Asserted byte-for-byte to prevent paraphrasing.
	const literal = "AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17"

	_, err := Migrate(context.Background(), "")
	if err == nil {
		t.Fatal("Migrate(ctx, \"\"): want error, got nil")
	}
	if err.Error() != literal {
		t.Errorf("Migrate error: want exact %q, got %q", literal, err.Error())
	}
}

func TestReset_MissingURLFailsFast(t *testing.T) {
	// Reset uses the same literal — it's a DDL operation requiring the migrate role.
	const literal = "AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17"

	err := Reset(context.Background(), "")
	if err == nil {
		t.Fatal("Reset(ctx, \"\"): want error, got nil")
	}
	if err.Error() != literal {
		t.Errorf("Reset error: want exact %q, got %q", literal, err.Error())
	}
}

func TestEnsureRoles_RejectsEmptyInputs(t *testing.T) {
	cases := []struct {
		name              string
		bootstrap, app, m string
		wantSubstr        string
	}{
		{"empty bootstrap URL", "", "p1", "p2", "bootstrapURL is empty"},
		{"empty app password", "postgres://x:y@localhost/aura", "", "p2", "must be non-empty"},
		{"empty migrate password", "postgres://x:y@localhost/aura", "p1", "", "must be non-empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EnsureRoles(context.Background(), tc.bootstrap, tc.app, tc.m)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestEnsureRoles_NoPlaintextInError(t *testing.T) {
	// Induce a connection-time error against an unreachable host; assert the
	// passwords we passed in do not appear anywhere in the resulting error.
	const appPwd = "uniquePasswordTokenABCxyz"
	const migPwd = "uniqueOtherTokenDEFuvw"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Bootstrap URL pointing at a closed port so pool open errors out cleanly.
	bootstrap := "postgres://aura:" + appPwd + "@127.0.0.1:1/aura?sslmode=disable"
	err := EnsureRoles(ctx, bootstrap, appPwd, migPwd)
	if err == nil {
		t.Skip("EnsureRoles unexpectedly succeeded against unreachable port; skipping plaintext check")
	}
	msg := err.Error()
	if strings.Contains(msg, appPwd) {
		t.Errorf("error message leaks appPassword: %q", msg)
	}
	if strings.Contains(msg, migPwd) {
		t.Errorf("error message leaks migratePassword: %q", msg)
	}
}

func TestConfig_PoolParams(t *testing.T) {
	// Verify that the package-internal defaults are the ones documented in db.go.
	if defaultMaxConns != 10 {
		t.Errorf("defaultMaxConns: want 10, got %d", defaultMaxConns)
	}
	if defaultMinConns != 1 {
		t.Errorf("defaultMinConns: want 1, got %d", defaultMinConns)
	}
	if defaultMaxConnIdleTime != 30*time.Second {
		t.Errorf("defaultMaxConnIdleTime: want 30s, got %s", defaultMaxConnIdleTime)
	}
}

func TestOpen_EmptyURLFailsFast(t *testing.T) {
	_, err := Open(context.Background(), &Config{URL: ""})
	if err == nil {
		t.Fatal("Open with empty URL: want error, got nil")
	}
	if !strings.Contains(err.Error(), "URL is empty") {
		t.Errorf("Open error: want 'URL is empty' substring, got %q", err.Error())
	}
}

func TestOpen_NilConfigFailsFast(t *testing.T) {
	_, err := Open(context.Background(), nil)
	if err == nil {
		t.Fatal("Open with nil cfg: want error, got nil")
	}
}

func TestRedactErrorPassword_NilErrorPassesThrough(t *testing.T) {
	if err := redactErrorPassword(nil, "p1", "p2"); err != nil {
		t.Errorf("nil error: want nil, got %v", err)
	}
}

func TestRedactErrorPassword_StripsLiteral(t *testing.T) {
	orig := errors.New("postgres returned: bad token=SekretBananas at line 1")
	scrubbed := redactErrorPassword(orig, "SekretBananas")
	if scrubbed == nil {
		t.Fatal("scrubbed: want non-nil")
	}
	if strings.Contains(scrubbed.Error(), "SekretBananas") {
		t.Errorf("plaintext still present: %q", scrubbed.Error())
	}
	if !strings.Contains(scrubbed.Error(), "***") {
		t.Errorf("expected *** substitution: %q", scrubbed.Error())
	}
}
