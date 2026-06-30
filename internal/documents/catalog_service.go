package documents

import (
	"context"
	"fmt"
	"strings"
)

const (
	defaultCatalogListLimit = 50
	maxCatalogListLimit     = 100
)

// CatalogStore persists logical documents and related catalog metadata.
type CatalogStore interface {
	CreateDocument(context.Context, CreateDocumentRequest) (Document, error)
	UpdateDocument(context.Context, UpdateDocumentRequest) (Document, error)
	ListDocuments(context.Context, ListDocumentsRequest) ([]DocumentSummary, error)
	GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error)
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
