package assets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5"
)

const (
	unitSharedBucket = "aura-assets"
	unitOwnerA       = "11111111-1111-1111-1111-111111111111"
	unitOwnerBucketA = "aura-11111111-1111-1111-1111-111111111111"
)

// fakeResolver returns fixed creds/err regardless of ctx — the recording StoreFactory below
// proves resolveObjects routed to it (or short-circuited to shared) as expected.
type fakeResolver struct {
	creds objectstore.Credentials
	err   error
}

func (f fakeResolver) Resolve(context.Context) (objectstore.Credentials, error) {
	return f.creds, f.err
}

type recordingFactory struct {
	calls []objectstore.Credentials
	store objectstore.Store
	err   error
}

func (r *recordingFactory) factory(creds objectstore.Credentials) (objectstore.Store, error) {
	r.calls = append(r.calls, creds)
	return r.store, r.err
}

func TestResolveObjectsFallbackDiscipline(t *testing.T) {
	shared := objectstore.NewFake()
	perID := objectstore.NewFake()

	t.Run("nil resolver/factory -> shared", func(t *testing.T) {
		rf := &recordingFactory{store: perID}
		got, bucket, err := resolveObjects(context.Background(), nil, rf.factory, shared, unitSharedBucket)
		if err != nil || got != shared || bucket != unitSharedBucket {
			t.Fatalf("nil resolver = (%v,%q,%v), want shared", got != shared, bucket, err)
		}
		got, bucket, err = resolveObjects(context.Background(), fakeResolver{creds: objectstore.Credentials{Bucket: unitOwnerBucketA}}, nil, shared, unitSharedBucket)
		if err != nil || got != shared || bucket != unitSharedBucket {
			t.Fatalf("nil factory = (%v,%q,%v), want shared", got != shared, bucket, err)
		}
		if len(rf.calls) != 0 {
			t.Fatalf("factory called %d times on the nil-seam path, want 0", len(rf.calls))
		}
	})

	t.Run("ErrNoRows (unprovisioned) -> shared, factory NOT called", func(t *testing.T) {
		rf := &recordingFactory{store: perID}
		res := fakeResolver{err: fmt.Errorf("objectstore identity: resolve: %w", pgx.ErrNoRows)}
		got, bucket, err := resolveObjects(context.Background(), res, rf.factory, shared, unitSharedBucket)
		if err != nil || got != shared || bucket != unitSharedBucket {
			t.Fatalf("ErrNoRows = (%v,%q,%v), want shared", got != shared, bucket, err)
		}
		if len(rf.calls) != 0 {
			t.Fatalf("factory called on the ErrNoRows fallback, want 0 (never a foreign bucket)")
		}
	})

	t.Run("local/shared principal (creds.Bucket==shared) -> shared, no rebuild", func(t *testing.T) {
		rf := &recordingFactory{store: perID}
		res := fakeResolver{creds: objectstore.Credentials{Bucket: unitSharedBucket, AccessKey: "SHARED"}}
		got, bucket, err := resolveObjects(context.Background(), res, rf.factory, shared, unitSharedBucket)
		if err != nil || got != shared || bucket != unitSharedBucket {
			t.Fatalf("local = (%v,%q,%v), want shared", got != shared, bucket, err)
		}
		if len(rf.calls) != 0 {
			t.Fatalf("factory rebuilt a store for the local principal, want 0 (reuse shared)")
		}
	})

	t.Run("per-identity -> factory store + owner bucket", func(t *testing.T) {
		rf := &recordingFactory{store: perID}
		res := fakeResolver{creds: objectstore.Credentials{Bucket: unitOwnerBucketA, AccessKey: "AKA", SecretKey: "SKA"}}
		got, bucket, err := resolveObjects(context.Background(), res, rf.factory, shared, unitSharedBucket)
		if err != nil {
			t.Fatalf("per-identity resolve err = %v", err)
		}
		if got != perID || bucket != unitOwnerBucketA {
			t.Fatalf("per-identity = (perID=%v,%q), want (perID,%q)", got == perID, bucket, unitOwnerBucketA)
		}
		if len(rf.calls) != 1 || rf.calls[0].AccessKey != "AKA" {
			t.Fatalf("factory calls = %#v, want one call with owner creds", rf.calls)
		}
	})

	t.Run("real resolve error propagates", func(t *testing.T) {
		rf := &recordingFactory{store: perID}
		boom := errors.New("decrypt failed")
		_, _, err := resolveObjects(context.Background(), fakeResolver{err: boom}, rf.factory, shared, unitSharedBucket)
		if !errors.Is(err, boom) {
			t.Fatalf("resolve error = %v, want propagated %v", err, boom)
		}
	})

	t.Run("factory error propagates", func(t *testing.T) {
		boom := errors.New("s3 build failed")
		rf := &recordingFactory{err: boom}
		res := fakeResolver{creds: objectstore.Credentials{Bucket: unitOwnerBucketA, AccessKey: "AKA"}}
		if _, _, err := resolveObjects(context.Background(), res, rf.factory, shared, unitSharedBucket); !errors.Is(err, boom) {
			t.Fatalf("factory error = %v, want propagated %v", err, boom)
		}
	})
}

func TestObjectResolverBundleStoreForAsset(t *testing.T) {
	shared := objectstore.NewFake()
	perID := objectstore.NewFake()
	ownerAsset := Asset{IdentityID: unitOwnerA, ObjectBucket: unitOwnerBucketA}

	t.Run("nil bundle -> shared", func(t *testing.T) {
		var b *ObjectResolverBundle
		got, err := b.storeForAsset(context.Background(), shared, ownerAsset)
		if err != nil || got != shared {
			t.Fatalf("nil bundle = (%v,%v), want shared", got == shared, err)
		}
	})

	t.Run("per-identity owner -> owner store (owner creds, NOT shared)", func(t *testing.T) {
		rf := &recordingFactory{store: perID}
		b := &ObjectResolverBundle{
			Resolver:         fakeResolver{creds: objectstore.Credentials{Bucket: unitOwnerBucketA, AccessKey: "AKA"}},
			PerIdentityStore: rf.factory,
			SharedBucket:     unitSharedBucket,
		}
		got, err := b.storeForAsset(context.Background(), shared, ownerAsset)
		if err != nil || got != perID {
			t.Fatalf("owner store = (perID=%v,%v), want perID", got == perID, err)
		}
		if len(rf.calls) != 1 || rf.calls[0].AccessKey != "AKA" {
			t.Fatalf("factory calls = %#v, want owner creds (F-007: never the shared global key)", rf.calls)
		}
	})

	t.Run("unprovisioned owner (ErrNoRows) -> shared, no crash", func(t *testing.T) {
		rf := &recordingFactory{store: perID}
		b := &ObjectResolverBundle{
			Resolver:         fakeResolver{err: fmt.Errorf("resolve: %w", pgx.ErrNoRows)},
			PerIdentityStore: rf.factory,
			SharedBucket:     unitSharedBucket,
		}
		got, err := b.storeForAsset(context.Background(), shared, ownerAsset)
		if err != nil || got != shared {
			t.Fatalf("unprovisioned = (%v,%v), want shared fallback", got == shared, err)
		}
	})
}

// TestServiceIngestRoutesBytesToPerIdentityBucket proves the WRITE path: with a resolver
// returning A's creds, IngestTelegramFile lands A's bytes in A's store/bucket — NOT the shared
// store. A nil resolver keeps the shared-path behavior (existing tests already cover it).
func TestServiceIngestRoutesBytesToPerIdentityBucket(t *testing.T) {
	store := newFakeAssetStore()
	shared := objectstore.NewFake()
	perID := objectstore.NewFake()
	rf := &recordingFactory{store: perID}
	svc := &Service{
		Store:            store,
		Objects:          shared,
		IdentityObjects:  fakeResolver{creds: objectstore.Credentials{Bucket: unitOwnerBucketA, AccessKey: "AKA", SecretKey: "SKA"}},
		PerIdentityStore: rf.factory,
		Limits:           Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100},
		Bucket:           unitSharedBucket,
	}

	asset, err := svc.IngestTelegramFile(context.Background(), TelegramIngestRequest{
		IdentityID: unitOwnerA,
		ChatID:     1,
		FileID:     "f1",
		FileName:   "voice.ogg",
		MIMEType:   "audio/ogg",
		Modality:   ModalityAudio,
		SizeBytes:  int64(len("OggS bytes")),
		Reader:     strings.NewReader("OggS bytes"),
	})
	if err != nil {
		t.Fatalf("IngestTelegramFile: %v", err)
	}
	if asset.ObjectBucket != unitOwnerBucketA {
		t.Fatalf("asset.ObjectBucket = %q, want owner bucket %q", asset.ObjectBucket, unitOwnerBucketA)
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	if _, err := perID.Head(context.Background(), ref); err != nil {
		t.Fatalf("owner object missing from per-identity store: %v", err)
	}
	if _, err := shared.Head(context.Background(), ref); err == nil {
		t.Fatal("owner bytes leaked into the SHARED store, want them ONLY in the per-identity store")
	}
	if len(rf.calls) == 0 || rf.calls[0].AccessKey != "AKA" {
		t.Fatalf("factory calls = %#v, want the per-identity write to build A's store", rf.calls)
	}
}

// TestServicePresignNilResolverKeepsSharedBucket pins the backward-compatible shared path:
// with no resolver every op targets the shared bucket (the operator/local principal).
func TestServicePresignNilResolverKeepsSharedBucket(t *testing.T) {
	store := newFakeAssetStore()
	svc := &Service{
		Store:   store,
		Objects: objectstore.NewFake(),
		Limits:  Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100},
		Bucket:  unitSharedBucket,
	}
	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        unitOwnerA,
		SourceKind:        SourceWeb,
		ThreadID:          "t1",
		FileName:          "note.txt",
		MIMEType:          "text/plain",
		DeclaredSizeBytes: 4,
		ModalityHint:      ModalityDocument,
	})
	if err != nil {
		t.Fatalf("Presign: %v", err)
	}
	if resp.Asset.ObjectBucket != unitSharedBucket {
		t.Fatalf("nil-resolver Presign bucket = %q, want shared %q", resp.Asset.ObjectBucket, unitSharedBucket)
	}
}
