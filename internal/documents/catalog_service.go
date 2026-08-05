package documents

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	defaultCatalogListLimit = 50
	maxCatalogListLimit     = 100
	defaultStorageKind      = "raw"
	defaultRetentionClass   = "standard"
	defaultConfigHash       = "default"
)

// CatalogStore persists logical documents and related catalog metadata.
type CatalogStore interface {
	CreateDocument(context.Context, CreateDocumentRequest) (Document, error)
	UpdateDocument(context.Context, UpdateDocumentRequest) (Document, error)
	ListDocuments(context.Context, ListDocumentsRequest) ([]DocumentSummary, error)
	GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error)
	SoftDeleteDocument(ctx context.Context, identityID, documentID string) (Document, error)
	RecordAssetVersion(context.Context, RecordAssetVersionRequest) (DocumentVersionRecord, error)
}

// CatalogService owns document metadata, tags, and list/detail access.
type CatalogService struct {
	Store CatalogStore
}

// CreateDocument creates logical document metadata with normalized searchable tags.
func (s *CatalogService) CreateDocument(ctx context.Context, req CreateDocumentRequest) (Document, error) {
	if s.Store == nil {
		return Document{}, fmt.Errorf("document catalog service has no store")
	}
	var err error
	req, err = normalizeCreateDocumentRequest(req)
	if err != nil {
		return Document{}, err
	}
	return s.Store.CreateDocument(ctx, req)
}

// UpdateDocument updates logical document metadata without creating a new content version.
func (s *CatalogService) UpdateDocument(ctx context.Context, req UpdateDocumentRequest) (Document, error) {
	if s.Store == nil {
		return Document{}, fmt.Errorf("document catalog service has no store")
	}
	var err error
	req, err = normalizeUpdateDocumentRequest(req)
	if err != nil {
		return Document{}, err
	}
	return s.Store.UpdateDocument(ctx, req)
}

// ListDocuments returns document summaries, optionally filtered by normalized tag.
func (s *CatalogService) ListDocuments(ctx context.Context, req ListDocumentsRequest) ([]DocumentSummary, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("document catalog service has no store")
	}
	var err error
	req, err = normalizeListDocumentsRequest(req)
	if err != nil {
		return nil, err
	}
	return s.Store.ListDocuments(ctx, req)
}

// GetDocument returns one document detail scoped to an identity.
func (s *CatalogService) GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error) {
	if s.Store == nil {
		return DocumentDetail{}, fmt.Errorf("document catalog service has no store")
	}
	if strings.TrimSpace(identityID) == "" {
		return DocumentDetail{}, fmt.Errorf("identity_id is required")
	}
	if strings.TrimSpace(documentID) == "" {
		return DocumentDetail{}, fmt.Errorf("document_id is required")
	}
	return s.Store.GetDocument(ctx, identityID, documentID)
}

// DeleteDocument soft-deletes one logical document scoped to an identity.
func (s *CatalogService) DeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	if s.Store == nil {
		return Document{}, fmt.Errorf("document catalog service has no store")
	}
	if strings.TrimSpace(identityID) == "" {
		return Document{}, fmt.Errorf("identity_id is required")
	}
	if strings.TrimSpace(documentID) == "" {
		return Document{}, fmt.Errorf("document_id is required")
	}
	return s.Store.SoftDeleteDocument(ctx, identityID, documentID)
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

func normalizeCreateDocumentRequest(req CreateDocumentRequest) (CreateDocumentRequest, error) {
	if strings.TrimSpace(req.IdentityID) == "" {
		return CreateDocumentRequest{}, fmt.Errorf("identity_id is required")
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return CreateDocumentRequest{}, fmt.Errorf("document title is required")
	}
	if req.Scope == "" {
		req.Scope = DocumentScopeLibrary
	}
	if req.Status == "" {
		req.Status = DocumentStatusDraft
	}
	if strings.TrimSpace(req.SearchDocumentID) == "" {
		req.SearchDocumentID = "catalog:" + uuid.NewString()
	}
	if strings.TrimSpace(req.SourceKind) == "" {
		req.SourceKind = "manual"
	}
	if strings.TrimSpace(req.SourceKey) == "" {
		req.SourceKey = req.SearchDocumentID
	}
	if req.PipelineGeneration < 0 {
		return CreateDocumentRequest{}, fmt.Errorf("pipeline_generation must be non-negative")
	}
	tags, err := NormalizeTags(req.Tags)
	if err != nil {
		return CreateDocumentRequest{}, err
	}
	req.Tags = tags
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	return req, nil
}

func normalizeUpdateDocumentRequest(req UpdateDocumentRequest) (UpdateDocumentRequest, error) {
	if strings.TrimSpace(req.IdentityID) == "" {
		return UpdateDocumentRequest{}, fmt.Errorf("identity_id is required")
	}
	if strings.TrimSpace(req.DocumentID) == "" {
		return UpdateDocumentRequest{}, fmt.Errorf("document_id is required")
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return UpdateDocumentRequest{}, fmt.Errorf("document title is required")
	}
	if req.Scope == "" {
		req.Scope = DocumentScopeLibrary
	}
	if req.Status == "" {
		req.Status = DocumentStatusDraft
	}
	tags, err := NormalizeTags(req.Tags)
	if err != nil {
		return UpdateDocumentRequest{}, err
	}
	req.Tags = tags
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	return req, nil
}

func normalizeListDocumentsRequest(req ListDocumentsRequest) (ListDocumentsRequest, error) {
	if strings.TrimSpace(req.IdentityID) == "" {
		return ListDocumentsRequest{}, fmt.Errorf("identity_id is required")
	}
	req.Query = strings.TrimSpace(req.Query)
	if strings.TrimSpace(req.Tag) != "" {
		tags, err := NormalizeTags([]string{req.Tag})
		if err != nil {
			return ListDocumentsRequest{}, err
		}
		if len(tags) > 0 {
			req.Tag = tags[0]
		}
	}
	if req.Limit <= 0 {
		req.Limit = defaultCatalogListLimit
	}
	if req.Limit > maxCatalogListLimit {
		req.Limit = maxCatalogListLimit
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	return req, nil
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
		req.DocumentStatus = DocumentStatusProcessing
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
		req.VersionStatus = "processing"
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
