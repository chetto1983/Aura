// The asset-version leg of the catalog store: turning an uploaded asset into (or onto)
// a catalog row. Split out of catalog_store.go on 2026-08-03 when the card work pushed
// that file past the 600-LOC cap (CLAUDE.md, no god class).
//
// It is its own concern for a reason beyond size. Everything in catalog_store.go acts on
// a document the caller already names; this leg has to DECIDE whether a document exists
// yet — and getting that wrong is what produced two catalog rows per upload, with
// document_open resolving the newer one, which was the row without a card.

package documents

import (
	"context"
	"errors"
	"maps"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// RecordAssetVersion attaches an uploaded asset to a catalog document: the raw storage
// object, the first version, and the active-version link. It REUSES an existing document
// when one already carries this search_document_id rather than creating a second — the
// ingest path writes the catalog row first, and creating another here produced two rows
// per upload with document_open resolving the newer, card-less one.
func (s *PostgresCatalogStore) RecordAssetVersion(ctx context.Context, req RecordAssetVersionRequest) (DocumentVersionRecord, error) {
	doc, err := s.documentForAssetVersion(ctx, req)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	pgDocumentID, err := pgUUID("document_id", doc.ID)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	pgAssetID, err := pgOptionalUUID("asset_id", req.AssetID)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	storageRow, err := s.q.CreateStorageObject(ctx, sqlc.CreateStorageObjectParams{
		IdentityID:     pgIdentityID,
		DocumentID:     pgDocumentID,
		AssetID:        pgAssetID,
		Bucket:         req.ObjectBucket,
		ObjectKey:      req.ObjectKey,
		Kind:           req.StorageKind,
		Sha1:           req.SHA1,
		Sha256:         req.SHA256,
		Etag:           req.ObjectETag,
		SizeBytes:      req.SizeBytes,
		ContentType:    req.MIMEType,
		RetentionClass: req.RetentionClass,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	versionRow, err := s.q.CreateDocumentVersion(ctx, sqlc.CreateDocumentVersionParams{
		DocumentID:         pgDocumentID,
		AssetID:            pgAssetID,
		VersionNumber:      int32(req.VersionNumber), //nolint:gosec // normalized positive and starts at 1.
		Status:             req.VersionStatus,
		Sha1:               req.SHA1,
		Sha256:             req.SHA256,
		ContentType:        req.MIMEType,
		SizeBytes:          req.SizeBytes,
		StorageObjectID:    storageRow.ID,
		ChunkingConfigHash: req.ChunkingConfigHash,
		PipelineConfigHash: req.PipelineConfigHash,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	if _, err := s.q.UpdateStorageObjectVersion(ctx, sqlc.UpdateStorageObjectVersionParams{
		ID:        storageRow.ID,
		VersionID: versionRow.ID,
	}); err != nil {
		return DocumentVersionRecord{}, err
	}
	version := catalogVersionFromSQL(versionRow)
	doc, err = s.UpdateDocument(ctx, UpdateDocumentRequest{
		IdentityID:      req.IdentityID,
		DocumentID:      doc.ID,
		Scope:           doc.Scope,
		Title:           doc.Title,
		Tags:            doc.Tags,
		Metadata:        doc.Metadata,
		ActiveVersionID: version.ID,
		Status:          req.DocumentStatus,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	return DocumentVersionRecord{Document: doc, Version: version}, nil
}

// documentForAssetVersion returns the catalog row this asset's document already
// has, or creates one.
//
// Reusing it is not an optimisation. An upload passes through TWO writers —
// documents.Service registers the file and writes its card, then the version
// recorder records the asset — and if the second one INSERTS, every uploaded file
// ends up as two catalog rows with the same search_document_id: the library shows
// the file twice, and document_open resolves the newest, which is the one without
// the card. One file, one row, whichever writer arrives first.
func (s *PostgresCatalogStore) documentForAssetVersion(
	ctx context.Context, req RecordAssetVersionRequest,
) (Document, error) {
	existing, found, err := s.documentBySearchID(ctx, req.IdentityID, req.SearchDocumentID)
	if err != nil {
		return Document{}, err
	}
	if !found {
		return s.CreateDocument(ctx, CreateDocumentRequest{
			IdentityID: req.IdentityID,
			Scope:      req.Scope,
			Title:      req.Title,
			Metadata:   req.Metadata,
			Status:     req.DocumentStatus,
		})
	}
	// Tags and title are left as they are found: a person may already have edited
	// them in the cockpit, and this call knows only what the uploader said.
	return s.UpdateDocument(ctx, UpdateDocumentRequest{
		IdentityID:      req.IdentityID,
		DocumentID:      existing.ID,
		Scope:           existing.Scope,
		Title:           existing.Title,
		Tags:            existing.Tags,
		Metadata:        mergeMetadata(existing.Metadata, req.Metadata),
		ActiveVersionID: existing.ActiveVersionID,
		Status:          req.DocumentStatus,
	})
}

// documentBySearchID finds one identity's live catalog row for a search id. A
// missing row is not an error: it is the first writer's normal case.
func (s *PostgresCatalogStore) documentBySearchID(
	ctx context.Context, identityID, searchDocumentID string,
) (Document, bool, error) {
	if strings.TrimSpace(searchDocumentID) == "" || strings.TrimSpace(identityID) == "" {
		return Document{}, false, nil
	}
	id, err := s.catalogIDForSearchDocument(ctx, searchDocumentID)
	if err != nil {
		return Document{}, false, nil //nolint:nilerr // not-catalogued is the create path, not a failure.
	}
	owner, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Document{}, false, err
	}
	row, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{ID: id, IdentityID: owner})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, false, nil
		}
		return Document{}, false, err
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		return Document{}, false, err
	}
	return doc, true, nil
}

func mergeMetadata(existing, incoming map[string]any) map[string]any {
	merged := make(map[string]any, len(existing)+len(incoming))
	maps.Copy(merged, existing)
	maps.Copy(merged, incoming)
	return merged
}
