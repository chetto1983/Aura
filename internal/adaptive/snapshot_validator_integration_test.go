//go:build db_integration

package adaptive

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
)

type snapshotValidatorQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func snapshotValidatorAccepts(
	t *testing.T,
	ctx context.Context,
	pool snapshotValidatorQuerier,
	snapshot *PolicySnapshot,
) bool {
	t.Helper()
	artifact, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	actionCatalog, err := json.Marshal(snapshot.Actions())
	if err != nil {
		t.Fatal(err)
	}
	return snapshotValidatorAcceptsPayload(
		t,
		ctx,
		pool,
		snapshot,
		artifact,
		actionCatalog,
	)
}

func snapshotValidatorAcceptsPayload(
	t *testing.T,
	ctx context.Context,
	pool snapshotValidatorQuerier,
	snapshot *PolicySnapshot,
	artifact []byte,
	actionCatalog []byte,
) bool {
	t.Helper()
	featureSchema, err := json.Marshal(snapshot.FeatureSchema())
	if err != nil {
		t.Fatal(err)
	}
	scope := snapshot.Scope()
	var valid bool
	if err := pool.QueryRow(
		ctx,
		`SELECT aura.adaptive_policy_snapshot_artifact_valid(
		    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		 )`,
		artifact,
		scope.OwnerID,
		scope.Domain,
		scope.Point,
		scope.ProviderID,
		scope.ModelID,
		scope.PolicyVersion,
		scope.TrainingCutoff,
		featureSchema,
		actionCatalog,
		snapshot.ChampionActionID(),
	).Scan(&valid); err != nil {
		t.Fatal(err)
	}
	return valid
}
