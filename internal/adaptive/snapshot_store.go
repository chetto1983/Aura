package adaptive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrUnverifiedSnapshot rejects nil, forged, or internally inconsistent snapshots.
	ErrUnverifiedSnapshot = errors.New("adaptive policy snapshot is not verified")
	// ErrSnapshotConflict reports reuse of a snapshot identity with different content.
	ErrSnapshotConflict = errors.New("adaptive policy snapshot conflicts with persisted content")
	// ErrSnapshotNotFound hides absent and foreign owner-scoped snapshots identically.
	ErrSnapshotNotFound = errors.New("adaptive policy snapshot not found")
	// ErrPersistedSnapshotInvalid rejects storage that no longer matches the typed artifact.
	ErrPersistedSnapshotInvalid = errors.New("persisted adaptive policy snapshot is invalid")
)

// SnapshotStore owns authoritative immutable policy snapshot persistence.
type SnapshotStore struct {
	pool *pgxpool.Pool
}

// NewSnapshotStore binds immutable policy snapshots to PostgreSQL.
func NewSnapshotStore(pool *pgxpool.Pool) *SnapshotStore {
	return &SnapshotStore{pool: pool}
}

// Save persists a verified snapshot exactly once.
func (store *SnapshotStore) Save(
	ctx context.Context,
	snapshot *PolicySnapshot,
) error {
	artifact, err := verifiedSnapshotArtifact(snapshot)
	if err != nil {
		return err
	}
	if store == nil || store.pool == nil {
		return errors.New("adaptive snapshot store requires a database pool")
	}
	scope := snapshot.Scope()
	featureSchema, err := json.Marshal(snapshot.FeatureSchema())
	if err != nil {
		return fmt.Errorf("marshal adaptive snapshot feature schema: %w", err)
	}
	actionCatalog, err := json.Marshal(snapshot.Actions())
	if err != nil {
		return fmt.Errorf("marshal adaptive snapshot action catalog: %w", err)
	}
	digest, err := hex.DecodeString(snapshot.SHA256())
	if err != nil {
		return fmt.Errorf("%w: decode snapshot digest: %v", ErrUnverifiedSnapshot, err)
	}

	err = db.WithIdentityTx(
		ctx,
		store.pool,
		scope.OwnerID.String(),
		func(q *sqlc.Queries) error {
			if err := lockAdaptiveOwnerTx(ctx, q, scope.OwnerID); err != nil {
				return err
			}
			rows, err := q.InsertAdaptivePolicySnapshot(
				ctx,
				sqlc.InsertAdaptivePolicySnapshotParams{
					SnapshotID:       dbUUID(snapshot.ID()),
					OwnerID:          dbUUID(scope.OwnerID),
					SnapshotSha256:   digest,
					SchemaVersion:    SnapshotSchemaVersion,
					Domain:           string(scope.Domain),
					DecisionPoint:    string(scope.Point),
					ProviderID:       scope.ProviderID,
					ModelID:          scope.ModelID,
					PolicyVersion:    scope.PolicyVersion,
					TrainingCutoff:   dbTime(scope.TrainingCutoff),
					FeatureSchema:    featureSchema,
					ActionCatalog:    actionCatalog,
					ChampionActionID: snapshot.ChampionActionID(),
					Artifact:         artifact,
				},
			)
			if err != nil {
				return fmt.Errorf("insert adaptive policy snapshot: %w", err)
			}
			if rows == 1 {
				return nil
			}
			row, err := q.GetAdaptivePolicySnapshot(
				ctx,
				sqlc.GetAdaptivePolicySnapshotParams{
					OwnerID:    dbUUID(scope.OwnerID),
					SnapshotID: dbUUID(snapshot.ID()),
				},
			)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSnapshotConflict
			}
			if err != nil {
				return fmt.Errorf("read replayed adaptive policy snapshot: %w", err)
			}
			persisted, err := snapshotFromPersistedRow(
				row,
				scope.OwnerID,
				snapshot.ID(),
			)
			if err != nil ||
				persisted.SHA256() != snapshot.SHA256() ||
				!bytes.Equal(row.Artifact, artifact) {
				return ErrSnapshotConflict
			}
			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("save adaptive policy snapshot %s: %w", snapshot.ID(), err)
	}
	return nil
}

// Load reconstructs and verifies one owner-scoped snapshot from PostgreSQL.
func (store *SnapshotStore) Load(
	ctx context.Context,
	ownerID uuid.UUID,
	snapshotID uuid.UUID,
) (*PolicySnapshot, error) {
	if ownerID == uuid.Nil || snapshotID == uuid.Nil {
		return nil, ErrSnapshotNotFound
	}
	if store == nil || store.pool == nil {
		return nil, errors.New("adaptive snapshot store requires a database pool")
	}
	var snapshot *PolicySnapshot
	err := db.WithIdentityTx(
		ctx,
		store.pool,
		ownerID.String(),
		func(q *sqlc.Queries) error {
			if err := lockAdaptiveOwnerTx(ctx, q, ownerID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrSnapshotNotFound
				}
				return err
			}
			row, err := q.GetAdaptivePolicySnapshot(
				ctx,
				sqlc.GetAdaptivePolicySnapshotParams{
					OwnerID:    dbUUID(ownerID),
					SnapshotID: dbUUID(snapshotID),
				},
			)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSnapshotNotFound
			}
			if err != nil {
				return fmt.Errorf("read adaptive policy snapshot: %w", err)
			}
			snapshot, err = snapshotFromPersistedRow(row, ownerID, snapshotID)
			return err
		},
	)
	if err != nil {
		return nil, fmt.Errorf("load adaptive policy snapshot %s: %w", snapshotID, err)
	}
	return snapshot, nil
}

func verifiedSnapshotArtifact(snapshot *PolicySnapshot) ([]byte, error) {
	artifact, err := snapshot.canonicalArtifact()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnverifiedSnapshot, err)
	}
	sum := sha256.Sum256(artifact)
	if snapshot.SHA256() != hex.EncodeToString(sum[:]) {
		return nil, ErrUnverifiedSnapshot
	}
	return artifact, nil
}

func snapshotFromPersistedRow(
	row sqlc.AuraAdaptivePolicySnapshots,
	ownerID uuid.UUID,
	snapshotID uuid.UUID,
) (*PolicySnapshot, error) {
	document, err := decodeCanonicalSnapshot(row.Artifact)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPersistedSnapshotInvalid, err)
	}
	snapshot, err := NewPolicySnapshot(SnapshotSpec{
		Scope:            document.Scope,
		ChampionActionID: document.ChampionActionID,
		FeatureSchema:    document.FeatureSchema,
		Actions:          document.Actions,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: reconstruct: %v", ErrPersistedSnapshotInvalid, err)
	}
	artifact, err := snapshot.canonicalArtifact()
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize: %v", ErrPersistedSnapshotInvalid, err)
	}
	featureSchema, err := decodeSnapshotFeatureSchema(row.FeatureSchema)
	if err != nil {
		return nil, fmt.Errorf("%w: feature schema: %v", ErrPersistedSnapshotInvalid, err)
	}
	actionCatalog, err := decodeSnapshotActionCatalog(row.ActionCatalog)
	if err != nil {
		return nil, fmt.Errorf("%w: action catalog: %v", ErrPersistedSnapshotInvalid, err)
	}
	scope := snapshot.Scope()
	digest, digestErr := hex.DecodeString(snapshot.SHA256())
	rowOwner := uuid.UUID(row.OwnerID.Bytes)
	rowID := uuid.UUID(row.ID.Bytes)
	switch {
	case document.SchemaVersion != SnapshotSchemaVersion ||
		row.SchemaVersion != SnapshotSchemaVersion:
		return nil, fmt.Errorf("%w: schema version mismatch", ErrPersistedSnapshotInvalid)
	case !row.OwnerID.Valid || rowOwner != ownerID || scope.OwnerID != ownerID:
		return nil, fmt.Errorf("%w: owner mismatch", ErrPersistedSnapshotInvalid)
	case !row.ID.Valid || rowID != snapshotID || snapshot.ID() != snapshotID:
		return nil, fmt.Errorf("%w: snapshot identity mismatch", ErrPersistedSnapshotInvalid)
	case digestErr != nil || !bytes.Equal(row.Sha256, digest):
		return nil, fmt.Errorf("%w: digest mismatch", ErrPersistedSnapshotInvalid)
	case row.Domain != string(scope.Domain) ||
		row.DecisionPoint != string(scope.Point) ||
		row.ProviderID != scope.ProviderID ||
		row.ModelID != scope.ModelID ||
		row.PolicyVersion != scope.PolicyVersion:
		return nil, fmt.Errorf("%w: scope mismatch", ErrPersistedSnapshotInvalid)
	case !row.TrainingCutoff.Valid ||
		!row.TrainingCutoff.Time.Equal(scope.TrainingCutoff):
		return nil, fmt.Errorf("%w: training cutoff mismatch", ErrPersistedSnapshotInvalid)
	case row.ChampionActionID != snapshot.ChampionActionID():
		return nil, fmt.Errorf("%w: champion mismatch", ErrPersistedSnapshotInvalid)
	case !slices.Equal(featureSchema, snapshot.FeatureSchema()):
		return nil, fmt.Errorf("%w: feature schema mismatch", ErrPersistedSnapshotInvalid)
	case !snapshotActionsEqual(actionCatalog, snapshot.Actions()):
		return nil, fmt.Errorf("%w: action catalog mismatch", ErrPersistedSnapshotInvalid)
	case !bytes.Equal(row.Artifact, artifact):
		return nil, fmt.Errorf("%w: artifact is not canonical", ErrPersistedSnapshotInvalid)
	}
	return snapshot, nil
}

func decodeCanonicalSnapshot(artifact []byte) (canonicalSnapshot, error) {
	var document canonicalSnapshot
	if err := decodeStrictJSON(artifact, &document); err != nil {
		return canonicalSnapshot{}, err
	}
	return document, nil
}

func decodeSnapshotFeatureSchema(
	payload []byte,
) ([]SnapshotFeatureDefinition, error) {
	var schema []SnapshotFeatureDefinition
	if err := decodeStrictJSON(payload, &schema); err != nil {
		return nil, err
	}
	return schema, nil
}

func decodeSnapshotActionCatalog(payload []byte) ([]SnapshotAction, error) {
	var actions []SnapshotAction
	if err := decodeStrictJSON(payload, &actions); err != nil {
		return nil, err
	}
	return actions, nil
}

func decodeStrictJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON contains trailing content")
	}
	return nil
}
