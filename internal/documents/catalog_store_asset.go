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
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// RecordAssetVersion attaches an uploaded asset to a catalog document as a candidate: the
// raw storage object and immutable version. It REUSES an existing document
// when one already carries this search_document_id rather than creating a second — the
// ingest path writes the catalog row first, and creating another here produced two rows
// per upload with document_open resolving the newer, card-less one.
//
// All of it is ONE identity-scoped transaction, opened here and handed down to the
// unexported workers. That is not only the RLS carrier: the storage object, the version,
// the storage-object back-link and the active-version link are one indivisible fact about
// an upload, and reaching them through the public CreateDocument/UpdateDocument methods
// would open a fresh transaction per statement and lose exactly the atomicity whose
// absence already cost this package a duplicate row per file.
func (s *PostgresCatalogStore) RecordAssetVersion(
	ctx context.Context, req RecordAssetVersionRequest,
) (DocumentVersionRecord, error) {
	return scopedValue(ctx, s, req.IdentityID, func(sc catalogTx) (DocumentVersionRecord, error) {
		return sc.recordAssetVersion(ctx, req)
	})
}

func (sc catalogTx) recordAssetVersion(
	ctx context.Context, req RecordAssetVersionRequest,
) (DocumentVersionRecord, error) {
	doc, err := sc.documentForAssetVersion(ctx, req)
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
	storageRow, err := sc.q.CreateStorageObject(ctx, sqlc.CreateStorageObjectParams{
		IdentityID:         pgIdentityID,
		DocumentID:         pgDocumentID,
		VersionID:          pgtype.UUID{},
		AssetID:            pgAssetID,
		Bucket:             req.ObjectBucket,
		ObjectKey:          req.ObjectKey,
		Kind:               req.StorageKind,
		Sha1:               req.SHA1,
		Sha256:             req.SHA256,
		Etag:               req.ObjectETag,
		SizeBytes:          req.SizeBytes,
		ContentType:        req.MIMEType,
		RetentionClass:     req.RetentionClass,
		Status:             "live",
		PipelineGeneration: req.PipelineGeneration,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	versionRow, err := sc.q.CreateDocumentVersion(ctx, sqlc.CreateDocumentVersionParams{
		ID:                 deterministicDocumentVersionID(doc.ID, req.SHA256),
		IdentityID:         pgIdentityID,
		DocumentID:         pgDocumentID,
		AssetID:            pgAssetID,
		Status:             req.VersionStatus,
		Sha1:               req.SHA1,
		Sha256:             req.SHA256,
		ContentType:        req.MIMEType,
		SizeBytes:          req.SizeBytes,
		StorageObjectID:    storageRow.ID,
		ChunkingConfigHash: req.ChunkingConfigHash,
		PipelineConfigHash: req.PipelineConfigHash,
		PipelineGeneration: req.PipelineGeneration,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	if _, err := sc.q.UpdateStorageObjectVersion(ctx, sqlc.UpdateStorageObjectVersionParams{
		ID: storageRow.ID, IdentityID: pgIdentityID, VersionID: versionRow.ID,
		PipelineGeneration: versionRow.PipelineGeneration,
	}); err != nil {
		return DocumentVersionRecord{}, err
	}
	if pgAssetID.Valid {
		if _, err := sc.q.LinkAssetDocumentVersion(ctx, sqlc.LinkAssetDocumentVersionParams{
			ID: pgAssetID, IdentityID: pgIdentityID, SearchDocumentID: doc.SearchDocumentID,
			CatalogDocumentID: pgDocumentID, DocumentVersionID: versionRow.ID,
			PipelineGeneration: versionRow.PipelineGeneration,
		}); err != nil {
			return DocumentVersionRecord{}, err
		}
	}
	return DocumentVersionRecord{Document: doc, Version: catalogVersionFromSQL(versionRow)}, nil
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
func (sc catalogTx) documentForAssetVersion(
	ctx context.Context, req RecordAssetVersionRequest,
) (Document, error) {
	existing, found, err := sc.documentBySearchID(ctx, req.IdentityID, req.SearchDocumentID)
	if err != nil {
		return Document{}, err
	}
	if !found {
		return sc.createDocument(ctx, CreateDocumentRequest{
			IdentityID: req.IdentityID, SourceKind: req.SourceKind, SourceKey: req.SourceKey,
			SearchDocumentID: req.SearchDocumentID, PipelineGeneration: req.PipelineGeneration,
			Scope: req.Scope, Title: req.Title, Metadata: req.Metadata, Status: req.DocumentStatus,
		})
	}
	return existing, nil
}

// documentBySearchID finds one identity's live catalog row for a search id. A
// missing row is not an error: it is the first writer's normal case.
func (sc catalogTx) documentBySearchID(
	ctx context.Context, identityID, searchDocumentID string,
) (Document, bool, error) {
	if strings.TrimSpace(searchDocumentID) == "" || strings.TrimSpace(identityID) == "" {
		return Document{}, false, nil
	}
	owner, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Document{}, false, err
	}
	row, err := sc.q.GetDocumentBySearchID(ctx, sqlc.GetDocumentBySearchIDParams{
		IdentityID: owner, SearchDocumentID: searchDocumentID,
	})
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

func deterministicDocumentVersionID(documentID, rawSHA256 string) pgtype.UUID {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(documentID+":"+strings.ToLower(rawSHA256)))
	return pgtype.UUID{Bytes: id, Valid: true}
}
