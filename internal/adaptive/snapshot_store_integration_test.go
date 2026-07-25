//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestSnapshotStorePersistsAndLoadsExtremeNonzeroVectors(t *testing.T) {
	tests := []struct {
		name       string
		feature    FeatureKey
		scale      float64
		value      float64
		otherValue float64
	}{
		{
			name:       "huge normalized vector",
			feature:    FeatureCandidateCount,
			scale:      1e-191,
			value:      1_000_000_000,
			otherValue: 1,
		},
		{
			name:       "subnormal normalized vector",
			feature:    FeatureKNNSimilarity,
			scale:      1,
			value:      math.SmallestNonzeroFloat64,
			otherValue: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := adaptiveIntegrationPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			owner := seedTypedLedgerOwner(t, pool)
			snapshot := extremeSnapshotForOwner(
				t,
				owner,
				test.feature,
				test.scale,
				test.value,
				test.otherValue,
			)
			store := NewSnapshotStore(pool)

			if err := store.Save(ctx, snapshot); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			loaded, err := store.Load(ctx, owner, snapshot.ID())
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			assertSnapshotsEqual(t, loaded, snapshot)
		})
	}
}

func TestSnapshotValidatorMatchesGoForExtremeNonzeroVectors(t *testing.T) {
	tests := []struct {
		name       string
		feature    FeatureKey
		scale      float64
		value      float64
		otherValue float64
	}{
		{
			name:       "huge normalized vector",
			feature:    FeatureCandidateCount,
			scale:      1e-191,
			value:      1_000_000_000,
			otherValue: 1,
		},
		{
			name:       "subnormal normalized vector",
			feature:    FeatureKNNSimilarity,
			scale:      1,
			value:      math.SmallestNonzeroFloat64,
			otherValue: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := adaptiveIntegrationPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			owner := seedTypedLedgerOwner(t, pool)
			snapshot := extremeSnapshotForOwner(
				t,
				owner,
				test.feature,
				test.scale,
				test.value,
				test.otherValue,
			)

			if !snapshotValidatorAccepts(t, ctx, pool, snapshot) {
				t.Fatal("PostgreSQL validator rejected a Go-valid nonzero vector")
			}
		})
	}
}

func TestSnapshotStoreReplaysAndLoadsNanosecondCutoff(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	spec := snapshotTestSpec()
	spec.Scope.OwnerID = owner
	spec.Scope.TrainingCutoff = spec.Scope.TrainingCutoff.Add(123 * time.Nanosecond)
	for actionIndex := range spec.Actions {
		for exampleIndex := range spec.Actions[actionIndex].Examples {
			spec.Actions[actionIndex].Examples[exampleIndex].OwnerID = owner
		}
	}
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatal(err)
	}
	store := NewSnapshotStore(pool)

	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() exact replay error = %v", err)
	}
	loaded, err := store.Load(ctx, owner, snapshot.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertSnapshotsEqual(t, loaded, snapshot)
}

func TestSnapshotStorePersistsAndLoadsExactReplay(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	store := NewSnapshotStore(pool)

	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() exact replay error = %v", err)
	}
	migratePool := typedLedgerMigrationPool(t, ctx)
	var rows int
	if err := migratePool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_policy_snapshots
		 WHERE owner_id=$1 AND id=$2`,
		owner,
		snapshot.ID(),
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("stored snapshot rows = %d, want 1", rows)
	}

	loaded, err := store.Load(ctx, owner, snapshot.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertSnapshotsEqual(t, loaded, snapshot)

	loadedActions := loaded.Actions()
	loadedActions[0].ActionID = "mutated"
	reloaded, err := store.Load(ctx, owner, snapshot.ID())
	if err != nil {
		t.Fatalf("Load() after caller mutation error = %v", err)
	}
	assertSnapshotsEqual(t, reloaded, snapshot)
}

func TestSnapshotStoreScopesLoadsByOwner(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	foreignOwner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1,$2,'user')`,
		foreignOwner,
		"snapshot-corruption-"+foreignOwner.String(),
	); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotForOwner(t, owner)
	store := NewSnapshotStore(pool)
	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load(ctx, foreignOwner, snapshot.ID()); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("cross-owner Load() error = %v, want ErrSnapshotNotFound", err)
	}
	if _, err := store.Load(
		ctx,
		uuid.Must(uuid.NewV7()),
		snapshot.ID(),
	); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("missing-owner Load() error = %v, want ErrSnapshotNotFound", err)
	}
}

func TestSnapshotStoreRejectsFencedOwner(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	migratePool := typedLedgerMigrationPool(t, ctx)
	var fenced bool
	if err := migratePool.QueryRow(
		ctx,
		`SELECT aura.fence_adaptive_identity($1)`,
		owner,
	).Scan(&fenced); err != nil {
		t.Fatal(err)
	}
	if !fenced {
		t.Fatal("fence_adaptive_identity() = false")
	}

	err := NewSnapshotStore(pool).Save(ctx, snapshot)
	if !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("Save() after owner fence error = %v, want ErrOwnerTombstoned", err)
	}
	if _, err := NewSnapshotStore(pool).Load(
		ctx,
		owner,
		snapshot.ID(),
	); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("Load() after owner fence error = %v, want ErrOwnerTombstoned", err)
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
		t.Fatalf("snapshot rows after owner fence = %d, want 0", rows)
	}
}

func TestSnapshotStoreDetectsStoredConflictAndCorruption(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t,
		ctx,
		"aura_snapshot_corruption",
		68,
	)
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	store := NewSnapshotStore(pool)
	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	migratePool := openSnapshotMigrationPool(t, ctx, migrateURL)

	if _, err := migratePool.Exec(
		ctx,
		`ALTER TABLE aura.adaptive_policy_snapshots
		 DROP CONSTRAINT adaptive_policy_snapshots_artifact_scope_check`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := migratePool.Exec(
		ctx,
		`UPDATE aura.adaptive_policy_snapshots
		 SET provider_id='tampered-provider' WHERE id=$1`,
		snapshot.ID(),
	); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, snapshot); !errors.Is(err, ErrSnapshotConflict) {
		t.Fatalf("Save() over conflicting row error = %v, want ErrSnapshotConflict", err)
	}
	if _, err := store.Load(ctx, owner, snapshot.ID()); !errors.Is(err, ErrPersistedSnapshotInvalid) {
		t.Fatalf("Load() provider-mismatched row error = %v, want ErrPersistedSnapshotInvalid", err)
	}

	scope := snapshot.Scope()
	if _, err := migratePool.Exec(
		ctx,
		`UPDATE aura.adaptive_policy_snapshots
		 SET provider_id=$2 WHERE id=$1`,
		snapshot.ID(),
		scope.ProviderID,
	); err != nil {
		t.Fatal(err)
	}
	foreignOwner := seedTypedLedgerOwner(t, pool)
	for _, corruption := range []struct {
		name       string
		setSQL     string
		setValue   any
		loadOwner  uuid.UUID
		restoreSQL string
		restore    any
	}{
		{
			name:       "model",
			setSQL:     "model_id=$2",
			setValue:   "tampered-model",
			loadOwner:  owner,
			restoreSQL: "model_id=$2",
			restore:    scope.ModelID,
		},
		{
			name:       "policy",
			setSQL:     "policy_version=$2",
			setValue:   "tampered-policy",
			loadOwner:  owner,
			restoreSQL: "policy_version=$2",
			restore:    scope.PolicyVersion,
		},
		{
			name:       "owner",
			setSQL:     "owner_id=$2",
			setValue:   foreignOwner,
			loadOwner:  foreignOwner,
			restoreSQL: "owner_id=$2",
			restore:    owner,
		},
	} {
		t.Run(corruption.name, func(t *testing.T) {
			if _, err := migratePool.Exec(
				ctx,
				"UPDATE aura.adaptive_policy_snapshots SET "+
					corruption.setSQL+" WHERE id=$1",
				snapshot.ID(),
				corruption.setValue,
			); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(
				ctx,
				corruption.loadOwner,
				snapshot.ID(),
			); !errors.Is(err, ErrPersistedSnapshotInvalid) {
				t.Fatalf(
					"Load() %s-mismatched row error = %v, want ErrPersistedSnapshotInvalid",
					corruption.name,
					err,
				)
			}
			if _, err := migratePool.Exec(
				ctx,
				"UPDATE aura.adaptive_policy_snapshots SET "+
					corruption.restoreSQL+" WHERE id=$1",
				snapshot.ID(),
				corruption.restore,
			); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := migratePool.Exec(
		ctx,
		`ALTER TABLE aura.adaptive_policy_snapshots
		 DROP CONSTRAINT adaptive_policy_snapshots_artifact_digest_check`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := migratePool.Exec(
		ctx,
		`UPDATE aura.adaptive_policy_snapshots
		 SET sha256=decode(repeat('00',32),'hex') WHERE id=$1`,
		snapshot.ID(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, owner, snapshot.ID()); !errors.Is(err, ErrPersistedSnapshotInvalid) {
		t.Fatalf("Load() digest-mismatched row error = %v, want ErrPersistedSnapshotInvalid", err)
	}
	if _, err := migratePool.Exec(
		ctx,
		`UPDATE aura.adaptive_policy_snapshots
		 SET sha256=decode($2,'hex') WHERE id=$1`,
		snapshot.ID(),
		snapshot.SHA256(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := migratePool.Exec(
		ctx,
		`UPDATE aura.adaptive_policy_snapshots
		 SET artifact=artifact || convert_to(' ', 'UTF8') WHERE id=$1`,
		snapshot.ID(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, owner, snapshot.ID()); !errors.Is(err, ErrPersistedSnapshotInvalid) {
		t.Fatalf("Load() noncanonical artifact error = %v, want ErrPersistedSnapshotInvalid", err)
	}
}

func snapshotForOwner(t *testing.T, owner uuid.UUID) *PolicySnapshot {
	t.Helper()
	spec := snapshotTestSpec()
	spec.Scope.OwnerID = owner
	for actionIndex := range spec.Actions {
		for exampleIndex := range spec.Actions[actionIndex].Examples {
			spec.Actions[actionIndex].Examples[exampleIndex].OwnerID = owner
		}
	}
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func extremeSnapshotForOwner(
	t *testing.T,
	owner uuid.UUID,
	feature FeatureKey,
	scale float64,
	value float64,
	otherValue float64,
) *PolicySnapshot {
	t.Helper()
	spec := snapshotTestSpec()
	spec.Scope.OwnerID = owner
	spec.FeatureSchema = []SnapshotFeatureDefinition{
		{Key: feature, Scale: scale},
	}
	spec.Actions[0].Examples = []SnapshotExample{
		snapshotSingleFeatureExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000041",
			0.9,
			feature,
			value,
		),
	}
	spec.Actions[1].Examples = []SnapshotExample{
		snapshotSingleFeatureExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000042",
			0.1,
			feature,
			otherValue,
		),
	}
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertSnapshotsEqual(t *testing.T, got, want *PolicySnapshot) {
	t.Helper()
	gotArtifact, gotErr := got.canonicalArtifact()
	wantArtifact, wantErr := want.canonicalArtifact()
	if gotErr != nil || wantErr != nil {
		t.Fatalf("canonical snapshot artifacts = (%v, %v)", gotErr, wantErr)
	}
	if got.ID() != want.ID() ||
		got.SHA256() != want.SHA256() ||
		got.Scope() != want.Scope() ||
		got.ChampionActionID() != want.ChampionActionID() ||
		!bytes.Equal(gotArtifact, wantArtifact) {
		t.Fatalf("loaded snapshot does not equal stored snapshot")
	}
}

func assertSnapshotRowMatches(
	t *testing.T,
	row sqlc.AuraAdaptivePolicySnapshots,
	snapshot *PolicySnapshot,
) {
	t.Helper()
	artifact, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	if !row.ID.Valid ||
		uuid.UUID(row.ID.Bytes) != snapshot.ID() ||
		!bytes.Equal(row.Artifact, artifact) {
		t.Fatalf("snapshot row = %+v, want ID %s and canonical artifact", row, snapshot.ID())
	}
}

func snapshotRowForOwner(
	t *testing.T,
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	snapshotID uuid.UUID,
) sqlc.AuraAdaptivePolicySnapshots {
	t.Helper()
	row, err := queries.GetAdaptivePolicySnapshot(
		ctx,
		sqlc.GetAdaptivePolicySnapshotParams{
			OwnerID:    dbUUID(ownerID),
			SnapshotID: dbUUID(snapshotID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("snapshot row %s not found", snapshotID)
	}
	if err != nil {
		t.Fatal(err)
	}
	return row
}
