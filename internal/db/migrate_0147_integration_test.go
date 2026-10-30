//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

// 0147 gives the remote-access resources their own account, zone and legacy WARP hostname, so a
// change to the desired columns stops redefining what the resources belong to. The table is a
// singleton, so each backfill shape is one pass over the one row: up, read, down.
func TestMigrate0147BackfillsOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0147_ownership")
	migrateToVersion(t, ctx, migrateURL, admin, 146)
	assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "resource_account_id", false)

	cases := []struct {
		name                         string
		ids                          []string
		account, zone, owned, legacy string
	}{
		{"tunnel and warp dns", []string{"tunnel_id", "warp_dns_id"}, "A", "a.it", "aura.a.it", "aura-warp.a.it"},
		{"pending legacy row, zone only", []string{"zone_id"}, "A", "a.it", "aura.a.it", ""},
		{"partial delete, otp provider only", []string{"otp_idp_id"}, "A", "a.it", "aura.a.it", ""},
		{"warp posture only", []string{"warp_posture_id"}, "A", "a.it", "aura.a.it", "aura-warp.a.it"},
		{"warp app only", []string{"warp_app_id"}, "A", "a.it", "aura.a.it", "aura-warp.a.it"},
		{"warp policy only", []string{"warp_policy_id"}, "A", "a.it", "aura.a.it", "aura-warp.a.it"},
		{"no resource id", nil, "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := admin.Exec(ctx, `
UPDATE aura.cloudflare_remote_access
SET account_id = 'A', zone_name = 'a.it', public_label = 'aura', warp_label = 'aura-warp', owned_hostname = '',
    zone_id = '', tunnel_id = '', public_dns_id = '', warp_dns_id = '', otp_idp_id = '',
    public_app_id = '', public_policy_id = '', warp_app_id = '', warp_policy_id = '', warp_posture_id = ''`); err != nil {
				t.Fatalf("seed the 0146 shape: %v", err)
			}
			for _, col := range tc.ids {
				if _, err := admin.Exec(ctx, `UPDATE aura.cloudflare_remote_access SET `+col+` = 'x'`); err != nil {
					t.Fatalf("seed %s: %v", col, err)
				}
			}
			if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
				t.Fatalf("migrate 0147 up: %v", err)
			}
			var account, zone, owned, legacy string
			if err := admin.QueryRow(ctx, `SELECT resource_account_id, owned_zone_name, owned_hostname, legacy_warp_hostname
FROM aura.cloudflare_remote_access WHERE singleton = true`).Scan(&account, &zone, &owned, &legacy); err != nil {
				t.Fatalf("read the 0147 backfill: %v", err)
			}
			if account != tc.account || zone != tc.zone || owned != tc.owned || legacy != tc.legacy {
				t.Fatalf("backfill = account:%q zone:%q owned:%q legacy:%q, want %q/%q/%q/%q",
					account, zone, owned, legacy, tc.account, tc.zone, tc.owned, tc.legacy)
			}
			if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
				t.Fatalf("migrate 0147 down: %v", err)
			}
			assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "resource_account_id", false)
			assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "owned_zone_name", false)
			assertColumnExists(t, ctx, admin, "cloudflare_remote_access", "legacy_warp_hostname", false)
		})
	}
}
