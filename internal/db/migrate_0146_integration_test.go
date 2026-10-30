//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

// 0146 replaces the remote-access diagnostic sentence with a reason code and records the hostname
// a row's resources belong to and the hostname the operator accepted. The table is a singleton,
// so the positive and negative backfill shapes run as two passes over the one row.
func TestMigrate0146BackfillsReasonAndHostnames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0146_reasons")
	migrateToVersion(t, ctx, migrateURL, admin, 145)
	assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "last_error", true)
	assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "reason", false)

	seed := func(lastError, dnsID string, healthy bool) {
		t.Helper()
		if _, err := admin.Exec(ctx, `
UPDATE aura.cloudflare_remote_access
SET last_error = $1, public_dns_id = $2, observed_healthy = $3, public_label = 'aura', zone_name = 'a.it'`,
			lastError, dnsID, healthy); err != nil {
			t.Fatalf("seed the 0145 shape: %v", err)
		}
	}
	seed("legacy diagnostic", "d1", true)
	up := func() {
		t.Helper()
		if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
			t.Fatalf("migrate 0146 up: %v", err)
		}
	}
	read := func() (reason, owned, accepted string) {
		t.Helper()
		if err := admin.QueryRow(ctx, `SELECT reason, owned_hostname, accepted_hostname
FROM aura.cloudflare_remote_access WHERE singleton = true`).Scan(&reason, &owned, &accepted); err != nil {
			t.Fatalf("read the 0146 backfill: %v", err)
		}
		return reason, owned, accepted
	}
	up()
	if reason, owned, accepted := read(); reason != "unknown" || owned != "aura.a.it" || accepted != "aura.a.it" {
		t.Fatalf("backfill = reason:%q owned:%q accepted:%q, want unknown/aura.a.it/aura.a.it", reason, owned, accepted)
	}
	assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "last_error", false)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0146 down: %v", err)
	}
	assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "last_error", true)
	assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "reason", false)
	var restored string
	if err := admin.QueryRow(ctx, `SELECT last_error FROM aura.cloudflare_remote_access WHERE singleton = true`).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored == "" {
		t.Fatal("0146 down did not restore last_error for a row that had a reason")
	}

	seed("", "", false)
	up()
	if reason, owned, accepted := read(); reason != "" || owned != "" || accepted != "" {
		t.Fatalf("clean row backfill = reason:%q owned:%q accepted:%q, want all empty", reason, owned, accepted)
	}
}
