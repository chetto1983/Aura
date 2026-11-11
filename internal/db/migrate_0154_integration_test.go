//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

func TestMigrate0154AddsTheDefaultChannel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0154_notify")
	migrateToVersion(t, ctx, migrateURL, admin, 153)
	var identityID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.identity_profiles (identity_id, display_name) VALUES ($1, 'A')`, identityID); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatal(err)
	}
	var route string
	if err := admin.QueryRow(ctx, `SELECT default_notify FROM aura.identity_profiles WHERE identity_id = $1`, identityID).Scan(&route); err != nil {
		t.Fatal(err)
	}
	if route != "" {
		t.Fatalf("existing profile: default_notify %q, want not set", route)
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.identity_profiles SET default_notify = 'telegram' WHERE identity_id = $1`, identityID); err != nil {
		t.Fatalf("telegram refused: %v", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.identity_profiles SET default_notify = 'sms' WHERE identity_id = $1`, identityID); err == nil {
		t.Fatal("constraint admitted sms")
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `SELECT default_notify FROM aura.identity_profiles`); err == nil {
		t.Fatal("rollback retained default_notify")
	}
}
