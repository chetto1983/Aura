package assets

import (
	"context"
	"errors"
	"slices"
	"sort"
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

// unlistableStore fails ListDeleting for one identity, as a lost connection would mid-sweep.
type unlistableStore struct {
	*fakeAssetStore
	identityID string
}

func (s unlistableStore) ListDeleting(ctx context.Context, identityID string, limit int) ([]Asset, error) {
	if identityID == s.identityID {
		return nil, errors.New("connection reset")
	}
	return s.fakeAssetStore.ListDeleting(ctx, identityID, limit)
}

// One identity the sweep cannot read does not cost the others their run.
func TestDeleteSweepCarriesOnPastAnIdentityItCannotRead(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	readable := seedStoredAsset(t, svc, store, "owner-b", "b-stuck", time.Now())
	markStuck(store, readable)
	svc.Store = unlistableStore{fakeAssetStore: store, identityID: "owner-a"}

	finished, err := DeleteSweep{Assets: svc, Identities: identityList{"owner-a", "owner-b"}}.
		SweepExpired(context.Background(), time.Now())
	if finished != 1 || err == nil || !strings.Contains(err.Error(), "owner-a") {
		t.Fatalf("sweep = %d, %v; want owner-b finished and owner-a named", finished, err)
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

// RetireIdle mirrors the real store: the identity's rows in one of statuses untouched since
// before, oldest first and at most limit, become deleting with deleted_at stamped.
func (s *fakeAssetStore) RetireIdle(_ context.Context, identityID string, statuses []Status, before time.Time, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var stale []Asset
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && slices.Contains(statuses, asset.Status) &&
			asset.DeletedAt.IsZero() && asset.UpdatedAt.Before(before) {
			stale = append(stale, asset)
		}
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].UpdatedAt.Before(stale[j].UpdatedAt) })
	for i, asset := range stale {
		if i == limit {
			break
		}
		asset.Status, asset.DeletedAt = StatusDeleting, time.Now()
		s.assets[asset.ID] = asset
	}
	return nil
}

// presign puts a row in the state the lab VM held 7 of on 2026-09-28: presigned, its URL
// issued at touched and nothing since. The seeded object stands for bytes PUT and never
// finalized; dropping it stands for a URL never used.
func presign(store *fakeAssetStore, asset Asset, touched time.Time) Asset {
	store.mu.Lock()
	defer store.mu.Unlock()
	asset.Status, asset.CreatedAt, asset.UpdatedAt = StatusPresigned, touched, touched
	store.assets[asset.ID] = asset
	return asset
}

func dropObject(t *testing.T, svc *Service, asset Asset) {
	t.Helper()
	if err := svc.Objects.Delete(context.Background(), assetRef(asset)); err != nil {
		t.Fatalf("Delete object of %s: %v", asset.ID, err)
	}
}

// requireGone and requireKept read the row and the object together: a retired upload leaves
// neither, a kept one keeps both exactly as they were.
func requireGone(t *testing.T, svc *Service, store *fakeAssetStore, assets ...Asset) {
	t.Helper()
	for _, asset := range assets {
		if row, ok := store.row(asset.ID); ok || objectExists(t, svc.Objects, asset) {
			t.Fatalf("%s survived the sweep: row %+v (present %v)", asset.ID, row, ok)
		}
	}
}

func requireKept(t *testing.T, svc *Service, store *fakeAssetStore, status Status, assets ...Asset) {
	t.Helper()
	for _, asset := range assets {
		row, ok := store.row(asset.ID)
		if !ok || row.Status != status || !row.DeletedAt.IsZero() || !objectExists(t, svc.Objects, asset) {
			t.Fatalf("%s = %+v (present %v), want it %s with its object", asset.ID, row, ok, status)
		}
	}
}

// An upload is abandoned an hour after its URL expires, measured from the last write to its
// row. Its bytes go if they arrived, and a URL never used counts as removed.
func TestDeleteSweepRetiresUploadsAbandonedAnHourPastTheirURL(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	svc.PresignTTL = 10 * time.Minute
	now := time.Now()
	abandoned := now.Add(-svc.PresignTTL - time.Hour)
	putNeverFinalized := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-put", now), abandoned.Add(-time.Minute))
	neverPut := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-unused", now), abandoned.Add(-72*time.Hour))
	dropObject(t, svc, neverPut)
	streaming := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-streaming", now), abandoned.Add(time.Minute))
	theirs := presign(store, seedStoredAsset(t, svc, store, "owner-b", "b-put", now), abandoned.Add(-time.Minute))
	accepted := seedStoredAsset(t, svc, store, "owner-a", "a-accepted", now.Add(-72*time.Hour))

	finished, err := DeleteSweep{Assets: svc, Identities: identityList{"owner-a", "owner-b"}}.SweepExpired(context.Background(), now)
	if err != nil || finished != 3 {
		t.Fatalf("sweep = %d, %v; want the three abandoned uploads finished", finished, err)
	}
	requireGone(t, svc, store, putNeverFinalized, neverPut, theirs)
	requireKept(t, svc, store, StatusPresigned, streaming)
	requireKept(t, svc, store, StatusAccepted, accepted)
}

// The hour is added to the URL lifetime the deployment configures, not to the default.
func TestDeleteSweepWaitsOutTheConfiguredURLLifetime(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	svc.PresignTTL = 6 * time.Hour
	now := time.Now()
	insideLongURL := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-inside", now), now.Add(-3*time.Hour))
	pastLongURL := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-past", now), now.Add(-7*time.Hour-time.Minute))

	finished, err := DeleteSweep{Assets: svc, Identities: identityList{"owner-a"}}.SweepExpired(context.Background(), now)
	if err != nil || finished != 1 {
		t.Fatalf("sweep = %d, %v; want only the upload past six hours and one", finished, err)
	}
	requireGone(t, svc, store, pastLongURL)
	requireKept(t, svc, store, StatusPresigned, insideLongURL)
}

func TestDeleteSweepRetiresTheOldestAbandonedUploadsWithinTheBatch(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	now := time.Now()
	oldest := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-oldest", now), now.Add(-5*time.Hour))
	older := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-older", now), now.Add(-4*time.Hour))
	newer := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-newer", now), now.Add(-3*time.Hour))
	sweep := DeleteSweep{Assets: svc, Identities: identityList{"owner-a"}, Batch: 2}

	if finished, err := sweep.SweepExpired(context.Background(), now); err != nil || finished != 2 {
		t.Fatalf("first sweep = %d, %v; want the batch of two", finished, err)
	}
	requireGone(t, svc, store, oldest, older)
	requireKept(t, svc, store, StatusPresigned, newer)
	if finished, err := sweep.SweepExpired(context.Background(), now); err != nil || finished != 1 {
		t.Fatalf("second sweep = %d, %v; want the upload the bound held back", finished, err)
	}
	requireGone(t, svc, store, newer)
}

// unretirableStore fails the retirement of one identity's rows, as a lost connection would.
type unretirableStore struct {
	*fakeAssetStore
	identityID string
}

func (s unretirableStore) RetireIdle(ctx context.Context, identityID string, statuses []Status, before time.Time, limit int) error {
	if identityID == s.identityID {
		return errors.New("connection reset")
	}
	return s.fakeAssetStore.RetireIdle(ctx, identityID, statuses, before, limit)
}

// A failed retirement costs neither that identity's pending deletes nor anyone else's uploads.
func TestDeleteSweepFinishesDeletesPastAFailedRetirement(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	now := time.Now()
	stuck := seedStoredAsset(t, svc, store, "owner-a", "a-stuck", now)
	markStuck(store, stuck)
	unreached := presign(store, seedStoredAsset(t, svc, store, "owner-a", "a-unreached", now), now.Add(-5*time.Hour))
	theirs := presign(store, seedStoredAsset(t, svc, store, "owner-b", "b-put", now), now.Add(-5*time.Hour))
	svc.Store = unretirableStore{fakeAssetStore: store, identityID: "owner-a"}

	finished, err := DeleteSweep{Assets: svc, Identities: identityList{"owner-a", "owner-b"}}.SweepExpired(context.Background(), now)
	if finished != 2 || err == nil || !strings.Contains(err.Error(), "owner-a") || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("sweep = %d, %v; want owner-a's delete and owner-b's upload finished, owner-a named", finished, err)
	}
	requireGone(t, svc, store, stuck, theirs)
	requireKept(t, svc, store, StatusPresigned, unreached)
}

// spend puts a row in the state an upload the pipeline gave up on is left in: failed or
// refused, last written at touched.
func spend(store *fakeAssetStore, asset Asset, status Status, touched time.Time) Asset {
	store.mu.Lock()
	defer store.mu.Unlock()
	asset.Status, asset.UpdatedAt = status, touched
	store.assets[asset.ID] = asset
	return asset
}

// A refused or failed row outlives its use: a refused upload's bytes are gone, and a failed
// one is retried within minutes if at all. Past its lifetime it leaves like any delete, taking
// the object a failed refusal could not drop.
func TestDeleteSweepRetiresFailedAndRefusedRowsPastTheirLifetime(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	lifetime := 14 * 24 * time.Hour
	now := time.Now()
	expired := now.Add(-lifetime - time.Hour)
	failed := spend(store, seedStoredAsset(t, svc, store, "owner-a", "a-failed", now), StatusFailed, expired)
	refused := spend(store, seedStoredAsset(t, svc, store, "owner-a", "a-refused", now), StatusRefused, expired)
	dropObject(t, svc, refused)
	recent := spend(store, seedStoredAsset(t, svc, store, "owner-a", "a-recent", now), StatusFailed, now.Add(-lifetime+time.Hour))
	keptBytes := spend(store, seedStoredAsset(t, svc, store, "owner-b", "b-refused", now), StatusRefused, expired)
	accepted := seedStoredAsset(t, svc, store, "owner-a", "a-accepted", now.Add(-4*lifetime))

	finished, err := DeleteSweep{Assets: svc, Identities: identityList{"owner-a", "owner-b"}, FailedLifetime: lifetime}.
		SweepExpired(context.Background(), now)
	if err != nil || finished != 3 {
		t.Fatalf("sweep = %d, %v; want the three rows past their lifetime finished", finished, err)
	}
	requireGone(t, svc, store, failed, refused, keptBytes)
	requireKept(t, svc, store, StatusFailed, recent)
	requireKept(t, svc, store, StatusAccepted, accepted)
}

func TestDeleteSweepKeepsFailedRowsWithoutALifetime(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{})
	now := time.Now()
	failed := spend(store, seedStoredAsset(t, svc, store, "owner-a", "a-failed", now), StatusFailed, now.Add(-365*24*time.Hour))

	finished, err := DeleteSweep{Assets: svc, Identities: identityList{"owner-a"}}.SweepExpired(context.Background(), now)
	if err != nil || finished != 0 {
		t.Fatalf("sweep = %d, %v; want nothing retired without a lifetime", finished, err)
	}
	requireKept(t, svc, store, StatusFailed, failed)
}
