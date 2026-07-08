package assets

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

const otherServiceIdentityID = "00000000-0000-0000-0000-000000000002"

// recordingObjectStore counts Get calls to prove the OpenForIdentity ownership gate precedes any
// object-store read (T-IDOR / D-12): a non-owner request must never reach Get. Every other method
// delegates to the embedded fake.
type recordingObjectStore struct {
	objectstore.Store
	getCalls int
}

func (r *recordingObjectStore) Get(ctx context.Context, ref objectstore.ObjectRef) (io.ReadCloser, objectstore.Attrs, error) {
	r.getCalls++
	return r.Store.Get(ctx, ref)
}

func newOpenForIdentityRig(t *testing.T) (*Service, *fakeAssetStore, *recordingObjectStore) {
	t.Helper()
	store := newFakeAssetStore()
	rec := &recordingObjectStore{Store: objectstore.NewFake()}
	svc := &Service{
		Store:      store,
		Objects:    rec,
		Bucket:     "asset-test",
		PresignTTL: time.Minute,
	}
	return svc, store, rec
}

func seedOwnedAsset(t *testing.T, svc *Service, store *fakeAssetStore, identityID, body string) Asset {
	t.Helper()
	key := objectstore.AssetKey(identityID, "seed-"+identityID)
	asset, err := store.Create(context.Background(), CreateRequest{
		IdentityID:   identityID,
		SourceKind:   SourceAgent,
		ThreadID:     "thread-open",
		Scope:        ScopeThread,
		Modality:     ModalityDocument,
		FileName:     "report.pdf",
		MIMEType:     "application/pdf",
		ObjectBucket: "asset-test",
		ObjectKey:    key,
		Metadata:     map[string]any{},
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, strings.NewReader(body), objectstore.PutOptions{MIMEType: "application/pdf", Size: int64(len(body))}); err != nil {
		t.Fatalf("seed Put: %v", err)
	}
	return asset
}

// TestOpenForIdentityOwnerReturnsBodyAndAsset proves the owner path: the stored bytes stream back
// through the returned ReadCloser and the asset is returned alongside.
func TestOpenForIdentityOwnerReturnsBodyAndAsset(t *testing.T) {
	svc, store, rec := newOpenForIdentityRig(t)
	const body = "streamed agent deliverable bytes"
	seeded := seedOwnedAsset(t, svc, store, serviceIdentityID, body)
	rec.getCalls = 0 // the seed Put does not touch Get; reset before the read under test.

	rc, got, err := svc.OpenForIdentity(context.Background(), seeded.ID, serviceIdentityID)
	if err != nil {
		t.Fatalf("OpenForIdentity(owner) error = %v", err)
	}
	if rc == nil {
		t.Fatal("OpenForIdentity(owner) ReadCloser = nil, want a stream")
	}
	defer func() { _ = rc.Close() }()
	if got.ID != seeded.ID {
		t.Fatalf("asset.ID = %q, want %q", got.ID, seeded.ID)
	}
	streamed, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(streamed) != body {
		t.Fatalf("streamed bytes = %q, want %q", streamed, body)
	}
	if rec.getCalls != 1 {
		t.Fatalf("objects.Get calls = %d, want 1 (owner streams once)", rec.getCalls)
	}
}

// TestOpenForIdentityNonOwnerBlocksBeforeStoreRead proves the T-IDOR mitigation: a non-owner's
// GetForIdentity miss returns an error BEFORE any objects.Get, so the store is never read.
func TestOpenForIdentityNonOwnerBlocksBeforeStoreRead(t *testing.T) {
	svc, store, rec := newOpenForIdentityRig(t)
	seeded := seedOwnedAsset(t, svc, store, serviceIdentityID, "owner-only bytes")
	rec.getCalls = 0

	rc, _, err := svc.OpenForIdentity(context.Background(), seeded.ID, otherServiceIdentityID)
	if err == nil {
		t.Fatal("OpenForIdentity(non-owner) error = nil, want an ownership miss")
	}
	if rc != nil {
		t.Fatalf("OpenForIdentity(non-owner) ReadCloser = %v, want nil", rc)
	}
	if rec.getCalls != 0 {
		t.Fatalf("objects.Get calls = %d, want 0 (ownership gate must precede the store read, T-IDOR)", rec.getCalls)
	}
}
