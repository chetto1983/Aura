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
	return captureLogs(t, slog.LevelWarn)
}

// captureLogs sends the default logger's records at level and above to the returned buffer for
// the rest of the test.
func captureLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: level})))
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

// Only the first step can fail the call: without a store, or for a row the caller cannot mark,
// nothing was deleted and the caller must hear so.
func TestServiceDeleteFailsWhenTheRowCannotBeMarked(t *testing.T) {
	if _, err := (&Service{}).Delete(context.Background(), serviceIdentityID, "asset-x"); err == nil {
		t.Fatal("an unconfigured service reported a delete")
	}
	svc, store := newAssetServiceTestRig(t, Limits{})
	asset := seedStoredAsset(t, svc, store, serviceIdentityID, "asset-del-4", time.Now())
	if _, err := svc.Delete(context.Background(), "someone-else", asset.ID); err == nil {
		t.Fatal("another identity's delete was reported as done")
	}
	if !objectExists(t, svc.Objects, asset) {
		t.Fatal("a refused delete removed the object")
	}
}

// A row with no object has nothing to remove from a bucket and leaves the table at once.
func TestServiceDeleteFinishesARowWithoutAnObject(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	store.assets["asset-bare"] = Asset{ID: "asset-bare", IdentityID: serviceIdentityID, Status: StatusFailed}
	if _, err := svc.Delete(context.Background(), serviceIdentityID, "asset-bare"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.row("asset-bare"); ok {
		t.Fatal("a row without an object stayed")
	}
}

// When the owner's bucket cannot be reached at all the object may still exist, so the row is
// not finalized: it stays deleting for the sweep, like any other failed removal.
func TestServiceDeleteKeepsTheRowWhenTheOwnersStoreIsUnreachable(t *testing.T) {
	for name, configure := range map[string]func(*Service){
		"resolver fails": func(svc *Service) {
			svc.IdentityObjects = fakeResolver{err: errors.New("no credentials for this identity")}
			svc.PerIdentityStore = (&recordingFactory{store: svc.Objects}).factory
		},
		"no object store": func(svc *Service) { svc.Objects = nil },
	} {
		t.Run(name, func(t *testing.T) {
			svc, store := newAssetServiceTestRig(t, Limits{})
			asset := seedStoredAsset(t, svc, store, serviceIdentityID, "asset-del-5", time.Now())
			configure(svc)
			if _, err := svc.Delete(context.Background(), serviceIdentityID, asset.ID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if row, ok := store.row(asset.ID); !ok || row.Status != StatusDeleting {
				t.Fatalf("row = %+v (present %v), want it deleting for the sweep", row, ok)
			}
		})
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
