//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

func TestMigrate0152AddsTheShadowColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0152_shadow")
	migrateToVersion(t, ctx, migrateURL, admin, 151)
	var identityID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.pim_watch_verdicts
(identity_id, account_id, resource, item_id, tier, decided_by) VALUES ($1, 'acc', 'mail', 'm1', 'today', 'people')`, identityID); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatal(err)
	}
	var ladderNull, writtenTo, bulk bool
	if err := admin.QueryRow(ctx, `SELECT ladder_tier IS NULL, written_to, bulk FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND item_id = 'm1'`, identityID).Scan(&ladderNull, &writtenTo, &bulk); err != nil {
		t.Fatal(err)
	}
	if !ladderNull || writtenTo || bulk {
		t.Fatalf("existing row: ladder_tier null %v, written_to %v, bulk %v", ladderNull, writtenTo, bulk)
	}
	for _, refused := range []string{
		`UPDATE aura.pim_watch_verdicts SET ladder_tier = 'soon' WHERE item_id = 'm1'`,
		`UPDATE aura.pim_watch_verdicts SET ladder_by = 'person' WHERE item_id = 'm1'`,
		`UPDATE aura.pim_watch_verdicts SET centroid_tier = 'soon' WHERE item_id = 'm1'`,
		`UPDATE aura.pim_watch_verdicts SET sender_memory_tier = 'soon' WHERE item_id = 'm1'`,
	} {
		if _, err := admin.Exec(ctx, refused); err == nil {
			t.Fatalf("constraint admitted: %s", refused)
		}
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.pim_watch_verdicts SET ladder_tier = 'now', ladder_by = 'rule',
centroid_tier = 'later', centroid_margin = 0.04, sender_memory_tier = 'today' WHERE item_id = 'm1'`); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `SELECT ladder_tier FROM aura.pim_watch_verdicts`); err == nil {
		t.Fatal("rollback retained ladder_tier")
	}
	var tier string
	if err := admin.QueryRow(ctx, `SELECT tier FROM aura.pim_watch_verdicts WHERE item_id = 'm1'`).Scan(&tier); err != nil || tier != "today" {
		t.Fatalf("rollback lost the verdict: %q, %v", tier, err)
	}
}
