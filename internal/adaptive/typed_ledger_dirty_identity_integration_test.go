//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchema2DirtyAuditLoadersRejectNoncanonicalIdentity(t *testing.T) {
	tests := []struct {
		name           string
		database       string
		seed           func(*testing.T, context.Context, *pgxpool.Pool, uuid.UUID) Assignment
		wantAssignment bool
		wantFacts      []EventKind
	}{
		{
			name:     "arbitrary assignment and decision identity",
			database: "aura_schema2_dirty_arbitrary_assignment",
			seed:     seedArbitrarySchema2AssignmentIdentity,
		},
		{
			name:     "altered decision event identity",
			database: "aura_schema2_dirty_decision_event",
			seed:     seedAlteredSchema2DecisionEventIdentity,
		},
		{
			name:           "altered delivery event identity",
			database:       "aura_schema2_dirty_delivery_event",
			seed:           seedAlteredSchema2DeliveryEventIdentity,
			wantAssignment: true,
			wantFacts:      []EventKind{EventDecision},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			pool, migrateURL := schema2MigrationDatabase(
				t, ctx, tt.database, 60,
			)
			owner := seedTypedLedgerOwner(t, pool)
			poison := typedLedgerAssignment(t, owner)
			if err := insertUnsafeSchema2Assignment(
				ctx, pool, poison, 1,
			); err != nil {
				t.Fatalf("seed audit poison: %v", err)
			}
			target := tt.seed(t, ctx, pool, owner)
			canonical := typedLedgerAssignment(t, owner)
			canonicalAssignment, canonicalDelivery := insertCanonicalSchema2Facts(
				t, ctx, pool, canonical,
			)
			if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
				t.Fatalf("upgrade fixture to 0061: %v", err)
			}
			err := db.MigrateSteps(ctx, migrateURL, 1)
			assertSchema2AuditFailure(
				t, err, "23514", "schema-2 audit found an invalid assignment",
			)
			assertSchema2RejectedUpgradeState(t, ctx, migrateURL)

			queries := sqlc.New(pool)
			assertDirtySchema2IdentityLoaders(
				t, ctx, queries, target, tt.wantAssignment, tt.wantFacts,
			)
			assertCanonicalSchema2Loaders(
				t, ctx, queries, canonical,
				canonicalAssignment, canonicalDelivery,
			)
		})
	}
}

func assertDirtySchema2IdentityLoaders(
	t *testing.T,
	ctx context.Context,
	queries *sqlc.Queries,
	target Assignment,
	wantAssignment bool,
	wantFacts []EventKind,
) {
	t.Helper()
	locked, err := queries.LockSchema2AdaptiveAssignment(
		ctx,
		sqlc.LockSchema2AdaptiveAssignmentParams{
			OwnerID:      dbUUID(target.OwnerID),
			AssignmentID: dbUUID(target.AssignmentID),
		},
	)
	if wantAssignment {
		if err != nil || locked.EventKind != string(EventDecision) {
			t.Fatalf("dirty assignment loader = %+v err %v", locked, err)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("dirty assignment loader error = %v, want no rows", err)
	}
	if _, err := queries.GetSchema2AdaptiveDelivery(
		ctx,
		sqlc.GetSchema2AdaptiveDeliveryParams{
			OwnerID:      dbUUID(target.OwnerID),
			AssignmentID: dbUUID(target.AssignmentID),
		},
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("dirty delivery loader error = %v, want no rows", err)
	}
	facts, err := queries.ListEligibleSchema2AdaptiveAggregateFacts(
		ctx,
		sqlc.ListEligibleSchema2AdaptiveAggregateFactsParams{
			OwnerID:     dbUUID(target.OwnerID),
			AggregateID: target.RequestID.String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != len(wantFacts) {
		t.Fatalf("dirty eligible facts = %+v, want kinds %v", facts, wantFacts)
	}
	for index, wantKind := range wantFacts {
		if facts[index].EventKind != string(wantKind) {
			t.Fatalf(
				"dirty eligible fact %d kind = %q, want %q",
				index, facts[index].EventKind, wantKind,
			)
		}
	}
}

func seedArbitrarySchema2AssignmentIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) Assignment {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	alternate := uuid.Must(uuid.NewV7())
	payload := schema2PayloadWithAssignmentID(t, event.Payload, alternate)
	if err := insertAdaptiveLedgerRow(
		ctx, pool, mustEventID(alternate, EventDecision, "assignment"),
		owner, assignment.RequestID.String(), 1, alternate,
		EventDecision, payload,
	); err != nil {
		t.Fatalf("seed arbitrary assignment identity: %v", err)
	}
	assignment.AssignmentID = alternate
	return assignment
}

func seedAlteredSchema2DecisionEventIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) Assignment {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner,
		assignment.RequestID.String(), 1, assignment.AssignmentID,
		EventDecision, event.Payload,
	); err != nil {
		t.Fatalf("seed altered decision event identity: %v", err)
	}
	return assignment
}

func seedAlteredSchema2DeliveryEventIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) Assignment {
	t.Helper()
	assignment := typedLedgerAssignment(t, owner)
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		t.Fatalf("seed canonical assignment: %v", err)
	}
	deliveryEvent, err := NewDeliveryEvent(
		assignment, typedLedgerDelivery(t, assignment),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerRow(
		ctx, pool, uuid.Must(uuid.NewV7()), owner,
		assignment.RequestID.String(), 2, assignment.AssignmentID,
		EventDelivery, deliveryEvent.Payload,
	); err != nil {
		t.Fatalf("seed altered delivery event identity: %v", err)
	}
	return assignment
}
