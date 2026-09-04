//go:build db_integration && garage_integration

// Live saga resumability + de-provision symmetry coverage (Phase 36 D-14/D-27 — the
// high-risk track). It drives the REAL journaled resource legs against a disposable
// migrated Postgres DB (aura.provisioning_saga + aura.identity_object_store) AND the live
// Garage Admin API v2, so the forward-recovery + idempotency + symmetric-teardown claims
// are proven against the orphan-critical stores, not fakes.
//
// It proves: (1) a failure injected at a mid-leg point (the filesystem leg, after the
// Garage leg committed) leaves a journaled partial state; (2) re-running the saga converges
// to ONE consistent identity — the Garage leg is NOT re-provisioned (idempotent + journal
// skip), exactly one bucket + one identity_object_store row + the dirs exist; (3)
// de-provisioning is symmetric — after Purge no orphaned Garage bucket, object-store row,
// per-identity directory, aura identity, or Authula user remains.
//
// Run (stack up: Postgres + Garage admin):
//
//	go test -tags 'db_integration garage_integration' -run TestProvisioningSagaResumable ./internal/agui -count=1 -p 1
//
// Requires POSTGRES_PASSWORD (+ optional PGHOST/PGPORT) and AURA_GARAGE_ADMIN_ENDPOINT +
// AURA_GARAGE_ADMIN_TOKEN. No-skip-as-green: envOrSkip t.Fatals under $CI when unset.

package agui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/idroot"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

// testAuthulaSecret is a valid 64-hex-char (32-byte) AURA_AUTHULA_SECRET for the
// IdentityStore KEK derivation (test-only; never a real secret).
const testAuthulaSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// liveJournal is the raw-SQL SagaJournal over aura.provisioning_saga (migration 0028,
// MUTABLE). The agui package can import pgx directly in a test.
type liveJournal struct{ pool *pgxpool.Pool }

func (j liveJournal) Record(ctx context.Context, sagaID, identityID, kind, step, status string) error {
	_, err := j.pool.Exec(ctx,
		`INSERT INTO aura.provisioning_saga (saga_id, identity_id, kind, step, status, updated_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, now())
		 ON CONFLICT (saga_id, step) DO UPDATE SET status = EXCLUDED.status, updated_at = now()`,
		sagaID, identityID, kind, step, status)
	return err
}

func (j liveJournal) CompletedSteps(ctx context.Context, sagaID string) (map[string]bool, error) {
	rows, err := j.pool.Query(ctx,
		`SELECT step FROM aura.provisioning_saga WHERE saga_id = $1::uuid AND status = 'done'`, sagaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var step string
		if err := rows.Scan(&step); err != nil {
			return nil, err
		}
		out[step] = true
	}
	return out, rows.Err()
}

func (j liveJournal) stepStatus(t *testing.T, sagaID, step string) string {
	t.Helper()
	var status string
	err := j.pool.QueryRow(ownerCtx(),
		`SELECT status FROM aura.provisioning_saga WHERE saga_id = $1::uuid AND step = $2`, sagaID, step).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read journal step %q: %v", step, err)
	}
	return status
}

// liveObjectStore satisfies ObjectStoreProvisioner over the real garageadmin client + the
// plan-06 identity_store. ProvisionObjectStore is idempotent (skip when the secret row
// already exists); it remembers each bucket's internal id so DeprovisionObjectStore can
// delete it. provisions counts how many times a real CreateBucket ran (to prove a re-run
// does not duplicate).
type liveObjectStore struct {
	client     *garageadmin.Client
	store      *objectstore.IdentityStore
	mu         sync.Mutex
	bucketIDs  map[string]string
	provisions int
}

func newLiveObjectStore(client *garageadmin.Client, store *objectstore.IdentityStore) *liveObjectStore {
	return &liveObjectStore{client: client, store: store, bucketIDs: map[string]string{}}
}

func (a *liveObjectStore) ProvisionObjectStore(ctx context.Context, id string) error {
	ictx := identityctx.WithIdentityID(ctx, id)
	if _, err := a.store.Resolve(ictx); err == nil {
		return nil // already provisioned — idempotent no-op
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	bucket, err := garageadmin.BucketForIdentity(id)
	if err != nil {
		return err
	}
	bucketID, err := a.client.CreateBucket(ctx, bucket)
	if err != nil {
		return err
	}
	ak, sk, err := a.client.CreateKey(ctx, "aura-"+id)
	if err != nil {
		return err
	}
	if err := a.client.AllowBucketKey(ctx, bucketID, ak, garageadmin.ReadWrite); err != nil {
		return err
	}
	if err := a.store.Put(ctx, id, bucket, ak, sk); err != nil {
		return err
	}
	a.mu.Lock()
	a.bucketIDs[id] = bucketID
	a.provisions++
	a.mu.Unlock()
	return nil
}

func (a *liveObjectStore) DeprovisionObjectStore(ctx context.Context, id string) error {
	ictx := identityctx.WithIdentityID(ctx, id)
	if creds, err := a.store.Resolve(ictx); err == nil {
		_ = a.client.DeleteKey(ctx, creds.AccessKey)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	a.mu.Lock()
	bucketID := a.bucketIDs[id]
	a.mu.Unlock()
	if bucketID != "" {
		if err := a.client.DeleteBucket(ctx, bucketID); err != nil {
			return err
		}
	}
	return a.store.Delete(ctx, id)
}

func (a *liveObjectStore) rememberedBucket(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bucketIDs[id]
}

func (a *liveObjectStore) provisionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provisions
}

// liveFilesystem satisfies FilesystemProvisioner over real temp-dir roots, each rooted
// through the traversal guard (T-36-08-T). failProvision injects a mid-leg crash.
type liveFilesystem struct {
	root          string
	failProvision bool
}

func (a *liveFilesystem) subRoots(id string) (map[string]string, error) {
	out := map[string]string{}
	for _, sub := range []string{"mcp", "skills", "pyscripts"} {
		dir, err := idroot.RootIdentityDir(filepath.Join(a.root, sub), id)
		if err != nil {
			return nil, err
		}
		out[sub] = dir
	}
	return out, nil
}

func (a *liveFilesystem) ProvisionIdentityDirs(_ context.Context, id string) error {
	if a.failProvision {
		return errors.New("injected: filesystem provisioning crash")
	}
	roots, err := a.subRoots(id)
	if err != nil {
		return err
	}
	for _, dir := range roots {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (a *liveFilesystem) DeprovisionIdentityDirs(_ context.Context, id string) error {
	roots, err := a.subRoots(id)
	if err != nil {
		return err
	}
	for _, dir := range roots {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

func (a *liveFilesystem) dirsExist(t *testing.T, id string) bool {
	t.Helper()
	roots, err := a.subRoots(id)
	if err != nil {
		t.Fatalf("subRoots: %v", err)
	}
	_, err = os.Stat(roots["mcp"])
	return err == nil
}

func newLiveIdentityStore(t *testing.T, pool *pgxpool.Pool) *objectstore.IdentityStore {
	t.Helper()
	shared := objectstore.Credentials{Bucket: "aura-assets", AccessKey: "GKtest", SecretKey: "sharedsecret"}
	store, err := objectstore.NewIdentityStore(pool, testAuthulaSecret, shared, "local")
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}
	return store
}

func seedResumableIdentity(t *testing.T, pool *pgxpool.Pool) (id, name string) {
	t.Helper()
	id = uuid.NewString()
	name = "resumable+" + id[:8] + "@aura.local"
	if _, err := pool.Exec(ownerCtx(),
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`, id, name); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ownerCtx(), `DELETE FROM aura.identities WHERE id=$1`, id)
	})
	return id, name
}

func newGarageClient(t *testing.T) *garageadmin.Client {
	t.Helper()
	endpoint := envOrSkip(t, "AURA_GARAGE_ADMIN_ENDPOINT")
	token := envOrSkip(t, "AURA_GARAGE_ADMIN_TOKEN")
	c, err := garageadmin.New(endpoint, token)
	if err != nil {
		t.Fatalf("garageadmin.New: %v", err)
	}
	return c
}

// TestProvisioningSagaResumable proves the D-14/D-27 forward-recovery + de-provision
// symmetry against the live Postgres + Garage stores.
func TestProvisioningSagaResumable(t *testing.T) {
	client := newGarageClient(t) // env-gated first (skips before any DB work)
	ctx, cancel := context.WithTimeout(ownerCtx(), 120*time.Second)
	t.Cleanup(cancel)
	pool := disposableLiveSagaPool(t, ctx)
	store := newLiveIdentityStore(t, pool)
	journal := liveJournal{pool: pool}

	t.Run("mid-leg failure journals partial state; re-run converges to one identity", func(t *testing.T) {
		id, _ := seedResumableIdentity(t, pool)
		os := newLiveObjectStore(client, store)
		fs := &liveFilesystem{root: t.TempDir(), failProvision: true}
		svc := newOnboardingService(OnboardingDeps{Journal: journal, ObjectStore: os, Filesystem: fs})
		t.Cleanup(func() { _ = os.DeprovisionObjectStore(ownerCtx(), id) })

		// First run crashes at the filesystem leg: Garage is journaled done, no compensation.
		if err := svc.resumeResourceProvisioning(ctx, id); err == nil {
			t.Fatal("want error on the crashing (filesystem) leg")
		}
		sid := sagaID(sagaKindProvision, id)
		if got := journal.stepStatus(t, sid, sagaStepGarage); got != sagaDone {
			t.Fatalf("garage journal status = %q, want done", got)
		}
		if got := journal.stepStatus(t, sid, sagaStepFilesystem); got == sagaDone {
			t.Fatalf("filesystem journal status = %q, want not-done after the crash", got)
		}
		if _, err := store.Resolve(identityctx.WithIdentityID(ctx, id)); err != nil {
			t.Fatalf("object-store row must exist after the Garage leg: %v", err)
		}

		// Recover: the filesystem leg now succeeds. The re-run must SKIP Garage (journal done
		// + idempotent) and complete the filesystem — converging to one of each.
		fs.failProvision = false
		if err := svc.resumeResourceProvisioning(ctx, id); err != nil {
			t.Fatalf("recovery resume: %v", err)
		}
		if os.provisionCount() != 1 {
			t.Fatalf("Garage provisioned %d times — a journaled-done + idempotent leg must run once", os.provisionCount())
		}
		if !fs.dirsExist(t, id) {
			t.Fatal("per-identity dirs missing after the recovery resume")
		}
		var rows int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_object_store WHERE identity_id=$1::uuid`, id).Scan(&rows); err != nil {
			t.Fatalf("count object-store rows: %v", err)
		}
		if rows != 1 {
			t.Fatalf("object-store rows = %d, want exactly 1 (no duplicate bucket key)", rows)
		}
	})

	t.Run("de-provision is symmetric — no orphan bucket/row/dir/identity/authula", func(t *testing.T) {
		id, name := seedResumableIdentity(t, pool)
		au := newStatefulAuthula()
		user, err := au.CreateUser(ctx, name)
		if err != nil {
			t.Fatalf("seed authula user: %v", err)
		}
		os := newLiveObjectStore(client, store)
		fs := &liveFilesystem{root: t.TempDir()}
		svc := newOnboardingService(OnboardingDeps{Journal: journal, ObjectStore: os, Filesystem: fs})
		if err := svc.resumeResourceProvisioning(ctx, id); err != nil {
			t.Fatalf("provision resources: %v", err)
		}
		bucketBefore := os.rememberedBucket(id)
		if bucketBefore == "" {
			t.Fatal("no bucket id remembered after provisioning")
		}

		dep := NewDeprovisioner(DeprovisionDeps{
			Journal:        journal,
			ObjectStore:    os,
			Filesystem:     fs,
			IdentityDelete: liveAuraLeg{pool: pool},
			AuthulaDelete:  au,
			Conversations:  &fakeConvPurger{},
			Memory:         &fakeMemoryPurger{},
		})
		target := DeprovisionTarget{IdentityID: id, IdentityName: name, AuthulaUserID: user.ID}
		if err := dep.Purge(ctx, target); err != nil {
			t.Fatalf("Purge: %v", err)
		}

		// Object-store row gone (fail-closed ErrNoRows), dirs gone, identity gone, authula gone.
		if _, err := store.Resolve(identityctx.WithIdentityID(ctx, id)); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("object-store row not purged: err=%v", err)
		}
		if fs.dirsExist(t, id) {
			t.Fatal("per-identity dirs not purged")
		}
		var idRows int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM aura.identities WHERE id=$1::uuid`, id).Scan(&idRows); err != nil {
			t.Fatalf("count identity rows: %v", err)
		}
		if idRows != 0 {
			t.Fatalf("identity row not purged (count=%d)", idRows)
		}
		if au.liveUsers() != 0 {
			t.Fatalf("Authula user not purged (live=%d)", au.liveUsers())
		}
		// The Garage bucket is gone: re-creating the alias yields a NEW internal id (an
		// existing bucket would return the same id via idempotent create-by-alias).
		newBucketID, err := client.CreateBucket(ctx, "aura-"+id)
		if err != nil {
			t.Fatalf("post-purge CreateBucket probe: %v", err)
		}
		t.Cleanup(func() { _ = client.DeleteBucket(ownerCtx(), newBucketID) })
		if newBucketID == bucketBefore {
			t.Fatalf("Garage bucket %q still exists after purge — orphaned", bucketBefore)
		}
	})
}
