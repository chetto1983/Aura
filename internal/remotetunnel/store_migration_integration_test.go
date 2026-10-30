//go:build db_integration

package remotetunnel

import (
	"net/url"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

// The step runs on a throwaway database: stepping the shared integration database down would
// delete every other package's fixtures (see internal/cron/store_test.go disposableMigrateURL).
func TestMigrationMapsLegacyErrorToUnknown(t *testing.T) {
	adminURL := os.Getenv("AURA_DB_BOOTSTRAP_URL")
	if adminURL == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_BOOTSTRAP_URL required in CI")
		}
		t.Skip("requires disposable PostgreSQL")
	}
	const freshDB = "aura_remote_access_0146_drill"
	dbtest.DrillDatabase(t, adminURL, freshDB)
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + freshDB
	parsed.User = url.UserPassword("aura_migrate", mustPassword(parsed))
	migrateURL := parsed.String()
	ctx := t.Context()
	if _, err = db.Migrate(ctx, migrateURL); err != nil {
		t.Fatal(err)
	}
	if err = db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("step down 0146: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, "UPDATE aura.cloudflare_remote_access SET last_error = 'legacy diagnostic'"); err != nil {
		t.Fatalf("seed the 0145 shape: %v", err)
	}
	if err = db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("step up 0146: %v", err)
	}
	var reason string
	if err = pool.QueryRow(ctx, "SELECT reason FROM aura.cloudflare_remote_access WHERE singleton = true").Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "unknown" {
		t.Fatalf("reason=%q, want unknown", reason)
	}
}

func mustPassword(u *url.URL) string {
	p, _ := u.User.Password()
	return p
}
