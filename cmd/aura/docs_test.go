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
			ID:         "job-1",
			DocumentID: "doc-1",
			Status:     documents.JobSearchable,
			FileName:   "manual.pdf",
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

func TestDocsIngestThreadsTheOperatorIdentity(t *testing.T) {
	const operator = "00000000-0000-0000-0000-000000000001"
	svc := &fakeDocsService{ingestJob: &documents.Job{ID: "job-1"}}
	ctx := identityctx.WithIdentityID(t.Context(), operator)
	if err := runDocsCommand(ctx, []string{"ingest", "manual.pdf"}, &bytes.Buffer{}, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	if svc.ingestReq.IdentityID != operator {
		t.Fatalf("ingest identity = %q, want %q", svc.ingestReq.IdentityID, operator)
	}
}

func TestDocsSearchPrintsHits(t *testing.T) {
	svc := &fakeDocsService{
		hits: []documents.DigestHit{{DocumentID: "doc-1", Title: "manual.pdf", Digest: "hello"}},
	}
	var out bytes.Buffer
	if err := runDocsCommand(t.Context(), []string{"search", "--limit", "5", "hello"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Query string                `json:"query"`
		Hits  []documents.DigestHit `json:"hits"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Query != "hello" || len(decoded.Hits) != 1 {
		t.Fatalf("decoded = %#v", decoded)
	}
	if svc.searchLimit != 5 {
		t.Fatalf("limit = %d", svc.searchLimit)
	}
}

// TestDocsSearchBlankQueryListsTheLibrary pins the one question the machine answers
// most cheaply: "what have I actually got in here". A blank query is not an error at any
// other layer — `SearchDocumentDigests`' own comment says it lists newest-first, the
// document_search tool trims and passes it straight through, and that tool's description
// promises the model "leave query empty to list the library". This verb used to be the
// single place that refused, so the operator could not ask what their own agent could.
func TestDocsSearchBlankQueryListsTheLibrary(t *testing.T) {
	svc := &fakeDocsService{
		hits: []documents.DigestHit{
			{DocumentID: "doc-1", Title: "Clienti.xlsx"},
			{DocumentID: "doc-2", Title: "manual.pdf"},
		},
	}
	var out bytes.Buffer
	if err := runDocsCommand(t.Context(), []string{"search"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatalf("a blank query must list the library, not error: %v", err)
	}
	var decoded struct {
		Query string                `json:"query"`
		Hits  []documents.DigestHit `json:"hits"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Query != "" {
		t.Errorf("query = %q, want it passed through empty", decoded.Query)
	}
	if len(decoded.Hits) != 2 {
		t.Errorf("hits = %d, want the whole library", len(decoded.Hits))
	}
	// The flags still have to work without a query in front of them, or "list my library,
	// newest 3" is unsayable.
	out.Reset()
	if err := runDocsCommand(t.Context(), []string{"search", "--limit", "3"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatalf("blank query with a flag: %v", err)
	}
	if svc.searchLimit != 3 {
		t.Errorf("limit = %d, want 3", svc.searchLimit)
	}
}

func TestDocsSearchAcceptsDocumentedTrailingFlags(t *testing.T) {
	svc := &fakeDocsService{
		hits: []documents.DigestHit{
			{DocumentID: "doc-1", Title: "manual.pdf"},
			{DocumentID: "doc-2", Title: "other.pdf"},
		},
	}
	var out bytes.Buffer
	args := []string{"search", "hello", "world", "--document-id", "doc-1", "--limit", "3"}
	if err := runDocsCommand(t.Context(), args, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Query string                `json:"query"`
		Hits  []documents.DigestHit `json:"hits"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Query != "hello world" {
		t.Fatalf("query = %q, want flags excluded", decoded.Query)
	}
	if svc.searchLimit != 3 {
		t.Fatalf("limit = %d", svc.searchLimit)
	}
	if len(decoded.Hits) != 1 || decoded.Hits[0].DocumentID != "doc-1" {
		t.Fatalf("--document-id did not scope the answer: %#v", decoded.Hits)
	}
}

func TestDocsSearchRejectsUnknownTrailingFlag(t *testing.T) {
	err := runDocsCommand(
		t.Context(),
		[]string{"search", "hello", "--unknown", "value"},
		&bytes.Buffer{},
		fakeDocsFactory(&fakeDocsService{}),
	)
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err = %v, want unknown flag", err)
	}
}

func TestDocsSearchRejectsLimitOutsideInt32(t *testing.T) {
	for _, value := range []string{"2147483648", "9223372036854775807"} {
		_, _, _, err := parseDocsSearchArgs([]string{"--limit", value})
		if err == nil || !strings.Contains(err.Error(), "positive integer") {
			t.Fatalf("--limit %s: err = %v, want bounded-integer error", value, err)
		}
	}
}

// TestDocsSearchThreadsTheOperatorIdentity guards the half of the fix a shape assertion
// cannot see. The digest query is identity-scoped in SQL, so a service that forgot the
// principal is a no-op that exits 0 with zero hits.
func TestDocsSearchThreadsTheOperatorIdentity(t *testing.T) {
	const operator = "00000000-0000-0000-0000-000000000001"
	svc := &fakeDocsService{}
	ctx := identityctx.WithIdentityID(t.Context(), operator)
	if err := runDocsCommand(ctx, []string{"search", "hello"}, &bytes.Buffer{}, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	if svc.searchIdentity != operator {
		t.Fatalf("search identity = %q, want the resolved operator %q", svc.searchIdentity, operator)
	}
}

func TestDocsStatusPrintsJob(t *testing.T) {
	svc := &fakeDocsService{statusJob: &documents.Job{ID: "job-1", Status: documents.JobSearchable}}
	var out bytes.Buffer
	if err := runDocsCommand(t.Context(), []string{"status", "job-1"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded documents.Job
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "job-1" || decoded.Status != documents.JobSearchable {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func fakeDocsFactory(svc *fakeDocsService) docsServiceFactory {
	return func(context.Context) (docsCLIService, func(), error) {
		return svc, func() {}, nil
	}
}

type fakeDocsService struct {
	ingestJob      *documents.Job
	ingestReq      documents.IngestRequest
	statusJob      *documents.Job
	hits           []documents.DigestHit
	searchIdentity string
	searchQuery    string
	searchLimit    int
}

func (f *fakeDocsService) IngestPath(_ context.Context, req documents.IngestRequest, _ string) (*documents.Job, error) {
	f.ingestReq = req
	return f.ingestJob, nil
}

func (f *fakeDocsService) SearchDigests(_ context.Context, identityID, query string, limit int) ([]documents.DigestHit, error) {
	f.searchIdentity, f.searchQuery, f.searchLimit = identityID, query, limit
	return f.hits, nil
}

func (f *fakeDocsService) GetJob(context.Context, string) (*documents.Job, error) {
	return f.statusJob, nil
}

func (f *fakeDocsService) ListJobs(context.Context, int) ([]documents.Job, error) {
	return nil, nil
}
