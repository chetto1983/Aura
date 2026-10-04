//go:build db_integration

package dbtest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The point of DrillDatabase is the end of the test, not the start: the helpers it replaced
// created their database fine and left it behind on every run. So the assertion that matters
// is made after the subtest that owns the database has finished.
func TestDrillDatabaseLeavesNothingBehind(t *testing.T) {
	pwd := os.Getenv("POSTGRES_PASSWORD")
	if pwd == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("dbtest integration requires POSTGRES_PASSWORD under CI")
		}
		t.Skip("dbtest integration requires POSTGRES_PASSWORD")
	}
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	admin := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	const name = "aura_dbtest_drill"

	t.Run("owner", func(t *testing.T) {
		DrillDatabase(t, admin, name)
		if !databaseExists(t, admin, name) {
			t.Fatalf("DrillDatabase did not create %s", name)
		}
		if err := execErr(withDatabase(t, admin, name), "SET ROLE aura_migrate; CREATE TABLE drill_probe (id int)"); err != nil {
			t.Fatalf("aura_migrate cannot create in the drill database: %v", err)
		}
	})
	if databaseExists(t, admin, name) {
		t.Fatalf("%s outlived the test that created it", name)
	}
}

func databaseExists(t *testing.T, admin, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil {
		t.Fatalf("look up %s: %v", name, err)
	}
	return exists
}
