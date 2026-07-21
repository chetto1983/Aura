package agui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

func TestObjectStoreExportDestinationIsDurableAndOwnerScoped(t *testing.T) {
	t.Parallel()

	objects := objectstore.NewFake()
	destination := NewObjectStoreExportDestination(objects, "exports")
	owner := uuid.NewString()
	exportID := uuid.NewString()
	expiresAt := time.Now().UTC().Add(time.Hour)
	if err := destination.Publish(context.Background(), owner, exportID, strings.NewReader("zip"), 3, "ignored", expiresAt); err != nil {
		t.Fatal(err)
	}
	body, err := destination.Open(context.Background(), owner, exportID, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	_ = body.Close()
	if string(got) != "zip" {
		t.Fatalf("archive = %q", got)
	}
	if _, err := destination.Open(context.Background(), uuid.NewString(), exportID, expiresAt); err == nil {
		t.Fatal("foreign owner opened durable export")
	}
}

func TestObjectStoreExportSweepPhysicallyDeletesAndRetries(t *testing.T) {
	objects := objectstore.NewFake()
	failing := &failOnceDeleteStore{Store: objects, failures: 1}
	destination := NewObjectStoreExportDestination(failing, "exports")
	owner := uuid.NewString()
	exportID := uuid.NewString()
	expiresAt := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	destination.now = func() time.Time { return expiresAt.Add(time.Second) }
	archive, err := destination.ref(owner, exportID, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	failing.failRef = archive
	if err := destination.Publish(context.Background(), owner, exportID, strings.NewReader("zip"), 3, "ignored", expiresAt); err != nil {
		t.Fatal(err)
	}

	if _, err := destination.Open(context.Background(), owner, exportID, expiresAt); !errors.Is(err, ErrOwnerExportNotFound) {
		t.Fatalf("Open() after expiry = %v, want not found", err)
	}
	if deleted, err := destination.SweepExpired(context.Background(), expiresAt.Add(time.Second)); err == nil || deleted != 0 {
		t.Fatalf("first sweep = (%d, %v), want retryable delete failure", deleted, err)
	}
	if _, err := objects.Head(context.Background(), archive); err != nil {
		t.Fatalf("archive disappeared after failed delete: %v", err)
	}

	if deleted, err := destination.SweepExpired(context.Background(), expiresAt.Add(time.Second)); err != nil || deleted != 1 {
		t.Fatalf("retry sweep = (%d, %v), want one physical delete", deleted, err)
	}
	if _, err := objects.Head(context.Background(), archive); err == nil {
		t.Fatal("Head() found archive after successful expiry sweep")
	}
	if _, _, err := objects.Get(context.Background(), archive); err == nil {
		t.Fatal("Get() found archive after successful expiry sweep")
	}
	records, err := objects.List(context.Background(), objectstore.ListRequest{Bucket: "exports", Prefix: ownerExportExpiryPrefix})
	if err != nil || len(records) != 0 {
		t.Fatalf("expiry records after finalized sweep = %d, %v", len(records), err)
	}
}

func TestObjectStoreExportSweepIsBatchBounded(t *testing.T) {
	objects := objectstore.NewFake()
	destination := NewObjectStoreExportDestination(objects, "exports")
	expiresAt := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	owner := uuid.NewString()
	for range ownerExportSweepBatch + 1 {
		if err := destination.Publish(context.Background(), owner, uuid.NewString(), strings.NewReader("x"), 1, "ignored", expiresAt); err != nil {
			t.Fatal(err)
		}
	}

	if deleted, err := destination.SweepExpired(context.Background(), expiresAt); err != nil || deleted != ownerExportSweepBatch {
		t.Fatalf("first bounded sweep = (%d, %v), want %d", deleted, err, ownerExportSweepBatch)
	}
	if deleted, err := destination.SweepExpired(context.Background(), expiresAt); err != nil || deleted != 1 {
		t.Fatalf("second bounded sweep = (%d, %v), want 1", deleted, err)
	}
}

type failOnceDeleteStore struct {
	objectstore.Store
	failRef  objectstore.ObjectRef
	failures int
}

func (s *failOnceDeleteStore) Delete(ctx context.Context, ref objectstore.ObjectRef) error {
	if ref == s.failRef && s.failures > 0 {
		s.failures--
		return errors.New("injected object delete failure")
	}
	return s.Store.Delete(ctx, ref)
}
