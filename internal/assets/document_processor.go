//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"context"
	"crypto/sha1" // #nosec G505 -- compatibility metadata only; SHA-256 remains canonical.
	"crypto/sha256"
	"encoding/hex"
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
	RecordDocumentAsset(
		ctx context.Context,
		asset Asset,
		job documents.Job,
		hashes documents.ContentHashes,
	) (documents.DocumentVersionRecord, error)
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
	if p.Objects == nil || p.Ingest == nil || p.VersionRecorder == nil {
		return Result{}, fmt.Errorf("document processor is not configured")
	}
	claim, ok := documents.ClaimedIngestionJob(ctx)
	if !ok || claim.ID == "" || claim.LockedBy == "" || claim.LeaseGeneration <= 0 {
		return Result{}, fmt.Errorf("document processor requires a fenced ingestion claim")
	}
	if claim.IdentityID != asset.IdentityID || (claim.AssetID != "" && claim.AssetID != asset.ID) {
		return Result{}, fmt.Errorf("document processor ingestion claim does not own the asset")
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
	path, hashes, cleanup, err := writeTempAsset(rc, asset.FileName)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	job, err := p.Ingest.IngestPath(ctx, documents.IngestRequest{
		SourceID:     firstNonBlank(asset.SourceRef, asset.ID),
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
	record, err := p.VersionRecorder.RecordDocumentAsset(ctx, asset, *job, hashes)
	if err != nil {
		return Result{}, err
	}
	if record.ReplayedActive {
		return replayedAssetResult(asset, record), nil
	}
	// The bytes are in the bucket and the version is on record; indexing is the ingest
	// sidecar's job now, reconciled from the bucket rather than driven from here.
	//
	// StatusProcessing, NOT StatusSearchable, and the difference is not cosmetic: nothing
	// here has produced a passage, so claiming searchable would be the skip-as-green lie --
	// context.go:63 hands the agent only StatusSearchable assets, and it would be handing
	// over documents retrieval cannot find. WHAT FLIPS IT IS AN OPEN GAP: the in-process
	// pipeline used to, and "searchable" is now a property of ArcadeDB (does this
	// source_key have passages?) rather than of a Postgres column. Tracked in
	// docs/superpowers/specs/2026-08-08-document-plane-two-store-design.md.
	return Result{
		Status:     StatusProcessing,
		DocumentID: record.Document.SearchDocumentID,
		Summary:    fmt.Sprintf("%s is stored; the ingest sidecar indexes it from the bucket.", job.FileName),
		Metadata: map[string]any{
			"document_job_id":     job.ID,
			"document_version_id": record.Version.ID,
			"object_bucket":       asset.ObjectBucket,
			"object_key":          asset.ObjectKey,
		},
	}, nil
}

// replayedAssetResult settles an upload whose bytes are ALREADY the document's published
// version: the passages, embeddings and projections behind them exist, so re-running the
// pipeline would spend a full conversion — up to fifteen minutes — to arrive back here.
//
// Only ReplayedActive earns this. Every other outcome, a replay onto a version still
// processing included, falls through to Pipeline.Run, whose beginStage resumes from the
// stage ledger exactly as it does on a first upload.
//
// It deliberately omits pipeline_activation_job_id. That key is the ingestion handler's
// signal that the activation statement already settled the job (asset_processing_handler.go);
// nothing settled it here, so the key's absence is what lets the queue worker close the job
// itself instead of leaving it leased until it dead-letters.
func replayedAssetResult(asset Asset, record documents.DocumentVersionRecord) Result {
	return Result{
		Status:     StatusSearchable,
		DocumentID: record.Document.SearchDocumentID,
		Summary: fmt.Sprintf(
			"%s was already indexed; reused the active version without reprocessing.",
			asset.FileName,
		),
		Metadata: map[string]any{
			"document_version_id": record.Version.ID,
			"pipeline_generation": record.Version.PipelineGeneration,
			"replayed_active":     true,
		},
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func writeTempAsset(src io.Reader, fileName string) (string, documents.ContentHashes, func(), error) {
	suffix := filepath.Ext(fileName)
	if suffix == "" {
		suffix = ".bin"
	}
	f, err := os.CreateTemp("", "aura-asset-doc-*"+suffix)
	if err != nil {
		return "", documents.ContentHashes{}, func() {}, err
	}
	path := f.Name()
	sha1Hash := sha1.New() //nolint:gosec // SHA-1 is compatibility metadata; SHA-256 remains canonical.
	sha256Hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(f, sha1Hash, sha256Hash), src); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", documents.ContentHashes{}, func() {}, err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return "", documents.ContentHashes{}, func() {}, err
	}
	hashes := documents.ContentHashes{
		SHA1:   hex.EncodeToString(sha1Hash.Sum(nil)),
		SHA256: hex.EncodeToString(sha256Hash.Sum(nil)),
	}
	return path, hashes, func() { _ = os.Remove(path) }, nil
}
