package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
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
	if svc.retrieveReq.Limit != 5 {
		t.Fatalf("limit = %d", svc.retrieveReq.Limit)
	}
}

// TestDocsSearchThreadsTheOperatorIdentity guards the half of the fix a shape assertion
// cannot see. With AURA_MUSR_ISOLATION on, Searcher.Search and every scoped seed query
// return before any I/O when IdentityID is empty, so a field-identical service that
// forgot the principal is still a no-op that exits 0 with zero hits.
func TestDocsSearchThreadsTheOperatorIdentity(t *testing.T) {
	const operator = "00000000-0000-0000-0000-000000000001"
	svc := &fakeDocsService{}
	ctx := identityctx.WithIdentityID(t.Context(), operator)
	if err := runDocsCommand(ctx, []string{"search", "hello"}, &bytes.Buffer{}, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	if svc.retrieveReq.IdentityID != operator {
		t.Fatalf("retrieve identity = %q, want the resolved operator %q", svc.retrieveReq.IdentityID, operator)
	}
}

func TestDocsBenchScoresZeroHitsAndThreadsTheOperatorIdentity(t *testing.T) {
	const operator = "00000000-0000-0000-0000-000000000001"
	svc := &fakeDocsService{
		ingestJob: &documents.Job{ID: "job-1", DocumentID: "doc-1", FileName: "manual.pdf", SparseChunks: 3},
	}
	var out bytes.Buffer
	ctx := identityctx.WithIdentityID(t.Context(), operator)
	args := []string{"bench", "--query", "hello", "manual.pdf"}
	if err := runDocsCommand(ctx, args, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Hits  int     `json:"hits"`
		Score float64 `json:"industrial_score"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if svc.retrieveReq.IdentityID != operator || svc.retrieveReq.DocumentID != "doc-1" {
		t.Fatalf("bench retrieve request = %#v", svc.retrieveReq)
	}
	if decoded.Hits != 0 {
		t.Fatalf("hits = %d, want the reported 0", decoded.Hits)
	}
	if decoded.Score > 75 {
		t.Fatalf("industrial_score = %.1f for a retrieval that returned nothing", decoded.Score)
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
	ingestJob   *documents.Job
	statusJob   *documents.Job
	hits        []documents.SearchHit
	retrieveReq documents.SearchRequest
}

func (f *fakeDocsService) IngestPath(context.Context, documents.IngestRequest, string) (*documents.Job, error) {
	return f.ingestJob, nil
}

func (f *fakeDocsService) Retrieve(_ context.Context, req documents.SearchRequest) ([]documents.SearchHit, error) {
	f.retrieveReq = req
	return f.hits, nil
}

func (f *fakeDocsService) GetJob(context.Context, string) (*documents.Job, error) {
	return f.statusJob, nil
}

func (f *fakeDocsService) ListJobs(context.Context, int) ([]documents.Job, error) {
	return nil, nil
}
