package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

type fakeDocumentOpener struct {
	identityID string
	documentID string
	body       string
	reader     io.ReadCloser // when set, overrides body (used for mid-copy failure)
	meta       documents.OpenedDocument
	err        error
}

func (f *fakeDocumentOpener) OpenDocument(
	_ context.Context,
	identityID, documentID string,
) (io.ReadCloser, documents.OpenedDocument, error) {
	f.identityID, f.documentID = identityID, documentID
	if f.err != nil {
		return nil, documents.OpenedDocument{}, f.err
	}
	if f.reader != nil {
		return f.reader, f.meta, nil
	}
	return io.NopCloser(strings.NewReader(f.body)), f.meta, nil
}

func openedPayload(t *testing.T, result ToolResult) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Preview), &payload); err != nil {
		t.Fatalf("result is not JSON (%q): %v", result.Preview, err)
	}
	return payload
}

func TestDocumentOpen_WritesOriginalIntoWorkspace(t *testing.T) {
	root := t.TempDir()
	backend := &fakeDocumentOpener{
		body: "row,row,row",
		meta: documents.OpenedDocument{
			DocumentID: "doc_9f2c", FileName: "Clienti.xlsx",
			MIMEType: "application/vnd.ms-excel", SizeBytes: 11, SHA256: "abc123",
		},
	}
	tool := &DocumentOpen{Documents: backend, WorkspaceRoot: root}

	result, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_9f2c"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, openedDocumentsDir, "Clienti.xlsx")
	got, err := os.ReadFile(want) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("file was not written to %s: %v", want, err)
	}
	if string(got) != "row,row,row" {
		t.Fatalf("written bytes = %q, want the streamed body", got)
	}

	payload := openedPayload(t, result)
	if payload["path"] != want {
		t.Fatalf("path = %v, want %s", payload["path"], want)
	}
	if payload["file_name"] != "Clienti.xlsx" || payload["sha256"] != "abc123" {
		t.Fatalf("payload lost metadata: %#v", payload)
	}
	// size_bytes reports what was ACTUALLY written, not what the catalog claimed:
	// a truncated stream must not be reported as a whole file.
	if payload["size_bytes"] != float64(len("row,row,row")) {
		t.Fatalf("size_bytes = %v, want the byte count copied", payload["size_bytes"])
	}
	if backend.documentID != "doc_9f2c" {
		t.Fatalf("backend document id = %q", backend.documentID)
	}
	if backend.identityID == "" {
		t.Fatal("expected the owning identity (ownerFromContext) to reach the backend")
	}
	if result.Provenance == nil || result.Provenance.Source != "document_open" {
		t.Fatalf("provenance = %#v, want an untrusted document_open source", result.Provenance)
	}
}

func TestDocumentOpen_HonoursCallerFileName(t *testing.T) {
	root := t.TempDir()
	tool := &DocumentOpen{
		Documents:     &fakeDocumentOpener{body: "x", meta: documents.OpenedDocument{FileName: "original.xlsx"}},
		WorkspaceRoot: root,
	}
	if _, err := tool.Execute(toolTestContext(t),
		json.RawMessage(`{"document_id":"doc_1","file_name":"clienti.xlsx"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, openedDocumentsDir, "clienti.xlsx")); err != nil {
		t.Fatalf("caller file_name was not used: %v", err)
	}
}

func TestDocumentOpen_RejectsPathsAsFileName(t *testing.T) {
	// A traversal must be REFUSED, not silently basename-d into acceptance: the
	// caller asked for a location it may not have, and answering "fine, I wrote it
	// elsewhere" hides that.
	for _, name := range []string{
		"../escape.xlsx", `..\escape.xlsx`, "sub/dir.xlsx", "/etc/passwd", "..", ".", ".hidden",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			backend := &fakeDocumentOpener{body: "x", meta: documents.OpenedDocument{FileName: "ok.xlsx"}}
			tool := &DocumentOpen{Documents: backend, WorkspaceRoot: root}
			raw, err := json.Marshal(map[string]string{"document_id": "doc_1", "file_name": name})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			if _, err := tool.Execute(toolTestContext(t), raw); err == nil {
				t.Fatalf("file_name %q was accepted", name)
			}
			if backend.documentID != "" {
				t.Fatal("the name was validated only AFTER opening the document; " +
					"a rejected call must not reach the backend")
			}
		})
	}
}

func TestDocumentOpen_RequiresDocumentID(t *testing.T) {
	tool := &DocumentOpen{Documents: &fakeDocumentOpener{}, WorkspaceRoot: t.TempDir()}
	for _, raw := range []string{`{}`, `{"document_id":"   "}`} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err == nil {
			t.Fatalf("args %s were accepted", raw)
		}
	}
}

func TestDocumentOpen_UnconfiguredAndMalformed(t *testing.T) {
	if _, err := (&DocumentOpen{WorkspaceRoot: t.TempDir()}).
		Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`)); err == nil {
		t.Fatal("a tool with no backend must not report success")
	}
	tool := &DocumentOpen{Documents: &fakeDocumentOpener{}, WorkspaceRoot: t.TempDir()}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":`)); err == nil {
		t.Fatal("malformed args were accepted")
	}
}

func TestDocumentOpen_RequiresWorkspaceRoot(t *testing.T) {
	tool := &DocumentOpen{Documents: &fakeDocumentOpener{body: "x"}}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`)); err == nil {
		t.Fatal("expected an error when no workspace root is configured")
	}
}

func TestDocumentOpen_PropagatesBackendError(t *testing.T) {
	tool := &DocumentOpen{
		Documents:     &fakeDocumentOpener{err: errors.New("document not found")},
		WorkspaceRoot: t.TempDir(),
	}
	_, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_missing"}`))
	if err == nil || !strings.Contains(err.Error(), "document not found") {
		t.Fatalf("error = %v, want the backend's reason preserved", err)
	}
}

// failingReader fails partway through, standing in for a Garage stream that dies
// mid-download.
type failingReader struct{ remaining int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("connection reset")
	}
	n := min(len(p), r.remaining)
	for i := range n {
		p[i] = 'x'
	}
	r.remaining -= n
	return n, nil
}

func (r *failingReader) Close() error { return nil }

func TestDocumentOpen_RemovesPartialFileOnStreamFailure(t *testing.T) {
	root := t.TempDir()
	tool := &DocumentOpen{
		Documents: &fakeDocumentOpener{
			reader: &failingReader{remaining: 64},
			meta:   documents.OpenedDocument{FileName: "half.xlsx"},
		},
		WorkspaceRoot: root,
	}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`)); err == nil {
		t.Fatal("a failed stream reported success")
	}
	// A half-written spreadsheet that LOOKS whole is worse than none: the agent
	// would compute a confident wrong total from it.
	if _, err := os.Stat(filepath.Join(root, openedDocumentsDir, "half.xlsx")); !os.IsNotExist(err) {
		t.Fatalf("partial file survived the failure (stat err = %v)", err)
	}
}

func TestDocumentOpen_SpecNamesTheAggregateCase(t *testing.T) {
	spec := (&DocumentOpen{}).Spec()
	if spec.Name != "document_open" || !spec.Deferred {
		t.Fatalf("spec = %q deferred=%v", spec.Name, spec.Deferred)
	}
	// The tool only earns its place if the model reaches for it on the questions
	// retrieval cannot answer, so the description must say so in the words a
	// tool_search query would carry.
	for _, want := range []string{"document_search", "/workspace", "how many", "spreadsheet"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("description never mentions %q", want)
		}
	}
}

// TestDocumentSearchPointsAtDocumentOpen guards the other half of the handoff:
// document_search is the visible entry point, so if it stops naming document_open
// the model has no path to the file for an aggregate question.
func TestDocumentSearchPointsAtDocumentOpen(t *testing.T) {
	if !strings.Contains((&DocumentSearch{}).Spec().Description, "document_open") {
		t.Error("document_search no longer names document_open")
	}
}
