//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	migratedb "github.com/golang-migrate/migrate/v4/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchema2LedgerAuditRejectsPreHardeningAssignment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_schema2_audit_assignment", 60,
	)
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	if err := insertUnsafeSchema2Assignment(ctx, pool, assignment, 1); err != nil {
		t.Fatalf("seed pre-hardening assignment: %v", err)
	}
	canonical := typedLedgerAssignment(t, owner)
	canonicalAssignment, canonicalDelivery := insertCanonicalSchema2Facts(
		t, ctx, pool, canonical,
	)
	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("upgrade fixture to 0061: %v", err)
	}
	before := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)

	err := db.MigrateSteps(ctx, migrateURL, 1)
	assertSchema2AuditFailure(
		t, err, "23514", "schema-2 audit found an invalid assignment",
	)
	assertSchema2RejectedUpgradeState(t, ctx, migrateURL)
	after := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	if after != before {
		t.Fatalf("rejected assignment audit mutated ledger: before=%+v after=%+v", before, after)
	}
	queries := sqlc.New(pool)
	if _, err := queries.LockSchema2AdaptiveAssignment(
		ctx,
		sqlc.LockSchema2AdaptiveAssignmentParams{
			OwnerID: dbUUID(owner), AssignmentID: dbUUID(assignment.AssignmentID),
		},
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("dirty assignment loader error = %v, want no rows", err)
	}
	if _, err := queries.GetSchema2AdaptiveDelivery(
		ctx,
		sqlc.GetSchema2AdaptiveDeliveryParams{
			OwnerID: dbUUID(owner), AssignmentID: dbUUID(assignment.AssignmentID),
		},
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("dirty assignment delivery loader error = %v, want no rows", err)
	}
	eligible, err := queries.ListEligibleSchema2AdaptiveAggregateFacts(
		ctx,
		sqlc.ListEligibleSchema2AdaptiveAggregateFactsParams{
			OwnerID: dbUUID(owner), AggregateID: assignment.RequestID.String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 0 {
		t.Fatalf("dirty invalid assignment remained eligible: %+v", eligible)
	}
	assertCanonicalSchema2Loaders(
		t, ctx, queries, canonical, canonicalAssignment, canonicalDelivery,
	)
}

func TestSchema2LedgerAuditRejectsPreHardeningDeliveryMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_schema2_audit_delivery", 60,
	)
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	if err := insertMismatchedSchema2Delivery(t, ctx, pool, assignment); err != nil {
		t.Fatalf("seed pre-hardening delivery mismatch: %v", err)
	}
	canonical := typedLedgerAssignment(t, owner)
	canonicalAssignment, canonicalDelivery := insertCanonicalSchema2Facts(
		t, ctx, pool, canonical,
	)
	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("upgrade fixture to 0061: %v", err)
	}
	before := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)

	err := db.MigrateSteps(ctx, migrateURL, 1)
	assertSchema2AuditFailure(
		t, err, "23514", "schema-2 audit found a delivery not bound to its assignment",
	)
	assertSchema2RejectedUpgradeState(t, ctx, migrateURL)
	after := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	if after != before {
		t.Fatalf("rejected delivery audit mutated ledger: before=%+v after=%+v", before, after)
	}
	queries := sqlc.New(pool)
	if _, err := queries.LockSchema2AdaptiveAssignment(
		ctx,
		sqlc.LockSchema2AdaptiveAssignmentParams{
			OwnerID: dbUUID(owner), AssignmentID: dbUUID(assignment.AssignmentID),
		},
	); err != nil {
		t.Fatalf("valid assignment hidden with dirty delivery: %v", err)
	}
	if _, err := queries.GetSchema2AdaptiveDelivery(
		ctx,
		sqlc.GetSchema2AdaptiveDeliveryParams{
			OwnerID: dbUUID(owner), AssignmentID: dbUUID(assignment.AssignmentID),
		},
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("dirty mismatched delivery loader error = %v, want no rows", err)
	}
	eligible, err := queries.ListEligibleSchema2AdaptiveAggregateFacts(
		ctx,
		sqlc.ListEligibleSchema2AdaptiveAggregateFactsParams{
			OwnerID: dbUUID(owner), AggregateID: assignment.RequestID.String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 1 || eligible[0].EventKind != string(EventDecision) {
		t.Fatalf("dirty delivery eligible facts = %+v, want assignment only", eligible)
	}
	assertCanonicalSchema2Loaders(
		t, ctx, queries, canonical, canonicalAssignment, canonicalDelivery,
	)
}

func TestSchema2LedgerAuditAcceptsCanonical0061Database(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_schema2_audit_canonical", 61,
	)
	owner := seedTypedLedgerOwner(t, pool)
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
	if err := insertAdaptiveLedgerEvent(ctx, pool, deliveryEvent, 2); err != nil {
		t.Fatalf("seed canonical delivery: %v", err)
	}
	before := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("upgrade canonical 0061 database through audit: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 62)
	after := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	if after != before {
		t.Fatalf("successful audit mutated ledger: before=%+v after=%+v", before, after)
	}

	eligible, err := sqlc.New(pool).ListEligibleSchema2AdaptiveAggregateFacts(
		ctx,
		sqlc.ListEligibleSchema2AdaptiveAggregateFactsParams{
			OwnerID: dbUUID(owner), AggregateID: assignment.RequestID.String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 2 ||
		eligible[0].EventKind != string(EventDecision) ||
		eligible[1].EventKind != string(EventDelivery) {
		t.Fatalf("eligible facts after audit = %+v, want decision and delivery", eligible)
	}
}

type schema2LedgerSnapshot struct {
	rows   int
	digest string
}

func schema2LedgerSnapshotForOwner(
	t *testing.T,
	ctx context.Context,
	querier interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	owner uuid.UUID,
) schema2LedgerSnapshot {
	t.Helper()
	var snapshot schema2LedgerSnapshot
	if err := querier.QueryRow(ctx, `
SELECT count(*),
       coalesce(
           string_agg(
               to_jsonb(ledger)::text,
               ',' ORDER BY ledger.sequence, ledger.id
           ),
           ''
       )
FROM aura.adaptive_outbox AS ledger
WHERE ledger.owner_id = $1`,
		owner,
	).Scan(&snapshot.rows, &snapshot.digest); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertSchema2AuditFailure(
	t *testing.T,
	err error,
	wantState string,
	wantMessage string,
) {
	t.Helper()
	if err == nil {
		t.Fatalf("audit upgrade succeeded, want SQLSTATE %s", wantState)
	}
	var migrationErr migratedb.Error
	if !errors.As(err, &migrationErr) {
		t.Fatalf("audit error = %T %v, want migration database error", err, err)
	}
	type sqlStateError interface {
		SQLState() string
	}
	var stateErr sqlStateError
	if !errors.As(migrationErr.OrigErr, &stateErr) ||
		stateErr.SQLState() != wantState {
		t.Fatalf("audit SQLSTATE error = %v, want %s", migrationErr.OrigErr, wantState)
	}
	if !strings.Contains(migrationErr.Err, wantMessage) {
		t.Fatalf("audit message = %q, want %q", migrationErr.Err, wantMessage)
	}
}

func assertSchema2RejectedUpgradeState(
	t *testing.T,
	ctx context.Context,
	migrateURL string,
) {
	t.Helper()
	version, dirty := schema2MigrationState(t, ctx, migrateURL)
	if version != 62 || !dirty {
		t.Fatalf(
			"rejected audit state = version %d dirty %t, want version 62 dirty",
			version, dirty,
		)
	}
	err := db.MigrateSteps(ctx, migrateURL, 1)
	if err == nil || !strings.Contains(err.Error(), "Dirty database version 62") {
		t.Fatalf("dirty audit retry error = %v, want fail-closed dirty-version error", err)
	}
}

func insertCanonicalSchema2Facts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	assignment Assignment,
) (Event, Event) {
	t.Helper()
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		t.Fatalf("insert canonical assignment: %v", err)
	}
	deliveryEvent, err := NewDeliveryEvent(
		assignment, typedLedgerDelivery(t, assignment),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, deliveryEvent, 2); err != nil {
		t.Fatalf("insert canonical delivery: %v", err)
	}
	return assignmentEvent, deliveryEvent
}

func assertCanonicalSchema2Loaders(
	t *testing.T,
	ctx context.Context,
	queries *sqlc.Queries,
	assignment Assignment,
	assignmentEvent Event,
	deliveryEvent Event,
) {
	t.Helper()
	locked, err := queries.LockSchema2AdaptiveAssignment(
		ctx,
		sqlc.LockSchema2AdaptiveAssignmentParams{
			OwnerID:      dbUUID(assignment.OwnerID),
			AssignmentID: dbUUID(assignment.AssignmentID),
		},
	)
	if err != nil || locked.ID.Bytes != assignmentEvent.ID {
		t.Fatalf("canonical assignment loader = %+v err %v", locked, err)
	}
	delivery, err := queries.GetSchema2AdaptiveDelivery(
		ctx,
		sqlc.GetSchema2AdaptiveDeliveryParams{
			OwnerID:      dbUUID(assignment.OwnerID),
			AssignmentID: dbUUID(assignment.AssignmentID),
		},
	)
	if err != nil || delivery.ID.Bytes != deliveryEvent.ID {
		t.Fatalf("canonical delivery loader = %+v err %v", delivery, err)
	}
	eligible, err := queries.ListEligibleSchema2AdaptiveAggregateFacts(
		ctx,
		sqlc.ListEligibleSchema2AdaptiveAggregateFactsParams{
			OwnerID:     dbUUID(assignment.OwnerID),
			AggregateID: assignment.RequestID.String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 2 ||
		eligible[0].ID.Bytes != assignmentEvent.ID ||
		eligible[1].ID.Bytes != deliveryEvent.ID {
		t.Fatalf("canonical eligible facts = %+v", eligible)
	}
}
