//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutcomeChainLimitCleanInstallAtHead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t,
		ctx,
		"aura_outcome_chain_clean",
		shippedMigrationSteps(t),
	)
	assertSchema2MigrationVersion(t, ctx, migrateURL, shippedMigrationSteps(t))
	assertOutcomeChainFunctionIsHardened(
		t,
		readOutcomeChainFunctionContract(t, ctx, pool),
	)
	assignment, overLimitEvent := seedMaximumOutcomeChain(t, ctx, pool)
	assertDirectCorrectionLimit(
		t,
		ctx,
		pool,
		assignment,
		overLimitEvent,
	)
}

type outcomeChainFunctionContract struct {
	owner            string
	securityDefiner  bool
	searchPath       string
	appCanExecute    bool
	publicCanExecute bool
}

func readOutcomeChainFunctionContract(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) outcomeChainFunctionContract {
	t.Helper()
	var contract outcomeChainFunctionContract
	if err := pool.QueryRow(
		ctx,
		`SELECT pg_get_userbyid(p.proowner),
		        p.prosecdef,
		        COALESCE(p.proconfig::text, ''),
		        has_function_privilege('aura_app', p.oid, 'EXECUTE'),
		        EXISTS (
		            SELECT 1
		            FROM aclexplode(
		                COALESCE(p.proacl, acldefault('f', p.proowner))
		            ) AS acl
		            WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
		        )
		 FROM pg_proc AS p
		 JOIN pg_namespace AS n ON n.oid=p.pronamespace
		 WHERE n.nspname='aura'
		   AND p.proname='enforce_adaptive_schema2_outcome_chain'`,
	).Scan(
		&contract.owner,
		&contract.securityDefiner,
		&contract.searchPath,
		&contract.appCanExecute,
		&contract.publicCanExecute,
	); err != nil {
		t.Fatalf("read outcome-chain function contract: %v", err)
	}
	return contract
}

func assertOutcomeChainFunctionIsHardened(
	t *testing.T,
	contract outcomeChainFunctionContract,
) {
	t.Helper()
	if contract.owner == "" ||
		contract.securityDefiner ||
		contract.searchPath != "{search_path=pg_catalog}" ||
		!contract.appCanExecute ||
		contract.publicCanExecute {
		t.Fatalf("unsafe outcome-chain function contract: %+v", contract)
	}
}

// assertOutcomeChainFunctionContractPreserved is consumed by the snapshot migration
// drills, which step across versions and check the outcome-chain function survives
// each step unchanged.
func assertOutcomeChainFunctionContractPreserved(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	want outcomeChainFunctionContract,
) {
	t.Helper()
	got := readOutcomeChainFunctionContract(t, ctx, pool)
	if got != want {
		t.Fatalf("outcome-chain function contract = %+v, want %+v", got, want)
	}
	assertOutcomeChainFunctionIsHardened(t, got)
}

func seedMaximumOutcomeChain(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) (Assignment, Event) {
	t.Helper()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	store := NewStore(pool, StoreConfig{})
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	registration := validEvaluatorRegistration(EvaluatorDeterministic)
	catalog, err := NewEvaluatorCatalog(registration)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewOutcomeRecorder(store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	outcome := validOutcomeObservation(assignment, registration)
	if _, err := recorder.RecordOutcome(ctx, outcome); err != nil {
		t.Fatal(err)
	}
	root, err := NewOutcomeEvent(assignment, outcome, catalog)
	if err != nil {
		t.Fatal(err)
	}
	leafID := root.ID
	for index := 0; index < maxCorrectionChainLength-1; index++ {
		correction := validCorrectionObservation(assignment, registration, leafID)
		correction.CorrectionSourceID = uuid.Must(uuid.NewV7())
		event, err := NewCorrectionEvent(assignment, correction, catalog)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recorder.RecordCorrection(ctx, correction); err != nil {
			t.Fatalf("RecordCorrection exact-limit index %d: %v", index, err)
		}
		leafID = event.ID
	}
	overLimit := validCorrectionObservation(assignment, registration, leafID)
	overLimit.CorrectionSourceID = uuid.Must(uuid.NewV7())
	event, err := NewCorrectionEvent(assignment, overLimit, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return assignment, event
}

func insertOutcomeEventAndRollback(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	event Event,
	sequence int64,
) error {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(
			context.WithoutCancel(t.Context()),
			5*time.Second,
		)
		defer rollbackCancel()
		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil {
			t.Errorf("rollback direct correction insert: %v", rollbackErr)
		}
	}()
	return insertAdaptiveLedgerRow(
		ctx,
		tx,
		event.ID,
		event.OwnerID,
		event.AggregateID,
		sequence,
		event.DecisionID,
		event.Kind,
		event.Payload,
	)
}

func assertDirectCorrectionLimit(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	assignment Assignment,
	event Event,
) {
	t.Helper()
	err := insertOutcomeEventAndRollback(
		t,
		ctx,
		pool,
		event,
		int64(maxCorrectionChainLength+2),
	)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "54000" {
		t.Fatalf("over-limit direct insert error = %v, want SQLSTATE 54000", err)
	}
	assertAdaptiveEventAbsent(t, ctx, pool, event.ID)
	assertAdaptiveNextSequence(
		t,
		pool,
		assignment.OwnerID,
		assignment.RequestID.String(),
		int64(maxCorrectionChainLength+2),
	)
}

func assertAdaptiveEventAbsent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	eventID uuid.UUID,
) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(
		ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM aura.adaptive_outbox WHERE id=$1
		)`,
		eventID,
	).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("adaptive event %s persisted after rolled-back or rejected insert", eventID)
	}
}
