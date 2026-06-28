package documents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServiceMakesDocumentSearchableBeforeEmbedding(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	jobs := newFakeJobStore()
	extractor := &fakeExtractor{resp: oneChunkResponse()}
	indexer := &fakeSparseIndexer{count: 1}
	embedder := &fakeEmbedQueue{}
	service := &Service{
		Jobs:      jobs,
		Extractor: extractor,
		Indexer:   indexer,
		Embedder:  embedder,
		Clock:     func() time.Time { return time.Unix(10, 0) },
	}

	job, err := service.IngestPath(t.Context(), IngestRequest{SourceID: "cli"}, path)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobSearchable {
		t.Fatalf("status = %s", job.Status)
	}
	if job.SparseChunks != 1 {
		t.Fatalf("sparse chunks = %d", job.SparseChunks)
	}
	if len(embedder.docs) != 1 {
		t.Fatalf("embedding queue docs = %d", len(embedder.docs))
	}
	if indexer.docs[0].ID != job.DocumentID {
		t.Fatalf("indexed doc id = %q job doc id = %q", indexer.docs[0].ID, job.DocumentID)
	}
}

func TestServiceRefusesFilesOverConfiguredLimit(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	service := &Service{
		Jobs:      newFakeJobStore(),
		Extractor: &fakeExtractor{resp: oneChunkResponse()},
		Indexer:   &fakeSparseIndexer{count: 1},
		MaxBytes:  1,
	}
	_, err := service.IngestPath(t.Context(), IngestRequest{}, path)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("want ErrFileTooLarge, got %v", err)
	}
}

func TestServiceReusesExistingJobForSameSourceDocumentHash(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	jobs := newFakeJobStore()
	service := &Service{
		Jobs:      jobs,
		Extractor: &fakeExtractor{resp: oneChunkResponse()},
		Indexer:   &fakeSparseIndexer{count: 1},
	}
	first, err := service.IngestPath(t.Context(), IngestRequest{SourceID: "cli"}, path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.IngestPath(t.Context(), IngestRequest{SourceID: "cli"}, path)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("job ids should be reused: %q != %q", first.ID, second.ID)
	}
	if jobs.created != 2 {
		t.Fatalf("create calls = %d", jobs.created)
	}
}

func TestServiceMarksFailedWhenExtractionFails(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	jobs := newFakeJobStore()
	service := &Service{
		Jobs:      jobs,
		Extractor: &fakeExtractor{err: errors.New("extract failed")},
		Indexer:   &fakeSparseIndexer{count: 1},
	}
	job, err := service.IngestPath(t.Context(), IngestRequest{}, path)
	if err == nil || !strings.Contains(err.Error(), "extract failed") {
		t.Fatalf("want extract error, got %v", err)
	}
	if job == nil || job.Status != JobFailed {
		t.Fatalf("job status = %#v", job)
	}
}

func TestServiceMarksFailedWhenSparseIndexingFails(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	jobs := newFakeJobStore()
	service := &Service{
		Jobs:      jobs,
		Extractor: &fakeExtractor{resp: oneChunkResponse()},
		Indexer:   &fakeSparseIndexer{err: errors.New("neo4j failed")},
	}
	job, err := service.IngestPath(t.Context(), IngestRequest{}, path)
	if err == nil || !strings.Contains(err.Error(), "neo4j failed") {
		t.Fatalf("want index error, got %v", err)
	}
	if job == nil || job.Status != JobFailed {
		t.Fatalf("job status = %#v", job)
	}
}

func TestServiceDelegatesSearchGetAndList(t *testing.T) {
	jobs := newFakeJobStore()
	job, err := jobs.Create(t.Context(), CreateJobParams{
		SourceID:    "cli",
		SourceKind:  "local",
		DocumentID:  "doc_1",
		ContentHash: "hash",
		FileName:    "manual.pdf",
		Status:      JobSearchable,
	})
	if err != nil {
		t.Fatal(err)
	}
	searcher := &fakeSearchBackend{hits: []SearchHit{{DocumentID: "doc_1", ChunkID: "chunk_1"}}}
	service := &Service{Jobs: jobs, Searcher: searcher}

	hits, err := service.Search(t.Context(), SearchRequest{Query: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || searcher.requests[0].Query != "manual" {
		t.Fatalf("search = %#v requests=%#v", hits, searcher.requests)
	}
	got, err := service.GetJob(t.Context(), job.ID)
	if err != nil || got.ID != job.ID {
		t.Fatalf("GetJob = (%#v, %v)", got, err)
	}
	list, err := service.ListJobs(t.Context(), 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListJobs = (%#v, %v)", list, err)
	}
}

func TestServiceMissingDependencies(t *testing.T) {
	if _, err := (&Service{}).Search(t.Context(), SearchRequest{Query: "x"}); err == nil {
		t.Fatal("Search without backend: want error")
	}
	if _, err := (&Service{}).GetJob(t.Context(), "job-1"); err == nil {
		t.Fatal("GetJob without store: want error")
	}
	if _, err := (&Service{}).ListJobs(t.Context(), 1); err == nil {
		t.Fatal("ListJobs without store: want error")
	}
}

func TestServiceRejectsUnsupportedAndDirectoryPaths(t *testing.T) {
	service := &Service{Jobs: newFakeJobStore(), Extractor: &fakeExtractor{resp: oneChunkResponse()}, Indexer: &fakeSparseIndexer{count: 1}}
	if _, err := service.IngestPath(t.Context(), IngestRequest{}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("directory path error = %v", err)
	}
	// .txt is now in the supportedDocumentExt allowlist; use a genuinely
	// unsupported extension to exercise the rejection path.
	blob := writeNamedTempFile(t, "notes.exe", "payload")
	if _, err := service.IngestPath(t.Context(), IngestRequest{}, blob); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported path error = %v", err)
	}
}

func oneChunkResponse() *ExtractorResponse {
	return &ExtractorResponse{
		Title: "Manual",
		Chunks: []ExtractedChunk{
			{Kind: "page", Text: "hello", Locator: Locator{Page: 1}},
		},
	}
}

func writeNamedTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + string(os.PathSeparator) + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type fakeExtractor struct {
	resp *ExtractorResponse
	err  error
}

func (f *fakeExtractor) ExtractFile(context.Context, string, IngestRequest) (*ExtractorResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

type fakeSparseIndexer struct {
	count int
	err   error
	docs  []ExtractedDocument
}

func (f *fakeSparseIndexer) UpsertSparse(_ context.Context, doc ExtractedDocument) (int, error) {
	f.docs = append(f.docs, doc)
	if f.err != nil {
		return 0, f.err
	}
	return f.count, nil
}

type fakeEmbedQueue struct {
	docs []ExtractedDocument
	err  error
}

func (f *fakeEmbedQueue) Enqueue(_ context.Context, doc ExtractedDocument) error {
	f.docs = append(f.docs, doc)
	return f.err
}

type fakeSearchBackend struct {
	hits     []SearchHit
	err      error
	requests []SearchRequest
}

func (f *fakeSearchBackend) Search(_ context.Context, req SearchRequest) ([]SearchHit, error) {
	f.requests = append(f.requests, req)
	return f.hits, f.err
}

type fakeJobStore struct {
	byID    map[string]Job
	byKey   map[string]string
	next    int
	created int
}

func newFakeJobStore() *fakeJobStore {
	return &fakeJobStore{byID: map[string]Job{}, byKey: map[string]string{}}
}

func (f *fakeJobStore) Create(_ context.Context, params CreateJobParams) (Job, error) {
	f.created++
	key := params.SourceID + "|" + params.DocumentID + "|" + params.ContentHash
	if id := f.byKey[key]; id != "" {
		job := f.byID[id]
		job.OriginalPath = params.OriginalPath
		job.FileName = params.FileName
		job.MIMEType = params.MIMEType
		job.SizeBytes = params.SizeBytes
		f.byID[id] = job
		return job, nil
	}
	f.next++
	id := fmt.Sprintf("job-%d", f.next)
	job := Job{
		ID:           id,
		SourceID:     params.SourceID,
		SourceKind:   params.SourceKind,
		DocumentID:   params.DocumentID,
		ContentHash:  params.ContentHash,
		OriginalPath: params.OriginalPath,
		FileName:     params.FileName,
		MIMEType:     params.MIMEType,
		SizeBytes:    params.SizeBytes,
		Status:       params.Status,
	}
	f.byID[id] = job
	f.byKey[key] = id
	return job, nil
}

func (f *fakeJobStore) Get(_ context.Context, id string) (Job, error) {
	job, ok := f.byID[id]
	if !ok {
		return Job{}, errors.New("not found")
	}
	return job, nil
}

func (f *fakeJobStore) GetByDocumentID(_ context.Context, documentID string) (Job, error) {
	for _, job := range f.byID {
		if job.DocumentID == documentID {
			return job, nil
		}
	}
	return Job{}, errors.New("not found")
}

func (f *fakeJobStore) UpdateStatus(_ context.Context, id string, status JobStatus, message string) (Job, error) {
	job := f.byID[id]
	job.Status = status
	job.Error = message
	f.byID[id] = job
	return job, nil
}

func (f *fakeJobStore) UpdateProgress(_ context.Context, id string, status JobStatus, sparseChunks, embeddedChunks int) (Job, error) {
	job := f.byID[id]
	job.Status = status
	job.SparseChunks = sparseChunks
	job.EmbeddedChunks = embeddedChunks
	f.byID[id] = job
	return job, nil
}

func (f *fakeJobStore) ListRecent(context.Context, int) ([]Job, error) {
	out := make([]Job, 0, len(f.byID))
	for _, job := range f.byID {
		out = append(out, job)
	}
	return out, nil
}
