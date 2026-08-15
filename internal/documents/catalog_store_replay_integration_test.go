//go:build db_integration

// The same-SHA replay leg of the production recorder (finding F3).
//
// These tests exist because the wiring they cover was absent once already:
// ReservePipelineCandidateVersion had zero production callers while the recorder created
// and catalogued in four separate statements, so a second upload of identical bytes wrote
// a fresh raw object and attached it to the version already on record. Nothing failed
// loudly when that happened, which is why it survived — TestCatalogStoreSameSHAReplay is
// the assertion that makes its removal a red build rather than a silent regression.

package documents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestCatalogStoreSameSHAReplay drives the production entry point twice with two distinct
// assets carrying identical bytes under different object keys, and pins the four facts a
// replay must preserve: one version, one raw object, the version still owned by the FIRST
// asset, and the second asset bound to that same version and document.
func TestCatalogStoreSameSHAReplay(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newReplayFixture(t, ctx, pool)

	first := fixture.record(t, ctx, "one")
	second := fixture.record(t, ctx, "two")

	if first.Version.ID != second.Version.ID {
		t.Fatalf("replay minted a second version: first %s, second %s",
			first.Version.ID, second.Version.ID)
	}
	if first.Document.ID != second.Document.ID {
		t.Fatalf("replay minted a second document: first %s, second %s",
			first.Document.ID, second.Document.ID)
	}
	if got := fixture.count(t, ctx,
		`SELECT count(*) FROM aura.document_versions
		 WHERE document_id = $1 AND deleted_at IS NULL`); got != 1 {
		t.Fatalf("live versions = %d, want 1", got)
	}
	if got := fixture.count(t, ctx,
		`SELECT count(*) FROM aura.storage_objects
		 WHERE document_id = $1 AND kind = 'raw' AND status = 'live' AND deleted_at IS NULL`); got != 1 {
		t.Fatalf("live raw storage objects = %d, want 1", got)
	}

	var versionAsset, secondCatalog, secondVersion string
	var secondSearchID string
	if err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT asset_id::text FROM aura.document_versions WHERE document_id = $1`,
			fixture.documentUUID(t, ctx),
		).Scan(&versionAsset); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`SELECT catalog_document_id::text, document_version_id::text, document_id
			 FROM aura.assets WHERE id = $1`, fixture.assets["two"],
		).Scan(&secondCatalog, &secondVersion, &secondSearchID)
	}); err != nil {
		t.Fatalf("read replay bindings: %v", err)
	}
	if versionAsset != fixture.assets["one"] {
		t.Fatalf("version asset_id = %s, want the FIRST asset %s", versionAsset, fixture.assets["one"])
	}
	if secondVersion != first.Version.ID || secondCatalog != first.Document.ID {
		t.Fatalf("second asset bound to version %s document %s, want %s / %s",
			secondVersion, secondCatalog, first.Version.ID, first.Document.ID)
	}
	if secondSearchID != fixture.searchID {
		t.Fatalf("second asset search document id = %q, want %q", secondSearchID, fixture.searchID)
	}

	// The duplicate's bytes must stay reachable from the document that owns them: the
	// ledger row is the only record tying a Garage object to a document_id, and an unbound
	// row is an object no future sweep can find.
	if got := fixture.count(t, ctx,
		`SELECT count(*) FROM aura.storage_objects
		 WHERE document_id = $1 AND kind <> 'raw' AND status = 'live' AND deleted_at IS NULL`); got != 1 {
		t.Fatalf("duplicate ledger rows = %d, want 1", got)
	}
}

// TestCatalogStoreReplayedActiveTracksActivation pins the distinction the short-circuit
// rests on: identity of bytes is NOT liveness. A replay onto a version that is still
// processing must report false, or a caller would publish a document with nothing indexed
// behind it; only a replay onto the document's published version reports true.
func TestCatalogStoreReplayedActiveTracksActivation(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("version not activated", func(t *testing.T) {
		fixture := newReplayFixture(t, ctx, pool)
		if first := fixture.record(t, ctx, "one"); first.ReplayedActive {
			t.Fatal("a freshly reserved version reported ReplayedActive")
		}
		if second := fixture.record(t, ctx, "two"); second.ReplayedActive {
			t.Fatal("replay onto a still-processing version reported ReplayedActive")
		}
	})

	t.Run("version activated", func(t *testing.T) {
		fixture := newReplayFixture(t, ctx, pool)
		first := fixture.record(t, ctx, "one")
		fixture.activate(t, ctx, first.Version.ID)
		second := fixture.record(t, ctx, "two")
		if !second.ReplayedActive {
			t.Fatalf("replay onto the published version = %#v, want ReplayedActive", second)
		}
		if second.Version.ID != first.Version.ID {
			t.Fatalf("activated replay version = %s, want %s", second.Version.ID, first.Version.ID)
		}
	})
}

type replayFixture struct {
	store      *PostgresCatalogStore
	pool       *pgxpool.Pool
	identityID string
	searchID   string
	prefix     string
	assets     map[string]string
}

func newReplayFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *replayFixture {
	t.Helper()
	fixture := &replayFixture{
		store: NewPostgresCatalogStore(pool), pool: pool,
		identityID: seedDocumentTestIdentity(t, ctx, pool),
		searchID:   "doc_replay_" + uuid.NewString(),
		prefix:     "replay-test/" + uuid.NewString(),
		assets:     map[string]string{},
	}
	return fixture
}

// record seeds one asset and drives PostgresCatalogStore.RecordAssetVersion for it. Every
// call uses the same identity, sha256, source kind and source key, so the second lands on
// the document the first created and differs only in asset id and object key.
func (f *replayFixture) record(t *testing.T, ctx context.Context, suffix string) DocumentVersionRecord {
	t.Helper()
	assetID := uuid.NewString()
	objectKey := f.prefix + "/" + suffix + ".pdf"
	f.assets[suffix] = assetID
	if err := asDocumentIdentity(ctx, f.pool, f.identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO aura.assets (
    id, identity_id, source_kind, source_ref, scope, modality, status,
    file_name, mime_type, size_bytes, content_hash, object_bucket, object_key
) VALUES ($1, $2, 'cli', $3, 'library', 'document', 'accepted',
          $4, 'application/pdf', 42, $5, 'documents', $6)`,
			assetID, f.identityID, objectKey, suffix+".pdf", replaySHA256, objectKey)
		return err
	}); err != nil {
		t.Fatalf("seed replay asset %s: %v", suffix, err)
	}
	record, err := f.store.RecordAssetVersion(ctx, RecordAssetVersionRequest{
		IdentityID: f.identityID, AssetID: assetID,
		SourceKind: "cli", SourceKey: f.prefix, SearchDocumentID: f.searchID,
		Scope: DocumentScopeLibrary, Title: "replay fixture",
		FileName: suffix + ".pdf", MIMEType: "application/pdf", SizeBytes: 42,
		ObjectBucket: "documents", ObjectKey: objectKey, ObjectETag: "etag-" + suffix,
		SHA256: replaySHA256,
		// "queued" is a legal value for both aura.documents_status_check and
		// aura.document_versions_status_check -- this is the vocabulary the constraints
		// admit, not a workaround. The recorder used to default to "processing" for
		// both, which neither constraint accepted; commit 38d78e403 fixed that. Both
		// are literals because Go declares no constant for a stage it never writes.
		DocumentStatus: "queued", VersionStatus: "queued",
		StorageKind: defaultStorageKind, RetentionClass: defaultRetentionClass,
	})
	if err != nil {
		t.Fatalf("RecordAssetVersion %s: %v", suffix, err)
	}
	return record
}

// activate publishes the version the way ActivatePipelineCandidate's swap leaves it, which
// is the only state replay_is_active accepts.
func (f *replayFixture) activate(t *testing.T, ctx context.Context, versionID string) {
	t.Helper()
	if err := asDocumentIdentity(ctx, f.pool, f.identityID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE aura.document_versions
SET status = 'ready', ready_at = now(), activated_at = now()
WHERE id = $1 AND identity_id = $2`, versionID, f.identityID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
UPDATE aura.documents
SET active_version_id = $1, status = 'ready', pipeline_generation = GREATEST(pipeline_generation, 1)
WHERE search_document_id = $2 AND identity_id = $3`, versionID, f.searchID, f.identityID)
		return err
	}); err != nil {
		t.Fatalf("activate replay version: %v", err)
	}
}

func (f *replayFixture) documentUUID(t *testing.T, ctx context.Context) string {
	t.Helper()
	var id string
	if err := asDocumentIdentity(ctx, f.pool, f.identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id::text FROM aura.documents WHERE search_document_id = $1 AND identity_id = $2`,
			f.searchID, f.identityID).Scan(&id)
	}); err != nil {
		t.Fatalf("resolve replay document: %v", err)
	}
	return id
}

func (f *replayFixture) count(t *testing.T, ctx context.Context, query string) int {
	t.Helper()
	documentID := f.documentUUID(t, ctx)
	var n int
	if err := asDocumentIdentity(ctx, f.pool, f.identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, documentID).Scan(&n)
	}); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

var replaySHA256 = fmt.Sprintf("%064x", 0xF3)
