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
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
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
