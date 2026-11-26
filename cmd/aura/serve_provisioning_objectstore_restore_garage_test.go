//go:build garage_integration

// Live proof of the restore against a real Garage: a credential row naming a bucket and a key
// Garage has never held, the state a Postgres restored without its Garage leaves, resolves to a
// bucket the identity's own stored key can write and read again.
//
//	go test -tags garage_integration -run TestEnsureForIdentityRestoresAgainstALiveGarage ./cmd/aura/
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
	"github.com/chetto1983/aura/internal/testenv"
	"github.com/google/uuid"
)

func restoreRandHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestEnsureForIdentityRestoresAgainstALiveGarage(t *testing.T) {
	client, err := garageadmin.New(testenv.Require(t, "AURA_GARAGE_ADMIN_ENDPOINT"), testenv.Require(t, "AURA_GARAGE_ADMIN_TOKEN"))
	if err != nil {
		t.Fatalf("garageadmin.New: %v", err)
	}
	s3Endpoint := testenv.Require(t, "AURA_OBJECTSTORE_ENDPOINT")
	region := os.Getenv("AURA_OBJECTSTORE_REGION")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	id := uuid.NewString()
	stored := objectstore.Credentials{Bucket: "aura-" + id, AccessKey: "GK" + restoreRandHex(t, 12), SecretKey: restoreRandHex(t, 32)}
	resolver := newFakeResolver(id)
	resolver.creds[id] = stored
	ref := objectstore.ObjectRef{Bucket: stored.Bucket, Key: "restore-probe.txt"}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if store, err := objectstore.NewS3(cctx, objectstore.S3Config{Endpoint: s3Endpoint, Region: region,
			AccessKey: stored.AccessKey, SecretKey: stored.SecretKey, PathStyle: true}); err == nil {
			_ = store.Delete(cctx, ref)
		}
		if bucketID, err := client.BucketIDByAlias(cctx, stored.Bucket); err == nil {
			_ = client.DeleteBucket(cctx, bucketID)
		}
		_ = client.DeleteKey(cctx, stored.AccessKey)
	})

	got, err := newObjectStoreProvisionAdapter(client, resolver).EnsureForIdentity(ctx, id)
	if err != nil || got != stored {
		t.Fatalf("EnsureForIdentity = %+v, %v; want the stored credentials", got, err)
	}
	if _, err := client.BucketIDByAlias(ctx, stored.Bucket); err != nil {
		t.Fatalf("the bucket was not put back: %v", err)
	}
	if exists, err := client.KeyExists(ctx, stored.AccessKey); err != nil || !exists {
		t.Fatalf("the key was not put back: exists=%t err=%v", exists, err)
	}

	store, err := objectstore.NewS3(ctx, objectstore.S3Config{Endpoint: s3Endpoint, Region: region,
		AccessKey: stored.AccessKey, SecretKey: stored.SecretKey, PathStyle: true})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	const body = "restored"
	if _, err := store.Put(ctx, ref, strings.NewReader(body), objectstore.PutOptions{MIMEType: "text/plain", Size: int64(len(body))}); err != nil {
		t.Fatalf("the identity's stored key cannot write its restored bucket: %v", err)
	}
	rc, _, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("the identity's stored key cannot read its restored bucket: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if read, err := io.ReadAll(rc); err != nil || string(read) != body {
		t.Fatalf("read back %q, %v; want %q", read, err, body)
	}

	// A later process finds Garage whole and leaves it as it is.
	if _, err := newObjectStoreProvisionAdapter(client, resolver).EnsureForIdentity(ctx, id); err != nil {
		t.Fatalf("a second resolve: %v", err)
	}
	rc2, _, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("the object is gone after a second resolve: %v", err)
	}
	_ = rc2.Close()
}
