package documents

import (
	"context"
	"fmt"
	"strings"
)

const (
	defaultStorageKind    = "raw"
	defaultRetentionClass = "standard"
	defaultConfigHash     = "default"
)

// CatalogStore records an uploaded asset as a logical document version.
//
// It is one method wide because that is the whole of what production asks a catalog
// store for. The operator-facing create/read/update/delete surface that used to sit
// here had no caller: no CLI verb, no HTTP route and no scheduler path constructed a
// CatalogService to reach it, and an interface method nobody calls is a promise the
// store keeps paying for.
type CatalogStore interface {
	RecordAssetVersion(context.Context, RecordAssetVersionRequest) (DocumentVersionRecord, error)
}

// CatalogService normalizes an asset-version record before the store writes it.
type CatalogService struct {
	Store CatalogStore
}

// RecordAssetVersion records a stored asset as a non-visible candidate version.
func (s *CatalogService) RecordAssetVersion(ctx context.Context, req RecordAssetVersionRequest) (DocumentVersionRecord, error) {
	if s.Store == nil {
		return DocumentVersionRecord{}, fmt.Errorf("document catalog service has no store")
	}
	var err error
	req, err = normalizeRecordAssetVersionRequest(req)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	return s.Store.RecordAssetVersion(ctx, req)
}

func normalizeRecordAssetVersionRequest(req RecordAssetVersionRequest) (RecordAssetVersionRequest, error) {
	if strings.TrimSpace(req.IdentityID) == "" {
		return RecordAssetVersionRequest{}, fmt.Errorf("identity_id is required")
	}
	if strings.TrimSpace(req.AssetID) == "" {
		return RecordAssetVersionRequest{}, fmt.Errorf("asset_id is required")
	}
	req.Title = strings.TrimSpace(req.Title)
	req.FileName = strings.TrimSpace(req.FileName)
	if req.Title == "" {
		req.Title = req.FileName
	}
	if req.Title == "" {
		return RecordAssetVersionRequest{}, fmt.Errorf("document title is required")
	}
	if req.Scope == "" {
		req.Scope = DocumentScopeLibrary
	}
	if req.DocumentStatus == "" {
		req.DocumentStatus = DocumentStatusStored
	}
	if req.PipelineGeneration <= 0 {
		req.PipelineGeneration = 1
	}
	if strings.TrimSpace(req.SearchDocumentID) == "" {
		req.SearchDocumentID = "asset:" + req.AssetID
	}
	if strings.TrimSpace(req.SourceKind) == "" {
		req.SourceKind = "asset"
	}
	if strings.TrimSpace(req.SourceKey) == "" {
		req.SourceKey = req.AssetID
	}
	if req.VersionStatus == "" {
		req.VersionStatus = DocumentVersionStatusStored
	}
	if strings.TrimSpace(req.MIMEType) == "" {
		req.MIMEType = "application/octet-stream"
	}
	if req.SizeBytes < 0 {
		return RecordAssetVersionRequest{}, fmt.Errorf("size_bytes must be non-negative")
	}
	if strings.TrimSpace(req.ObjectBucket) == "" || strings.TrimSpace(req.ObjectKey) == "" {
		return RecordAssetVersionRequest{}, fmt.Errorf("object bucket and key are required")
	}
	if strings.TrimSpace(req.SHA256) == "" {
		return RecordAssetVersionRequest{}, fmt.Errorf("sha256 is required")
	}
	if req.VersionNumber <= 0 {
		req.VersionNumber = 1
	}
	if req.StorageKind == "" {
		req.StorageKind = defaultStorageKind
	}
	if req.RetentionClass == "" {
		req.RetentionClass = defaultRetentionClass
	}
	if req.ChunkingConfigHash == "" {
		req.ChunkingConfigHash = defaultConfigHash
	}
	if req.PipelineConfigHash == "" {
		req.PipelineConfigHash = defaultConfigHash
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	if req.SearchDocumentID != "" {
		req.Metadata["search_document_id"] = req.SearchDocumentID
	}
	if req.JobID != "" {
		req.Metadata["document_job_id"] = req.JobID
	}
	if req.SparseChunks > 0 {
		req.Metadata["sparse_chunks"] = req.SparseChunks
	}
	req.Metadata["source_asset_id"] = req.AssetID
	return req, nil
}
