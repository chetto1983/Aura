package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

func TestDocsCommandRequiresSubcommand(t *testing.T) {
	err := runDocsCommand(t.Context(), nil, &bytes.Buffer{}, fakeDocsFactory(&fakeDocsService{}))
	if err == nil || !strings.Contains(err.Error(), "usage: aura docs") {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestDocsIngestPrintsSearchableJob(t *testing.T) {
	svc := &fakeDocsService{
		ingestJob: &documents.Job{
			ID:           "job-1",
			DocumentID:   "doc-1",
			Status:       documents.JobSearchable,
			FileName:     "manual.pdf",
			SparseChunks: 3,
		},
	}
	var out bytes.Buffer
	if err := runDocsCommand(t.Context(), []string{"ingest", "manual.pdf"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["status"] != string(documents.JobSearchable) {
		t.Fatalf("status = %#v", decoded["status"])
	}
	if decoded["document_id"] != "doc-1" {
		t.Fatalf("document_id = %#v", decoded["document_id"])
	}
}

func TestDocsSearchPrintsHits(t *testing.T) {
	svc := &fakeDocsService{
		hits: []documents.SearchHit{{DocumentID: "doc-1", ChunkID: "chunk-1", Text: "hello"}},
	}
	var out bytes.Buffer
	if err := runDocsCommand(t.Context(), []string{"search", "--limit", "5", "hello"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Query string                `json:"query"`
		Hits  []documents.SearchHit `json:"hits"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Query != "hello" || len(decoded.Hits) != 1 {
		t.Fatalf("decoded = %#v", decoded)
	}
	if svc.searchReq.Limit != 5 {
		t.Fatalf("limit = %d", svc.searchReq.Limit)
	}
}

func TestDocsStatusPrintsJob(t *testing.T) {
	svc := &fakeDocsService{statusJob: &documents.Job{ID: "job-1", Status: documents.JobComplete}}
	var out bytes.Buffer
	if err := runDocsCommand(t.Context(), []string{"status", "job-1"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded documents.Job
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "job-1" || decoded.Status != documents.JobComplete {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func fakeDocsFactory(svc *fakeDocsService) docsServiceFactory {
	return func(context.Context) (docsCLIService, func(), error) {
		return svc, func() {}, nil
	}
}

type fakeDocsService struct {
	ingestJob *documents.Job
	statusJob *documents.Job
	hits      []documents.SearchHit
	searchReq documents.SearchRequest
}

func (f *fakeDocsService) IngestPath(context.Context, documents.IngestRequest, string) (*documents.Job, error) {
	return f.ingestJob, nil
}

func (f *fakeDocsService) Search(_ context.Context, req documents.SearchRequest) ([]documents.SearchHit, error) {
	f.searchReq = req
	return f.hits, nil
}

func (f *fakeDocsService) GetJob(context.Context, string) (*documents.Job, error) {
	return f.statusJob, nil
}

func (f *fakeDocsService) ListJobs(context.Context, int) ([]documents.Job, error) {
	return nil, nil
}
