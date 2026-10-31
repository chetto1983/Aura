//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

// 0148 lets the row name a refusal at the tunnel or Access step. Down has nowhere else to put the
// two codes, so they fall back to the one they were split from.
func TestMigrate0148AdmitsTheRefusalReasons(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0148_reasons")
	migrateToVersion(t, ctx, migrateURL, admin, 147)
	setReason := func(reason string) error {
		_, err := admin.Exec(ctx, `UPDATE aura.cloudflare_remote_access SET reason = $1`, reason)
		return err
	}
	if err := setReason("access_unavailable"); err == nil {
		t.Fatal("0147 admitted a reason it does not list")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0148 up: %v", err)
	}
	for _, reason := range []string{"tunnel_permission", "access_unavailable", "token_rejected", ""} {
		if err := setReason(reason); err != nil {
			t.Fatalf("set %q: %v", reason, err)
		}
	}
	if err := setReason("nonsense"); err == nil {
		t.Fatal("the check admits anything")
	}
	if err := setReason("tunnel_permission"); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0148 down: %v", err)
	}
	var reason string
	if err := admin.QueryRow(ctx, `SELECT reason FROM aura.cloudflare_remote_access WHERE singleton = true`).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "token_rejected" {
		t.Fatalf("down left reason %q", reason)
	}
}
