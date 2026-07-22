//go:build db_integration

package conversations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestExportSnapshotVersionRejectsRenameAndMetadataRaces proves fields that are
// serialized into conversation.json advance the same monotonic token used by the
// delete reservation. A stale exporter therefore preserves the live row.
func TestExportSnapshotVersionRejectsRenameAndMetadataRaces(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(*Store, string) error
	}{
		{
			name: "rename",
			mutate: func(store *Store, conversationID string) error {
				_, err := store.RenameForIdentity(ctx, conversationID, localID, "new title")
				return err
			},
		},
		{
			name: "reasoning metadata",
			mutate: func(store *Store, conversationID string) error {
				_, err := store.UpdateReasoningEffortForIdentity(ctx, conversationID, localID, "high")
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(t, migratedPool(t))
			conversationID := newConversation(t, store)
			before, err := store.VersionForIdentity(ctx, conversationID, localID)
			if err != nil {
				t.Fatalf("initial version: %v", err)
			}
			if err := tc.mutate(store, conversationID); err != nil {
				t.Fatalf("concurrent mutation: %v", err)
			}
			after, err := store.VersionForIdentity(ctx, conversationID, localID)
			if err != nil || after <= before {
				t.Fatalf("version before/after = %d/%d, err=%v", before, after, err)
			}
			affected, err := store.ReserveDeleteForIdentityIfVersion(ctx, conversationID, localID, before, "stale-export")
			if err != nil || affected != 0 {
				t.Fatalf("stale reservation affected=%d err=%v, want 0/nil", affected, err)
			}
			if _, err := store.GetForIdentity(ctx, conversationID, localID); err != nil {
				t.Fatalf("stale reservation removed live conversation: %v", err)
			}
		})
	}
}

// TestExportSnapshotVersionCoversAssetsAndReservationFreezesWriters proves
// thread assets participate in the token and all snapshot writers are rejected
// once the matching reservation commits.
func TestExportSnapshotVersionCoversAssetsAndReservationFreezesWriters(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	store := newStore(t, pool)
	conversationID := newConversation(t, store)

	before, err := store.VersionForIdentity(ctx, conversationID, localID)
	if err != nil {
		t.Fatal(err)
	}
	assetID := uuid.NewString()
	_, err = pool.Exec(ctx, `
		INSERT INTO aura.assets
			(id, identity_id, source_kind, thread_id, scope, modality, status,
			 file_name, object_bucket, object_key)
		VALUES ($1, $2, 'web', $3, 'thread', 'document', 'presigned',
			'proof.txt', 'test', $4)`, assetID, localID, conversationID, "proof/"+assetID)
	if err != nil {
		t.Fatalf("insert thread asset: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM aura.assets WHERE id=$1", assetID) })
	afterInsert, err := store.VersionForIdentity(ctx, conversationID, localID)
	if err != nil || afterInsert <= before {
		t.Fatalf("asset insert version before/after=%d/%d err=%v", before, afterInsert, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE aura.assets SET status='uploaded', updated_at=now() WHERE id=$1", assetID); err != nil {
		t.Fatalf("update thread asset: %v", err)
	}
	afterUpdate, err := store.VersionForIdentity(ctx, conversationID, localID)
	if err != nil || afterUpdate <= afterInsert {
		t.Fatalf("asset update version before/after=%d/%d err=%v", afterInsert, afterUpdate, err)
	}

	const reservation = "asset-freeze-proof"
	affected, err := store.ReserveDeleteForIdentityIfVersion(ctx, conversationID, localID, afterUpdate, reservation)
	if err != nil || affected != 1 {
		t.Fatalf("reserve matching snapshot affected=%d err=%v", affected, err)
	}
	if _, err := store.RenameForIdentity(ctx, conversationID, localID, "must not land"); err == nil {
		t.Fatal("rename succeeded after delete reservation")
	}
	if _, err := pool.Exec(ctx, "UPDATE aura.assets SET status='accepted', updated_at=now() WHERE id=$1", assetID); err == nil {
		t.Fatal("asset update succeeded after delete reservation")
	}
	const worker = "asset-freeze-worker"
	if affected, err = store.ClaimDeleteTeardown(ctx, conversationID, localID, reservation, worker, time.Now().Add(time.Minute)); err != nil || affected != 1 {
		t.Fatalf("claim reserved teardown affected=%d err=%v", affected, err)
	}
	if affected, err = store.DeleteForIdentityIfReservation(ctx, conversationID, localID, reservation, worker); err != nil || affected != 1 {
		t.Fatalf("reserved final delete affected=%d err=%v", affected, err)
	}
}

func TestExportDeleteLeasePreventsAdoptionAndReservationReleaseAfterTeardown(t *testing.T) {
	ctx := context.Background()
	store := newStore(t, migratedPool(t))
	conversationID := newConversation(t, store)
	version, err := store.VersionForIdentity(ctx, conversationID, localID)
	if err != nil {
		t.Fatal(err)
	}
	const reservation = "same-operation-reservation"
	if affected, err := store.ReserveDeleteForIdentityIfVersion(ctx, conversationID, localID, version, reservation); err != nil || affected != 1 {
		t.Fatalf("reserve affected=%d err=%v", affected, err)
	}
	if affected, err := store.ClaimDeleteTeardown(ctx, conversationID, localID, reservation, "worker-one", time.Now().Add(time.Minute)); err != nil || affected != 1 {
		t.Fatalf("first claim affected=%d err=%v", affected, err)
	}
	if affected, err := store.ClaimDeleteTeardown(ctx, conversationID, localID, reservation, "worker-two", time.Now().Add(time.Minute)); err != nil || affected != 0 {
		t.Fatalf("competing worker adopted live lease: affected=%d err=%v", affected, err)
	}
	if affected, err := store.ReleaseReservedDelete(ctx, conversationID, localID, reservation); err != nil || affected != 0 {
		t.Fatalf("operation reservation released after teardown: affected=%d err=%v", affected, err)
	}
	if affected, err := store.ReleaseDeleteLease(ctx, conversationID, localID, reservation, "worker-one"); err != nil || affected != 1 {
		t.Fatalf("release failed worker lease affected=%d err=%v", affected, err)
	}
	if affected, err := store.ClaimDeleteTeardown(ctx, conversationID, localID, reservation, "worker-two", time.Now().Add(time.Minute)); err != nil || affected != 1 {
		t.Fatalf("same operation did not resume after lease release: affected=%d err=%v", affected, err)
	}
	if affected, err := store.DeleteForIdentityIfReservation(ctx, conversationID, localID, reservation, "worker-two"); err != nil || affected != 1 {
		t.Fatalf("resumed worker final delete affected=%d err=%v", affected, err)
	}
}
