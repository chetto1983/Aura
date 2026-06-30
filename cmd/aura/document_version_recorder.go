package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimeDocumentVersionRecorder struct {
	catalog *documents.CatalogService
	objects objectstore.Store
}

func newRuntimeDocumentVersionRecorder(pool *pgxpool.Pool, objects objectstore.Store) *runtimeDocumentVersionRecorder {
	return &runtimeDocumentVersionRecorder{
		catalog: &documents.CatalogService{Store: documents.NewPostgresCatalogStore(pool)},
		objects: objects,
	}
}

func (r *runtimeDocumentVersionRecorder) RecordDocumentAsset(ctx context.Context, asset assets.Asset, job documents.Job) error {
	if r == nil || r.catalog == nil {
		return fmt.Errorf("document version recorder is not configured")
	}
	if r.objects == nil {
		return fmt.Errorf("document version recorder has no object store")
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	rc, _, err := r.objects.Get(ctx, ref)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	hashes, err := documents.ContentHashesReader(rc)
	if err != nil {
		return err
	}
	if asset.ContentHash != "" && hashes.SHA256 != asset.ContentHash {
		return fmt.Errorf("asset content hash mismatch: asset sha256 %s, object sha256 %s", asset.ContentHash, hashes.SHA256)
	}
	_, err = r.catalog.RecordAssetVersion(ctx, documents.RecordAssetVersionRequest{
		IdentityID:       asset.IdentityID,
		AssetID:          asset.ID,
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
	})
	return err
}
