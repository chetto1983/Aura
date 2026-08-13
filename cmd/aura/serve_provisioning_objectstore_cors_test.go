package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

// auraKey is the daemon's OWN object-store key -- the one that may own a bucket. No
// identity's key ever appears here, which is the point of every assertion below.
const auraKey = "GK-aura-static"

// Browser uploads have been failing on every per-identity bucket since the "one bucket per
// identity" split, and nothing said so: the presign succeeds, the browser's PUT is refused by
// CORS before it reaches Garage, and the asset row simply stays at 'presigned' forever.
// Measured on the live deployment 2026-08-13 -- every 'web' asset row sits at 'presigned',
// only 'agent' rows (server-side Put, no browser, no CORS) complete -- and confirmed with an
// unauthenticated preflight against the identity bucket, which 403s for content-type alone.
//
// The cause is a gap, not a wrong rule: ConfigureBrowserUploadCORS runs once at boot against
// cfg.ObjectStoreBucket, the SHARED bucket. A minted identity bucket never received it.
func TestEnsureForIdentityConfiguresCORSOnAMintedBucket(t *testing.T) {
	const id = "22222222-2222-2222-2222-222222222222"
	resolver := newFakeResolver(id)
	minter := &fakeMinter{bucketID: "internal", accessKey: "AK", secretKey: "SK"}
	adapter := newObjectStoreProvisionAdapter(minter, resolver)
	cors := &recordingCORS{}
	adapter.ensureCORS = cors.ensure
	adapter.auraAccessKey = auraKey

	if _, err := adapter.EnsureForIdentity(context.Background(), id); err != nil {
		t.Fatalf("EnsureForIdentity: %v", err)
	}
	if got := cors.buckets(); len(got) != 1 || got[0] != "aura-"+id {
		t.Fatalf("CORS configured for %v, want the minted bucket once", got)
	}
}

// The repair matters more than the mint: the buckets that need this rule already exist. An
// identity provisioned before today resolves through the fast path and never mints, so a fix
// that only ran on CreateBucket would leave every live deployment broken.
func TestEnsureForIdentityConfiguresCORSOnAnAlreadyProvisionedBucket(t *testing.T) {
	const id = "33333333-3333-3333-3333-333333333333"
	resolver := newFakeResolver(id)
	resolver.creds[id] = objectstore.Credentials{Bucket: "aura-" + id, AccessKey: "AK", SecretKey: "SK"}
	minter := &fakeMinter{}
	adapter := newObjectStoreProvisionAdapter(minter, resolver)
	cors := &recordingCORS{}
	adapter.ensureCORS = cors.ensure
	adapter.auraAccessKey = auraKey

	for range 3 {
		if _, err := adapter.EnsureForIdentity(context.Background(), id); err != nil {
			t.Fatalf("EnsureForIdentity: %v", err)
		}
	}
	if minter.createBucketCalls != 0 {
		t.Fatalf("an already-provisioned identity minted %d buckets", minter.createBucketCalls)
	}
	// Once per process, not once per call: this is on the path every presign takes.
	if got := cors.buckets(); len(got) != 1 {
		t.Fatalf("CORS configured %d times across 3 resolves, want 1", len(got))
	}
}

// A rule that cannot be written must not take the upload down with it. The bucket is
// provisioned and every server-side path still works; refusing to hand back credentials
// would break more than the CORS gap does.
func TestEnsureForIdentitySurvivesACORSFailure(t *testing.T) {
	const id = "44444444-4444-4444-4444-444444444444"
	resolver := newFakeResolver(id)
	resolver.creds[id] = objectstore.Credentials{Bucket: "aura-" + id, AccessKey: "AK", SecretKey: "SK"}
	adapter := newObjectStoreProvisionAdapter(&fakeMinter{}, resolver)
	adapter.auraAccessKey = auraKey
	adapter.ensureCORS = func(context.Context, string) error {
		return errors.New("garage said no")
	}

	got, err := adapter.EnsureForIdentity(context.Background(), id)
	if err != nil {
		t.Fatalf("a CORS failure took the whole resolve down: %v", err)
	}
	if got.Bucket != "aura-"+id {
		t.Fatalf("credentials = %+v", got)
	}
}

// The shared principal is answered before any minting and its bucket is configured at boot
// by cmd/aura/objectstore.go. Re-doing it here would be a second writer of the same rule.
func TestEnsureForIdentityLeavesTheSharedBucketAlone(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	resolver := newFakeResolver(localID)
	resolver.creds[localID] = objectstore.Credentials{Bucket: "aura-assets", AccessKey: "ak", SecretKey: "sk"}
	adapter := newObjectStoreProvisionAdapter(&fakeMinter{}, resolver)
	cors := &recordingCORS{}
	adapter.ensureCORS = cors.ensure
	adapter.auraAccessKey = auraKey

	if _, err := adapter.EnsureForIdentity(context.Background(), localID); err != nil {
		t.Fatalf("EnsureForIdentity: %v", err)
	}
	if got := cors.buckets(); len(got) != 0 {
		t.Fatalf("the shared bucket was reconfigured: %v", got)
	}
}

// The boundary, asserted rather than trusted to a comment. Garage gates PutBucketCors behind
// the owner permission, and the one-character way to satisfy it would be to give the
// identity's own key owner on its own bucket -- which would let that identity re-grant and
// delete the bucket from the S3 data plane, exactly what garageadmin.ReadWrite refuses. The
// key that owns the bucket is the daemon's.
func TestOnlyAurasOwnKeyIsEverGrantedBucketOwnership(t *testing.T) {
	const id = "55555555-5555-5555-5555-555555555555"
	resolver := newFakeResolver(id)
	minter := &fakeMinter{bucketID: "internal", accessKey: "AK-identity", secretKey: "SK-identity"}
	adapter := newObjectStoreProvisionAdapter(minter, resolver)
	adapter.ensureCORS = (&recordingCORS{}).ensure
	adapter.auraAccessKey = auraKey

	if _, err := adapter.EnsureForIdentity(context.Background(), id); err != nil {
		t.Fatalf("EnsureForIdentity: %v", err)
	}

	var sawIdentityGrant, sawAuraOwner bool
	for _, grant := range minter.grants {
		if grant == "AK-identity:owner=true" {
			t.Fatalf("the identity's own key was granted ownership of its bucket: %v", minter.grants)
		}
		if grant == "AK-identity:owner=false" {
			sawIdentityGrant = true
		}
		if grant == auraKey+":owner=true" {
			sawAuraOwner = true
		}
	}
	if !sawIdentityGrant {
		t.Fatalf("the identity never got its read+write grant: %v", minter.grants)
	}
	if !sawAuraOwner {
		t.Fatalf("Aura's own key never took ownership, so the CORS write cannot succeed: %v", minter.grants)
	}
	// Not a claim about garageadmin's literal, a claim about what it means.
	if !garageadmin.ReadWriteOwner.Owner || garageadmin.ReadWrite.Owner {
		t.Fatal("the two grants no longer differ on ownership")
	}
}

type recordingCORS struct {
	mu   sync.Mutex
	seen []string
}

func (r *recordingCORS) ensure(_ context.Context, bucket string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, bucket)
	return nil
}

func (r *recordingCORS) buckets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}
