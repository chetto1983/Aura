//go:build db_integration

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigration0026LocalAdminCapsRoundTrip proves migration 0026 seeds `local`'s
// three EXPLICIT admin capabilities idempotently and that its down migration
// removes exactly those three rows while leaving the system-managed `*` wildcard
// (seeded by 0004) intact.
func TestMigration0026LocalAdminCapsRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	const freshDB = "aura_migrate0026_drill"
	dsn := func(role, db string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, db)
	}

	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)"); err != nil {
		t.Fatalf("pre-drop fresh db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+freshDB); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate"); err != nil {
		t.Fatalf("grant create on fresh db: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", freshDB)})
	if err != nil {
		t.Fatalf("open fresh-db admin pool: %v", err)
	}
	defer freshAdmin.Close()
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant create on public schema: %v", err)
	}

	migrateURL := dsn("aura_migrate", freshDB)
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("full Migrate up on fresh db: %v", err)
	}
	head := currentMigrationVersion(t, ctx, freshAdmin)
	if head < 26 {
		t.Fatalf("full Migrate up reached version %d, want at least 26", head)
	}

	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	const localID = "00000000-0000-0000-0000-000000000001"
	explicit := []string{"governance.write", "identity.create", "agent.run"}

	// After HEAD: local holds the seeded `*` plus the three explicit admin caps.
	caps := localCapabilitySet0026(t, ctx, app, localID)
	requireCap0026(t, caps, "*", true)
	for _, c := range explicit {
		requireCap0026(t, caps, c, true)
	}

	// Position DOWN to exactly version 26 before the ±1 straddle. golang-migrate's
	// Steps(n) is RELATIVE to the current head, so a bare -1 from HEAD would reverse the
	// NEWEST migration (0032 owner-RLS today), not 0026. Stepping this non-positive delta
	// reverses 0027..HEAD but leaves 0026 applied, so the straddle below isolates
	// migration 0026's OWN down/up regardless of how many migrations now sit above it.
	// MigrateSteps counts MIGRATIONS, not version numbers. The embedded sequence has
	// gaps since the adaptive plane's twenty-six migrations were removed, so
	// `26 - head` overshot by exactly the size of the gap and golang-migrate
	// refused with an opaque "limit N short". Ask the catalog how far back it is.
	stepsAbove26, err := MigrationStepsAbove(26)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(26): %v", err)
	}
	stepDownToV26 := -stepsAbove26
	if err := MigrateSteps(ctx, migrateURL, stepDownToV26); err != nil {
		t.Fatalf("MigrateSteps(%d) position down to v26: %v", stepDownToV26, err)
	}

	// Step DOWN 0026 (26 -> 25): the three explicit caps are removed; `*` survives.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("MigrateSteps(-1) down 0026: %v", err)
	}
	caps = localCapabilitySet0026(t, ctx, app, localID)
	requireCap0026(t, caps, "*", true)
	for _, c := range explicit {
		requireCap0026(t, caps, c, false)
	}

	// Step UP 0026 again (25 -> 26): the three explicit caps are restored idempotently.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0026: %v", err)
	}
	caps = localCapabilitySet0026(t, ctx, app, localID)
	requireCap0026(t, caps, "*", true)
	for _, c := range explicit {
		requireCap0026(t, caps, c, true)
	}

	// Restore to HEAD: re-applying 0027..HEAD brings back exactly head-26 migrations.
	reapplied, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip Migrate back to HEAD: %v", err)
	}
	// stepsAbove26 again, not `head - 26`: what comes back up is the COUNT of migrations
	// above 26, and the sequence has gaps.
	if reapplied != stepsAbove26 {
		t.Errorf("post-round-trip Migrate re-applied %d migrations, want %d (0027..HEAD)", reapplied, stepsAbove26)
	}

	// A second Migrate must be a no-op — the 0026 round-trip left no lingering pending.
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0026 reversible), got %d", n)
	}
}

// TestMigration0026RetireSafe_LocalAbsent proves the permanent fix: 0026 applies CLEANLY when
// the seeded local identity has been retired (Phase 36 cutover deletes it — serve_auth.go). A
// bare INSERT would FK-violate on capability_grants.identity_id and dirty the tracker, blocking
// every later migration; the WHERE EXISTS guard makes it a correct no-op instead (the retire flow
// already migrated local's caps onto the real user).
func TestMigration0026RetireSafe_LocalAbsent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	const freshDB = "aura_migrate0026_retire_drill"
	dsn := func(role, db string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, db)
	}
	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)"); err != nil {
		t.Fatalf("pre-drop fresh db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+freshDB); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate"); err != nil {
		t.Fatalf("grant create on fresh db: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", freshDB)})
	if err != nil {
		t.Fatalf("open fresh-db admin pool: %v", err)
	}
	defer freshAdmin.Close()
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant create on public schema: %v", err)
	}

	// Migrate to HEAD, then position DOWN to exactly v25 (below 0026).
	migrateURL := dsn("aura_migrate", freshDB)
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("full Migrate up: %v", err)
	}
	// MigrateSteps counts MIGRATIONS, not version numbers — see MigrationStepsAbove.
	stepsAbove25, err := MigrationStepsAbove(25)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(25): %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, -stepsAbove25); err != nil {
		t.Fatalf("MigrateSteps(%d) position down to v25: %v", -stepsAbove25, err)
	}

	// Simulate the Phase 36 retire: local's caps migrate away and the local row is deleted.
	const localID = "00000000-0000-0000-0000-000000000001"
	if _, err := freshAdmin.Exec(ctx, `DELETE FROM aura.capability_grants WHERE identity_id = $1::uuid`, localID); err != nil {
		t.Fatalf("delete local caps: %v", err)
	}
	if _, err := freshAdmin.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1::uuid`, localID); err != nil {
		t.Fatalf("delete local identity: %v", err)
	}

	// The crux: 0026 must step UP cleanly with local ABSENT — no FK violation, no dirty tracker.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("0026 must be retire-safe with local absent, got: %v", err)
	}
	if v := currentMigrationVersion(t, ctx, freshAdmin); v != 26 {
		t.Fatalf("version = %d after retire-safe 0026, want 26 (clean, not dirty)", v)
	}

	// And it seeded nothing for the absent local identity (retire already moved those caps).
	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()
	if caps := localCapabilitySet0026(t, ctx, app, localID); len(caps) != 0 {
		t.Fatalf("retire-safe 0026 seeded %v for an absent local identity, want nothing", caps)
	}
}

func localCapabilitySet0026(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID string) map[string]bool {
	t.Helper()
	id := pgtype.UUID{Bytes: uuid.MustParse(identityID), Valid: true}
	set := map[string]bool{}
	// Identity-scoped because aura.capability_grants fails closed under RLS from 0087
	// onward: an unscoped read is not an error, it is an EMPTY result — which read as
	// "migration 0026 never seeded the `*` capability" and pointed the blame at the
	// wrong migration entirely. Bind the identity whose grants are being asserted.
	if err := WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT capability FROM aura.capability_grants WHERE identity_id = $1`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var capability string
			if err := rows.Scan(&capability); err != nil {
				return err
			}
			set[capability] = true
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("query local capability_grants: %v", err)
	}
	return set
}

func requireCap0026(t *testing.T, set map[string]bool, capability string, want bool) {
	t.Helper()
	if set[capability] != want {
		t.Fatalf("capability %q present=%v, want present=%v (set=%v)", capability, set[capability], want, set)
	}
}
