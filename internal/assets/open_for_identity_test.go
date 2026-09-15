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

// recordingObjectStore counts Head, Get and GetFrom calls to prove the ownership gate precedes any
// object-store call (T-IDOR / D-12): a non-owner request must never reach the store. Every other
// method delegates to the embedded fake.
type recordingObjectStore struct {
	objectstore.Store
	headCalls      int
	getCalls       int
	getFromOffsets []int64
}

func (r *recordingObjectStore) Head(ctx context.Context, ref objectstore.ObjectRef) (objectstore.Attrs, error) {
	r.headCalls++
	return r.Store.Head(ctx, ref)
}

func (r *recordingObjectStore) Get(ctx context.Context, ref objectstore.ObjectRef) (io.ReadCloser, objectstore.Attrs, error) {
	r.getCalls++
	return r.Store.Get(ctx, ref)
}

func (r *recordingObjectStore) GetFrom(ctx context.Context, ref objectstore.ObjectRef, offset int64) (io.ReadCloser, error) {
	r.getFromOffsets = append(r.getFromOffsets, offset)
	return r.Store.GetFrom(ctx, ref, offset)
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

// TestOpenSeekableForIdentityOwnerSeeksWithoutReadingWhole proves the Range path: the owner gets
// a reader sized from one Head of the object (not from the row, which is seeded wrong here), the
// bytes are opened only when read, and a seek opens them at that offset.
func TestOpenSeekableForIdentityOwnerSeeksWithoutReadingWhole(t *testing.T) {
	svc, store, rec := newOpenForIdentityRig(t)
	const body = "0123456789 streamed clip bytes"
	seeded := seedOwnedAsset(t, svc, store, serviceIdentityID, body)
	if _, err := store.MarkAccepted(context.Background(), seeded.ID, serviceIdentityID, 5, "hash", "video/mp4"); err != nil {
		t.Fatalf("seed MarkAccepted: %v", err)
	}

	object, got, err := svc.OpenSeekableForIdentity(context.Background(), seeded.ID, serviceIdentityID)
	if err != nil {
		t.Fatalf("OpenSeekableForIdentity(owner) error = %v", err)
	}
	defer func() { _ = object.Close() }()
	if got.ID != seeded.ID {
		t.Fatalf("asset.ID = %q, want %q", got.ID, seeded.ID)
	}
	if end, err := object.Seek(0, io.SeekEnd); err != nil || end != int64(len(body)) {
		t.Fatalf("Seek(0, SeekEnd) = %d, %v, want the stored size %d", end, err, len(body))
	}
	if _, err := object.Seek(11, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	tail, err := io.ReadAll(object)
	if err != nil || string(tail) != body[11:] {
		t.Fatalf("read from 11 = %q, %v, want %q", tail, err, body[11:])
	}
	if rec.headCalls != 1 || rec.getCalls != 0 || len(rec.getFromOffsets) != 1 || rec.getFromOffsets[0] != 11 {
		t.Fatalf("Head = %d, Get = %d, GetFrom offsets = %v, want one Head, no Get and one GetFrom at 11",
			rec.headCalls, rec.getCalls, rec.getFromOffsets)
	}
}

// TestOpenSeekableForIdentityRefusesAMissingObject: the owner's row exists but the object does
// not, so the open fails on the Head, before any byte is read, as download's Get fails.
func TestOpenSeekableForIdentityRefusesAMissingObject(t *testing.T) {
	svc, store, rec := newOpenForIdentityRig(t)
	seeded := seedOwnedAsset(t, svc, store, serviceIdentityID, "about to vanish")
	if err := svc.Objects.Delete(context.Background(), objectstore.ObjectRef{Bucket: seeded.ObjectBucket, Key: seeded.ObjectKey}); err != nil {
		t.Fatal(err)
	}

	object, _, err := svc.OpenSeekableForIdentity(context.Background(), seeded.ID, serviceIdentityID)
	if !objectstore.IsNotFound(err) || object != nil {
		t.Fatalf("OpenSeekableForIdentity(missing object) = %v, %v, want nil and not found", object, err)
	}
	if rec.headCalls != 1 || len(rec.getFromOffsets) != 0 {
		t.Fatalf("Head = %d, GetFrom offsets = %v, want one Head and no open", rec.headCalls, rec.getFromOffsets)
	}
}

// TestOpenSeekableForIdentityNonOwnerBlocksBeforeStoreRead is the T-IDOR gate for the streaming
// reader: the same GetForIdentity miss as OpenForIdentity, before any store call, Head included.
func TestOpenSeekableForIdentityNonOwnerBlocksBeforeStoreRead(t *testing.T) {
	svc, store, rec := newOpenForIdentityRig(t)
	seeded := seedOwnedAsset(t, svc, store, serviceIdentityID, "owner-only bytes")

	object, _, err := svc.OpenSeekableForIdentity(context.Background(), seeded.ID, otherServiceIdentityID)
	if err == nil {
		t.Fatal("OpenSeekableForIdentity(non-owner) error = nil, want an ownership miss")
	}
	if object != nil {
		t.Fatalf("OpenSeekableForIdentity(non-owner) reader = %v, want nil", object)
	}
	if rec.headCalls != 0 || rec.getCalls != 0 || len(rec.getFromOffsets) != 0 {
		t.Fatalf("store calls = %d Head, %d Get, %v GetFrom, want none before the ownership gate",
			rec.headCalls, rec.getCalls, rec.getFromOffsets)
	}
}
