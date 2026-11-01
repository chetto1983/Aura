//go:build db_integration

package db

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestMigrate0149AddsBoundedProgressAndPreservesExistingCards(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0149_progress")
	migrateToVersion(t, ctx, migrateURL, admin, 148)
	var identityID, boardID, cardID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO aura.boards (identity_id, name, columns)
VALUES ($1, 'progress migration', '[]') RETURNING id::text`, identityID).Scan(&boardID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO aura.board_cards
(identity_id, board_id, column_id, position, label, source, updated_by)
VALUES ($1, $2, 'todo', 1, 'renew the contract', 'cockpit', 'operator') RETURNING id::text`, identityID, boardID).Scan(&cardID); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatal(err)
	}
	var progress float64
	if err := admin.QueryRow(ctx, `SELECT progress FROM aura.board_cards WHERE id=$1`, cardID).Scan(&progress); err != nil || progress != 0 {
		t.Fatalf("existing card default: %v, %v", progress, err)
	}
	for _, valid := range []float64{0, 0.55, 1} {
		if _, err := admin.Exec(ctx, `UPDATE aura.board_cards SET progress=$1 WHERE id=$2`, valid, cardID); err != nil {
			t.Fatalf("valid progress %v: %v", valid, err)
		}
	}
	for _, invalid := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := admin.Exec(ctx, `UPDATE aura.board_cards SET progress=$1 WHERE id=$2`, invalid, cardID); err == nil {
			t.Fatalf("constraint admitted %v", invalid)
		}
	}
	if _, err := admin.Exec(ctx, `UPDATE aura.board_cards SET progress=NULL WHERE id=$1`, cardID); err == nil {
		t.Fatal("progress admitted NULL")
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatal(err)
	}
	var label string
	if err := admin.QueryRow(ctx, `SELECT label FROM aura.board_cards WHERE id=$1`, cardID).Scan(&label); err != nil || label != "renew the contract" {
		t.Fatalf("rollback lost the card: %q, %v", label, err)
	}
	if _, err := admin.Exec(ctx, `SELECT progress FROM aura.board_cards`); err == nil {
		t.Fatal("rollback retained progress")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT progress FROM aura.board_cards WHERE id=$1`, cardID).Scan(&progress); err != nil || progress != 0 {
		t.Fatalf("reapplied default: %v, %v", progress, err)
	}
}
