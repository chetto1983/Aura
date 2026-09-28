//go:build db_integration

// The delete lifecycle against Postgres itself, as aura_app under migration 0090's fail-closed
// floor: the stamp that hides a deleting row, the hard delete and what cascades from it, and
// the tombstone a media_job pin forces. The fake store in service_test.go mirrors these rules;
// this is where they are proven.

package assets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func createAcceptedAsset(t *testing.T, ctx context.Context, store *Store, req CreateRequest) Asset {
	t.Helper()
	created, err := store.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create %s: %v", req.ObjectKey, err)
	}
	accepted, err := store.SetStatus(ctx, created.ID, req.IdentityID, StatusAccepted, "", "")
	if err != nil {
		t.Fatalf("SetStatus accepted %s: %v", req.ObjectKey, err)
	}
	return accepted
}

func deleteFixture(identityID, thread, key string, scope Scope) CreateRequest {
	return CreateRequest{
		IdentityID: identityID, SourceKind: SourceWeb, ThreadID: thread, Scope: scope,
		Modality: ModalityDocument, FileName: "gone.pdf", MIMEType: "application/pdf",
		DeclaredSizeBytes: 4, ObjectBucket: "asset-test", ObjectKey: key, Metadata: map[string]any{},
	}
}

// countAs counts rows as identityID sees them, deleted or not: the store's own reads hide a
// deleted row, which is exactly what these tests must see past.
func countAs(t *testing.T, pool *pgxpool.Pool, identityID, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), query, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func execAs(t *testing.T, pool *pgxpool.Pool, identityID, statement string, args ...any) {
	t.Helper()
	if err := db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), statement, args...)
		return err
	}); err != nil {
		t.Fatalf("exec %q: %v", statement, err)
	}
}

// The stamp is what fixes the leak the operator's stuck rows exposed: with deleted_at NULL a
// deleting row came back from every listing and a late status write could revive it.
func TestStoreDeleteHidesTheRowFromEveryReadAndWrite(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UnixNano()
	store := NewStore(pool)
	thread := fmt.Sprintf("delete-hide-%d", now)
	key := fmt.Sprintf("chat/%d-hidden.pdf", now)
	asset := createAcceptedAsset(t, ctx, store, deleteFixture(localIdentityID, thread, key, ScopeLibrary))

	deleted, err := store.Delete(ctx, asset.ID, localIdentityID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted.Status != StatusDeleting || deleted.DeletedAt.IsZero() {
		t.Fatalf("Delete = %s, deleted_at %v; want deleting with deleted_at stamped", deleted.Status, deleted.DeletedAt)
	}

	if _, err := store.GetForIdentity(ctx, asset.ID, localIdentityID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetForIdentity = %v, want no rows", err)
	}
	if _, err := store.ByObjectKey(ctx, localIdentityID, key); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ByObjectKey = %v, want no rows", err)
	}
	listings := map[string]func() ([]Asset, error){
		"thread":  func() ([]Asset, error) { return store.ListForThread(ctx, localIdentityID, thread) },
		"library": func() ([]Asset, error) { return store.ListForLibrary(ctx, localIdentityID, 1000) },
		"recent": func() ([]Asset, error) {
			return store.ListRecent(ctx, localIdentityID, []Modality{ModalityDocument}, 48)
		},
	}
	for name, list := range listings {
		listed, err := list()
		if err != nil {
			t.Fatalf("%s listing: %v", name, err)
		}
		for _, row := range listed {
			if row.ID == asset.ID {
				t.Fatalf("the %s listing still returns the deleted asset", name)
			}
		}
	}
	if names, err := store.AssetsByKey(ctx, localIdentityID, []string{key}); err != nil || len(names) != 0 {
		t.Fatalf("AssetsByKey = %v, %v; want the deleted key unnamed", names, err)
	}

	if _, err := store.SetStatus(ctx, asset.ID, localIdentityID, StatusComplete, "", ""); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetStatus complete on a deleting row = %v, want no rows: a late job must not revive it", err)
	}
	if _, err := store.SetResult(ctx, asset.ID, localIdentityID, Result{Status: StatusComplete}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("SetResult on a deleting row = %v, want no rows", err)
	}
	if _, err := store.Delete(ctx, asset.ID, localIdentityID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a second Delete = %v, want no rows", err)
	}

	pending, err := store.ListDeleting(ctx, localIdentityID, 1000)
	if err != nil {
		t.Fatalf("ListDeleting: %v", err)
	}
	if !containsAsset(pending, asset.ID) {
		t.Fatal("ListDeleting does not offer the deleting row to the sweep")
	}
	if err := store.Finalize(ctx, asset.ID, localIdentityID); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
}

// Finalize is a hard delete as aura_app: the grant 0001's default privileges give, the RLS
// policy that admits the owner, and the cascades the lab VM's stuck rows carry (99 of 258 had
// an ingestion job, and those jobs 206 events). A library key is fixed by the file name, so it
// must be free again for the same file to be uploaded.
func TestStoreFinalizeRemovesTheRowWithItsJobsAndFreesTheKey(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UnixNano()
	store := NewStore(pool)
	key := fmt.Sprintf("chat/%d-library.pdf", now)
	request := deleteFixture(localIdentityID, "", key, ScopeLibrary)
	asset := createAcceptedAsset(t, ctx, store, request)

	execAs(t, pool, localIdentityID,
		`INSERT INTO aura.asset_events (asset_id, seq, to_status) VALUES ($1, 1, 'accepted')`, asset.ID)
	jobID := uuid.NewString()
	execAs(t, pool, localIdentityID, `
INSERT INTO aura.ingestion_jobs (id, job_type, status, idempotency_key, payload, identity_id, asset_id)
VALUES ($1, 'asset_process', 'succeeded', $2, '{}'::jsonb, $3, $4)`,
		jobID, "delete-test-"+jobID, localIdentityID, asset.ID)
	execAs(t, pool, localIdentityID, `
INSERT INTO aura.ingestion_events (entity_type, entity_id, job_id, event_type, identity_id)
VALUES ('asset', $1, $2, 'processed', $3)`, asset.ID, jobID, localIdentityID)

	if _, err := store.Delete(ctx, asset.ID, localIdentityID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Finalize(ctx, asset.ID, localIdentityID); err != nil {
		t.Fatalf("Finalize as aura_app: %v", err)
	}

	for table, query := range map[string]string{
		"assets":         `SELECT count(*) FROM aura.assets WHERE id = $1`,
		"asset_events":   `SELECT count(*) FROM aura.asset_events WHERE asset_id = $1`,
		"ingestion_jobs": `SELECT count(*) FROM aura.ingestion_jobs WHERE asset_id = $1`,
	} {
		if n := countAs(t, pool, localIdentityID, query, asset.ID); n != 0 {
			t.Fatalf("%s kept %d row(s) of the finalized asset", table, n)
		}
	}
	if n := countAs(t, pool, localIdentityID,
		`SELECT count(*) FROM aura.ingestion_events WHERE entity_id = $1 AND job_id IS NULL`, asset.ID); n != 1 {
		t.Fatalf("ingestion events kept with job_id NULL = %d, want the 1 event (ON DELETE SET NULL (job_id))", n)
	}
	if err := store.Finalize(ctx, asset.ID, localIdentityID); err != nil {
		t.Fatalf("a second Finalize = %v, want a no-op", err)
	}

	again, err := store.Create(ctx, request)
	if err != nil {
		t.Fatalf("re-creating the freed key: %v", err)
	}
	if _, err := store.Delete(ctx, again.ID, localIdentityID); err != nil {
		t.Fatalf("cleanup Delete: %v", err)
	}
	if err := store.Finalize(ctx, again.ID, localIdentityID); err != nil {
		t.Fatalf("cleanup Finalize: %v", err)
	}
}

// A completed media_job must keep the pointer to the clip it paid for (migration 0128 gives
// the key no ON DELETE), so a pinned row ends as a deleted tombstone instead of leaving.
func TestStoreFinalizeLeavesATombstoneForAMediaJobsClip(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UnixNano()
	store := NewStore(pool)
	conversation := fmt.Sprintf("delete-clip-%d", now)
	clip := createAcceptedAsset(t, ctx, store, CreateRequest{
		IdentityID: localIdentityID, SourceKind: SourceAgent, SourceRef: conversation, ThreadID: conversation,
		Scope: ScopeThread, Modality: ModalityVideo, FileName: "clip.mp4", MIMEType: "video/mp4",
		DeclaredSizeBytes: 4, ObjectBucket: "asset-test", ObjectKey: fmt.Sprintf("media/%d-clip.mp4", now),
		Metadata: map[string]any{},
	})
	execAs(t, pool, localIdentityID, `
INSERT INTO aura.media_job (identity_id, conversation_id, tool_call_id, provider_job_id, model, request,
                            status, asset_id, completed_at)
VALUES ($1, $2, 'call-clip', $3, 'test/video', '{}'::jsonb, 'completed', $4, now())`,
		localIdentityID, conversation, "provider-"+conversation, clip.ID)

	if _, err := store.Delete(ctx, clip.ID, localIdentityID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Finalize(ctx, clip.ID, localIdentityID); err != nil {
		t.Fatalf("Finalize of a pinned clip: %v", err)
	}
	if n := countAs(t, pool, localIdentityID,
		`SELECT count(*) FROM aura.assets WHERE id = $1 AND status = 'deleted' AND deleted_at IS NOT NULL`, clip.ID); n != 1 {
		t.Fatalf("pinned clip tombstones = %d, want the row kept as deleted", n)
	}
	if n := countAs(t, pool, localIdentityID,
		`SELECT count(*) FROM aura.media_job WHERE asset_id = $1`, clip.ID); n != 1 {
		t.Fatalf("media jobs still pointing at the clip = %d, want 1", n)
	}
	pending, err := store.ListDeleting(ctx, localIdentityID, 1000)
	if err != nil {
		t.Fatalf("ListDeleting: %v", err)
	}
	if containsAsset(pending, clip.ID) {
		t.Fatal("a tombstone is still offered to the sweep")
	}
}

// The identity in the call scopes every statement: another identity can neither finish nor
// list someone else's delete, and a live row is never finalized.
func TestStoreFinalizeTouchesOnlyTheCallersDeletingRow(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UnixNano()
	store := NewStore(pool)
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user') ON CONFLICT (id) DO NOTHING",
		otherIdentityID, fmt.Sprintf("asset-delete-other-%d", now)); err != nil {
		t.Fatalf("seed second identity: %v", err)
	}
	theirs := createAcceptedAsset(t, ctx, store,
		deleteFixture(otherIdentityID, "", fmt.Sprintf("chat/%d-theirs.pdf", now), ScopeThread))
	live := createAcceptedAsset(t, ctx, store,
		deleteFixture(localIdentityID, "", fmt.Sprintf("chat/%d-live.pdf", now), ScopeThread))
	if _, err := store.Delete(ctx, theirs.ID, otherIdentityID); err != nil {
		t.Fatalf("Delete theirs: %v", err)
	}

	if err := store.Finalize(ctx, theirs.ID, localIdentityID); err != nil {
		t.Fatalf("Finalize of another identity's row = %v, want a no-op", err)
	}
	if err := store.Finalize(ctx, live.ID, localIdentityID); err != nil {
		t.Fatalf("Finalize of a live row = %v, want a no-op", err)
	}
	if _, err := store.GetForIdentity(ctx, live.ID, localIdentityID); err != nil {
		t.Fatalf("the live row was touched: %v", err)
	}
	mine, err := store.ListDeleting(ctx, localIdentityID, 1000)
	if err != nil {
		t.Fatalf("ListDeleting mine: %v", err)
	}
	if containsAsset(mine, theirs.ID) {
		t.Fatal("ListDeleting leaked another identity's row")
	}
	pending, err := store.ListDeleting(ctx, otherIdentityID, 1000)
	if err != nil || !containsAsset(pending, theirs.ID) {
		t.Fatalf("their deleting row was finished by a stranger: %v", err)
	}
	if err := store.Finalize(ctx, theirs.ID, otherIdentityID); err != nil {
		t.Fatalf("Finalize as the owner: %v", err)
	}
}

// The operator's report end to end on the real store: after a delete neither the row nor the
// object remains.
func TestServiceDeleteLeavesNothingBehindInPostgres(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().UnixNano()
	svc := &Service{Store: NewStore(pool), Objects: objectstore.NewFake(), Bucket: "asset-test"}
	asset := createAcceptedAsset(t, ctx, NewStore(pool),
		deleteFixture(localIdentityID, "", fmt.Sprintf("chat/%d-service.pdf", now), ScopeThread))
	if _, err := svc.Objects.Put(ctx, assetRef(asset), strings.NewReader("%PDF"), objectstore.PutOptions{Size: 4}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := svc.Delete(ctx, localIdentityID, asset.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n := countAs(t, pool, localIdentityID, `SELECT count(*) FROM aura.assets WHERE id = $1`, asset.ID); n != 0 {
		t.Fatalf("the deleted asset left %d row(s) in aura.assets", n)
	}
	if _, err := svc.Objects.Head(ctx, assetRef(asset)); !objectstore.IsNotFound(err) {
		t.Fatalf("Head after delete = %v, want not found", err)
	}
}

func containsAsset(assets []Asset, id string) bool {
	for _, asset := range assets {
		if asset.ID == id {
			return true
		}
	}
	return false
}
