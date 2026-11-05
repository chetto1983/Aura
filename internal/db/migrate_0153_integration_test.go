//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

func TestMigrate0153AddsTheDecisionColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0153_decide")
	migrateToVersion(t, ctx, migrateURL, admin, 152)
	var identityID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.pim_watch_labels (identity_id, name) VALUES ($1, 'L')`, identityID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.pim_watch_verdicts
(identity_id, account_id, resource, item_id, tier, decided_by, ladder_by) VALUES ($1, 'acc', 'mail', 'm1', 'today', 'people', 'people')`, identityID); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatal(err)
	}
	var description string
	var discard, discarded, skipped, probabilityNull bool
	if err := admin.QueryRow(ctx, `SELECT description, discard FROM aura.pim_watch_labels WHERE name = 'L'`).Scan(&description, &discard); err != nil {
		t.Fatal(err)
	}
	if description != "" || discard {
		t.Fatalf("existing label: description %q, discard %v", description, discard)
	}
	if err := admin.QueryRow(ctx, `SELECT discarded, model_skipped, model_probability IS NULL FROM aura.pim_watch_verdicts
WHERE item_id = 'm1'`).Scan(&discarded, &skipped, &probabilityNull); err != nil {
		t.Fatal(err)
	}
	if discarded || skipped || !probabilityNull {
		t.Fatalf("existing row: discarded %v, model_skipped %v, probability null %v", discarded, skipped, probabilityNull)
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.pim_watch_verdicts SET decided_by = 'model' WHERE item_id = 'm1'`); err != nil {
		t.Fatalf("decided_by model refused: %v", err)
	}
	for _, refused := range []string{
		`UPDATE aura.pim_watch_labels SET description = repeat('a', 301) WHERE name = 'L'`,
		`UPDATE aura.pim_watch_verdicts SET model_tier = 'soon' WHERE item_id = 'm1'`,
		`UPDATE aura.pim_watch_verdicts SET model_probability = 1.5 WHERE item_id = 'm1'`,
		`UPDATE aura.pim_watch_verdicts SET model_options = 1 WHERE item_id = 'm1'`,
	} {
		if _, err := admin.Exec(ctx, refused); err == nil {
			t.Fatalf("constraint admitted: %s", refused)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.pim_watch_verdicts
(identity_id, account_id, resource, item_id, tier, decided_by) VALUES ($1, 'acc', 'mail', 'm2', 'later', 'model')`, identityID); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatal(err)
	}
	for item, want := range map[string]string{"m1": "people", "m2": "default"} {
		var by string
		if err := admin.QueryRow(ctx, `SELECT decided_by FROM aura.pim_watch_verdicts WHERE item_id = $1`, item).Scan(&by); err != nil || by != want {
			t.Fatalf("rollback of %s: decided_by %q, %v, want %q", item, by, err, want)
		}
	}
	for _, gone := range []string{
		`SELECT description FROM aura.pim_watch_labels`,
		`SELECT discard FROM aura.pim_watch_labels`,
		`SELECT label_descriptions_seeded FROM aura.pim_watch_settings`,
		`SELECT model_probability FROM aura.pim_watch_verdicts`,
		`SELECT discarded FROM aura.pim_watch_verdicts`,
		`SELECT identity_id FROM aura.pim_watch_accounts`,
	} {
		if _, err := admin.Exec(ctx, gone); err == nil {
			t.Fatalf("rollback retained: %s", gone)
		}
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.pim_watch_verdicts SET decided_by = 'model' WHERE item_id = 'm1'`); err == nil {
		t.Fatal("rollback restored a CHECK that still admits model")
	}
}
