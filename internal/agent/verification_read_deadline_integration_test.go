//go:build db_integration

// The live-pool half of the ledger read deadline.
//
// verification_evidence_store_test.go pins that verificationReadContext BUILDS a
// bounded context; that is a property of the helper, and a helper can be correct
// while the one line that matters calls context.Background() instead. Proving the
// CALL SITE is bounded needs a pool that can actually block, which is why this test
// lives under db_integration rather than being faked.
//
// Requires a running Postgres with the migrations applied:
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration ./internal/agent -run VerificationRead -count=1
//
// No-skip-as-green: dbEnvOrSkip t.Fatals under $CI when a DSN is unset.
package agent

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func dbEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// singleConnPool opens a migrated database with room for exactly ONE connection, so
// holding that connection wedges every later Begin. A dedicated pool is what makes
// the wedge deterministic: starving a shared one would depend on what else is running.
func singleConnPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	appURL := dbEnvOrSkip(t, "AURA_DB_URL")
	if _, err := db.Migrate(ctx, dbEnvOrSkip(t, "AURA_DB_MIGRATE_URL")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		t.Fatalf("parse AURA_DB_URL: %v", err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open single-connection pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

// ledgerProject is a directory the detector recognises, so VerificationStatusFor gets past
// ProjectFactsFor and actually reaches the store. It is a stub rather than a real
// directory because the production detector reads the per-identity sandbox box, not this
// process's filesystem — what this file exercises is the POOL, and the detector only has
// to get out of the way.
const ledgerProject = "/workspace/fixture"

func ledgerProjectDetector() ProjectDetector {
	return stubDetector{ledgerProject: {Found: true, Root: ledgerProject}}
}

func TestVerificationReadAnswersFromALivePool(t *testing.T) {
	// The positive control for the wedge below: against a pool that is NOT starved,
	// the same call reaches Postgres and reports a real state. Without this, a broken
	// DSN would make the wedge test pass for the wrong reason.
	pool := singleConnPool(t)
	ledger := LedgerAdapter{
		Detector:   ledgerProjectDetector(),
		Store:      NewEvidenceStore(pool),
		IdentityID: uuid.Must(uuid.NewV7()).String(),
	}

	status := ledger.VerificationStatusFor("session-"+uuid.Must(uuid.NewV7()).String(), ledgerProject)
	if statusState(status) != StatusUnverified {
		t.Fatalf("status = %q, want %q: a workspace with no recorded event is unverified",
			statusState(status), StatusUnverified)
	}
}

func TestVerificationReadIsBoundedAgainstAWedgedPool(t *testing.T) {
	// The read runs on the TURN's own goroutine at a voluntary termination. An
	// unbounded one holds the turn open for as long as an exhausted pgxpool takes to
	// hand back a connection — which, while the only connection is held, is never.
	pool := singleConnPool(t)
	ledger := LedgerAdapter{
		Detector:   ledgerProjectDetector(),
		Store:      NewEvidenceStore(pool),
		IdentityID: uuid.Must(uuid.NewV7()).String(),
	}
	root := ledgerProject

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire the pool's only connection: %v", err)
	}
	var once sync.Once
	release := func() { once.Do(held.Release) }
	defer release()

	answered := make(chan VerificationStatus, 1)
	started := time.Now()
	go func() { answered <- ledger.VerificationStatusFor("session-wedged", root) }()

	// The slack is generous on purpose: this asserts BOUNDED, not fast. A call that
	// only returns because the test released the connection is the failure.
	select {
	case status := <-answered:
		elapsed := time.Since(started)
		if elapsed < verificationReadTimeout {
			t.Fatalf("read answered in %s, before the %s deadline could have fired — "+
				"the pool was not actually wedged, so this proves nothing", elapsed, verificationReadTimeout)
		}
		if statusState(status) != StatusNotApplicable {
			t.Fatalf("status = %q, want %q: a ledger that could not be asked has nothing to "+
				"say, and must not be reported as a workspace needing proof",
				statusState(status), StatusNotApplicable)
		}
	case <-time.After(verificationReadTimeout + 10*time.Second):
		release() // unwedge so the blocked goroutine finishes and goleak stays clean
		<-answered
		t.Fatalf("the ledger read never returned: the call site is unbounded, so a stalled " +
			"Postgres or an exhausted pool wedges the turn that was ending")
	}
}
