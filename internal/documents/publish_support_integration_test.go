//go:build db_integration

package documents

// Test support for the one state SearchDigests will actually rank.
//
// SearchDigests does not return every catalogued row. It returns documents that are
// PUBLISHED: status 'ready' at a positive pipeline generation, whose active version is
// itself ready at that same generation, whose bytes are a live raw storage object, and
// whose asset is not being deleted. A bare `INSERT INTO aura.documents ... 'ready'`
// satisfies none of that, and migration 0093 is where the gap opened — before it, ingest
// published a document the moment it registered one, so a one-row fixture was a faithful
// stand-in. It is not one any more.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// publishForSearch attaches bytes to the document carrying searchDocumentID and leaves it
// in the state SearchDigests ranks. The document must already exist — RecordAssetVersion
// reuses the catalogued row rather than creating a second one, which is what the ingest
// paths rely on.
//
// The asset, the raw object and the candidate version come from RecordAssetVersion, the
// production statement. The final publish is written here instead of calling
// PostgresPipelineStore.ActivateCandidate, and that is a deliberate line rather than a
// shortcut: activation refuses a candidate with no passages outright
// (`expected_passage_count > 0` in ActivatePipelineCandidate), so reaching it would mean
// fabricating a chunk, an embedding vector and a projection commit inside tests whose
// subject is text ranking. The activation contract has an owner already —
// TestPipelineStoreActivatesOnlyAFencedCandidate drives the real statement end to end —
// and duplicating it here would buy nothing.
//
// The columns below are therefore the ones ActivatePipelineCandidate's ready_document /
// ready_version / searchable_asset CTEs write. If that statement's idea of "published"
// changes, this must change with it.
func publishForSearch(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID, searchDocumentID, fileName string,
) DocumentVersionRecord {
	t.Helper()

	assetID := uuid.NewString()
	objectKey := "publish-for-search/" + assetID
	// Deterministic per asset, and real hex: the reservation keys replay detection off the
	// raw sha256, so two documents published in one test must not collide on it. A uuid is
	// 32 hex digits once its dashes are gone; the zero prefix pads it to the 64 a sha256
	// digest has. (fmt's %064s would pad with SPACES, which is not a digest.)
	sha256 := strings.Repeat("0", 32) + strings.ReplaceAll(assetID, "-", "")

	if err := asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO aura.assets (
    id, identity_id, source_kind, source_ref, scope, modality, status,
    file_name, mime_type, size_bytes, content_hash, object_bucket, object_key
) VALUES ($1, $2, 'cli', $3, 'library', 'document', 'accepted',
          $4, 'application/octet-stream', 42, $5, 'documents', $3)`,
			assetID, identityID, objectKey, fileName, sha256)
		return err
	}); err != nil {
		t.Fatalf("publishForSearch: seed asset: %v", err)
	}

	record, err := NewPostgresCatalogStore(pool).RecordAssetVersion(ctx, RecordAssetVersionRequest{
		IdentityID: identityID, AssetID: assetID,
		SearchDocumentID: searchDocumentID, FileName: fileName, Title: fileName,
		MIMEType: "application/octet-stream", SizeBytes: 42,
		ObjectBucket: "documents", ObjectKey: objectKey, SHA256: sha256,
		VersionNumber: 1, VersionStatus: "queued",
		StorageKind: defaultStorageKind, RetentionClass: defaultRetentionClass,
	})
	if err != nil {
		t.Fatalf("publishForSearch: record asset version: %v", err)
	}

	if err := asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
UPDATE aura.document_versions
SET status = 'ready', ready_at = COALESCE(ready_at, now()), updated_at = now()
WHERE id = $1 AND identity_id = $2`, record.Version.ID, identityID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE aura.assets
SET status = 'searchable', pipeline_generation = $3, updated_at = now()
WHERE id = $1 AND identity_id = $2`,
			assetID, identityID, record.Version.PipelineGeneration); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
UPDATE aura.documents
SET active_version_id = $3, pipeline_generation = $4, status = 'ready',
    error_code = '', error_message = '', deleted_at = NULL, updated_at = now()
WHERE id = $1 AND identity_id = $2`,
			record.Document.ID, identityID, record.Version.ID, record.Version.PipelineGeneration)
		return err
	}); err != nil {
		t.Fatalf("publishForSearch: publish: %v", err)
	}
	return record
}
