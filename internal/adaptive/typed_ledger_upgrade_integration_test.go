//go:build db_integration

package adaptive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchema2LedgerUpgradeFrom0060SealsExistingDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_schema2_upgrade_drill", 60,
	)
	queries := sqlc.New(pool)
	owner := seedTypedLedgerOwner(t, pool)

	before := typedLedgerAssignment(t, owner)
	if err := insertUnsafeSchema2Assignment(ctx, pool, before, 1); err != nil {
		t.Fatalf("0060 rejected its known permissive payload: %v", err)
	}
	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("upgrade 0060 to 0061: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 61)

	after := typedLedgerAssignment(t, owner)
	if err := insertUnsafeSchema2Assignment(ctx, pool, after, 1); err == nil {
		t.Fatal("0061 accepted assignment with unknown private top-level field")
	}

	canonical := typedLedgerAssignment(t, owner)
	assignmentEvent, err := NewAssignmentEvent(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		t.Fatalf("insert canonical assignment after upgrade: %v", err)
	}
	deliveryEvent, err := NewDeliveryEvent(canonical, typedLedgerDelivery(t, canonical))
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, deliveryEvent, 2); err != nil {
		t.Fatalf("insert canonical delivery after upgrade: %v", err)
	}
	for index, kind := range []EventKind{EventOutcome, EventCorrection} {
		if err := insertAdaptiveLedgerRow(
			ctx, pool, uuid.Must(uuid.NewV7()), owner, canonical.RequestID.String(),
			int64(index+3), canonical.AssignmentID, kind,
			[]byte(`{"schema_version":"2.0","untyped":true}`),
		); err != nil {
			t.Fatalf("insert schema-2 %s history: %v", kind, err)
		}
	}
	eligible, err := queries.ListEligibleSchema2AdaptiveAggregateFacts(
		ctx,
		sqlc.ListEligibleSchema2AdaptiveAggregateFactsParams{
			OwnerID: dbUUID(owner), AggregateID: canonical.RequestID.String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 2 ||
		eligible[0].EventKind != string(EventDecision) ||
		eligible[1].EventKind != string(EventDelivery) {
		t.Fatalf("eligible facts after upgrade = %+v, want decision and delivery", eligible)
	}

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("downgrade 0061 to 0060: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 60)
	afterDown := typedLedgerAssignment(t, owner)
	if err := insertUnsafeSchema2Assignment(ctx, pool, afterDown, 1); err != nil {
		t.Fatalf("0061 down did not restore 0060 behavior: %v", err)
	}
	downDelivery := typedLedgerAssignment(t, owner)
	if err := insertMismatchedSchema2Delivery(t, ctx, pool, downDelivery); err != nil {
		t.Fatalf("0061 down did not restore the 0060 delivery trigger: %v", err)
	}

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-upgrade 0060 to 0061: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 61)
	afterReUp := typedLedgerAssignment(t, owner)
	if err := insertUnsafeSchema2Assignment(ctx, pool, afterReUp, 1); err == nil {
		t.Fatal("re-applied 0061 accepted unsafe assignment")
	}
	reUpDelivery := typedLedgerAssignment(t, owner)
	err = insertMismatchedSchema2Delivery(t, ctx, pool, reUpDelivery)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) ||
		pgErr.Code != "23514" ||
		!strings.Contains(pgErr.Message, "does not match its assignment") {
		t.Fatalf("re-applied 0061 mismatched delivery error = %v, want semantic trigger rejection", err)
	}
}

// shippedMigrationSteps is the number of migrations on disk — the step count that lands a
// disposable database on TODAY's schema. Fixtures that mean "the current schema" must derive
// it: a literal is a number somebody has to remember to bump with every migration, and the
// one nobody bumped (0074 shipped, this fixture stayed at 73) produced a green local run and
// a red CI, with the production query reading a column its own test schema did not have.
// Fixtures that deliberately pin an OLD version to exercise an upgrade path keep their literal.
func shippedMigrationSteps(t *testing.T) int {
	t.Helper()
	head, err := db.MigrationHead()
	if err != nil {
		t.Fatalf("read embedded migration head: %v", err)
	}
	return int(head)
}

func schema2MigrationDatabase(
	t *testing.T,
	ctx context.Context,
	database string,
	steps int,
) (*pgxpool.Pool, string) {
	t.Helper()
	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("integration test requires POSTGRES_PASSWORD under CI")
		}
		t.Skip("integration test requires POSTGRES_PASSWORD")
	}
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	dsn := func(role, database string) string {
		return fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			role, password, host, port, database,
		)
	}
	if err := db.EnsureRoles(ctx, dsn("aura", "aura"), password); err != nil {
		t.Fatal(err)
	}
	admin, err := db.Open(ctx, &db.Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+database+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+database+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+database+" TO aura_migrate"); err != nil {
		t.Fatal(err)
	}
	freshAdmin, err := db.Open(ctx, &db.Config{URL: dsn("aura", database)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		freshAdmin.Close()
		t.Fatal(err)
	}
	freshAdmin.Close()

	migrateURL := dsn("aura_migrate", database)
	if err := db.MigrateSteps(ctx, migrateURL, steps); err != nil {
		t.Fatalf("install migrations through %04d: %v", steps, err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: dsn("aura_app", database)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	assertSchema2MigrationVersion(t, ctx, migrateURL, steps)
	return pool, migrateURL
}

func insertUnsafeSchema2Assignment(
	ctx context.Context,
	pool *pgxpool.Pool,
	assignment Assignment,
	sequence int64,
) error {
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return err
	}
	payload := map[string]any{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	payload["query"] = "private"
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return insertAdaptiveLedgerRow(
		ctx, pool, event.ID, assignment.OwnerID,
		assignment.RequestID.String(), sequence, assignment.AssignmentID,
		EventDecision, encoded,
	)
}

func insertMismatchedSchema2Delivery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	assignment Assignment,
) error {
	t.Helper()
	assignmentEvent, err := NewAssignmentEvent(assignment)
	if err != nil {
		return err
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, assignmentEvent, 1); err != nil {
		return err
	}
	deliveryEvent, err := NewDeliveryEvent(assignment, typedLedgerDelivery(t, assignment))
	if err != nil {
		return err
	}
	payload := map[string]any{}
	if err := json.Unmarshal(deliveryEvent.Payload, &payload); err != nil {
		return err
	}
	payload["intended_action_id"], payload["actual_action_id"] = "candidate", "candidate"
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return insertAdaptiveLedgerRow(
		ctx, pool, deliveryEvent.ID, assignment.OwnerID,
		assignment.RequestID.String(), 2, assignment.AssignmentID,
		EventDelivery, encoded,
	)
}

func assertSchema2MigrationVersion(
	t *testing.T,
	ctx context.Context,
	migrateURL string,
	want int,
) {
	t.Helper()
	version, dirty := schema2MigrationState(t, ctx, migrateURL)
	if version != want || dirty {
		t.Fatalf("schema migration state = version %d dirty %t, want %d clean", version, dirty, want)
	}
}

func schema2MigrationState(
	t *testing.T,
	ctx context.Context,
	migrateURL string,
) (int, bool) {
	t.Helper()
	pool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err := pool.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	return version, dirty
}
