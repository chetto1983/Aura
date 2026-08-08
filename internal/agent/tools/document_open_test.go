package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

// The id a retrieval hit carries and the caller passes back. It keys the staging
// directory, which used to be keyed by a catalog uuid that no longer exists.
const toolTestDocumentID = "doc_" + "10000000000000000000000000000001"

// document_open writes into the caller's per-identity BOX, so these tests drive it over the same
// daemon-free fakeBox harness the fs_* tools use (fs_box_fake_router_test.go) rather than a host
// temp dir. That is not only a coverage decision — docker_integration contributes zero coverage —
// it is the point of the tool's fix: a host temp dir is exactly the filesystem the agent cannot
// see, and asserting against one would re-pin the defect.

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

// openedDoc builds a backend whose recorded size agrees with its body, which is what the
// reconciler records for a real object. A disagreement is its own test below.
//
// The staging key is DocumentID now, not a catalog uuid: there is no catalog, and the id a
// retrieval hit carries is the one the caller passes back to document_open.
func openedDoc(body string, meta documents.OpenedDocument) *fakeDocumentOpener {
	if meta.DocumentID == "" {
		meta.DocumentID = toolTestDocumentID
	}
	meta.SizeBytes = int64(len(body))
	return &fakeDocumentOpener{body: body, meta: meta}
}

func openedPayload(t *testing.T, result ToolResult) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Preview), &payload); err != nil {
		t.Fatalf("result is not JSON (%q): %v", result.Preview, err)
	}
	return payload
}

// The destination is fixed and lives under the BOX workspace mount. It is asserted on its own
// because the entire defect this tool was fixed for was a destination that resolved against the
// wrong filesystem while every other assertion still passed.
func TestOpenedDocumentsDestinationIsUnderTheBoxWorkspace(t *testing.T) {
	if openedDocumentsBoxDir != "/workspace/documents" {
		t.Fatalf("openedDocumentsBoxDir = %q, want the box workspace mount", openedDocumentsBoxDir)
	}
}

func TestDocumentOpen_WritesOriginalIntoTheBox(t *testing.T) {
	backend := openedDoc("row,row,row", documents.OpenedDocument{
		DocumentID: "doc_9f2c", FileName: "Clienti.xlsx",
		MIMEType: "application/vnd.ms-excel", SHA256: "abc123",
	})
	be := &fakeBox{}
	tool := &DocumentOpen{Documents: backend, Router: routerWith(be)}

	result, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_9f2c"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Keyed by the document id the caller passed, not by the fixture default.
	dir, err := StagedDocumentDirectory("doc_9f2c")
	if err != nil {
		t.Fatal(err)
	}
	want := dir + "/Clienti.xlsx"
	got, ok := be.written[want]
	if !ok {
		t.Fatalf("nothing was written into the box at %s: %+v", want, be.written)
	}
	if got != "row,row,row" {
		t.Fatalf("written bytes = %q, want the streamed body", got)
	}

	payload := openedPayload(t, result)
	// The reported path must be the one the agent's own shell_exec and fs_read reach. Reporting a
	// path that is real somewhere else is the whole bug (Clienti.xlsx, 2026-08-03).
	if payload["path"] != want {
		t.Fatalf("path = %v, want the box path %s", payload["path"], want)
	}
	if payload["file_name"] != "Clienti.xlsx" || payload["sha256"] != "abc123" {
		t.Fatalf("payload lost metadata: %#v", payload)
	}
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
	be := &fakeBox{}
	tool := &DocumentOpen{
		Documents: openedDoc("x", documents.OpenedDocument{FileName: "original.xlsx"}),
		Router:    routerWith(be),
	}
	if _, err := tool.Execute(toolTestContext(t),
		json.RawMessage(`{"document_id":"doc_1","file_name":"clienti.xlsx"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, err := StagedDocumentPath(toolTestDocumentID, "clienti.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := be.written[want]; !ok {
		t.Fatalf("caller file_name was not used: %+v", be.written)
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
			backend := openedDoc("x", documents.OpenedDocument{FileName: "ok.xlsx"})
			be := &fakeBox{}
			tool := &DocumentOpen{Documents: backend, Router: routerWith(be)}
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
			if len(be.written) != 0 {
				t.Fatalf("a rejected name still wrote into the box: %+v", be.written)
			}
		})
	}
}

// The BACKEND's file name is validated by the same rule as the caller's: it becomes a path
// component, and a "../.." in it would join its way straight out of the documents directory.
func TestDocumentOpen_RefusesAnUnusableBackendFileName(t *testing.T) {
	for _, name := range []string{"", "   ", "../../etc/passwd"} {
		t.Run(name, func(t *testing.T) {
			be := &fakeBox{}
			tool := &DocumentOpen{
				Documents: openedDoc("x", documents.OpenedDocument{FileName: name}),
				Router:    routerWith(be),
			}
			if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`)); err == nil {
				t.Fatalf("backend file name %q was accepted", name)
			}
			if len(be.written) != 0 {
				t.Fatalf("wrote %+v; a document with no usable name must land nowhere", be.written)
			}
		})
	}
}

func TestDocumentOpen_RequiresDocumentID(t *testing.T) {
	tool := &DocumentOpen{Documents: &fakeDocumentOpener{}, Router: routerWith(&fakeBox{})}
	for _, raw := range []string{`{}`, `{"document_id":"   "}`} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err == nil {
			t.Fatalf("args %s were accepted", raw)
		}
	}
}

func TestDocumentOpen_UnconfiguredAndMalformed(t *testing.T) {
	if _, err := (&DocumentOpen{Router: routerWith(&fakeBox{})}).
		Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`)); err == nil {
		t.Fatal("a tool with no backend must not report success")
	}
	tool := &DocumentOpen{Documents: &fakeDocumentOpener{}, Router: routerWith(&fakeBox{})}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":`)); err == nil {
		t.Fatal("malformed args were accepted")
	}
}

// The containment invariant for this tool, and the replacement for the old
// TestDocumentOpen_RequiresWorkspaceRoot: there is no host destination any more, so an unreachable
// box DENIES (D-09/GATE-01) instead of falling back to a filesystem the agent cannot read. The
// deny must also arrive BEFORE the object store is touched — a call that cannot land its bytes
// must not pay to download them.
func TestDocumentOpen_DeniesWhenTheBoxIsUnreachable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		router func() (*fakeBox, *DocumentOpen)
	}{
		{"no router at all", func() (*fakeBox, *DocumentOpen) {
			return nil, &DocumentOpen{Documents: openedDoc("x", documents.OpenedDocument{FileName: "a.xlsx"})}
		}},
		{"box cannot be resolved", func() (*fakeBox, *DocumentOpen) {
			be := &fakeBox{resolveE: errors.New("dockerd down")}
			return be, &DocumentOpen{
				Documents: openedDoc("x", documents.OpenedDocument{FileName: "a.xlsx"}),
				Router:    routerWith(be),
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			be, tool := tc.router()
			res, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`))
			if err != nil {
				t.Fatalf("an unreachable box is a DENY result, not a Go error: %v", err)
			}
			var payload map[string]string
			if uerr := json.Unmarshal([]byte(res.Preview), &payload); uerr != nil {
				t.Fatalf("deny preview is not json: %q", res.Preview)
			}
			if payload["error"] != "sandbox_unavailable" || payload["tool"] != "document_open" {
				t.Fatalf("payload = %+v, want sandbox_unavailable for document_open", payload)
			}
			if !strings.Contains(payload["message"], "NOT run on the host") {
				t.Errorf("the deny must say the host was not used: %q", payload["message"])
			}
			opener, ok := tool.Documents.(*fakeDocumentOpener)
			if !ok {
				t.Fatalf("backend = %T", tool.Documents)
			}
			if opener.documentID != "" {
				t.Error("the document was downloaded for a call that could never write it")
			}
			if be != nil && len(be.written) != 0 {
				t.Fatalf("a denied call still wrote: %+v", be.written)
			}
		})
	}
}

func TestDocumentOpen_PropagatesBackendError(t *testing.T) {
	tool := &DocumentOpen{
		Documents: &fakeDocumentOpener{err: errors.New("document not found")},
		Router:    routerWith(&fakeBox{}),
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

// A half-written spreadsheet that LOOKS whole is worse than none: the agent would compute a
// confident wrong total from it. tar extracts as the daemon reads, so the box can hold a short file
// after a dead stream and the tool has to remove it explicitly.
func TestDocumentOpen_RemovesPartialCopyOnStreamFailure(t *testing.T) {
	be := &fakeBox{}
	tool := &DocumentOpen{
		Documents: &fakeDocumentOpener{
			reader: &failingReader{remaining: 64},
			meta:   documents.OpenedDocument{DocumentID: toolTestDocumentID, FileName: "half.xlsx", SizeBytes: 4096},
		},
		Router: routerWith(be),
	}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`)); err == nil {
		t.Fatal("a failed stream reported success")
	}
	if len(be.written) != 0 {
		t.Fatalf("a failed stream left a file behind: %+v", be.written)
	}
	partialPath, pathErr := StagedDocumentPath(toolTestDocumentID, "half.xlsx")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if len(be.execs) != 1 || !strings.Contains(be.execs[0].Command, "rm -f -- "+shellQuoteArg(partialPath)) {
		t.Fatalf("the partial copy was not removed from the box: %+v", be.execs)
	}
}

// The length handed to the copy-in is the CATALOG's, declared before a byte is read, so a document
// whose stored size disagrees with its bytes fails loudly instead of landing a file whose length
// contradicts the record it was opened from.
func TestDocumentOpen_DeclaresTheCatalogSize(t *testing.T) {
	be := &fakeBox{}
	tool := &DocumentOpen{
		Documents: &fakeDocumentOpener{
			body: "eleven byte",
			meta: documents.OpenedDocument{DocumentID: toolTestDocumentID, FileName: "stale.xlsx", SizeBytes: 5},
		},
		Router: routerWith(be),
	}
	_, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":"doc_1"}`))
	if err == nil || !strings.Contains(err.Error(), "declared 5") {
		t.Fatalf("error = %v, want a refusal naming the size the catalog declared", err)
	}
	if len(be.written) != 0 {
		t.Fatalf("a size mismatch still wrote: %+v", be.written)
	}
}

func TestDocumentOpen_SpecNamesTheAggregateCase(t *testing.T) {
	spec := (&DocumentOpen{}).Spec()
	if spec.Name != "document_open" {
		t.Fatalf("name = %q, want document_open", spec.Name)
	}
	// Deferred-or-loaded is asserted once, in TestOnlyTheWorkingSetIsAlwaysActive,
	// where the manifest budget is weighed as a whole.
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
