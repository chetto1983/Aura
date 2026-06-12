package documents

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultMaxIngestBytes int64 = 50 << 20

var ErrFileTooLarge = errors.New("document file too large")

type SparseIndexer interface {
	UpsertSparse(ctx context.Context, doc ExtractedDocument) (int, error)
}

type SearchBackend interface {
	Search(ctx context.Context, req SearchRequest) ([]SearchHit, error)
}

type EmbedQueue interface {
	Enqueue(ctx context.Context, doc ExtractedDocument) error
}

type Clock func() time.Time

type Service struct {
	Jobs      JobStore
	Extractor Extractor
	Indexer   SparseIndexer
	Searcher  SearchBackend
	Embedder  EmbedQueue
	Clock     Clock
	MaxBytes  int64
}

func (s *Service) IngestPath(ctx context.Context, req IngestRequest, path string) (*Job, error) {
	if s.Jobs == nil {
		return nil, fmt.Errorf("document service has no job store")
	}
	if s.Extractor == nil {
		return nil, fmt.Errorf("document service has no extractor")
	}
	if s.Indexer == nil {
		return nil, fmt.Errorf("document service has no indexer")
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
	job, err = s.Jobs.UpdateStatus(ctx, job.ID, JobExtracting, "")
	if err != nil {
		return &job, err
	}
	resp, err := s.Extractor.ExtractFile(ctx, path, req)
	if err != nil {
		return s.failJob(ctx, job, err)
	}
	doc, err := BuildExtractedDocument(req, contentHash, resp, s.now())
	if err != nil {
		return s.failJob(ctx, job, err)
	}
	indexed, err := s.Indexer.UpsertSparse(ctx, doc)
	if err != nil {
		return s.failJob(ctx, job, err)
	}
	job, err = s.Jobs.UpdateProgress(ctx, job.ID, JobSearchable, indexed, 0)
	if err != nil {
		return &job, err
	}
	if s.Embedder != nil {
		_ = s.Embedder.Enqueue(context.WithoutCancel(ctx), doc)
	}
	return &job, nil
}

func (s *Service) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	if s.Searcher == nil {
		return nil, fmt.Errorf("document service has no searcher")
	}
	return s.Searcher.Search(ctx, req)
}

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

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now().UTC()
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

func isSupportedDocument(fileName string) bool {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".pdf", ".xlsx", ".xlsm", ".docx":
		return true
	default:
		return false
	}
}
