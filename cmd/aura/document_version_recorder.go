package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimeDocumentVersionRecorder struct {
	catalog *documents.CatalogService
}

func newRuntimeDocumentVersionRecorder(pool *pgxpool.Pool) *runtimeDocumentVersionRecorder {
	return &runtimeDocumentVersionRecorder{
		catalog: &documents.CatalogService{Store: documents.NewPostgresCatalogStore(pool)},
	}
}

func (r *runtimeDocumentVersionRecorder) RecordDocumentAsset(
	ctx context.Context,
	asset assets.Asset,
	job documents.Job,
	hashes documents.ContentHashes,
) (documents.DocumentVersionRecord, error) {
	if r == nil || r.catalog == nil {
		return documents.DocumentVersionRecord{}, fmt.Errorf("document version recorder is not configured")
	}
	if hashes.SHA256 == "" {
		return documents.DocumentVersionRecord{}, fmt.Errorf("document version recorder has no canonical SHA-256")
	}
	if asset.ContentHash != "" && hashes.SHA256 != asset.ContentHash {
		return documents.DocumentVersionRecord{}, fmt.Errorf("asset content hash mismatch: asset sha256 %s, object sha256 %s", asset.ContentHash, hashes.SHA256)
	}
	sourceKey, err := documents.SourceKey(asset.SourceRef, asset.ID)
	if err != nil {
		return documents.DocumentVersionRecord{}, err
	}
	searchID, err := documents.SearchDocumentID(asset.IdentityID, string(asset.SourceKind), sourceKey)
	if err != nil {
		return documents.DocumentVersionRecord{}, err
	}
	if job.DocumentID != searchID {
		return documents.DocumentVersionRecord{}, fmt.Errorf("document search id mismatch")
	}
	return r.catalog.RecordAssetVersion(ctx, documents.RecordAssetVersionRequest{
		IdentityID:       asset.IdentityID,
		AssetID:          asset.ID,
		SourceKind:       string(asset.SourceKind),
		SourceKey:        sourceKey,
		Scope:            documents.DocumentScope(asset.Scope),
		FileName:         asset.FileName,
		MIMEType:         asset.MIMEType,
		SizeBytes:        asset.SizeBytes,
		ObjectBucket:     asset.ObjectBucket,
		ObjectKey:        asset.ObjectKey,
		ObjectETag:       asset.ObjectETag,
		SearchDocumentID: job.DocumentID,
		JobID:            job.ID,
		SparseChunks:     job.SparseChunks,
		SHA1:             hashes.SHA1,
		SHA256:           hashes.SHA256,
		DocumentStatus:   documents.DocumentStatusStored,
		VersionStatus:    documents.DocumentVersionStatusStored,
	})
}
