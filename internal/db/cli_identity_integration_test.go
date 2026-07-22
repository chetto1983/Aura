//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

// TestMigrateSeedsCLIServiceIdentity proves the durable FK owner required by
// deployment-time CLI idempotency exists after the full migration catalog runs.
// It deliberately carries no capability grant and is never a login identity.
func TestMigrateSeedsCLIServiceIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := Open(ctx, &Config{URL: envOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer pool.Close()

	var identityCount, grantCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*),
		       (SELECT count(*) FROM aura.capability_grants WHERE identity_id = $1::uuid)
		FROM aura.identities
		WHERE id = $1::uuid AND name = 'aura-cli' AND kind = 'service'`,
		"00000000-0000-0000-0000-000000000039",
	).Scan(&identityCount, &grantCount); err != nil {
		t.Fatalf("query CLI service identity: %v", err)
	}
	if identityCount != 1 || grantCount != 0 {
		t.Fatalf("CLI identity rows=%d grants=%d, want one identity and zero grants", identityCount, grantCount)
	}
}
