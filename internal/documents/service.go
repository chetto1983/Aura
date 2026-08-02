package documents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultMaxIngestBytes is the fallback per-file ingestion size ceiling.
const DefaultMaxIngestBytes int64 = 50 << 20

// ErrFileTooLarge is returned when a file exceeds the configured ingest ceiling.
var ErrFileTooLarge = errors.New("document file too large")

// IngestCatalog owns the logical catalog lifecycle for an ingested document.
type IngestCatalog interface {
	CreateDocument(context.Context, CreateDocumentRequest) (Document, error)
	SetSearchDocumentStatus(ctx context.Context, searchDocumentID string, status DocumentStatus, reason string) error
}

// Clock returns the current time; tests inject it for deterministic timestamps.
type Clock func() time.Time

// Service registers an ingested file in the document catalog.
//
// It no longer reads the file. Extraction, chunking, sparse indexing and chunk
// embedding all existed to answer "what does this document say" from passages,
// and that question is now answered by handing the agent the original file
// (document_open) instead of a ranked fragment of it. What ingestion still owes
// the rest of the system is the catalog row document_search ranks and
// document_open resolves — title, tags, digest — so that is all it writes.
type Service struct {
	Jobs     JobStore
	Catalog  IngestCatalog
	MaxBytes int64
}

// IngestPath registers a local document file and returns its searchable job.
func (s *Service) IngestPath(ctx context.Context, req IngestRequest, path string) (*Job, error) {
	if s.Jobs == nil {
		return nil, fmt.Errorf("document service has no job store")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("document path %q is a directory", path)
	}
	maxBytes := s.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxIngestBytes
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds %d", ErrFileTooLarge, info.Size(), maxBytes)
	}
	req = normalizeIngestRequest(req, path, info.Size())
	if !isSupportedDocument(req.FileName) {
		return nil, fmt.Errorf("unsupported document type %q", filepath.Ext(req.FileName))
	}

	contentHash, err := ContentHashPath(path)
	if err != nil {
		return nil, err
	}
	documentID := DocumentID(contentHash, req.SourceID)
	job, err := s.Jobs.Create(ctx, CreateJobParams{
		SourceID:     req.SourceID,
		SourceKind:   req.SourceKind,
		DocumentID:   documentID,
		ContentHash:  contentHash,
		OriginalPath: req.OriginalPath,
		FileName:     req.FileName,
		MIMEType:     req.MIMEType,
		SizeBytes:    req.SizeBytes,
		Status:       JobAccepted,
	})
	if err != nil {
		return nil, err
	}
	if err := s.recordCatalogDocument(ctx, req, documentID, job.ID); err != nil {
		return s.failJob(ctx, job, err)
	}
	job, err = s.Jobs.UpdateProgress(ctx, job.ID, JobSearchable, 0, 0)
	if err != nil {
		// The catalog row already advertises the document. Leaving it saying "ready"
		// while its job never reached searchable is the exact silence that once let a
		// document with nothing behind it look complete to the cockpit and the agent.
		s.markCatalogFailed(ctx, documentID, err)
		return &job, err
	}
	return &job, nil
}

// recordCatalogDocument writes the row document_search ranks and document_open
// resolves. Both the CLI and the runtime ingestor go through here: an asset
// upload gets its row from a version recorder, but a local path has none, and
// without it a file the agent indexed is invisible to the tool that promised it
// was searchable.
func (s *Service) recordCatalogDocument(ctx context.Context, req IngestRequest, documentID, jobID string) error {
	if s.Catalog == nil || strings.TrimSpace(req.IdentityID) == "" {
		return nil
	}
	_, err := s.Catalog.CreateDocument(ctx, CreateDocumentRequest{
		IdentityID: req.IdentityID,
		Scope:      DocumentScopeLibrary,
		Title:      req.FileName,
		Status:     DocumentStatusReady,
		Metadata: map[string]any{
			"search_document_id": documentID,
			"document_job_id":    jobID,
			"source_id":          req.SourceID,
			"source_kind":        req.SourceKind,
		},
	})
	if err != nil {
		return fmt.Errorf("catalog document: %w", err)
	}
	return nil
}

func (s *Service) markCatalogFailed(ctx context.Context, documentID string, cause error) {
	if s.Catalog == nil {
		return
	}
	reason := fmt.Sprintf("document ingest failed: %v", cause)
	if err := s.Catalog.SetSearchDocumentStatus(
		context.WithoutCancel(ctx), documentID, DocumentStatusFailed, reason,
	); err != nil {
		slog.Warn("documents: could not mark catalog document failed", "document_id", documentID, "err", err)
	}
}

// GetJob returns one document ingestion job by id.
func (s *Service) GetJob(ctx context.Context, id string) (*Job, error) {
	if s.Jobs == nil {
		return nil, fmt.Errorf("document service has no job store")
	}
	job, err := s.Jobs.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// ListJobs returns recent document ingestion jobs.
func (s *Service) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	if s.Jobs == nil {
		return nil, fmt.Errorf("document service has no job store")
	}
	return s.Jobs.ListRecent(ctx, limit)
}

func (s *Service) failJob(ctx context.Context, job Job, cause error) (*Job, error) {
	updated, err := s.Jobs.UpdateStatus(ctx, job.ID, JobFailed, cause.Error())
	if err == nil {
		job = updated
	}
	return &job, cause
}

func normalizeIngestRequest(req IngestRequest, path string, size int64) IngestRequest {
	if req.SourceID == "" {
		req.SourceID = "cli"
	}
	if req.SourceKind == "" {
		req.SourceKind = "local"
	}
	if req.OriginalPath == "" {
		req.OriginalPath = path
	}
	if req.FileName == "" {
		req.FileName = filepath.Base(path)
	}
	req.SizeBytes = size
	if req.MIMEType == "" {
		req.MIMEType = mime.TypeByExtension(strings.ToLower(filepath.Ext(req.FileName)))
	}
	if req.MIMEType == "" {
		req.MIMEType = "application/octet-stream"
	}
	return req
}
