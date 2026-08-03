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

	"github.com/chetto1983/aura/internal/documents/filecard"
)

// DefaultMaxIngestBytes is the fallback per-file ingestion size ceiling.
const DefaultMaxIngestBytes int64 = 50 << 20

// ErrFileTooLarge is returned when a file exceeds the configured ingest ceiling.
var ErrFileTooLarge = errors.New("document file too large")

// IngestCatalog owns the logical catalog lifecycle for an ingested document.
//
// Every method names an identity because every one of them writes an owner-scoped row:
// aura.documents fails closed without a principal (migration 0087), and the ingest request
// has been carrying one all along.
type IngestCatalog interface {
	CreateDocument(context.Context, CreateDocumentRequest) (Document, error)
	SetCard(ctx context.Context, identityID, documentID, card string) error
	SetSearchDocumentStatus(
		ctx context.Context, identityID, searchDocumentID string, status DocumentStatus, reason string,
	) error
}

// Clock returns the current time; tests inject it for deterministic timestamps.
type Clock func() time.Time

// Service registers an ingested file in the document catalog.
//
// It does not extract, chunk, or embed. Those existed to answer "what does this
// document say" from passages, and that question is now answered by handing the
// agent the original file (document_open) instead of a ranked fragment of it.
//
// It does read the file, once, to write a CARD: the structural description
// document_search ranks on (internal/documents/filecard). Before that card,
// nothing described a document until an agent had opened it, so an uploaded file
// nobody had opened could not be found — the library only knew what had already
// been found some other way.
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
	if err := s.recordCatalogDocument(ctx, req, documentID, job.ID, path); err != nil {
		return s.failJob(ctx, job, err)
	}
	job, err = s.Jobs.UpdateProgress(ctx, job.ID, JobSearchable, 0, 0)
	if err != nil {
		// The catalog row already advertises the document. Leaving it saying "ready"
		// while its job never reached searchable is the exact silence that once let a
		// document with nothing behind it look complete to the cockpit and the agent.
		s.markCatalogFailed(ctx, req.IdentityID, documentID, err)
		return &job, err
	}
	return &job, nil
}

// recordCatalogDocument writes the row document_search ranks and document_open
// resolves, then the card that makes it findable. Both the CLI and the runtime
// ingestor go through here: an asset upload gets its row from a version
// recorder, but a local path has none, and without it a file the agent indexed
// is invisible to the tool that promised it was searchable.
func (s *Service) recordCatalogDocument(ctx context.Context, req IngestRequest, documentID, jobID, path string) error {
	if s.Catalog == nil || strings.TrimSpace(req.IdentityID) == "" {
		return nil
	}
	doc, err := s.Catalog.CreateDocument(ctx, CreateDocumentRequest{
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
	s.writeCard(ctx, req, doc.ID, path)
	return nil
}

// writeCard describes the file and stores the description. It never fails the
// ingest: a card is how a document is FOUND, not whether it was stored, and
// refusing an upload because a zip was truncated would trade a real loss for a
// small one. Nothing is silent either — a file that could not be read says so in
// its own card, and the reason is logged here.
func (s *Service) writeCard(ctx context.Context, req IngestRequest, catalogID, path string) {
	card, err := filecard.Build(filecard.Request{
		Path:      path,
		FileName:  req.FileName,
		SizeBytes: req.SizeBytes,
	})
	if err != nil {
		slog.Warn("documents: file could not be read for its card",
			"document_id", catalogID, "file_name", req.FileName, "err", err)
	}
	if err := s.Catalog.SetCard(ctx, req.IdentityID, catalogID, card.Render()); err != nil {
		slog.Warn("documents: could not store the document card",
			"document_id", catalogID, "file_name", req.FileName, "err", err)
	}
}

// markCatalogFailed guards on the same condition recordCatalogDocument does, and must:
// without an identity no catalog row was ever written, so there is nothing to correct and
// the attempt would only fail closed on aura.documents and log a WARN about a row that
// does not exist.
func (s *Service) markCatalogFailed(ctx context.Context, identityID, documentID string, cause error) {
	if s.Catalog == nil || strings.TrimSpace(identityID) == "" {
		return
	}
	reason := fmt.Sprintf("document ingest failed: %v", cause)
	if err := s.Catalog.SetSearchDocumentStatus(
		context.WithoutCancel(ctx), identityID, documentID, DocumentStatusFailed, reason,
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
