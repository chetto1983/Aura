//go:build db_integration

// The fail-closed invariant migration 0090 put on aura.assets and aura.asset_events, pinned
// rather than merely observed once.
//
// Before 0090 these two tables had NO row security at all — not a fail-open policy, the
// absence of one. Measured on the live deployment on 2026-08-03: relrowsecurity=false and
// zero policies on both, so every asset of every identity was readable and writable by any
// aura_app connection and the only isolation was the WHERE identity_id clause in each store
// method. A test that only exercised the store would have passed just as happily then as
// now, which is why the raw-pool probes below assert on the POLICY and not on the Go API.
//
// The read half is the one that regresses silently — a policy that fails open returns MORE
// rows, never an error — so every read case asserts a COUNT, not just an absence of error.

package assets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedAssetRLSIdentity(t *testing.T, pool *pgxpool.Pool, id, name string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING",
		id, name); err != nil {
		t.Fatalf("seed identity %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id = $1", id)
	})
}

// insertAssetAs inserts one asset owned by identityID inside that identity's transaction and
// returns its id. It goes through the raw pool rather than the Store so that the policy, not
// the store's own WHERE clause, is what admits the row.
func insertAssetAs(t *testing.T, pool *pgxpool.Pool, identityID, key string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	err := db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `
INSERT INTO aura.assets (id, identity_id, source_kind, source_ref, thread_id, scope, modality,
                         status, file_name, mime_type, declared_size_bytes, object_bucket, object_key)
VALUES ($1, $2, 'web', 'https://example.test/x', 'rls-thread', 'thread', 'document',
        'presigned', 'x.pdf', 'application/pdf', 1, 'rls-bucket', $3)`,
			id, identityID, key)
		return e
	})
	if err != nil {
		t.Fatalf("seed asset for %s: %v", identityID, err)
	}
	return id
}

// TestAssetsRLSFailsClosedWithoutIdentity is proof that migration 0090's floor bites: with no
// app.current_identity an aura_app connection can neither read nor write aura.assets, and
// both succeed the moment the identity is bound.
func TestAssetsRLSFailsClosedWithoutIdentity(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	seedAssetRLSIdentity(t, pool, idA, "rlsasset-a-"+idA[:8])
	seedAssetRLSIdentity(t, pool, idB, "rlsasset-b-"+idB[:8])

	assetA := insertAssetAs(t, pool, idA, "rls/a/one.pdf")
	insertAssetAs(t, pool, idB, "rls/b/one.pdf")

	// 1. No identity: both tables are invisible, not merely filtered.
	for _, table := range []string{"aura.assets", "aura.asset_events"} {
		var visible int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&visible); err != nil {
			t.Fatalf("count %s with no identity: %v", table, err)
		}
		if visible != 0 {
			t.Errorf("%s returned %d row(s) to a connection with no app.current_identity, want 0 — RLS is failing OPEN", table, visible)
		}
	}

	// 2. No identity: the write is REFUSED, not silently dropped. The restrictive floor has
	// no WITH CHECK of its own, so it defaults to its USING clause and rejects the INSERT
	// outright with 42501 — the same SQLSTATE the 0087 outage produced on every idempotency
	// Begin, which is the failure this pairing of migration and store exists to avoid.
	_, err := pool.Exec(ctx, `
INSERT INTO aura.assets (id, identity_id, source_kind, source_ref, thread_id, scope, modality,
                         status, file_name, mime_type, declared_size_bytes, object_bucket, object_key)
VALUES ($1, $2, 'web', '', '', 'thread', 'document', 'presigned', 'x.pdf', 'application/pdf', 1, 'b', 'k')`,
		uuid.Must(uuid.NewV7()).String(), idA)
	if err == nil {
		t.Fatal("INSERT into aura.assets succeeded with no app.current_identity, want a row-level-security refusal")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("INSERT with no identity error = %v, want SQLSTATE 42501 (insufficient_privilege)", err)
	}

	// 3. Under identity A: A sees exactly its own row and none of B's, and the write it was
	// refused in (2) is accepted. Both halves matter — a floor that also locked the owner out
	// would pass (1) and (2) and still be an outage.
	if err := db.WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		var got int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM aura.assets").Scan(&got); e != nil {
			return e
		}
		if got != 1 {
			t.Errorf("identity A sees %d asset(s), want exactly its own 1", got)
		}
		var ownKey string
		if e := tx.QueryRow(ctx, "SELECT object_key FROM aura.assets WHERE id = $1", assetA).Scan(&ownKey); e != nil {
			return e
		}
		if ownKey != "rls/a/one.pdf" {
			t.Errorf("identity A read object_key=%q, want its own row", ownKey)
		}
		_, e := tx.Exec(ctx, `
INSERT INTO aura.assets (id, identity_id, source_kind, source_ref, thread_id, scope, modality,
                         status, file_name, mime_type, declared_size_bytes, object_bucket, object_key)
VALUES ($1, $2, 'web', '', '', 'thread', 'document', 'presigned', 'y.pdf', 'application/pdf', 1, 'b', 'rls/a/two.pdf')`,
			uuid.Must(uuid.NewV7()).String(), idA)
		return e
	}); err != nil {
		t.Fatalf("identity-scoped read+write for A: %v", err)
	}

	// 4. Identity A may not write a row owned by B. WITH CHECK defaults to USING on the
	// permissive policy too, so the owner column is not a field the caller gets to choose.
	err = db.WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `
INSERT INTO aura.assets (id, identity_id, source_kind, source_ref, thread_id, scope, modality,
                         status, file_name, mime_type, declared_size_bytes, object_bucket, object_key)
VALUES ($1, $2, 'web', '', '', 'thread', 'document', 'presigned', 'z.pdf', 'application/pdf', 1, 'b', 'k')`,
			uuid.Must(uuid.NewV7()).String(), idB)
		return e
	})
	if err == nil {
		t.Error("identity A inserted an asset owned by B, want a WITH CHECK refusal")
	}
}

// TestAssetEventsRLSScopesThroughParentOwner pins the shape of the child policy, which is the
// one thing in 0090 that is easy to get wrong. asset_events has no identity column, so it can
// only be scoped through aura.assets — but it names the parent's OWNER directly instead of
// checking that the parent row is merely visible. Migration 0032 made the latter mistake with
// conversation_turns and 0089 had to undo it: two layers that cede together are one layer.
func TestAssetEventsRLSScopesThroughParentOwner(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	seedAssetRLSIdentity(t, pool, idA, "rlsev-a-"+idA[:8])
	seedAssetRLSIdentity(t, pool, idB, "rlsev-b-"+idB[:8])

	assetA := insertAssetAs(t, pool, idA, "rls/ev/a.pdf")
	assetB := insertAssetAs(t, pool, idB, "rls/ev/b.pdf")

	for _, seed := range []struct{ identity, asset string }{{idA, assetA}, {idB, assetB}} {
		if err := db.WithIdentityTxRaw(ctx, pool, seed.identity, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				"INSERT INTO aura.asset_events (asset_id, seq, from_status, to_status, reason) VALUES ($1, 1, 'presigned', 'uploaded', 'rls-test')",
				seed.asset)
			return e
		}); err != nil {
			t.Fatalf("seed asset event for %s: %v", seed.identity, err)
		}
	}

	// A sees only the event hanging off its own asset.
	if err := db.WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		var got int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM aura.asset_events").Scan(&got); e != nil {
			return e
		}
		if got != 1 {
			t.Errorf("identity A sees %d asset event(s), want exactly its own 1", got)
		}
		var foreign int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM aura.asset_events WHERE asset_id = $1", assetB).Scan(&foreign); e != nil {
			return e
		}
		if foreign != 0 {
			t.Errorf("identity A sees %d event(s) on B's asset, want 0", foreign)
		}
		return nil
	}); err != nil {
		t.Fatalf("identity-scoped event read for A: %v", err)
	}

	// A may not append an event to B's asset. This is the assertion that would still pass if
	// the policy merely required the parent to EXIST — the foreign-key check bypasses row
	// security (PostgreSQL manual, 5.9: "referential integrity checks ... always bypass row
	// security"), so the parent is always findable. Only naming B's owner catches it.
	err := db.WithIdentityTxRaw(ctx, pool, idA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			"INSERT INTO aura.asset_events (asset_id, seq, from_status, to_status, reason) VALUES ($1, 2, 'uploaded', 'accepted', 'cross-tenant')",
			assetB)
		return e
	})
	if err == nil {
		t.Fatal("identity A appended an event to B's asset, want a row-level-security refusal")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("cross-tenant event INSERT error = %v, want SQLSTATE 42501", err)
	}
}

// TestAssetStoreWritesUnderFailClosedRLS is the other half of PRD amendment #107: the
// migration lands WITH the identity plumbing, so the store that owns these tables must still
// work. Every method here would have failed 42501 against the 0090 floor before NewStore
// learned to keep the pool and open an identity-scoped transaction.
//
// It is deliberately a full lifecycle rather than a single Create: the outage 0087 caused was
// not that one statement failed, it was that a store nobody had re-run against the new floor
// turned out to have ten of them.
func TestAssetStoreWritesUnderFailClosedRLS(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	owner := uuid.Must(uuid.NewV7()).String()
	stranger := uuid.Must(uuid.NewV7()).String()
	seedAssetRLSIdentity(t, pool, owner, "rlsstore-o-"+owner[:8])
	seedAssetRLSIdentity(t, pool, stranger, "rlsstore-s-"+stranger[:8])

	store := NewStore(pool)
	if store.pool == nil {
		t.Fatal("NewStore(pool) kept no pool — every method would run outside an identity transaction")
	}

	created, err := store.Create(ctx, CreateRequest{
		IdentityID:        owner,
		SourceKind:        SourceWeb,
		SourceRef:         "https://example.test/rls.pdf",
		ThreadID:          "rls-store-thread",
		Scope:             ScopeThread,
		Modality:          ModalityDocument,
		FileName:          "rls.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 9,
		ObjectBucket:      "rls-bucket",
		ObjectKey:         "rls/store/" + owner + ".pdf",
		Metadata:          map[string]any{"origin": "rls"},
	})
	if err != nil {
		t.Fatalf("Create under fail-closed RLS: %v", err)
	}

	if _, err := store.MarkUploaded(ctx, created.ID, owner, 9, "etag-1"); err != nil {
		t.Fatalf("MarkUploaded: %v", err)
	}
	if _, err := store.MarkAccepted(ctx, created.ID, owner, 9, "hash-1", "application/pdf"); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	if _, err := store.SetStatus(ctx, created.ID, owner, StatusProcessing, "", ""); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if _, err := store.SetResult(ctx, created.ID, owner, Result{
		Status:   StatusComplete,
		Summary:  "rls summary",
		Metadata: map[string]any{"pages": 1},
	}); err != nil {
		t.Fatalf("SetResult: %v", err)
	}
	if _, err := store.Promote(ctx, created.ID, owner); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	got, err := store.GetForIdentity(ctx, created.ID, owner)
	if err != nil {
		t.Fatalf("GetForIdentity: %v", err)
	}
	if got.Summary != "rls summary" {
		t.Errorf("GetForIdentity summary = %q, want the value SetResult wrote", got.Summary)
	}

	library, err := store.ListForLibrary(ctx, owner, 10)
	if err != nil {
		t.Fatalf("ListForLibrary: %v", err)
	}
	if len(library) != 1 {
		t.Errorf("ListForLibrary returned %d asset(s), want the 1 just promoted", len(library))
	}

	// ListForThread filters on thread_id and deleted_at only — Promote changes the scope, not
	// the thread — so the asset is still listed here. Asserted rather than assumed because a
	// scoped transaction that silently returned nothing would otherwise look like a pass.
	thread, err := store.ListForThread(ctx, owner, "rls-store-thread")
	if err != nil {
		t.Fatalf("ListForThread: %v", err)
	}
	if len(thread) != 1 {
		t.Errorf("ListForThread returned %d asset(s), want the owner's 1", len(thread))
	}

	// A stranger reaches nothing, and does so as "no rows" rather than an error — the policy
	// is the backstop under the store's WHERE clause, not a replacement for it, so callers
	// keep the not-found handling they already have.
	if _, err := store.GetForIdentity(ctx, created.ID, stranger); err == nil {
		t.Error("a stranger read the owner's asset through the store, want no rows")
	}
	strangerLibrary, err := store.ListForLibrary(ctx, stranger, 10)
	if err != nil {
		t.Fatalf("ListForLibrary as stranger: %v", err)
	}
	if len(strangerLibrary) != 0 {
		t.Errorf("stranger's library returned %d asset(s), want 0", len(strangerLibrary))
	}

	if _, err := store.Delete(ctx, created.ID, owner); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// TestAssetStoreWithoutPoolIsRefusedByTheFloor is the negative half, and the reason the two
// halves of this change had to land together.
//
// A Store built the pre-0090 way — queries over the pool, no pool retained, so no
// identity-scoped transaction — is exactly what shipped before this change. Against the
// fail-closed floor it cannot write at all. That is the 0087 outage reproduced deliberately
// and in miniature: ten tables went behind a restrictive floor while their stores still
// opened bare connections, and every idempotency Begin failed 42501 on the live deployment.
//
// Without this case the suite would pass just as well if NewStore silently stopped keeping
// the pool, because every other test here goes through the constructor that keeps it.
func TestAssetStoreWithoutPoolIsRefusedByTheFloor(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	owner := uuid.Must(uuid.NewV7()).String()
	seedAssetRLSIdentity(t, pool, owner, "rlsnopool-"+owner[:8])

	unscoped := &Store{q: sqlc.New(pool)}
	_, err := unscoped.Create(ctx, CreateRequest{
		IdentityID:        owner,
		SourceKind:        SourceWeb,
		ThreadID:          "rls-nopool-thread",
		Scope:             ScopeThread,
		Modality:          ModalityDocument,
		FileName:          "nopool.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 1,
		ObjectBucket:      "rls-bucket",
		ObjectKey:         "rls/nopool/" + owner + ".pdf",
	})
	if err == nil {
		t.Fatal("a pool-less Store wrote through the fail-closed floor — the floor is not biting")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("pool-less Create error = %v, want SQLSTATE 42501 (insufficient_privilege)", err)
	}
}
