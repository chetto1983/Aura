package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/objectstore/garageadmin"
)

// serve_provisioning_objectstore_deprovision_test.go covers the object-store reverse leg.
//
// MEASURED 2026-09-08 on the live deployment: purging twelve identities through the new
// `aura identity purge` verb removed every other plane and left all twelve Garage buckets
// AND all twelve keys behind, while the saga journalled the objectstore step `done`. Two
// independent causes, both pinned here: DeleteKey's error was discarded (`_ =`), so an
// unreachable admin API read as success; and the bucket id was only ever read from the
// in-process map the PROVISIONING process filled, which a later purge process cannot have.
// garageadmin.Client has published BucketIDByAlias the whole time.

// deprovisionMinter is a daemon-free objectStoreMinter that records the teardown calls and
// can fail either of them, which the shared fakeMinter deliberately cannot do (its
// DeleteBucket/DeleteKey are unconditional no-ops).
type deprovisionMinter struct {
	aliasID       string
	aliasErr      error
	keyIDByName   string
	keyNameErr    error
	deletedKeys   []string
	deleteKeyErr  error
	deletedBucket []string
	deleteBktErr  error
}

func (d *deprovisionMinter) KeyIDByName(context.Context, string) (string, error) {
	if d.keyNameErr != nil {
		return "", d.keyNameErr
	}
	return d.keyIDByName, nil
}

func (d *deprovisionMinter) CreateBucket(context.Context, string) (string, error) {
	return "", errors.New("deprovisionMinter: CreateBucket must not be called on teardown")
}

func (d *deprovisionMinter) CreateKey(context.Context, string) (string, string, error) {
	return "", "", errors.New("deprovisionMinter: CreateKey must not be called on teardown")
}

func (d *deprovisionMinter) AllowBucketKey(context.Context, string, string, garageadmin.Permissions) error {
	return errors.New("deprovisionMinter: AllowBucketKey must not be called on teardown")
}

func (d *deprovisionMinter) BucketIDByAlias(context.Context, string) (string, error) {
	if d.aliasErr != nil {
		return "", d.aliasErr
	}
	return d.aliasID, nil
}

func (d *deprovisionMinter) DeleteKey(_ context.Context, accessKeyID string) error {
	d.deletedKeys = append(d.deletedKeys, accessKeyID)
	return d.deleteKeyErr
}

func (d *deprovisionMinter) DeleteBucket(_ context.Context, bucketID string) error {
	d.deletedBucket = append(d.deletedBucket, bucketID)
	return d.deleteBktErr
}

const deprovisionTestID = "6d0ebdee-bf43-4f5b-abf6-469310d52a5a"

func deprovisionFixture(t *testing.T, minter objectStoreMinter) (*objectStoreProvisionAdapter, *fakeResolver) {
	t.Helper()
	bucket, err := garageadmin.BucketForIdentity(deprovisionTestID)
	if err != nil {
		t.Fatalf("BucketForIdentity: %v", err)
	}
	resolver := newFakeResolver(deprovisionTestID)
	resolver.creds[deprovisionTestID] = objectstore.Credentials{
		Bucket: bucket, AccessKey: "GK-live", SecretKey: "sk-live",
	}
	return newObjectStoreProvisionAdapter(minter, resolver), resolver
}

func TestDeprovisionObjectStorePropagatesKeyDeleteFailure(t *testing.T) {
	boom := errors.New("garageadmin: call DeleteKey: dial tcp: lookup garage: no such host")
	minter := &deprovisionMinter{aliasID: "bkt-1", deleteKeyErr: boom}
	adapter, resolver := deprovisionFixture(t, minter)

	err := adapter.DeprovisionObjectStore(context.Background(), deprovisionTestID)
	if !errors.Is(err, boom) {
		t.Fatalf("DeprovisionObjectStore err = %v, want the DeleteKey failure propagated — a swallowed one journals the step done with the key still live", err)
	}
	if _, ok := resolver.creds[deprovisionTestID]; !ok {
		t.Fatal("the credential row was deleted even though the key delete failed — the next run would lose the access key it needs to retry")
	}
}

func TestDeprovisionObjectStoreResolvesBucketByAliasInALaterProcess(t *testing.T) {
	minter := &deprovisionMinter{aliasID: "bkt-from-alias"}
	adapter, resolver := deprovisionFixture(t, minter)

	// No ProvisionObjectStore call precedes this one: the in-process bucketIDs map is empty,
	// which is exactly the state a purge process is always in.
	if err := adapter.DeprovisionObjectStore(context.Background(), deprovisionTestID); err != nil {
		t.Fatalf("DeprovisionObjectStore err = %v, want nil", err)
	}
	if len(minter.deletedKeys) != 1 || minter.deletedKeys[0] != "GK-live" {
		t.Fatalf("deleted keys = %v, want [GK-live]", minter.deletedKeys)
	}
	if len(minter.deletedBucket) != 1 || minter.deletedBucket[0] != "bkt-from-alias" {
		t.Fatalf("deleted buckets = %v, want [bkt-from-alias] resolved via BucketIDByAlias", minter.deletedBucket)
	}
	if _, ok := resolver.creds[deprovisionTestID]; ok {
		t.Fatal("the credential row survived a successful teardown")
	}
}

// TestDeprovisionObjectStoreFindsKeyByNameWhenTheRowIsGone covers the state the live
// deployment was actually left in on 2026-09-08: the credential row had already been
// deleted by a run whose key delete silently failed, so Resolve answers pgx.ErrNoRows and
// there is no access key id anywhere in Postgres. The key name is derivable from the
// identity — it is what CreateKey labelled it with — so the teardown can still converge.
func TestDeprovisionObjectStoreFindsKeyByNameWhenTheRowIsGone(t *testing.T) {
	minter := &deprovisionMinter{aliasID: "bkt-1", keyIDByName: "GK-orphan"}
	adapter, resolver := deprovisionFixture(t, minter)
	delete(resolver.creds, deprovisionTestID) // Resolve now answers pgx.ErrNoRows

	if err := adapter.DeprovisionObjectStore(context.Background(), deprovisionTestID); err != nil {
		t.Fatalf("DeprovisionObjectStore err = %v, want nil", err)
	}
	if len(minter.deletedKeys) != 1 || minter.deletedKeys[0] != "GK-orphan" {
		t.Fatalf("deleted keys = %v, want [GK-orphan] resolved by name", minter.deletedKeys)
	}
}

func TestDeprovisionObjectStoreTreatsAbsentKeyNameAsDone(t *testing.T) {
	minter := &deprovisionMinter{aliasErr: garageadmin.ErrBucketNotFound, keyNameErr: garageadmin.ErrKeyNotFound}
	adapter, resolver := deprovisionFixture(t, minter)
	delete(resolver.creds, deprovisionTestID)

	if err := adapter.DeprovisionObjectStore(context.Background(), deprovisionTestID); err != nil {
		t.Fatalf("DeprovisionObjectStore err = %v, want nil — nothing left on either plane is a converged step", err)
	}
	if len(minter.deletedKeys) != 0 {
		t.Fatalf("deleted keys = %v, want none", minter.deletedKeys)
	}
}

func TestDeprovisionObjectStoreFailsOnAmbiguousKeyLookup(t *testing.T) {
	boom := errors.New("garageadmin: GetKeyInfo returned status 400")
	minter := &deprovisionMinter{aliasID: "bkt-1", keyNameErr: boom}
	adapter, resolver := deprovisionFixture(t, minter)
	delete(resolver.creds, deprovisionTestID)

	if err := adapter.DeprovisionObjectStore(context.Background(), deprovisionTestID); !errors.Is(err, boom) {
		t.Fatalf("DeprovisionObjectStore err = %v, want the ambiguous lookup propagated — deleting the wrong key is worse than leaving this one", err)
	}
}

func TestDeprovisionObjectStoreTreatsAbsentBucketAsDone(t *testing.T) {
	minter := &deprovisionMinter{aliasErr: garageadmin.ErrBucketNotFound}
	adapter, _ := deprovisionFixture(t, minter)

	if err := adapter.DeprovisionObjectStore(context.Background(), deprovisionTestID); err != nil {
		t.Fatalf("DeprovisionObjectStore err = %v, want nil — an already-absent bucket is a converged step, and the saga re-runs", err)
	}
	if len(minter.deletedBucket) != 0 {
		t.Fatalf("deleted buckets = %v, want none for an absent bucket", minter.deletedBucket)
	}
}

func TestDeprovisionObjectStoreFailsOnUnreadableBucketLookup(t *testing.T) {
	boom := errors.New("garageadmin: GetBucketInfo returned status 500")
	minter := &deprovisionMinter{aliasErr: boom}
	adapter, _ := deprovisionFixture(t, minter)

	if err := adapter.DeprovisionObjectStore(context.Background(), deprovisionTestID); !errors.Is(err, boom) {
		t.Fatalf("DeprovisionObjectStore err = %v, want the lookup failure propagated — \"we could not check\" is not \"there is no bucket\"", err)
	}
}
