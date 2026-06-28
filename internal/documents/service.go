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

// DefaultMaxIngestBytes is the fallback per-file ingestion size ceiling.
const DefaultMaxIngestBytes int64 = 50 << 20

// ErrFileTooLarge is returned when a file exceeds the configured ingest ceiling.
var ErrFileTooLarge = errors.New("document file too large")

// SparseIndexer stores extracted document text for immediate sparse search.
type SparseIndexer interface {
	UpsertSparse(ctx context.Context, doc ExtractedDocument) (int, error)
}

// SearchBackend executes document search requests.
type SearchBackend interface {
	Search(ctx context.Context, req SearchRequest) ([]SearchHit, error)
}

// EmbedQueue accepts extracted documents for asynchronous embedding.
type EmbedQueue interface {
	Enqueue(ctx context.Context, doc ExtractedDocument) error
}

// Clock returns the current time; tests inject it for deterministic timestamps.
type Clock func() time.Time

// Service coordinates document extraction, sparse indexing, search, and embedding.
type Service struct {
	Jobs      JobStore
	Extractor Extractor
	Indexer   SparseIndexer
	Searcher  SearchBackend
	Embedder  EmbedQueue
	Clock     Clock
	MaxBytes  int64

	// Two-stage retrieval (RET-02) collaborators, all optional. When Reranker is
	// nil, Retrieve degrades to the sparse fulltext Search path with no regression.
	Knowledge       KnowledgeClient    // raw graph client for the vector seed + 1-hop expand
	QueryEmbedder   EmbeddingGenerator // embeds the query text into a 384d seed vector
	Reranker        Reranker           // reorders seed chunks by relevance (fail-soft)
	RerankThreshold float64            // non-monotonic guard: keep seed order when the top rerank score is below this
	RerankBlend     bool               // non-monotonic guard: blend seed rank + rerank rank (RRF) instead of the hard threshold gate

	// timeSource is the monotonic clock GraphRAG times its stages with (RET-04). A
	// nil value uses time.Now, whose monotonic reading makes elapsed-time subtraction
	// immune to wall-clock jumps; tests inject a deterministic source. It is distinct
	// from Clock, which stamps UTC wall-clock document times (UTC strips the monotonic
	// reading, so Clock must never be used to measure a duration).
	timeSource func() time.Time
}

// IngestPath ingests a local document file and returns once sparse search is ready.
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

// Search delegates to the configured document search backend.
func (s *Service) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	if s.Searcher == nil {
		return nil, fmt.Errorf("document service has no searcher")
	}
	return s.Searcher.Search(ctx, req)
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
