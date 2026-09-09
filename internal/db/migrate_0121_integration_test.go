//go:build db_integration

// Integration coverage for migration 0121 (retire the capability wildcard, RBAC-01/D-04):
// seeds one fresh identity holding ONLY the '*' wildcard, migrates up through 0121, and
// asserts the wildcard row is gone and exactly the six explicit rows are present for both
// the fresh identity and the already-explicit `local` identity (0026) — no duplicate row,
// no leftover wildcard. Then steps back down and asserts a '*' row is restored for both.
//
// Run via: go test -tags db_integration -race ./internal/db -run TestMigrate0121 -count=1

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0121_RetiresWildcard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0121_wildcard")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to head: %v", err)
	}
	headVersion := currentMigrationVersion(t, ctx, admin)
	if headVersion < 121 {
		t.Fatalf("full Migrate up reached version %d, want at least 121", headVersion)
	}
	// Step down to exactly the pre-0121 world. MigrationStepsAbove(120) counts every
	// migration above version 120 regardless of how far HEAD has since advanced past
	// 121, mirroring migrate_0019_integration_test.go's future-proofing.
	stepsToBefore0121, err := MigrationStepsAbove(120)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(120): %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, -stepsToBefore0121); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0121: %v", -stepsToBefore0121, err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 120 {
		t.Fatalf("version after stepping down = %d, want 120", got)
	}

	// A fresh, non-seeded identity holding ONLY the wildcard — the adjacency case
	// (must-haves truth: a '*' row and a real-capability row are distinct rows after
	// the migration, never both surviving for the same identity).
	freshID := uuid.New()
	if _, err := admin.Exec(ctx,
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, 'wildcard_only', 'service')",
		freshID,
	); err != nil {
		t.Fatalf("seed fresh identity: %v", err)
	}
	if _, err := admin.Exec(ctx,
		"INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1, '*')",
		freshID,
	); err != nil {
		t.Fatalf("seed fresh wildcard grant: %v", err)
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0121 up: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 121 {
		t.Fatalf("version after up = %d, want 121", got)
	}

	assertExplicitSixNoWildcard(t, ctx, admin, freshID.String())
	assertExplicitSixNoWildcard(t, ctx, admin, seededOperatorIdentity)

	var wildcardRows int
	if err := admin.QueryRow(ctx,
		"SELECT count(*) FROM aura.capability_grants WHERE capability = '*'",
	).Scan(&wildcardRows); err != nil {
		t.Fatalf("count wildcard rows after up: %v", err)
	}
	if wildcardRows != 0 {
		t.Fatalf("wildcard rows after up = %d, want 0", wildcardRows)
	}

	// Down: restores a '*' row for identities holding the full six-name set.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0121 down: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 120 {
		t.Fatalf("version after down = %d, want 120", got)
	}
	for _, id := range []string{freshID.String(), seededOperatorIdentity} {
		var n int
		if err := admin.QueryRow(ctx,
			"SELECT count(*) FROM aura.capability_grants WHERE identity_id = $1 AND capability = '*'",
			id,
		).Scan(&n); err != nil {
			t.Fatalf("count wildcard row for %s after down: %v", id, err)
		}
		if n != 1 {
			t.Errorf("wildcard row for %s after down = %d, want 1", id, n)
		}
	}
}

// assertExplicitSixNoWildcard asserts identityID holds exactly the six declared
// capability names (internal/identity.All()'s source order, alphabetized here since the
// query sorts by capability) and nothing else — no duplicate row, no leftover wildcard.
func assertExplicitSixNoWildcard(t *testing.T, ctx context.Context, admin *pgxpool.Pool, identityID string) {
	t.Helper()
	rows, err := admin.Query(ctx,
		"SELECT capability FROM aura.capability_grants WHERE identity_id = $1 ORDER BY capability", identityID)
	if err != nil {
		t.Fatalf("list grants for %s: %v", identityID, err)
	}
	defer rows.Close()
	var caps []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan grant for %s: %v", identityID, err)
		}
		caps = append(caps, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate grants for %s: %v", identityID, err)
	}
	want := []string{
		"agent.run", "governance.read", "governance.write",
		"identity.create", "identity.delete", "share.public",
	}
	if len(caps) != len(want) {
		t.Fatalf("grants for %s = %v, want %v", identityID, caps, want)
	}
	for i := range want {
		if caps[i] != want[i] {
			t.Errorf("grants for %s[%d] = %q, want %q", identityID, i, caps[i], want[i])
		}
	}
}
