package documents

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

func TestStorageOrphanDryRunFindsUnledgeredObjectsWithoutDeleting(t *testing.T) {
	ctx := context.Background()
	objects := objectstore.NewFake()
	ledgered := objectstore.ObjectRef{Bucket: "bucket", Key: "identity/a/asset/1/original"}
	orphan := objectstore.ObjectRef{Bucket: "bucket", Key: "identity/a/asset/2/original"}
	for _, ref := range []objectstore.ObjectRef{ledgered, orphan} {
		if _, err := objects.Put(ctx, ref, strings.NewReader(ref.Key), objectstore.PutOptions{MIMEType: "text/plain", Size: int64(len(ref.Key))}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &StorageOrphanService{
		Objects: objects,
		Ledger:  fakeStorageLedger{refs: []objectstore.ObjectRef{ledgered}},
	}

	report, err := svc.DryRun(ctx, StorageOrphanRequest{Bucket: "bucket", Prefix: "identity/a/"})
	if err != nil {
		t.Fatal(err)
	}
	if report.DryRunToken == "" {
		t.Fatal("dry-run token is empty")
	}
	if len(report.Orphans) != 1 || report.Orphans[0].Ref != orphan {
		t.Fatalf("orphans = %#v", report.Orphans)
	}
	if _, err := objects.Head(ctx, orphan); err != nil {
		t.Fatalf("dry-run deleted orphan: %v", err)
	}
}

func TestStorageOrphanCleanupRequiresTokenAndConfirmation(t *testing.T) {
	ctx := context.Background()
	objects := objectstore.NewFake()
	orphan := objectstore.ObjectRef{Bucket: "bucket", Key: "identity/a/asset/2/original"}
	if _, err := objects.Put(ctx, orphan, strings.NewReader("orphan"), objectstore.PutOptions{MIMEType: "text/plain", Size: 6}); err != nil {
		t.Fatal(err)
	}
	svc := &StorageOrphanService{Objects: objects, Ledger: fakeStorageLedger{}}
	report, err := svc.DryRun(ctx, StorageOrphanRequest{Bucket: "bucket", Prefix: "identity/a/"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Cleanup(ctx, StorageOrphanCleanupRequest{
		Bucket:      "bucket",
		Prefix:      "identity/a/",
		DryRunToken: report.DryRunToken,
		Confirm:     "wrong",
	}); err == nil {
		t.Fatal("expected confirmation error")
	}
	if _, err := objects.Head(ctx, orphan); err != nil {
		t.Fatalf("failed cleanup deleted object: %v", err)
	}

	cleaned, err := svc.Cleanup(ctx, StorageOrphanCleanupRequest{
		Bucket:      "bucket",
		Prefix:      "identity/a/",
		DryRunToken: report.DryRunToken,
		Confirm:     StorageOrphanCleanupConfirmation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.Deleted != 1 {
		t.Fatalf("deleted = %d", cleaned.Deleted)
	}
	if _, err := objects.Head(ctx, orphan); err == nil {
		t.Fatal("orphan still exists after confirmed cleanup")
	}
}

type fakeStorageLedger struct {
	refs []objectstore.ObjectRef
}

func (f fakeStorageLedger) ListStorageObjects(_ context.Context, bucket, prefix string) ([]objectstore.ObjectRef, error) {
	var out []objectstore.ObjectRef
	for _, ref := range f.refs {
		if ref.Bucket == bucket && strings.HasPrefix(ref.Key, prefix) {
			out = append(out, ref)
		}
	}
	return out, nil
}
