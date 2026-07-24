//go:build db_integration

package adaptive

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchema2LedgerSQLIdentityMatchesGo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, _ := schema2MigrationDatabase(
		t, ctx, "aura_schema2_identity_parity", 63,
	)
	owner := uuid.MustParse("17fc3d72-985a-4aa7-a80f-bc8e0f27d88c")
	request := uuid.MustParse("690d460e-6375-4637-ab93-59e07b2d81d6")
	tests := []struct {
		name    string
		point   DecisionPoint
		ordinal uint32
	}{
		{name: "reasoning zero", point: PointReasoning, ordinal: 0},
		{name: "tool discovery", point: PointToolDiscovery, ordinal: 1},
		{name: "skill routing", point: PointSkillRouting, ordinal: 42},
		{name: "knowledge retrieval", point: PointKnowledge, ordinal: 65536},
		{name: "memory recall max", point: PointMemoryRecall, ordinal: ^uint32(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goAssignment := mustAssignmentID(owner, request, tt.point, tt.ordinal)
			goDecision := mustEventID(goAssignment, EventDecision, "assignment")
			goDelivery := mustEventID(goAssignment, EventDelivery, "assignment")
			var sqlAssignment, sqlDecision, sqlDelivery string
			if err := pool.QueryRow(ctx, `
SELECT aura.adaptive_schema2_assignment_id($1, $2, $3, $4)::text,
       aura.adaptive_schema2_event_id($5, 'decision')::text,
       aura.adaptive_schema2_event_id($5, 'delivery')::text`,
				owner, request, tt.point, int64(tt.ordinal), goAssignment,
			).Scan(&sqlAssignment, &sqlDecision, &sqlDelivery); err != nil {
				t.Fatal(err)
			}
			if sqlAssignment != goAssignment.String() ||
				sqlDecision != goDecision.String() ||
				sqlDelivery != goDelivery.String() {
				t.Fatalf(
					"SQL identities = %s %s %s, Go = %s %s %s",
					sqlAssignment, sqlDecision, sqlDelivery,
					goAssignment, goDecision, goDelivery,
				)
			}
		})
	}
}

func TestSchema2LedgerRejectsNoncanonicalIdentity(t *testing.T) {
	tests := []struct {
		name string
		seed func(*testing.T, context.Context, *pgxpool.Pool, uuid.UUID) error
	}{
		{name: "altered assignment and decision ID", seed: insertAlteredAssignmentIdentity},
		{name: "altered assignment payload ID", seed: insertAlteredAssignmentPayloadID},
		{name: "altered decision event ID", seed: insertAlteredDecisionEventID},
		{name: "duplicate tuple under alternate UUID", seed: insertDuplicateAssignmentTuple},
		{name: "altered delivery event ID", seed: insertAlteredDeliveryEventID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			pool, _ := schema2MigrationDatabase(
				t, ctx, "aura_schema2_identity_enforcement", 63,
			)
			owner := seedTypedLedgerOwner(t, pool)
			err := tt.seed(t, ctx, pool, owner)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("noncanonical identity error = %v, want SQLSTATE 23514", err)
			}
		})
	}
}

func TestSchema2IdentityAuditRejectsPreexistingNoncanonicalDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_schema2_identity_audit", 62,
	)
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
		1, assignment.AssignmentID, EventDecision, event.Payload,
	); err != nil {
		t.Fatalf("seed pre-0063 noncanonical event ID: %v", err)
	}
	before := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)

	err = db.MigrateSteps(ctx, migrateURL, 1)
	assertSchema2AuditFailure(
		t, err, "23514", "schema-2 identity audit found an invalid fact",
	)
	version, dirty := schema2MigrationState(t, ctx, migrateURL)
	if version != 63 || !dirty {
		t.Fatalf("identity audit state = version %d dirty %t, want version 63 dirty", version, dirty)
	}
	after := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	if after != before {
		t.Fatalf("identity audit mutated ledger: before=%+v after=%+v", before, after)
	}
}

func TestSchema2IdentityAuditAcceptsCanonical0062Database(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_schema2_identity_canonical", 62,
	)
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	insertCanonicalSchema2Facts(t, ctx, pool, assignment)
	before := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("upgrade canonical 0062 database through identity audit: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 63)
	after := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	if after != before {
		t.Fatalf("successful identity audit mutated ledger: before=%+v after=%+v", before, after)
	}
}

func insertAlteredAssignmentIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) error {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return err
	}
	alternate := uuid.Must(uuid.NewV7())
	payload := schema2PayloadWithAssignmentID(t, event.Payload, alternate)
	return insertAdaptiveLedgerRow(
		ctx, pool, mustEventID(alternate, EventDecision, "assignment"),
		owner, assignment.RequestID.String(), 1, alternate, EventDecision, payload,
	)
}

func insertAlteredAssignmentPayloadID(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) error {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return err
	}
	payload := schema2PayloadWithAssignmentID(t, event.Payload, uuid.Must(uuid.NewV7()))
	return insertAdaptiveLedgerRow(
		ctx, pool, event.ID, owner, assignment.RequestID.String(),
		1, assignment.AssignmentID, EventDecision, payload,
	)
}

func insertAlteredDecisionEventID(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) error {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return err
	}
	return insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
		1, assignment.AssignmentID, EventDecision, event.Payload,
	)
}

func insertDuplicateAssignmentTuple(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) error {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return err
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, event, 1); err != nil {
		return err
	}
	alternate := uuid.Must(uuid.NewV7())
	payload := schema2PayloadWithAssignmentID(t, event.Payload, alternate)
	return insertAdaptiveLedgerRow(
		ctx, pool, mustEventID(alternate, EventDecision, "assignment"),
		owner, assignment.RequestID.String(), 2, alternate, EventDecision, payload,
	)
}

func insertAlteredDeliveryEventID(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) error {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		return err
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		return err
	}
	deliveryEvent, err := NewDeliveryEvent(
		assignment, typedLedgerDelivery(t, assignment),
	)
	if err != nil {
		return err
	}
	return insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner, assignment.RequestID.String(),
		2, assignment.AssignmentID, EventDelivery, deliveryEvent.Payload,
	)
}

func schema2PayloadWithAssignmentID(
	t *testing.T,
	payload []byte,
	assignmentID uuid.UUID,
) []byte {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["assignment_id"] = assignmentID.String()
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
