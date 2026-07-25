//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxSnapshotStatementTimeout = 8 * time.Second
	maxSnapshotEndToEndTimeout  = 20 * time.Second
)

func TestSnapshotStorePersistsMaximumCardinalityWithinBound(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), maxSnapshotEndToEndTimeout)
	defer cancel()
	basePool := adaptiveIntegrationPool(t)
	owner := seedTypedLedgerOwner(t, basePool)
	boundedPool := boundedSnapshotPool(t, ctx, maxSnapshotStatementTimeout)
	snapshotStarted := time.Now()
	snapshot := maximumCardinalitySnapshot(t, owner)
	t.Logf("constructed 32,768-outcome snapshot in %s", time.Since(snapshotStarted))
	store := NewSnapshotStore(boundedPool)

	saveStarted := time.Now()
	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() maximum-cardinality snapshot after %s: %v", time.Since(saveStarted), err)
	}
	t.Logf("saved 32,768-outcome snapshot in %s", time.Since(saveStarted))

	loadStarted := time.Now()
	loaded, err := store.Load(ctx, owner, snapshot.ID())
	if err != nil {
		t.Fatalf("Load() maximum-cardinality snapshot after %s: %v", time.Since(loadStarted), err)
	}
	t.Logf("loaded 32,768-outcome snapshot in %s", time.Since(loadStarted))
	if loaded.ID() != snapshot.ID() ||
		loaded.SHA256() != snapshot.SHA256() ||
		len(loaded.Actions()) != maxSnapshotActions {
		t.Fatal("loaded maximum-cardinality snapshot does not match stored identity")
	}
	for _, action := range loaded.Actions() {
		if len(action.Examples) != maxSnapshotExamplesPerAction {
			t.Fatalf(
				"loaded action %q examples = %d, want %d",
				action.ActionID,
				len(action.Examples),
				maxSnapshotExamplesPerAction,
			)
		}
	}
	assertSnapshotOwnerLockReleased(t, owner)

	if elapsed := time.Since(started); elapsed >= maxSnapshotEndToEndTimeout {
		t.Fatalf(
			"maximum-cardinality Save/Load elapsed %s, bound %s",
			elapsed,
			maxSnapshotEndToEndTimeout,
		)
	}
	t.Logf("maximum-cardinality persistence completed in %s", time.Since(started))
}

func TestSnapshotValidatorRejectsCrossActionDuplicateOutcome(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	artifact, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	actionCatalog, err := json.Marshal(snapshot.Actions())
	if err != nil {
		t.Fatal(err)
	}
	uniqueID := []byte("00000000-0000-4000-8000-000000000003")
	duplicateID := []byte("00000000-0000-4000-8000-000000000001")
	duplicateArtifact := bytes.Replace(artifact, uniqueID, duplicateID, 1)
	duplicateCatalog := bytes.Replace(actionCatalog, uniqueID, duplicateID, 1)
	if bytes.Equal(duplicateArtifact, artifact) ||
		bytes.Equal(duplicateCatalog, actionCatalog) {
		t.Fatal("cross-action duplicate fixture did not change the snapshot document")
	}

	if snapshotValidatorAcceptsPayload(
		t,
		ctx,
		pool,
		snapshot,
		duplicateArtifact,
		duplicateCatalog,
	) {
		t.Fatal("snapshot validator accepted a cross-action duplicate outcome")
	}
}

func maximumCardinalitySnapshot(t *testing.T, owner uuid.UUID) *PolicySnapshot {
	t.Helper()
	scope := snapshotTestSpec().Scope
	scope.OwnerID = owner
	actions := make([]SnapshotAction, maxSnapshotActions)
	outcomeOrdinal := 0
	for actionIndex := range actions {
		actionID := fmt.Sprintf("action-%03d", actionIndex)
		if actionIndex == len(actions)-1 {
			actionID = StaticActionID
		}
		examples := make([]SnapshotExample, maxSnapshotExamplesPerAction)
		for exampleIndex := range examples {
			outcomeOrdinal++
			examples[exampleIndex] = SnapshotExample{
				OutcomeID: uuid.MustParse(fmt.Sprintf(
					"00000000-0000-4000-8000-%012d",
					outcomeOrdinal,
				)),
				OwnerID:    scope.OwnerID,
				Domain:     scope.Domain,
				Point:      scope.Point,
				ProviderID: scope.ProviderID,
				ModelID:    scope.ModelID,
				ObservedAt: scope.TrainingCutoff.Add(-time.Hour),
				Eligible:   true,
				Utility:    1,
				Features: []SnapshotFeatureValue{
					{Key: FeatureCandidateCount, Value: 2},
				},
			}
		}
		actions[actionIndex] = SnapshotAction{
			ActionID: actionID,
			Examples: examples,
		}
	}
	snapshot, err := NewPolicySnapshot(SnapshotSpec{
		Scope:            scope,
		ChampionActionID: StaticActionID,
		FeatureSchema: []SnapshotFeatureDefinition{
			{Key: FeatureCandidateCount, Center: 1, Scale: 1},
		},
		Actions: actions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func boundedSnapshotPool(
	t *testing.T,
	ctx context.Context,
	statementTimeout time.Duration,
) *pgxpool.Pool {
	t.Helper()
	appURL, err := url.Parse(os.Getenv("AURA_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	query := appURL.Query()
	query.Set(
		"statement_timeout",
		strconv.FormatInt(statementTimeout.Milliseconds(), 10),
	)
	appURL.RawQuery = query.Encode()
	pool, err := db.Open(ctx, &db.Config{URL: appURL.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var configured string
	if err := pool.QueryRow(ctx, `SHOW statement_timeout`).Scan(&configured); err != nil {
		t.Fatal(err)
	}
	if configured != statementTimeout.String() {
		t.Fatalf(
			"statement_timeout = %q, want %s",
			configured,
			statementTimeout,
		)
	}
	return pool
}

func assertSnapshotOwnerLockReleased(t *testing.T, owner uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	pool := typedLedgerMigrationPool(t, ctx)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTypedTestTx(t, tx)
	var locked uuid.UUID
	if err := tx.QueryRow(
		ctx,
		`SELECT id FROM aura.identities WHERE id=$1 FOR UPDATE NOWAIT`,
		owner,
	).Scan(&locked); err != nil {
		t.Fatalf("owner lock remained held after snapshot persistence: %v", err)
	}
	if locked != owner {
		t.Fatalf("locked owner = %s, want %s", locked, owner)
	}
}
