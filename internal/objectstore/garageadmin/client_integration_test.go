//go:build garage_integration

// Package garageadmin live-stack test. It exercises the REAL Garage Admin API v2
// end to end and is the RESEARCH-A1 confirmation gate for the request/response wire
// shapes (globalAlias / accessKeyId / secretAccessKey / bucketId). Bring the stack
// up and run:
//
//	go test -tags garage_integration -run TestGarageAdmin ./internal/objectstore/garageadmin/
//
// It reads AURA_GARAGE_ADMIN_ENDPOINT + AURA_GARAGE_ADMIN_TOKEN from the env; a
// skipped tier must never pass as green (t.Fatal under $CI, per CLAUDE.md).
package garageadmin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("garage_integration requires %s, but it is unset under CI — "+
				"a skipped integration tier must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("garage_integration requires %s; bring the Garage stack up and set it", key)
	}
	return v
}

func randSuffix(t *testing.T) string {
	t.Helper()
	return randHex(t, 6)
}

// TestGarageAdmin provisions a per-identity bucket + scoped key, proves the create
// legs are idempotent, then de-provisions — all against the live admin API.
func TestGarageAdmin(t *testing.T) {
	endpoint := envOrSkip(t, "AURA_GARAGE_ADMIN_ENDPOINT")
	token := envOrSkip(t, "AURA_GARAGE_ADMIN_TOKEN")

	c, err := New(endpoint, token)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identity := "test-" + randSuffix(t)
	bucketName, err := BucketForIdentity(identity)
	if err != nil {
		t.Fatalf("BucketForIdentity: %v", err)
	}

	// CreateBucket, then assert idempotency (same id on a second create).
	bucketID, err := c.CreateBucket(ctx, bucketName)
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if bucketID == "" {
		t.Fatal("CreateBucket returned an empty bucket id")
	}
	// De-provision on exit — each leg is idempotent, so cleanup is safe even if a
	// later step failed partway.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_ = c.DeleteBucket(cctx, bucketID)
	})

	bucketID2, err := c.CreateBucket(ctx, bucketName)
	if err != nil {
		t.Fatalf("CreateBucket (idempotent re-run): %v", err)
	}
	if bucketID2 != bucketID {
		t.Fatalf("CreateBucket not idempotent: first id %q, second id %q", bucketID, bucketID2)
	}

	// CreateKey returns non-empty creds.
	accessKey, secretKey, err := c.CreateKey(ctx, "key-"+identity)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if accessKey == "" || secretKey == "" {
		t.Fatalf("CreateKey returned empty creds: ak=%q sk=%q", accessKey, secretKey)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_ = c.DeleteKey(cctx, accessKey)
	})

	// AllowBucketKey grants read+write, owner=false on ONLY this bucket, idempotently.
	if err := c.AllowBucketKey(ctx, bucketID, accessKey, ReadWrite); err != nil {
		t.Fatalf("AllowBucketKey: %v", err)
	}
	if err := c.AllowBucketKey(ctx, bucketID, accessKey, ReadWrite); err != nil {
		t.Fatalf("AllowBucketKey (idempotent re-run): %v", err)
	}

	// Deny + delete inverses succeed and are idempotent (second call = 404 → nil).
	if err := c.DenyBucketKey(ctx, bucketID, accessKey, ReadWrite); err != nil {
		t.Fatalf("DenyBucketKey: %v", err)
	}
	if err := c.DeleteKey(ctx, accessKey); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if err := c.DeleteKey(ctx, accessKey); err != nil {
		t.Fatalf("DeleteKey (idempotent 404): %v", err)
	}
	if err := c.DeleteBucket(ctx, bucketID); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
	if err := c.DeleteBucket(ctx, bucketID); err != nil {
		t.Fatalf("DeleteBucket (idempotent 404): %v", err)
	}
}

// TestGarageAdminImportsALostKey puts back a key Garage never held, the state a Postgres
// restored without its Garage leaves behind. A fresh id each run: Garage refuses to import an id
// it has ever held, deleted or not.
func TestGarageAdminImportsALostKey(t *testing.T) {
	c, err := New(envOrSkip(t, "AURA_GARAGE_ADMIN_ENDPOINT"), envOrSkip(t, "AURA_GARAGE_ADMIN_TOKEN"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accessKey := "GK" + randHex(t, 12)
	secretKey := randHex(t, 32)
	if exists, err := c.KeyExists(ctx, accessKey); err != nil || exists {
		t.Fatalf("KeyExists(never held) = %t, %v; want false, nil", exists, err)
	}
	if err := c.ImportKey(ctx, accessKey, secretKey, "import-"+randSuffix(t)); err != nil {
		t.Fatalf("ImportKey: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_ = c.DeleteKey(cctx, accessKey)
	})
	if exists, err := c.KeyExists(ctx, accessKey); err != nil || !exists {
		t.Fatalf("KeyExists(imported) = %t, %v; want true, nil", exists, err)
	}
	if err := c.ImportKey(ctx, accessKey, secretKey, "again"); err == nil {
		t.Fatal("a second ImportKey of the same id succeeded, want Garage's refusal")
	}
}

// TestGarageAdminPlacedReplicasMatchesTheConfiguredFactor reads the copies a live cluster's
// layout stores and holds them against the replication_factor its garage.toml configures:
// the repository's own (docker/garage/garage.toml, the stack this tier runs on) unless
// AURA_GARAGE_CONFIG names another.
func TestGarageAdminPlacedReplicasMatchesTheConfiguredFactor(t *testing.T) {
	c, err := New(envOrSkip(t, "AURA_GARAGE_ADMIN_ENDPOINT"), envOrSkip(t, "AURA_GARAGE_ADMIN_TOKEN"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := os.Getenv("AURA_GARAGE_CONFIG")
	if path == "" {
		path = filepath.Join("..", "..", "..", "docker", "garage", "garage.toml")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := regexp.MustCompile(`(?m)^replication_factor\s*=\s*(\d+)`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s sets no replication_factor", path)
	}
	want, _ := strconv.Atoi(string(m[1]))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := c.PlacedReplicas(ctx)
	if err != nil {
		t.Fatalf("PlacedReplicas: %v", err)
	}
	if got != want {
		t.Fatalf("PlacedReplicas = %d, want the replication_factor %d that %s configures", got, want, path)
	}
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}
