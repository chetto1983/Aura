// Unit-safe tests for internal/db — no build tag, no Postgres container required.
// Container-gated suite lives in db_test.go (//go:build db_integration).

package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
)

type fakeStepMigrator struct {
	errs  []error
	calls int
}

func (f *fakeStepMigrator) Steps(int) error {
	err := f.errs[f.calls]
	f.calls++
	return err
}

func TestMigrateAllCountingStepsCountsAppliedSteps(t *testing.T) {
	f := &fakeStepMigrator{errs: []error{nil, nil, migrate.ErrNoChange}}
	n, err := migrateAllCountingSteps(f)
	if err != nil {
		t.Fatalf("migrateAllCountingSteps: %v", err)
	}
	if n != 2 {
		t.Fatalf("applied steps = %d, want 2", n)
	}
	if f.calls != 3 {
		t.Fatalf("Steps calls = %d, want 3 including ErrNoChange probe", f.calls)
	}
}

func TestMigrateAllCountingStepsStopsOnNoNextMigration(t *testing.T) {
	f := &fakeStepMigrator{errs: []error{nil, os.ErrNotExist}}
	n, err := migrateAllCountingSteps(f)
	if err != nil {
		t.Fatalf("migrateAllCountingSteps: %v", err)
	}
	if n != 1 {
		t.Fatalf("applied steps = %d, want 1", n)
	}
	if f.calls != 2 {
		t.Fatalf("Steps calls = %d, want 2 including no-next probe", f.calls)
	}
}

func TestEnsureRolesGrantSQLUsesBootstrapDatabase(t *testing.T) {
	dbname, err := databaseNameFromDSN("postgres://aura:pw@127.0.0.1:5432/custom-db?sslmode=disable")
	if err != nil {
		t.Fatalf("databaseNameFromDSN: %v", err)
	}
	if dbname != "custom-db" {
		t.Fatalf("databaseNameFromDSN = %q, want custom-db", dbname)
	}
	if got, want := grantCreateDatabaseSQL(dbname), `GRANT CREATE ON DATABASE "custom-db" TO aura_migrate`; got != want {
		t.Fatalf("grant SQL = %q, want %q", got, want)
	}
}

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
			in:   "postgres://127.0.0.1:5432/aura", // no userinfo at all
			want: "postgres://127.0.0.1:5432/aura",
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

func TestMigrate_UnknownDriverWrapsError(t *testing.T) {
	// An unrecognized URL scheme fails at golang-migrate's driver lookup, BEFORE
	// any sql.OpenDB call — so it exercises Migrate's NewWithSourceInstance error
	// wrap without leaking a connection-opener goroutine (goleak-safe).
	_, err := Migrate(context.Background(), "bogus://nodriver/aura")
	if err == nil {
		t.Fatal("Migrate with unknown driver: want error, got nil")
	}
	if !strings.Contains(err.Error(), "migrator") {
		t.Errorf("Migrate error: want 'migrator' wrap context, got %q", err.Error())
	}
}

func TestReset_UnknownDriverWrapsError(t *testing.T) {
	err := Reset(context.Background(), "bogus://nodriver/aura")
	if err == nil {
		t.Fatal("Reset with unknown driver: want error, got nil")
	}
	if !strings.Contains(err.Error(), "migrator") {
		t.Errorf("Reset error: want 'migrator' wrap context, got %q", err.Error())
	}
}

func TestEnsureRoles_RejectsEmptyInputs(t *testing.T) {
	cases := []struct {
		name           string
		bootstrap, pwd string
		wantSubstr     string
	}{
		{"empty bootstrap URL", "", "p1", "bootstrapURL is empty"},
		{"empty password", "postgres://x:y@localhost/aura", "", "must be non-empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EnsureRoles(context.Background(), tc.bootstrap, tc.pwd)
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
	// password we passed in does not appear anywhere in the resulting error.
	const pwd = "uniquePasswordTokenABCxyz"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Bootstrap URL pointing at a closed port so pool open errors out cleanly.
	bootstrap := "postgres://aura:" + pwd + "@127.0.0.1:1/aura?sslmode=disable"
	err := EnsureRoles(ctx, bootstrap, pwd)
	if err == nil {
		t.Skip("EnsureRoles unexpectedly succeeded against unreachable port; skipping plaintext check")
	}
	msg := err.Error()
	if strings.Contains(msg, pwd) {
		t.Errorf("error message leaks password: %q", msg)
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

func TestPing_NilPool(t *testing.T) {
	_, err := Ping(context.Background(), nil)
	if err == nil {
		t.Fatal("Ping(ctx, nil): want error, got nil")
	}
	if !strings.Contains(err.Error(), "pool is nil") {
		t.Errorf("Ping error: want 'pool is nil' substring, got %q", err.Error())
	}
}

func TestStatus_NilPool(t *testing.T) {
	_, err := Status(context.Background(), nil)
	if err == nil {
		t.Fatal("Status(ctx, nil): want error, got nil")
	}
	if !strings.Contains(err.Error(), "pool is nil") {
		t.Errorf("Status error: want 'pool is nil' substring, got %q", err.Error())
	}
}

func TestOpen_UnparseableURLRedactsAndFails(t *testing.T) {
	// A DSN that pgxpool.ParseConfig rejects exercises the parse-error wrap.
	_, err := Open(context.Background(), &Config{URL: "postgres://%zz"})
	if err == nil {
		t.Fatal("Open with malformed URL: want error, got nil")
	}
	// The wrap must run the DSN through redactDSN, never echo a raw password.
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("Open error leaked plaintext: %q", err.Error())
	}
}

func TestOpen_AppliesExplicitPoolTuning(t *testing.T) {
	// Exercises the non-default branches of Open's pool tuning (MaxConns>0,
	// MinConns>0, MaxConnIdleTime>0). The connect still fails on the closed
	// port, but the tuning branches run before the Ping that fails.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cfg := &Config{
		URL:             "postgres://aura_app:pw@127.0.0.1:1/aura?sslmode=disable",
		MaxConns:        5,
		MinConns:        2,
		MaxConnIdleTime: time.Second,
	}
	if _, err := Open(ctx, cfg); err == nil {
		t.Skip("Open unexpectedly succeeded against a closed port; tuning branches still executed")
	}
}

func TestRedactDSNUsername_ParseErrorReturnsEmpty(t *testing.T) {
	// Internal helper: a DSN that url.Parse rejects must yield "" rather than panic.
	if got := redactDSNUsername("://%zz"); got != "" {
		t.Errorf("redactDSNUsername(malformed) = %q, want empty", got)
	}
	if got := redactDSNUsername("postgres://127.0.0.1/aura"); got != "" {
		t.Errorf("redactDSNUsername(no-userinfo) = %q, want empty", got)
	}
}

func TestOpen_UnreachableHostPingFails(t *testing.T) {
	// Valid DSN, closed port → pool constructs but the connect-time Ping fails,
	// exercising Open's NewWithConfig-success / Ping-failure branch. The wrapped
	// error must redact the password (assert the literal does not leak).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const pwd = "uniquePingFailToken123"
	cfg := &Config{URL: "postgres://aura_app:" + pwd + "@127.0.0.1:1/aura?sslmode=disable"}
	_, err := Open(ctx, cfg)
	if err == nil {
		t.Skip("Open unexpectedly succeeded against a closed port; skipping ping-fail assertion")
	}
	if strings.Contains(err.Error(), pwd) {
		t.Errorf("Open error leaked password: %q", err.Error())
	}
}

func TestEnsureRoles_MalformedBootstrapURLFailsFast(t *testing.T) {
	// A non-empty but unparseable bootstrap DSN must fail at pgxpool.New before
	// any network I/O, with the (empty) password never surfacing.
	err := EnsureRoles(context.Background(), "postgres://%zz", "pw")
	if err == nil {
		t.Fatal("EnsureRoles with malformed URL: want error, got nil")
	}
	if strings.Contains(err.Error(), "pw") {
		t.Errorf("EnsureRoles error leaked password: %q", err.Error())
	}
}

func TestRedactErrorPassword_NilErrorPassesThrough(t *testing.T) {
	if err := redactErrorPassword(nil, "p1", "p2"); err != nil {
		t.Errorf("nil error: want nil, got %v", err)
	}
}

func TestRedactErrorPassword_SkipsEmptyPasswordInList(t *testing.T) {
	// An empty entry in the password list must be skipped (not used as a no-op
	// replace target); the non-empty one is still scrubbed.
	orig := errors.New("token=RealPw123 leaked")
	scrubbed := redactErrorPassword(orig, "", "RealPw123")
	if scrubbed == nil {
		t.Fatal("scrubbed: want non-nil")
	}
	if strings.Contains(scrubbed.Error(), "RealPw123") {
		t.Errorf("plaintext still present: %q", scrubbed.Error())
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

func TestMigrationHeadMatchesEmbeddedCatalog(t *testing.T) {
	head, err := MigrationHead()
	if err != nil {
		t.Fatal(err)
	}
	// A deliberate pin, moved deliberately: 0106 adds aura.paused_states.pending_action_id
	// (D-12 fencing) and .owning_worker_id (D-13 level identity, plan 51-06a's checkpoint
	// decision `new-columns`) plus the paused_states_worker_attribution_exclusive CHECK
	// constraint. The test exists so a migration added without noticing breaks the build
	// rather than the deployment, so bumping it is the intended way to acknowledge one --
	// never a fixup to make a red go away.
	if head != 106 {
		t.Fatalf("MigrationHead=%d, want embedded head 106", head)
	}
}

func TestCheckMigrationHeadMissingURLFailsFast(t *testing.T) {
	err := CheckMigrationHead(context.Background(), "")
	if err == nil || err.Error() != errMissingMigrateURL {
		t.Fatalf("CheckMigrationHead empty URL error=%v, want %q", err, errMissingMigrateURL)
	}
}
