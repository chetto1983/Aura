//go:build db_integration

package adaptive

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
)

func TestSnapshotMigration0070UpgradeDownAndUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t,
		ctx,
		"aura_snapshot_duplicate_scale",
		69,
	)
	before := readSnapshotValidatorDefinition(t, ctx, pool)
	assertSnapshotMigrationContract(t, ctx, pool)
	owner := seedTypedLedgerOwner(t, pool)
	snapshots := []*PolicySnapshot{
		extremeSnapshotForOwner(
			t,
			owner,
			FeatureCandidateCount,
			1e-191,
			1_000_000_000,
			1,
		),
		extremeSnapshotForOwner(
			t,
			owner,
			FeatureKNNSimilarity,
			1,
			math.SmallestNonzeroFloat64,
			1,
		),
	}
	assertSnapshotValidatorsAccept(t, ctx, pool, snapshots)

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("upgrade 0069 to 0070: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 70)
	if err := db.CheckMigrationHead(ctx, migrateURL); err != nil {
		t.Fatalf("CheckMigrationHead() at 0070: %v", err)
	}
	assertSnapshotMigrationContract(t, ctx, pool)
	definition := readSnapshotValidatorDefinition(t, ctx, pool)
	if strings.Contains(definition, "outcome_ids") ||
		!strings.Contains(definition, "HAVING count(*) > 1") {
		t.Fatal("0070 validator did not replace the quadratic duplicate path")
	}
	assertSnapshotValidatorsAccept(t, ctx, pool, snapshots)

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("downgrade 0070 to 0069: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 69)
	assertSnapshotMigrationContract(t, ctx, pool)
	if got := readSnapshotValidatorDefinition(t, ctx, pool); got != before {
		t.Fatal("0070 down did not restore the exact 0069 validator")
	}
	assertSnapshotValidatorsAccept(t, ctx, pool, snapshots)

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-upgrade 0069 to 0070: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 70)
	assertSnapshotMigrationContract(t, ctx, pool)
	assertSnapshotValidatorsAccept(t, ctx, pool, snapshots)
}

func assertSnapshotValidatorsAccept(
	t *testing.T,
	ctx context.Context,
	pool snapshotValidatorQuerier,
	snapshots []*PolicySnapshot,
) {
	t.Helper()
	for _, snapshot := range snapshots {
		if !snapshotValidatorAccepts(t, ctx, pool, snapshot) {
			t.Fatalf("validator rejected snapshot %s", snapshot.ID())
		}
	}
}
