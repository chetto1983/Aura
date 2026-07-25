//go:build db_integration && neo4j_integration && garage_integration && authula_integration && musr_e2e

// Harness for the Phase-36 D-29 two-identity cross-deny acceptance E2E (see
// two_identity_e2e_test.go). It builds the live aura_app pool, provisions throwaway
// per-run identities, provisions per-identity Garage buckets/keys through the real
// Admin API v2 client, and carries a minimal Runner stub whose NewConversation models
// the D-02 defaultConversationOwner rule (owner = the identityctx principal) over the
// REAL conversations.Store so MUSR-02 ownership is exercised end to end.
//
// The five build tags gate this file to the full live stack; every helper reads its env
// through musrEnvOrSkip (t.Fatal under $CI) so a missing DSN/token fails the job rather
// than passing a skipped tier green (CLAUDE.md no-skip-as-green).
package main

import (
	"context"
	"fmt"
	"iter"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
	"github.com/chetto1983/aura/internal/runner"
)

// musrLocalIdentity is the seeded local operator identity (migration 0004) the auth
// boundary and defaultConversationOwner fall back to for an empty principal.
const musrLocalIdentity = "00000000-0000-0000-0000-000000000001"

// musrEnvOrSkip mirrors every other live tier: skip locally, t.Fatal under $CI so a
// missing var never reports a falsely-green integration job.
func musrEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("musr_e2e requires %s, but it is unset under CI — a skipped acceptance "+
				"tier must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("musr_e2e requires %s; bring the full stack up and set it", key)
	}
	return v
}

// musrMigratedPool applies roles + migrations against the shared aura DB and returns an
// aura_app pool (closed via t.Cleanup). Per-run identities + their cascaded rows are the
// isolation unit, not a disposable database.
func musrMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := musrEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := musrEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := musrEnvOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// musrProvisionIdentity inserts a fresh isolated user identity with an agent.run grant and
// returns its UUID. Deleting the identity at test end cascades its grants + conversations +
// documents rows (FK ON DELETE CASCADE), so the shared DB is left clean.
func musrProvisionIdentity(t *testing.T, pool *pgxpool.Pool, label string) string {
	t.Helper()
	id := uuid.NewString()
	name := fmt.Sprintf("musr-%s-%s@example.test", label, id[:8])
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`, id, name); err != nil {
		t.Fatalf("provision identity %s: %v", label, err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, 'agent.run')`, id); err != nil {
		t.Fatalf("grant agent.run to %s: %v", label, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1::uuid`, id)
	})
	return id
}

// musrGarageIdentity is one identity's provisioned per-identity Garage bucket + scoped key.
type musrGarageIdentity struct {
	Bucket    string
	AccessKey string
	SecretKey string
}

// musrProvisionGarage provisions a per-identity bucket + a scoped read/write key (owner=false)
// through the REAL Admin API v2 client — the object-store leg of the plan-08 saga. The bucket
// and key are torn down at test end.
func musrProvisionGarage(t *testing.T, admin *garageadmin.Client, identity string) musrGarageIdentity {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bucket, err := garageadmin.BucketForIdentity(identity)
	if err != nil {
		t.Fatalf("BucketForIdentity(%s): %v", identity, err)
	}
	bucketID, err := admin.CreateBucket(ctx, bucket)
	if err != nil {
		t.Fatalf("CreateBucket(%s): %v", bucket, err)
	}
	accessKey, secretKey, err := admin.CreateKey(ctx, "musr-"+identity)
	if err != nil {
		t.Fatalf("CreateKey(%s): %v", identity, err)
	}
	if err := admin.AllowBucketKey(ctx, bucketID, accessKey, garageadmin.Permissions{Read: true, Write: true}); err != nil {
		t.Fatalf("AllowBucketKey(%s): %v", identity, err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = admin.DeleteBucket(c, bucketID)
		_ = admin.DeleteKey(c, accessKey)
	})
	return musrGarageIdentity{Bucket: bucket, AccessKey: accessKey, SecretKey: secretKey}
}

// musrStubRunner satisfies agui.Runner over the REAL conversations.Store. NewConversation
// applies the defaultConversationOwner rule (owner = the identityctx principal, empty →
// local) so a B-created conversation is genuinely owned by B (MUSR-02); the delete path
// delegates to the owner-scoped DeleteForIdentity (rows==0 ⇒ 403/404). Turn yields one
// terminal event so a driven turn "runs" without the live LLM stack.
type musrStubRunner struct {
	conv *conversations.Store
}

func (r musrStubRunner) NewConversation(ctx context.Context) (string, error) {
	owner := identityctx.IdentityID(ctx)
	if owner == "" {
		owner = musrLocalIdentity
	}
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := r.conv.Create(ctx, conversations.CreateParams{ID: id, IdentityID: owner, Model: "musr-e2e"}); err != nil {
		return "", err
	}
	return id, nil
}

func (r musrStubRunner) DeleteConversationLifecycle(ctx context.Context, identityID, convID string) (int64, error) {
	return r.conv.DeleteForIdentity(ctx, convID, identityID)
}

func (r musrStubRunner) Turn(context.Context, string, *string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) { yield(&agent.Event{}, nil) }
}

func (r musrStubRunner) TurnBranch(context.Context, string, int) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) { yield(&agent.Event{}, nil) }
}

func (r musrStubRunner) SubmitAnswers(context.Context, map[string]runner.ResponseInput) (int, error) {
	return 0, nil
}

func (r musrStubRunner) SubmitAnswer(context.Context, string, runner.ResponseInput) (runner.ResolveDirective, error) {
	return runner.ResolveDirective{}, nil
}

// assertRLSCount opens a tx scoped to identityID (the WithIdentityTx set_config carrier that
// every owner-scoped web read routes through) and asserts the RAW visible-row count of one
// conversation. This is the kernel RLS backstop (36-04): under a foreign identity var the
// owner-RLS policy on aura.conversations filters A's row to 0 even for a bare SELECT, so a
// forgotten *ForIdentity WHERE still cannot leak.
func assertRLSCount(t *testing.T, pool *pgxpool.Pool, identityID, convID string, want int) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rls tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_identity', $1, true)`, identityID); err != nil {
		t.Fatalf("set identity var: %v", err)
	}
	var got int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM aura.conversations WHERE id = $1`, convID).Scan(&got); err != nil {
		t.Fatalf("rls count: %v", err)
	}
	if got != want {
		t.Errorf("RLS-scoped count under identity %s = %d, want %d", identityID, got, want)
	}
}
