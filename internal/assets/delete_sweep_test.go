package assets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type identityList []string

func (l identityList) IdentityIDs(context.Context) ([]string, error) { return l, nil }

type failingIdentities struct{ err error }

func (f failingIdentities) IdentityIDs(context.Context) ([]string, error) { return nil, f.err }

// markStuck puts a row in the state the operator found 258 of on 2026-09-28: deleting, with
// deleted_at never stamped, because the delete used to stop there.
func markStuck(store *fakeAssetStore, asset Asset) {
	store.mu.Lock()
	defer store.mu.Unlock()
	asset.Status = StatusDeleting
	store.assets[asset.ID] = asset
}

func TestDeleteSweepFinishesEachIdentitysOldestRowsInBoundedBatches(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	base := time.Now().Add(-time.Hour)
	oldest := seedStoredAsset(t, svc, store, "owner-a", "a-oldest", base)
	older := seedStoredAsset(t, svc, store, "owner-a", "a-older", base.Add(time.Minute))
	newest := seedStoredAsset(t, svc, store, "owner-a", "a-newest", base.Add(2*time.Minute))
	live := seedStoredAsset(t, svc, store, "owner-a", "a-live", base)
	theirs := seedStoredAsset(t, svc, store, "owner-b", "b-only", base.Add(3*time.Minute))
	for _, stuck := range []Asset{oldest, older, newest, theirs} {
		markStuck(store, stuck)
	}
	sweep := DeleteSweep{Assets: svc, Identities: identityList{"owner-a", "owner-b"}, Batch: 2}

	finished, err := sweep.SweepExpired(context.Background(), time.Now())
	if err != nil || finished != 3 {
		t.Fatalf("first sweep = %d, %v; want the two oldest of owner-a and owner-b's one", finished, err)
	}
	for _, gone := range []Asset{oldest, older, theirs} {
		if _, ok := store.row(gone.ID); ok || objectExists(t, svc.Objects, gone) {
			t.Fatalf("%s survived the sweep", gone.ID)
		}
	}
	if row, ok := store.row(newest.ID); !ok || row.Status != StatusDeleting || !objectExists(t, svc.Objects, newest) {
		t.Fatalf("%s is past the batch bound and must wait for the next run; row %+v", newest.ID, row)
	}
	if row, ok := store.row(live.ID); !ok || row.Status != StatusAccepted || !objectExists(t, svc.Objects, live) {
		t.Fatalf("the sweep touched a live asset: %+v", row)
	}

	if finished, err = sweep.SweepExpired(context.Background(), time.Now()); err != nil || finished != 1 {
		t.Fatalf("second sweep = %d, %v; want the row the bound held back", finished, err)
	}
	if finished, err = sweep.SweepExpired(context.Background(), time.Now()); err != nil || finished != 0 {
		t.Fatalf("third sweep = %d, %v; want nothing left to do", finished, err)
	}
}

func TestDeleteSweepReportsWhatItCouldNotRemoveAndRetriesItNextRun(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	stuck := seedStoredAsset(t, svc, store, "owner-a", "a-stuck", time.Now().Add(-time.Minute))
	fine := seedStoredAsset(t, svc, store, "owner-a", "a-fine", time.Now())
	markStuck(store, stuck)
	markStuck(store, fine)
	objects := &deleteFailingStore{Store: svc.Objects, fail: map[string]error{
		stuck.ObjectKey: errors.New("garage unreachable"),
	}}
	svc.Objects = objects
	sweep := DeleteSweep{Assets: svc, Identities: identityList{"owner-a"}}

	finished, err := sweep.SweepExpired(context.Background(), time.Now())
	if finished != 1 || err == nil || !strings.Contains(err.Error(), stuck.ID) || !strings.Contains(err.Error(), "garage unreachable") {
		t.Fatalf("sweep = %d, %v; want the fine row finished and the stuck one named with its cause", finished, err)
	}
	if row, ok := store.row(stuck.ID); !ok || row.Status != StatusDeleting {
		t.Fatalf("the stuck row = %+v (present %v), want it kept for the next run", row, ok)
	}

	objects.fail = nil
	if finished, err = sweep.SweepExpired(context.Background(), time.Now()); err != nil || finished != 1 {
		t.Fatalf("retry sweep = %d, %v; want the stuck row finished", finished, err)
	}
	if _, ok := store.row(stuck.ID); ok {
		t.Fatal("the stuck row survived a sweep that could reach its object")
	}
}

func TestDeleteSweepFailsWhenItCannotListIdentities(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{})
	want := errors.New("identities unavailable")
	finished, err := DeleteSweep{Assets: svc, Identities: failingIdentities{err: want}}.
		SweepExpired(context.Background(), time.Now())
	if finished != 0 || !errors.Is(err, want) {
		t.Fatalf("sweep = %d, %v; want %v", finished, err, want)
	}
}
