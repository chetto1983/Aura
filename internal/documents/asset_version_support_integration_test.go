//go:build db_integration

package documents

// Test support for the SECOND writer of a catalog row.
//
// An upload reaches aura.documents twice: documents.Service registers the file and writes
// its card, then the version recorder records the asset. RecordAssetVersion has to reuse
// the row ingest already wrote rather than insert its own, and proving that needs the real
// statement over a real asset — the reservation binds an asset, a raw storage object and a
// version in one statement, so a hand-rolled INSERT would prove nothing about it.
//
// It used to publish the document to 'ready' as well, because SearchDigests ranked only
// published rows. That ranking was deleted on 2026-08-15 and nothing left reads the state,
// so the publish went with it.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// recordAssetVersionFor attaches bytes to the document carrying searchDocumentID. The
// document must already exist — RecordAssetVersion reuses the catalogued row rather than
// creating a second one, which is what the ingest paths rely on.
func recordAssetVersionFor(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID, searchDocumentID, fileName string,
) DocumentVersionRecord {
	t.Helper()

	assetID := uuid.NewString()
	objectKey := "asset-version-support/" + assetID
	// Deterministic per asset, and real hex: the reservation keys replay detection off the
	// raw sha256, so two documents recorded in one test must not collide on it. A uuid is
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
		t.Fatalf("recordAssetVersionFor: seed asset: %v", err)
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
		t.Fatalf("recordAssetVersionFor: record asset version: %v", err)
	}
	return record
}
