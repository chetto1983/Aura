//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"strings"
	"testing"

	migratedb "github.com/golang-migrate/migrate/v4/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
