//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSnapshotStoreRejectsFreshInsertRepresentationDrift(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t,
		ctx,
		"aura_snapshot_insert_verification",
		69,
	)
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	migratePool := openSnapshotMigrationPool(t, ctx, migrateURL)
	if _, err := migratePool.Exec(
		ctx,
		`ALTER TABLE aura.adaptive_policy_snapshots
		 DROP CONSTRAINT adaptive_policy_snapshots_artifact_scope_check;
		 CREATE FUNCTION aura.test_drift_snapshot_cutoff()
		 RETURNS trigger
		 LANGUAGE plpgsql
		 SET search_path = pg_catalog
		 AS $$
		 BEGIN
		     NEW.training_cutoff := NEW.training_cutoff + interval '1 microsecond';
		     RETURN NEW;
		 END;
		 $$;
		 CREATE TRIGGER test_drift_snapshot_cutoff
		 BEFORE INSERT ON aura.adaptive_policy_snapshots
		 FOR EACH ROW EXECUTE FUNCTION aura.test_drift_snapshot_cutoff()`,
	); err != nil {
		t.Fatal(err)
	}

	err := NewSnapshotStore(pool).Save(ctx, snapshot)
	if !errors.Is(err, ErrPersistedSnapshotInvalid) {
		t.Fatalf("Save() error = %v, want ErrPersistedSnapshotInvalid", err)
	}
	var rows int
	if err := migratePool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_policy_snapshots WHERE id=$1`,
		snapshot.ID(),
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("fresh invalid snapshot rows = %d, want rolled back insert", rows)
	}
}
