package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

type fakeIndexer struct {
	calledPath string
	calledReq  documents.IngestRequest
	job        *documents.Job
	err        error
}

func (f *fakeIndexer) IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error) {
	f.calledPath = path
	f.calledReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.job, nil
}

func TestDocumentIndex_IndexesWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	fi := &fakeIndexer{job: &documents.Job{DocumentID: "doc-1", FileName: "r.docx", Status: "searchable"}}
	tool := &DocumentIndex{Indexer: fi, WorkspaceRoot: root}
	// NewResult requires tool-call context (session/toolcall/runDir); the sibling
	// document_search_test.go's toolTestContext helper provides it — a bare
	// context.Background() fails with "missing tool-call context".
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"path":"artifacts/r.docx"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "artifacts", "r.docx")
	if fi.calledPath != want {
		t.Fatalf("IngestPath path = %q, want %q", fi.calledPath, want)
	}
	if fi.calledReq.IdentityID == "" {
		t.Fatal("expected a non-empty owning identity (ownerFromContext) on the ingest request")
	}
}

func TestDocumentIndex_RejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	fi := &fakeIndexer{job: &documents.Job{}}
	tool := &DocumentIndex{Indexer: fi, WorkspaceRoot: root}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"/etc/passwd"}`)); err == nil {
		t.Fatal("expected rejection for a path outside the workspace")
	}
	if fi.calledPath != "" {
		t.Fatal("IngestPath must not run for an out-of-workspace path")
	}
}

func TestDocumentIndex_RequiresPath(t *testing.T) {
	tool := &DocumentIndex{Indexer: &fakeIndexer{}, WorkspaceRoot: t.TempDir()}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"   "}`)); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestDocumentIndex_NilBackendErrors(t *testing.T) {
	tool := &DocumentIndex{WorkspaceRoot: t.TempDir()}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"a.txt"}`)); err == nil {
		t.Fatal("expected error when indexer is not configured")
	}
}
