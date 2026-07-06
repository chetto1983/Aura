//go:build garage_integration && db_integration

// Live asset-path per-identity object isolation (Phase 36 VERIF-4/HI-01 / D-08). It drives the
// REAL asset Service write path against the live Garage stack + a disposable migrated Postgres,
// proving the per-identity resolver is actually consumed at request time: identity A's asset
// bytes land in A's OWN Garage bucket, are readable ONLY with A's resolved creds, and are DENIED
// to B's scoped creds; an UNPROVISIONED identity falls back to the shared bucket (no crash,
// never a foreign bucket).
//
// Run (stack up: Postgres + Garage admin):
//
//	go test -tags 'db_integration garage_integration' -run TestAssetObjectStore ./internal/assets -count=1 -p 1
//
// Requires POSTGRES_PASSWORD (+ optional PGHOST/PGPORT), AURA_GARAGE_ADMIN_ENDPOINT +
// AURA_GARAGE_ADMIN_TOKEN, and AURA_OBJECTSTORE_ENDPOINT + AURA_OBJECTSTORE_REGION. No-skip-as-
// green: assetEnvOrSkip (store_test.go) t.Fatals under $CI when a required var is unset.

package assets

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// resolverTestAuthulaSecret is a valid 64-hex-char (32-byte) AURA_AUTHULA_SECRET for the
// IdentityStore KEK derivation (test-only; never a real secret).
const resolverTestAuthulaSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestAssetObjectStoreCrossDenyLive(t *testing.T) {
	endpoint := assetEnvOrSkip(t, "AURA_GARAGE_ADMIN_ENDPOINT")
	adminToken := assetEnvOrSkip(t, "AURA_GARAGE_ADMIN_TOKEN")
	s3Endpoint := assetEnvOrSkip(t, "AURA_OBJECTSTORE_ENDPOINT")
	region := assetEnvOrSkip(t, "AURA_OBJECTSTORE_REGION")

	admin, err := garageadmin.New(endpoint, adminToken)
	if err != nil {
		t.Fatalf("garageadmin.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool := disposableAssetPool(t, ctx)

	const sharedBucket = "aura-assets"
	shared := objectstore.Credentials{
		Bucket:    sharedBucket,
		AccessKey: os.Getenv("AURA_OBJECTSTORE_ACCESS_KEY"),
		SecretKey: os.Getenv("AURA_OBJECTSTORE_SECRET_KEY"),
	}
	resolver, err := objectstore.NewIdentityStore(pool, resolverTestAuthulaSecret, shared, "local")
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}
	sharedStore, err := objectstore.NewS3(ctx, objectstore.S3Config{
		Endpoint: s3Endpoint, Region: region, AccessKey: shared.AccessKey, SecretKey: shared.SecretKey, PathStyle: true,
	})
	if err != nil {
		t.Fatalf("shared NewS3: %v", err)
	}
	factory := func(creds objectstore.Credentials) (objectstore.Store, error) {
		return objectstore.NewS3(ctx, objectstore.S3Config{
			Endpoint: s3Endpoint, Region: region, AccessKey: creds.AccessKey, SecretKey: creds.SecretKey, PathStyle: true,
		})
	}

	idA := seedAssetIdentity(t, ctx, pool)
	idB := seedAssetIdentity(t, ctx, pool)
	bucketA := provisionGarageIdentity(t, ctx, admin, resolver, idA)
	provisionGarageIdentity(t, ctx, admin, resolver, idB)

	svc := &Service{
		Store:            newFakeAssetStore(),
		Objects:          sharedStore,
		IdentityObjects:  resolver,
		PerIdentityStore: factory,
		Limits:           Limits{MaxDocumentBytes: 1 << 20, MaxImageBytes: 1 << 20, MaxAudioBytes: 1 << 20},
		Bucket:           sharedBucket,
	}

	// PUT as A via the SERVICE (ctx stamped A) — no Audio/Document processor set, so processAsset
	// just marks the asset complete after the routed object Put.
	payload := "A confidential asset bytes " + uuid.NewString()
	asset, err := svc.IngestTelegramFile(identityctx.WithIdentityID(ctx, idA), TelegramIngestRequest{
		IdentityID: idA, ChatID: 1, FileID: "fa", FileName: "note.txt",
		MIMEType: "text/plain", Modality: ModalityDocument, SizeBytes: int64(len(payload)),
		Reader: strings.NewReader(payload),
	})
	if err != nil {
		t.Fatalf("IngestTelegramFile as A: %v", err)
	}
	if asset.ObjectBucket != bucketA {
		t.Fatalf("asset landed in %q, want A's per-identity bucket %q", asset.ObjectBucket, bucketA)
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}

	credA, err := resolver.Resolve(identityctx.WithIdentityID(ctx, idA))
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	storeA, err := factory(credA)
	if err != nil {
		t.Fatalf("build A store: %v", err)
	}
	t.Cleanup(func() { _ = storeA.Delete(context.Background(), ref) }) // empty A's bucket before its cleanup

	// A reads its OWN object with A's resolved store.
	rc, _, err := storeA.Get(ctx, ref)
	if err != nil {
		t.Fatalf("A cannot read its own object with A's creds: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != payload {
		t.Fatalf("A read %q, want its own payload", string(got))
	}

	// B's scoped creds CANNOT read A's object (bucket-per-identity storage-enforced deny).
	credB, err := resolver.Resolve(identityctx.WithIdentityID(ctx, idB))
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	storeB, err := factory(credB)
	if err != nil {
		t.Fatalf("build B store: %v", err)
	}
	if rcB, _, err := storeB.Get(ctx, ref); err == nil {
		_ = rcB.Close()
		t.Error("B read A's object with B's scoped creds, want access denied (F-007)")
	}

	// An UNPROVISIONED identity C resolves to the SHARED bucket (ErrNoRows fallback), never a
	// foreign bucket and never a crash.
	idC := uuid.NewString()
	objC, bucketC, err := resolveObjects(identityctx.WithIdentityID(ctx, idC), resolver, factory, sharedStore, sharedBucket)
	if err != nil {
		t.Fatalf("unprovisioned C resolve err = %v, want shared fallback", err)
	}
	if bucketC != sharedBucket || objC != sharedStore {
		t.Fatalf("unprovisioned C = (bucket %q, sharedStore=%v), want the shared store+bucket", bucketC, objC == sharedStore)
	}
}

// provisionGarageIdentity creates a real per-identity Garage bucket + scoped key, grants the key
// read+write on ONLY that bucket, and persists the encrypted creds via the shipped IdentityStore.
// It returns the bucket name and registers teardown (key + bucket delete).
func provisionGarageIdentity(t *testing.T, ctx context.Context, admin *garageadmin.Client, store *objectstore.IdentityStore, id string) string {
	t.Helper()
	bucket, err := garageadmin.BucketForIdentity(id)
	if err != nil {
		t.Fatalf("BucketForIdentity: %v", err)
	}
	bucketID, err := admin.CreateBucket(ctx, bucket)
	if err != nil {
		t.Fatalf("CreateBucket %q: %v", bucket, err)
	}
	ak, sk, err := admin.CreateKey(ctx, "aura-"+id)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if err := admin.AllowBucketKey(ctx, bucketID, ak, garageadmin.ReadWrite); err != nil {
		t.Fatalf("AllowBucketKey: %v", err)
	}
	if err := store.Put(ctx, id, bucket, ak, sk); err != nil {
		t.Fatalf("persist creds: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.DeleteKey(context.Background(), ak)
		_ = admin.DeleteBucket(context.Background(), bucketID)
	})
	return bucket
}

func seedAssetIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	name := "asset-musr+" + id[:8] + "@aura.local"
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`, id, name); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	return id
}

// disposableAssetPool spins up a fresh migrated Postgres DB (self-contained: independent of the
// shared local stack's migration version) and returns the aura_app pool. Mirrors the shipped
// agui disposableLiveSagaPool harness.
func disposableAssetPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pwd := assetEnvOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	dsn := func(role, name string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, name)
	}
	if err := db.EnsureRoles(ctx, dsn("aura", "aura"), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	dbName := "aura_assets_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := db.Open(ctx, &db.Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	cleanupNow := true
	defer func() {
		if cleanupNow {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
			admin.Close()
		}
	}()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create disposable db: %v", err)
	}
	freshAdmin, err := db.Open(ctx, &db.Config{URL: dsn("aura", dbName)})
	if err != nil {
		t.Fatalf("open disposable admin pool: %v", err)
	}
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+dbName+" TO aura_migrate"); err != nil {
		freshAdmin.Close()
		t.Fatalf("grant create on disposable db: %v", err)
	}
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		freshAdmin.Close()
		t.Fatalf("grant create on public schema: %v", err)
	}
	if _, err := db.Migrate(ctx, dsn("aura_migrate", dbName)); err != nil {
		freshAdmin.Close()
		t.Fatalf("Migrate disposable db: %v", err)
	}
	app, err := db.Open(ctx, &db.Config{URL: dsn("aura_app", dbName)})
	if err != nil {
		freshAdmin.Close()
		t.Fatalf("open disposable app pool: %v", err)
	}
	cleanupNow = false
	t.Cleanup(func() {
		app.Close()
		freshAdmin.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
		admin.Close()
	})
	return app
}
