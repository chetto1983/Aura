//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
)

type DocumentIngestor interface {
	IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error)
}

type DocumentVersionRecorder interface {
	RecordDocumentAsset(ctx context.Context, asset Asset, job documents.Job) error
}

type DocumentProcessor struct {
	Objects         objectstore.Store
	Ingest          DocumentIngestor
	VersionRecorder DocumentVersionRecorder
	// PerIdentityObjects, when set, resolves the ASSET OWNER's per-identity store so the
	// object read targets the owner's bucket with the owner's creds. Nil → the shared Objects.
	PerIdentityObjects *ObjectResolverBundle
}

func (p *DocumentProcessor) ProcessAsset(ctx context.Context, asset Asset) (Result, error) {
	if p.Objects == nil || p.Ingest == nil {
		return Result{}, fmt.Errorf("document processor is not configured")
	}
	objects, err := p.PerIdentityObjects.storeForAsset(ctx, p.Objects, asset)
	if err != nil {
		return Result{}, err
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	rc, _, err := objects.Get(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = rc.Close() }()
	path, cleanup, err := writeTempAsset(rc, asset.FileName)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	job, err := p.Ingest.IngestPath(ctx, documents.IngestRequest{
		SourceID:     asset.ID,
		SourceKind:   string(asset.SourceKind),
		OriginalPath: "object://" + asset.ObjectBucket + "/" + asset.ObjectKey,
		FileName:     asset.FileName,
		MIMEType:     asset.MIMEType,
		SizeBytes:    asset.SizeBytes,
		// The owner identity scopes the catalog row: retrieval is identity-scoped in SQL,
		// so a row written without one is owned by nobody and invisible to every read
		// meant to return it. Assets already carry it.
		IdentityID: asset.IdentityID,
	}, path)
	if err != nil {
		return Result{}, err
	}
	if p.VersionRecorder != nil {
		if err := p.VersionRecorder.RecordDocumentAsset(ctx, asset, *job); err != nil {
			return Result{}, err
		}
	}
	// The summary said "indexed with N searchable chunks" and N was ALWAYS 0: ingest
	// stopped chunking when the document plane became a catalog, and the count it read
	// is now hard-wired to zero. It shipped that to the user on every single upload.
	// What actually happened is the honest sentence, and it is also the one that tells
	// the agent what to do next.
	return Result{
		Status:     StatusSearchable,
		DocumentID: job.DocumentID,
		Summary:    fmt.Sprintf("%s is in the document catalog; open it to read or compute on it.", job.FileName),
		Metadata: map[string]any{
			"document_job_id": job.ID,
		},
	}, nil
}

func writeTempAsset(src io.Reader, fileName string) (string, func(), error) {
	suffix := filepath.Ext(fileName)
	if suffix == "" {
		suffix = ".bin"
	}
	f, err := os.CreateTemp("", "aura-asset-doc-*"+suffix)
	if err != nil {
		return "", func() {}, err
	}
	path := f.Name()
	if _, err = io.Copy(f, src); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", func() {}, err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return "", func() {}, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}
