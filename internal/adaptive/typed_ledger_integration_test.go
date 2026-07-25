//go:build db_integration

package adaptive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchema2LedgerPersistsTypedFactsAndExcludesHistoricalSchemas(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	deliveryEvent, err := NewDeliveryEvent(assignment, typedLedgerDelivery(t, assignment))
	if err != nil {
		t.Fatal(err)
	}

	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
		1, uuid.Must(uuid.NewV7()), EventDecision,
		[]byte(`{"schema_version":"1.0","action":"static"}`),
	); err != nil {
		t.Fatalf("insert schema-1 history: %v", err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 2); err != nil {
		t.Fatalf("insert typed assignment: %v", err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, deliveryEvent, 3); err != nil {
		t.Fatalf("insert typed delivery: %v", err)
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
		4, assignment.AssignmentID, EventOutcome,
		[]byte(`{"schema_version":"9.9","observed":true}`),
	); err != nil {
		t.Fatalf("insert unknown-version history: %v", err)
	}
	for sequence, kind := range []EventKind{EventOutcome, EventCorrection} {
		if err := insertAdaptiveLedgerRow(
			ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
			int64(sequence+5), assignment.AssignmentID, kind,
			[]byte(`{"schema_version":"2.0","untyped":true}`),
		); err != nil {
			t.Fatalf("insert untyped schema-2 %s: %v", kind, err)
		}
	}

	queries := sqlc.New(pool)
	loadedDelivery, err := queries.GetSchema2AdaptiveDelivery(
		ctx,
		sqlc.GetSchema2AdaptiveDeliveryParams{
			OwnerID: dbUUID(owner), AssignmentID: dbUUID(assignment.AssignmentID),
		},
	)
	if err != nil {
		t.Fatalf("GetSchema2AdaptiveDelivery: %v", err)
	}
	if loadedDelivery.ID.Bytes != deliveryEvent.ID {
		t.Fatalf("delivery id = %s, want %s", uuid.UUID(loadedDelivery.ID.Bytes), deliveryEvent.ID)
	}
	eligible, err := queries.ListEligibleSchema2AdaptiveAggregateFacts(
		ctx,
		sqlc.ListEligibleSchema2AdaptiveAggregateFactsParams{
			OwnerID: dbUUID(owner), AggregateID: assignment.RequestID.String(),
		},
	)
	if err != nil {
		t.Fatalf("ListEligibleSchema2AdaptiveAggregateFacts: %v", err)
	}
	if len(eligible) != 2 {
		t.Fatalf("eligible facts = %d, want assignment and delivery only", len(eligible))
	}
	if eligible[0].ID.Bytes != assignmentEvent.ID ||
		eligible[1].ID.Bytes != deliveryEvent.ID ||
		eligible[0].Sequence >= eligible[1].Sequence {
		t.Fatalf("eligible facts are not schema-2 sequence order: %+v", eligible)
	}
	var historical int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM aura.adaptive_outbox
WHERE owner_id=$1
  AND aggregate_id=$2
  AND payload->>'schema_version' IN ('1.0', '9.9')`,
		owner, assignment.RequestID.String()).Scan(&historical); err != nil {
		t.Fatal(err)
	}
	if historical != 2 {
		t.Fatalf("readable historical facts = %d, want 2", historical)
	}
}

func TestSchema2LedgerLocksAssignmentByOwnerAndAssignmentID(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, event, 1); err != nil {
		t.Fatal(err)
	}
	params := sqlc.LockSchema2AdaptiveAssignmentParams{
		OwnerID: dbUUID(owner), AssignmentID: dbUUID(assignment.AssignmentID),
	}

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(context.Background()) }()
	locked, err := sqlc.New(first).LockSchema2AdaptiveAssignment(ctx, params)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if locked.ID.Bytes != event.ID {
		t.Fatalf("locked assignment id = %s, want %s", uuid.UUID(locked.ID.Bytes), event.ID)
	}

	second, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback(context.Background()) }()
	if _, err := second.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
		t.Fatal(err)
	}
	_, err = sqlc.New(second).LockSchema2AdaptiveAssignment(ctx, params)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("contending assignment lock error = %v, want SQLSTATE 55P03", err)
	}
}

func TestSchema2LedgerAcceptsTypedDeliveryResult(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		t.Fatal(err)
	}
	result, err := NewArtifactResultID(uuid.Must(uuid.NewV7()).String())
	if err != nil {
		t.Fatal(err)
	}
	delivery := typedLedgerDelivery(t, assignment)
	delivery.ResultCount, delivery.ResultIDs = 1, []ResultID{result}
	deliveryEvent, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, deliveryEvent, 2); err != nil {
		t.Fatalf("insert typed delivery result: %v", err)
	}
}

func TestSchema2LedgerRejectsDuplicateAssignmentAndDelivery(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	deliveryEvent, err := NewDeliveryEvent(assignment, typedLedgerDelivery(t, assignment))
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
		2, assignment.AssignmentID, EventDecision, assignmentEvent.Payload,
	); err == nil {
		t.Fatal("different event ID created a duplicate schema-2 assignment")
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, deliveryEvent, 2); err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
		3, assignment.AssignmentID, EventDelivery, deliveryEvent.Payload,
	); err == nil {
		t.Fatal("different event ID created a duplicate schema-2 delivery")
	}
}

func typedLedgerAssignment(t *testing.T, owner uuid.UUID) Assignment {
	t.Helper()
	request := uuid.Must(uuid.NewV7())
	assignmentID, err := AssignmentIDForPoint(owner, request, PointReasoning, 1)
	if err != nil {
		t.Fatal(err)
	}
	return Assignment{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        assignmentID,
		OwnerID:             owner,
		RequestID:           request,
		Domain:              DomainReasoning,
		Point:               PointReasoning,
		PointOrdinal:        1,
		PolicyEpoch:         1,
		PolicyVersion:       "typed-ledger-v1",
		PolicyMode:          PolicyShadow,
		SnapshotID:          uuid.Must(uuid.NewV7()),
		SnapshotSHA256:      strings.Repeat("a", 64),
		Environment:         EvaluationProductionCanary,
		ProviderID:          "test-provider",
		ModelID:             "test-model",
		EligibleActions:     []string{"candidate", StaticActionID},
		EligibilitySHA256:   strings.Repeat("b", 64),
		CatalogSHA256:       strings.Repeat("c", 64),
		ChampionActionID:    StaticActionID,
		RecommendedActionID: "candidate",
		IntendedActionID:    StaticActionID,
		ActionProbabilities: []ActionProbability{
			{ActionID: "candidate", Probability: 0},
			{ActionID: StaticActionID, Probability: 1},
		},
		SelectionReason: SelectionShadowStatic,
		Features:        map[FeatureKey]float64{},
	}
}

func typedLedgerDelivery(t *testing.T, assignment Assignment) Delivery {
	t.Helper()
	probability := 1.0
	revisions, err := NewRevisionSet()
	if err != nil {
		t.Fatal(err)
	}
	return Delivery{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        assignment.AssignmentID,
		IntendedActionID:    assignment.IntendedActionID,
		ActualActionID:      assignment.IntendedActionID,
		Status:              DeliverySuccess,
		ExposureKnown:       true,
		ExposureProbability: &probability,
		ResultIDs:           []ResultID{},
		Revisions:           revisions,
		EffectiveLimits:     map[string]int{},
	}
}

func seedTypedLedgerOwner(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner,
		"typed-ledger-"+owner.String(),
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(
			context.Background(),
			`DELETE FROM aura.identities WHERE id=$1`,
			owner,
		)
	})
	return owner
}

func insertAdaptiveLedgerEvent(
	ctx context.Context,
	pool *pgxpool.Pool,
	event Event,
	sequence int64,
) error {
	return insertAdaptiveLedgerRow(
		ctx, pool, event.ID, event.OwnerID, event.AggregateID,
		sequence, event.DecisionID, event.Kind, event.Payload,
	)
}

func insertAdaptiveLedgerRow(
	ctx context.Context,
	executor interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	id uuid.UUID,
	owner uuid.UUID,
	aggregate string,
	sequence int64,
	decision uuid.UUID,
	kind EventKind,
	payload []byte,
) error {
	hash := sha256.Sum256(payload)
	_, err := executor.Exec(ctx, `
INSERT INTO aura.adaptive_outbox (
    id, owner_id, aggregate_id, sequence, decision_id, event_kind,
    payload, payload_hash, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, owner, aggregate, sequence, decision, kind,
		payload, hash[:], time.Now().UTC(),
	)
	return err
}

func decodeTypedLedgerPayload(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
