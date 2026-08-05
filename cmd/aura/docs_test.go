package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
)

func TestDocsCommandRequiresSubcommand(t *testing.T) {
	err := runDocsCommand(t.Context(), nil, &bytes.Buffer{}, fakeDocsFactory(&fakeDocsService{}))
	if err == nil || !strings.Contains(err.Error(), "usage: aura docs") {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestDocsIngestPrintsAcceptedAsset(t *testing.T) {
	svc := &fakeDocsService{
		ingestAsset: assets.Asset{
			ID:       "asset-1",
			Status:   assets.StatusAccepted,
			FileName: "manual.pdf",
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
	if decoded["status"] != string(assets.StatusAccepted) {
		t.Fatalf("status = %#v", decoded["status"])
	}
	if decoded["asset_id"] != "asset-1" {
		t.Fatalf("asset_id = %#v", decoded["asset_id"])
	}
}

func TestDocsIngestThreadsTheOperatorIdentity(t *testing.T) {
	const operator = "00000000-0000-0000-0000-000000000001"
	svc := &fakeDocsService{ingestAsset: assets.Asset{ID: "asset-1"}}
	ctx := identityctx.WithIdentityID(t.Context(), operator)
	if err := runDocsCommand(ctx, []string{"ingest", "manual.pdf"}, &bytes.Buffer{}, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	if svc.ingestReq.IdentityID != operator || svc.ingestReq.SourceKind != assets.SourceCLI {
		t.Fatalf("ingest identity = %q, want %q", svc.ingestReq.IdentityID, operator)
	}
}

func TestDocumentPathWithinRootRejectsEscape(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "manual.pdf")
	if err := os.WriteFile(inside, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, relative, err := documentPathWithinRoot(root, "manual.pdf")
	if err != nil {
		t.Fatalf("inside path: %v", err)
	}
	if resolved != inside || relative != "manual.pdf" {
		t.Fatalf("resolved = %q, relative = %q", resolved, relative)
	}

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.pdf")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := documentPathWithinRoot(root, outside); err == nil || !strings.Contains(err.Error(), "outside workspace root") {
		t.Fatalf("outside path error = %v, want workspace fence", err)
	}
}

func TestDocsSearchPrintsHits(t *testing.T) {
	svc := &fakeDocsService{
		response: documents.RetrievalResponse{
			Profile:   documents.ProductionRetrievalProfile,
			Status:    documents.RetrievalComplete,
			Documents: []documents.RetrievalDocument{{DocumentID: "doc-1", Title: "manual.pdf"}},
		},
	}
	var out bytes.Buffer
	if err := runDocsCommand(t.Context(), []string{"search", "--limit", "5", "hello"}, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Query     string                        `json:"query"`
		Documents []documents.RetrievalDocument `json:"documents"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Query != "hello" || len(decoded.Documents) != 1 {
		t.Fatalf("decoded = %#v", decoded)
	}
	if svc.retrievalRequest.Limit != 5 {
		t.Fatalf("limit = %d", svc.retrievalRequest.Limit)
	}
}

func TestDocsSearchRejectsBlankQuery(t *testing.T) {
	err := runDocsCommand(
		t.Context(), []string{"search", "--limit", "3"},
		&bytes.Buffer{}, fakeDocsFactory(&fakeDocsService{}),
	)
	if !errors.Is(err, documents.ErrEmptyDocumentQuery) {
		t.Fatalf("blank query error = %v", err)
	}
}

func TestDocsSearchAcceptsDocumentedTrailingFlags(t *testing.T) {
	svc := &fakeDocsService{
		response: documents.RetrievalResponse{
			Documents: []documents.RetrievalDocument{{DocumentID: "doc-1", Title: "manual.pdf"}},
		},
	}
	var out bytes.Buffer
	args := []string{"search", "hello", "world", "--document-id", "doc-1", "--limit", "3"}
	if err := runDocsCommand(t.Context(), args, &out, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Query     string                        `json:"query"`
		Documents []documents.RetrievalDocument `json:"documents"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Query != "hello world" {
		t.Fatalf("query = %q, want flags excluded", decoded.Query)
	}
	if svc.retrievalRequest.Limit != 3 {
		t.Fatalf("limit = %d", svc.retrievalRequest.Limit)
	}
	if len(svc.retrievalRequest.DocumentIDs) != 1 || svc.retrievalRequest.DocumentIDs[0] != "doc-1" ||
		len(decoded.Documents) != 1 {
		t.Fatalf("--document-id did not scope the request: %#v", svc.retrievalRequest)
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

// TestDocsSearchThreadsTheOperatorIdentity guards what a response-shape assertion
// cannot see: the shared retriever must receive the resolved operator identity.
func TestDocsSearchThreadsTheOperatorIdentity(t *testing.T) {
	const operator = "00000000-0000-0000-0000-000000000001"
	svc := &fakeDocsService{}
	ctx := identityctx.WithIdentityID(t.Context(), operator)
	if err := runDocsCommand(ctx, []string{"search", "hello"}, &bytes.Buffer{}, fakeDocsFactory(svc)); err != nil {
		t.Fatal(err)
	}
	if svc.retrievalRequest.IdentityID != operator {
		t.Fatalf("search identity = %q, want the resolved operator %q", svc.retrievalRequest.IdentityID, operator)
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
	ingestAsset      assets.Asset
	ingestReq        assets.DocumentIngestRequest
	statusJob        *documents.Job
	response         documents.RetrievalResponse
	retrievalRequest documents.RetrievalRequest
}

func (f *fakeDocsService) IngestDocumentPath(_ context.Context, req assets.DocumentIngestRequest, _ string) (assets.Asset, error) {
	f.ingestReq = req
	return f.ingestAsset, nil
}

func (f *fakeDocsService) Retrieve(
	_ context.Context,
	request documents.RetrievalRequest,
) (documents.RetrievalResponse, error) {
	f.retrievalRequest = request
	response := f.response
	response.Query = request.Query
	return response, nil
}

func (f *fakeDocsService) GetJob(context.Context, string) (*documents.Job, error) {
	return f.statusJob, nil
}

func (f *fakeDocsService) ListJobs(context.Context, int) ([]documents.Job, error) {
	return nil, nil
}
