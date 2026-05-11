package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubReindexer records ReindexWikiPage calls so tests can assert that wiki
// writes propagate to the vector index.
type stubReindexer struct {
	calls []string
}

func (s *stubReindexer) ReindexWikiPage(_ context.Context, slug string) error {
	s.calls = append(s.calls, slug)
	return nil
}

func newFilesTestDeps(t *testing.T) (Deps, string) {
	t.Helper()
	wikiDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wikiDir, "raw"), 0o755); err != nil {
		t.Fatalf("mkdir raw: %v", err)
	}
	deps := Deps{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		WikiDir:      wikiDir,
		WorkspaceDir: t.TempDir(),
		SkillsDir:    t.TempDir(),
	}
	return deps, wikiDir
}

func TestFilesList_ReturnsSortedEntries(t *testing.T) {
	deps, wikiDir := newFilesTestDeps(t)
	if err := os.WriteFile(filepath.Join(wikiDir, "alpha.md"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write alpha: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/files/wiki/tree", nil)
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesList(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var entries []fileEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Directories must come before files (operator scans the tree first).
	if len(entries) < 2 || entries[0].Type != "dir" {
		t.Fatalf("entries = %+v, want dir first", entries)
	}
}

func TestFilesRead_UTF8AndBase64(t *testing.T) {
	deps, wikiDir := newFilesTestDeps(t)
	if err := os.WriteFile(filepath.Join(wikiDir, "text.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}
	bin := []byte{0xff, 0xfe, 0x00, 0xff}
	if err := os.WriteFile(filepath.Join(wikiDir, "blob.bin"), bin, 0o644); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	read := func(path string) fileReadResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/files/wiki/file?path="+path, nil)
		req.SetPathValue("root", "wiki")
		w := httptest.NewRecorder()
		handleFilesRead(deps)(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("read %s: status = %d body = %s", path, w.Code, w.Body.String())
		}
		var resp fileReadResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	text := read("text.md")
	if text.Encoding != "utf-8" || text.Content != "hello" {
		t.Fatalf("text response = %+v", text)
	}
	blob := read("blob.bin")
	if blob.Encoding != "base64" || blob.Content == "" {
		t.Fatalf("blob response = %+v", blob)
	}
}

func TestFilesWrite_TriggersWikiReindex(t *testing.T) {
	deps, wikiDir := newFilesTestDeps(t)
	rx := &stubReindexer{}
	deps.WikiSearch = rx

	body, _ := json.Marshal(fileWriteRequest{Content: "# title\nbody", Encoding: "utf-8"})
	req := httptest.NewRequest(http.MethodPut, "/files/wiki/file?path=new-page.md", bytes.NewReader(body))
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesWrite(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(wikiDir, "new-page.md"))
	if err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if string(got) != "# title\nbody" {
		t.Fatalf("file content = %q", got)
	}
	if len(rx.calls) != 1 || rx.calls[0] != "new-page" {
		t.Fatalf("reindex calls = %#v, want [new-page]", rx.calls)
	}
}

func TestFilesWrite_WorkspaceDoesNotTriggerReindex(t *testing.T) {
	deps, _ := newFilesTestDeps(t)
	rx := &stubReindexer{}
	deps.WikiSearch = rx

	body, _ := json.Marshal(fileWriteRequest{Content: "scratch", Encoding: "utf-8"})
	req := httptest.NewRequest(http.MethodPut, "/files/workspace/file?path=scratch.txt", bytes.NewReader(body))
	req.SetPathValue("root", "workspace")
	w := httptest.NewRecorder()
	handleFilesWrite(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(rx.calls) != 0 {
		t.Fatalf("workspace write should not reindex; got calls=%#v", rx.calls)
	}
}

func TestFilesDelete_BlocksRecursiveSourceDelete(t *testing.T) {
	deps, wikiDir := newFilesTestDeps(t)
	srcDir := filepath.Join(wikiDir, "raw", "src_test")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/files/sources/file?path=src_test", nil)
	req.SetPathValue("root", "sources")
	w := httptest.NewRecorder()
	handleFilesDelete(deps)(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 to push the operator at DELETE /sources/{id}", w.Code)
	}
	if !strings.Contains(w.Body.String(), "memoryindex") {
		t.Fatalf("body = %s, want memoryindex guidance", w.Body.String())
	}
	// Directory must still exist.
	if _, err := os.Stat(srcDir); err != nil {
		t.Fatalf("source dir vanished anyway: %v", err)
	}
}

// TestFilesPanel_RecursiveTreeShape pins the exact JSON the dashboard's
// FilesPanel consumes from GET /files/{root}/tree?recursive=true. svar's
// React Filemanager expects every directory in the response to appear as
// its own entry (so the tree can be reconstructed from `name`/parent
// inference) AND every nested file to come back with a forward-slash
// path relative to the root. Regressing either invariant produces a
// non-functional file manager in the browser, which is hard to catch
// from pure unit tests on individual handlers — hence this end-to-end
// contract test.
func TestFilesPanel_RecursiveTreeShape(t *testing.T) {
	deps, wikiDir := newFilesTestDeps(t)
	mustWrite := func(rel, body string) {
		t.Helper()
		path := filepath.Join(wikiDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mustWrite("alpha.md", "a")
	mustWrite("notes/idea.md", "b")
	mustWrite("notes/nested/deep.md", "c")

	req := httptest.NewRequest(http.MethodGet, "/files/wiki/tree?recursive=true", nil)
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesList(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var entries []fileEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Build a set so we can assert by name without caring about order.
	got := map[string]string{}
	for _, e := range entries {
		got[e.Name] = e.Type
	}
	want := map[string]string{
		"alpha.md":              "file",
		"notes":                 "dir",
		"notes/idea.md":         "file",
		"notes/nested":          "dir",
		"notes/nested/deep.md":  "file",
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("entry %q: type=%q, want %q (full=%+v)", name, got[name], typ, entries)
		}
	}

	// Forward slashes only — svar uses lastIndexOf('/') to derive parents.
	for _, e := range entries {
		if strings.Contains(e.Name, "\\") {
			t.Errorf("entry name contains backslash, breaks tree parsing: %q", e.Name)
		}
	}
}

// TestFilesPanel_FullCRUDFlow exercises the end-to-end sequence the
// dashboard performs: list (empty) → write → list (sees file) → read
// → delete → list (empty again). This is the contract FilesPanel.tsx
// expects from every root.
func TestFilesPanel_FullCRUDFlow(t *testing.T) {
	deps, _ := newFilesTestDeps(t)

	listRecursive := func() []fileEntry {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/files/workspace/tree?recursive=true", nil)
		req.SetPathValue("root", "workspace")
		w := httptest.NewRecorder()
		handleFilesList(deps)(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("list status = %d body = %s", w.Code, w.Body.String())
		}
		var out []fileEntry
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("list decode: %v", err)
		}
		return out
	}

	if got := listRecursive(); len(got) != 0 {
		t.Fatalf("initial list = %+v, want empty", got)
	}

	body, _ := json.Marshal(fileWriteRequest{Content: "scratch", Encoding: "utf-8"})
	req := httptest.NewRequest(http.MethodPut, "/files/workspace/file?path=scratch.txt", bytes.NewReader(body))
	req.SetPathValue("root", "workspace")
	w := httptest.NewRecorder()
	handleFilesWrite(deps)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("write status = %d body = %s", w.Code, w.Body.String())
	}

	after := listRecursive()
	if len(after) != 1 || after[0].Name != "scratch.txt" || after[0].Type != "file" {
		t.Fatalf("post-write list = %+v, want [scratch.txt file]", after)
	}

	req = httptest.NewRequest(http.MethodGet, "/files/workspace/file?path=scratch.txt", nil)
	req.SetPathValue("root", "workspace")
	w = httptest.NewRecorder()
	handleFilesRead(deps)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("read status = %d body = %s", w.Code, w.Body.String())
	}
	var read fileReadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &read); err != nil {
		t.Fatalf("read decode: %v", err)
	}
	if read.Content != "scratch" || read.Encoding != "utf-8" {
		t.Fatalf("read = %+v, want content=scratch utf-8", read)
	}

	req = httptest.NewRequest(http.MethodDelete, "/files/workspace/file?path=scratch.txt", nil)
	req.SetPathValue("root", "workspace")
	w = httptest.NewRecorder()
	handleFilesDelete(deps)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d body = %s", w.Code, w.Body.String())
	}

	if final := listRecursive(); len(final) != 0 {
		t.Fatalf("post-delete list = %+v, want empty", final)
	}
}

func TestResolveFileRoot_RejectsTraversal(t *testing.T) {
	deps, _ := newFilesTestDeps(t)
	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "subdir/../../escape"} {
		if _, _, err := resolveFileRoot(deps, "wiki", bad); err == nil {
			t.Fatalf("resolveFileRoot(%q) = nil err, want rejection", bad)
		}
	}
}

func TestResolveFileRoot_UnknownRoot(t *testing.T) {
	deps, _ := newFilesTestDeps(t)
	if _, _, err := resolveFileRoot(deps, "bogus", ""); err == nil {
		t.Fatal("expected unknown-root error")
	}
}
