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
