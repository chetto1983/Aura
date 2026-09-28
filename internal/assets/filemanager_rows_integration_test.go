//go:build db_integration

// The file manager's writes against the asset rows in Postgres, as aura_app under RLS: a move
// or rename takes the rows of the keys it relocates, and a key a row holds is never written.

package assets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

func fileManagerFixture(t *testing.T, ctx context.Context, store *Store, identityID, key string) Asset {
	t.Helper()
	asset := createAcceptedAsset(t, ctx, store, deleteFixture(identityID, "", key, ScopeLibrary))
	t.Cleanup(func() { retireAsset(t, store, asset.ID, identityID) })
	return asset
}

func requireStoredKey(t *testing.T, ctx context.Context, store *Store, asset Asset, key, name string) {
	t.Helper()
	got, err := store.GetForIdentity(ctx, asset.ID, asset.IdentityID)
	if err != nil || got.ObjectKey != key || got.FileName != name {
		t.Fatalf("row %s = key %q name %q (%v), want key %q name %q", asset.ID, got.ObjectKey, got.FileName, err, key, name)
	}
}

// A moved folder takes the rows of every key beneath it and a renamed file takes its row and
// its name, so the asset still opens by its id and still lists under its name. Another
// identity's row on the same key belongs to other bytes and stays.
func TestFileManagerMoveAndRenameTakeTheAssetRows(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := NewStore(pool)
	seedOtherIdentity(t, ctx, pool, "asset-files-other")
	root := fmt.Sprintf("fm-%d", time.Now().UnixNano())
	top := fileManagerFixture(t, ctx, store, localIdentityID, root+"/inbox/a.pdf")
	nested := fileManagerFixture(t, ctx, store, localIdentityID, root+"/inbox/sotto/b.pdf")
	theirs := fileManagerFixture(t, ctx, store, otherIdentityID, root+"/inbox/a.pdf")
	objects := objectstore.NewFake()
	for _, asset := range []Asset{top, nested} {
		if _, err := objects.Put(ctx, assetRef(asset), strings.NewReader("%PDF"), objectstore.PutOptions{Size: 4}); err != nil {
			t.Fatalf("Put %s: %v", asset.ObjectKey, err)
		}
	}
	browser := &Browser{Objects: objects, SharedBucket: "asset-test", Rows: store}

	if _, err := browser.Move(ctx, localIdentityID, []string{"/" + root + "/inbox"}, "/"+root+"/archivio"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	requireStoredKey(t, ctx, store, top, root+"/archivio/inbox/a.pdf", "gone.pdf")
	requireStoredKey(t, ctx, store, nested, root+"/archivio/inbox/sotto/b.pdf", "gone.pdf")
	requireStoredKey(t, ctx, store, theirs, root+"/inbox/a.pdf", "gone.pdf")

	if _, err := browser.Rename(ctx, localIdentityID, "/"+root+"/archivio/inbox/a.pdf", "q3.pdf"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	requireStoredKey(t, ctx, store, top, root+"/archivio/inbox/q3.pdf", "q3.pdf")
}

// A row being deleted still holds its key until the sweep removes its object, so a move that
// lands there is refused rather than handing its bytes to that removal.
func TestFileManagerRefusesAKeyADeletingRowHolds(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := NewStore(pool)
	root := fmt.Sprintf("fm-%d", time.Now().UnixNano())
	deleting := fileManagerFixture(t, ctx, store, localIdentityID, root+"/held.pdf")
	if _, err := store.Delete(ctx, deleting.ID, localIdentityID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	objects := objectstore.NewFake()
	incoming := objectstore.ObjectRef{Bucket: "asset-test", Key: root + "/incoming/held.pdf"}
	if _, err := objects.Put(ctx, incoming, strings.NewReader("new"), objectstore.PutOptions{Size: 3}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	browser := &Browser{Objects: objects, SharedBucket: "asset-test", Rows: store}

	if _, err := browser.Move(ctx, localIdentityID, []string{"/" + incoming.Key}, "/"+root); !errors.Is(err, ErrDestinationHeld) {
		t.Fatalf("Move onto a deleting row's key: err = %v, want ErrDestinationHeld", err)
	}
	if _, err := objects.Head(ctx, incoming); err != nil {
		t.Fatalf("the refused move removed its source: %v", err)
	}
	requireRowState(t, pool, deleting, StatusDeleting, true)
}
