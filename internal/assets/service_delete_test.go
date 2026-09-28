package assets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

// deleteFailingStore is the fake object store with chosen keys refusing to be deleted.
type deleteFailingStore struct {
	objectstore.Store
	fail map[string]error
}

func (s *deleteFailingStore) Delete(ctx context.Context, ref objectstore.ObjectRef) error {
	if err := s.fail[ref.Key]; err != nil {
		return err
	}
	return s.Store.Delete(ctx, ref)
}

// seedStoredAsset records an accepted asset whose object is in the rig's store.
func seedStoredAsset(t *testing.T, svc *Service, store *fakeAssetStore, identityID, id string, created time.Time) Asset {
	t.Helper()
	asset := Asset{
		ID: id, IdentityID: identityID, Status: StatusAccepted, Scope: ScopeThread,
		Modality: ModalityDocument, FileName: id + ".pdf", ObjectBucket: "asset-test",
		ObjectKey: "chat/" + id + ".pdf", CreatedAt: created,
	}
	if _, err := svc.Objects.Put(context.Background(), assetRef(asset),
		strings.NewReader("%PDF"), objectstore.PutOptions{Size: 4}); err != nil {
		t.Fatalf("Put %s: %v", id, err)
	}
	store.mu.Lock()
	store.assets[id] = asset
	store.mu.Unlock()
	return asset
}

func objectExists(t *testing.T, objects objectstore.Store, asset Asset) bool {
	t.Helper()
	_, err := objects.Head(context.Background(), assetRef(asset))
	if err != nil && !objectstore.IsNotFound(err) {
		t.Fatalf("Head %s: %v", asset.ObjectKey, err)
	}
	return err == nil
}

func (s *fakeAssetStore) row(id string) (Asset, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	asset, ok := s.assets[id]
	return asset, ok
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

// The operator's report: deleted assets piled up in "deleting" because nothing ever took them
// further. A delete now ends with the object gone from the bucket and the row gone from the table.
func TestServiceDeleteRemovesTheObjectThenTheRow(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	asset := seedStoredAsset(t, svc, store, serviceIdentityID, "asset-del-1", time.Now())

	deleted, err := svc.Delete(context.Background(), serviceIdentityID, asset.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted.Status != StatusDeleting || deleted.DeletedAt.IsZero() {
		t.Fatalf("Delete returned %s with deleted_at %v, want the soft-deleted row", deleted.Status, deleted.DeletedAt)
	}
	if objectExists(t, svc.Objects, asset) {
		t.Fatal("the object is still in the bucket")
	}
	if row, ok := store.row(asset.ID); ok {
		t.Fatalf("the row is still in the table as %s", row.Status)
	}
}

// A failed object removal must not lose the delete: the row stays deleting, hidden from every
// read, for the retention sweep to finish, and the failure is logged instead of discarded.
func TestServiceDeleteLeavesTheRowDeletingWhenTheObjectStays(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	asset := seedStoredAsset(t, svc, store, serviceIdentityID, "asset-del-2", time.Now())
	svc.Objects = &deleteFailingStore{Store: svc.Objects, fail: map[string]error{
		asset.ObjectKey: errors.New("garage unreachable"),
	}}
	logs := captureWarnings(t)

	if _, err := svc.Delete(context.Background(), serviceIdentityID, asset.ID); err != nil {
		t.Fatalf("Delete: %v, want success: the intent is durable and the sweep retries", err)
	}
	row, ok := store.row(asset.ID)
	if !ok || row.Status != StatusDeleting {
		t.Fatalf("row = %+v (present %v), want it kept as deleting", row, ok)
	}
	if _, err := svc.GetForIdentity(context.Background(), asset.ID, serviceIdentityID); err == nil {
		t.Fatal("a deleting row is still readable")
	}
	if !strings.Contains(logs.String(), asset.ID) || !strings.Contains(logs.String(), "garage unreachable") {
		t.Fatalf("warning = %q, want the asset id and the cause", logs.String())
	}
}

// A key already gone from the bucket counts as removed, so a retry after a partial failure
// converges instead of failing forever on the object it already deleted.
func TestServiceDeleteTreatsAMissingObjectAsRemoved(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	asset := seedStoredAsset(t, svc, store, serviceIdentityID, "asset-del-3", time.Now())
	svc.Objects = &deleteFailingStore{Store: svc.Objects, fail: map[string]error{
		asset.ObjectKey: fmt.Errorf("remove: %w", fs.ErrNotExist),
	}}

	if _, err := svc.Delete(context.Background(), serviceIdentityID, asset.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.row(asset.ID); ok {
		t.Fatal("the row stayed although its object is already gone")
	}
}
