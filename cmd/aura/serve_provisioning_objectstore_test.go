package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

// fakeResolver is a daemon-free stand-in for objectStoreCredentialResolver (the real
// *objectstore.IdentityStore needs a live Postgres pool + a valid AES key). It is scoped to
// the SINGLE identity id each test drives, mirroring how the real Resolve reads the id off
// ctx: resolveErr, when set, simulates a genuine (non-pgx.ErrNoRows) failure; otherwise a
// present row resolves and an absent one fails closed with pgx.ErrNoRows, exactly like
// objectstore.IdentityStore.Resolve.
type fakeResolver struct {
	mu         sync.Mutex
	id         string
	creds      map[string]objectstore.Credentials
	resolveErr error
	putErr     error
	putCalls   int
}

func newFakeResolver(id string) *fakeResolver {
	return &fakeResolver{id: id, creds: map[string]objectstore.Credentials{}}
}

func (f *fakeResolver) Resolve(context.Context) (objectstore.Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveErr != nil {
		return objectstore.Credentials{}, f.resolveErr
	}
	if c, ok := f.creds[f.id]; ok {
		return c, nil
	}
	return objectstore.Credentials{}, pgx.ErrNoRows
}

func (f *fakeResolver) Put(_ context.Context, id, bucket, accessKey, secretKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls++
	if f.putErr != nil {
		return f.putErr
	}
	f.creds[id] = objectstore.Credentials{Bucket: bucket, AccessKey: accessKey, SecretKey: secretKey}
	return nil
}

func (f *fakeResolver) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.creds, id)
	return nil
}

// fakeMinter is a daemon-free stand-in for objectStoreMinter (the real *garageadmin.Client
// needs a live Garage admin API). It counts CreateBucket/CreateKey/AllowBucketKey calls so
// tests can assert minting happens exactly once for a not-yet-provisioned identity and never
// again on a subsequent EnsureForIdentity call.
type fakeMinter struct {
	mu                sync.Mutex
	createBucketCalls int
	createKeyCalls    int
	allowCalls        int
	createBucketErr   error
	createKeyErr      error
	allowErr          error
	bucketID          string
	accessKey         string
	secretKey         string
}

func (f *fakeMinter) CreateBucket(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createBucketCalls++
	if f.createBucketErr != nil {
		return "", f.createBucketErr
	}
	return f.bucketID, nil
}

func (f *fakeMinter) CreateKey(_ context.Context, _ string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createKeyCalls++
	if f.createKeyErr != nil {
		return "", "", f.createKeyErr
	}
	return f.accessKey, f.secretKey, nil
}

func (f *fakeMinter) AllowBucketKey(_ context.Context, _, _ string, _ garageadmin.Permissions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowCalls++
	return f.allowErr
}

func (f *fakeMinter) DeleteBucket(_ context.Context, _ string) error { return nil }
func (f *fakeMinter) DeleteKey(_ context.Context, _ string) error    { return nil }

// TestEnsureForIdentitySharedNoMint asserts the local/shared principal (D-11) resolves
// straight through with zero minting — Resolve already has the row, so CreateKey must
// never be called.
func TestEnsureForIdentitySharedNoMint(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	shared := objectstore.Credentials{Bucket: "aura-assets", AccessKey: "shared-ak", SecretKey: "shared-sk"}
	resolver := newFakeResolver(localID)
	resolver.creds[localID] = shared
	minter := &fakeMinter{bucketID: "should-not-be-used", accessKey: "should-not", secretKey: "should-not"}
	adapter := newObjectStoreProvisionAdapter(minter, resolver)

	got, err := adapter.EnsureForIdentity(context.Background(), localID)
	if err != nil {
		t.Fatalf("EnsureForIdentity(shared) unexpected error: %v", err)
	}
	if got != shared {
		t.Fatalf("EnsureForIdentity(shared) = %+v, want %+v", got, shared)
	}
	if minter.createKeyCalls != 0 {
		t.Fatalf("EnsureForIdentity(shared): CreateKey called %d times, want 0", minter.createKeyCalls)
	}
	if minter.createBucketCalls != 0 {
		t.Fatalf("EnsureForIdentity(shared): CreateBucket called %d times, want 0", minter.createBucketCalls)
	}

	// ProvisionObjectStore delegates to EnsureForIdentity and still returns nil.
	if err := adapter.ProvisionObjectStore(context.Background(), localID); err != nil {
		t.Fatalf("ProvisionObjectStore(shared) = %v, want nil", err)
	}
}

// TestEnsureForIdentityMintsOnFirstUseThenIdempotent asserts a non-local, unprovisioned
// identity mints exactly once on first EnsureForIdentity, and a second call returns the
// stored (not re-minted) credentials — CreateKey must stay at 1.
func TestEnsureForIdentityMintsOnFirstUseThenIdempotent(t *testing.T) {
	const id = "11111111-1111-1111-1111-111111111111"
	resolver := newFakeResolver(id)
	minter := &fakeMinter{bucketID: "internal-bucket-id", accessKey: "AK-" + id, secretKey: "SK-" + id}
	adapter := newObjectStoreProvisionAdapter(minter, resolver)

	got1, err := adapter.EnsureForIdentity(context.Background(), id)
	if err != nil {
		t.Fatalf("first EnsureForIdentity: unexpected error: %v", err)
	}
	wantBucket := "aura-" + id
	if got1.Bucket != wantBucket {
		t.Fatalf("first EnsureForIdentity: Bucket = %q, want %q", got1.Bucket, wantBucket)
	}
	if got1.AccessKey != minter.accessKey || got1.SecretKey != minter.secretKey {
		t.Fatalf("first EnsureForIdentity: creds = %+v, want AK/SK %q/%q", got1, minter.accessKey, minter.secretKey)
	}
	if minter.createKeyCalls != 1 {
		t.Fatalf("first EnsureForIdentity: CreateKey called %d times, want 1", minter.createKeyCalls)
	}
	if minter.createBucketCalls != 1 {
		t.Fatalf("first EnsureForIdentity: CreateBucket called %d times, want 1", minter.createBucketCalls)
	}
	if minter.allowCalls != 1 {
		t.Fatalf("first EnsureForIdentity: AllowBucketKey called %d times, want 1", minter.allowCalls)
	}
	if resolver.putCalls != 1 {
		t.Fatalf("first EnsureForIdentity: Put called %d times, want 1", resolver.putCalls)
	}

	got2, err := adapter.EnsureForIdentity(context.Background(), id)
	if err != nil {
		t.Fatalf("second EnsureForIdentity: unexpected error: %v", err)
	}
	if got2 != got1 {
		t.Fatalf("second EnsureForIdentity = %+v, want unchanged %+v", got2, got1)
	}
	if minter.createKeyCalls != 1 {
		t.Fatalf("second EnsureForIdentity: CreateKey called %d times, want still 1 (no re-mint)", minter.createKeyCalls)
	}
	if minter.createBucketCalls != 1 {
		t.Fatalf("second EnsureForIdentity: CreateBucket called %d times, want still 1 (no re-mint)", minter.createBucketCalls)
	}
}

// TestEnsureForIdentityFailClosedOnResolveError asserts a real (non-pgx.ErrNoRows) Resolve
// error propagates without minting anything — a transient DB error must never fall through
// to "treat as unprovisioned and mint a new bucket".
func TestEnsureForIdentityFailClosedOnResolveError(t *testing.T) {
	const id = "22222222-2222-2222-2222-222222222222"
	wantErr := errors.New("boom: connection reset")
	resolver := newFakeResolver(id)
	resolver.resolveErr = wantErr
	minter := &fakeMinter{bucketID: "x", accessKey: "x", secretKey: "x"}
	adapter := newObjectStoreProvisionAdapter(minter, resolver)

	_, err := adapter.EnsureForIdentity(context.Background(), id)
	if !errors.Is(err, wantErr) {
		t.Fatalf("EnsureForIdentity error = %v, want wrapping %v", err, wantErr)
	}
	if minter.createBucketCalls != 0 || minter.createKeyCalls != 0 {
		t.Fatalf("EnsureForIdentity on resolve error: minted anyway (CreateBucket=%d CreateKey=%d), want 0/0",
			minter.createBucketCalls, minter.createKeyCalls)
	}
}

// TestEnsureForIdentityFailClosedOnCreateKeyError asserts a CreateKey failure propagates and
// Put is never called — a half-minted bucket-without-key must not be persisted as a
// resolvable credential.
func TestEnsureForIdentityFailClosedOnCreateKeyError(t *testing.T) {
	const id = "33333333-3333-3333-3333-333333333333"
	wantErr := errors.New("garage: CreateKey unavailable")
	resolver := newFakeResolver(id)
	minter := &fakeMinter{bucketID: "internal-bucket-id", createKeyErr: wantErr}
	adapter := newObjectStoreProvisionAdapter(minter, resolver)

	_, err := adapter.EnsureForIdentity(context.Background(), id)
	if !errors.Is(err, wantErr) {
		t.Fatalf("EnsureForIdentity error = %v, want wrapping %v", err, wantErr)
	}
	if resolver.putCalls != 0 {
		t.Fatalf("EnsureForIdentity on CreateKey error: Put called %d times, want 0", resolver.putCalls)
	}
	if minter.createBucketCalls != 1 {
		t.Fatalf("EnsureForIdentity on CreateKey error: CreateBucket called %d times, want 1 (bucket minted before the key failed)",
			minter.createBucketCalls)
	}
}
