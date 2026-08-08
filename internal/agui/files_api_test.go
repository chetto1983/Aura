package agui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
)

const fileAPIIdentityID = "11111111-1111-4111-8111-111111111111"

type fakeFileBrowser struct {
	identity string
	prefix   string
	result   assets.BrowseResult
	err      error
}

func (f *fakeFileBrowser) List(
	_ context.Context, identityID, prefix string, _ int,
) (assets.BrowseResult, error) {
	f.identity, f.prefix = identityID, prefix
	return f.result, f.err
}

type fakeFileOpener struct {
	identity string
	key      string
	body     string
	err      error
}

func (f *fakeFileOpener) OpenObject(
	_ context.Context, identityID, key string,
) (io.ReadCloser, error) {
	f.identity, f.key = identityID, key
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(f.body)), nil
}

func fileServer(browser FileBrowser, opener FileObjectOpener) *Server {
	server := &Server{}
	server.files = browser
	server.fileObjects = opener
	return server
}

func serveFiles(t *testing.T, server *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	server.registerFileRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, target, nil), fileAPIIdentityID))
	return rec
}

func decodeEntries(t *testing.T, rec *httptest.ResponseRecorder) []fileEntry {
	t.Helper()
	var entries []fileEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return entries
}

func browsePage() assets.BrowseResult {
	return assets.BrowseResult{
		Bucket: "aura-assets",
		Prefix: "contabilita/",
		Entries: []assets.BrowseEntry{
			{Key: "2026/", Folder: true},
			{
				Key:        "fattura-acme.pdf",
				SizeBytes:  22083,
				ModifiedAt: time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC),
			},
		},
	}
}

// A folder id must be the WHOLE path, because the widget derives every row's parent by
// splitting its id -- an id relative to the folder being viewed would reparent the row to
// the root and the tree would flatten.
func TestFileListReturnsAbsoluteIDs(t *testing.T) {
	browser := &fakeFileBrowser{result: browsePage()}
	rec := serveFiles(t, fileServer(browser, nil), fileManagerBase+"/files/"+"%2Fcontabilita")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	entries := decodeEntries(t, rec)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].ID != "/contabilita/2026" || entries[0].Type != "folder" || !entries[0].Lazy {
		t.Fatalf("folder entry = %+v", entries[0])
	}
	// A folder is not an object: reporting a zero size or a zero date would be inventing one.
	if entries[0].Size != 0 || entries[0].Date != "" {
		t.Fatalf("folder carried object facts: %+v", entries[0])
	}
	if entries[1].ID != "/contabilita/fattura-acme.pdf" || entries[1].Type != "file" ||
		entries[1].Size != 22083 || entries[1].Date != "2026-06-11T10:00:00Z" {
		t.Fatalf("file entry = %+v", entries[1])
	}
}

// The percent-encoded id the widget sends must arrive as a real path. If it did not, every
// folder below the root would list the bucket root instead of itself.
func TestFileListPassesTheDecodedFolderAndTheCallerIdentity(t *testing.T) {
	browser := &fakeFileBrowser{result: browsePage()}
	serveFiles(t, fileServer(browser, nil), fileManagerBase+"/files/"+"%2Fcontabilita%2F2026")
	if browser.prefix != "/contabilita/2026" {
		t.Fatalf("prefix = %q", browser.prefix)
	}
	if browser.identity != fileAPIIdentityID {
		t.Fatalf("identity = %q, want the authenticated principal", browser.identity)
	}

	browser = &fakeFileBrowser{result: assets.BrowseResult{}}
	serveFiles(t, fileServer(browser, nil), fileManagerBase+"/files")
	if browser.prefix != "" {
		t.Fatalf("root prefix = %q, want empty", browser.prefix)
	}
}

// An empty folder must encode as [], not null: the widget reads null as a failed load and
// leaves the folder spinning rather than showing it as empty.
func TestFileListEncodesAnEmptyFolderAsAList(t *testing.T) {
	rec := serveFiles(t, fileServer(&fakeFileBrowser{}, nil), fileManagerBase+"/files")
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestFileListRefusesWithoutAPrincipalOrAProvider(t *testing.T) {
	mux := http.NewServeMux()
	fileServer(&fakeFileBrowser{}, nil).registerFileRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fileManagerBase+"/files", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous listing = %d, want 401", rec.Code)
	}

	// An unwired browser is a DEPLOYMENT fault and must not read as "this identity has no
	// files", which is what an empty success would say.
	if got := serveFiles(t, &Server{}, fileManagerBase+"/files").Code; got != http.StatusServiceUnavailable {
		t.Fatalf("unwired listing = %d, want 503", got)
	}
	if got := serveFiles(t, &Server{}, fileManagerBase+"/direct?id=x").Code; got != http.StatusServiceUnavailable {
		t.Fatalf("unwired download = %d, want 503", got)
	}
}

func TestFileListSurfacesBrowseFailures(t *testing.T) {
	browser := &fakeFileBrowser{err: errors.New("bucket unreachable")}
	if got := serveFiles(t, fileServer(browser, nil), fileManagerBase+"/files").Code; got != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got)
	}
}

// Downloads are the stored-XSS surface: user-supplied bytes served from the cockpit's own
// origin. The response must never invite the browser to render them.
func TestFileDirectServesAnAttachmentAndNeverTheObjectsOwnType(t *testing.T) {
	opener := &fakeFileOpener{body: "<script>alert(1)</script>"}
	rec := serveFiles(t, fileServer(nil, opener),
		fileManagerBase+"/direct?id=%2Fcontabilita%2Fevil.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("Content-Disposition = %q, want an attachment", got)
	}
	if rec.Body.String() != opener.body {
		t.Fatalf("body = %q", rec.Body.String())
	}
	// The leading slash of the widget's id is not part of the S3 key: a key that started
	// with one would match nothing and read as a missing file.
	if opener.key != "contabilita/evil.html" {
		t.Fatalf("key = %q", opener.key)
	}
	if opener.identity != fileAPIIdentityID {
		t.Fatalf("identity = %q, want the authenticated principal", opener.identity)
	}
}

func TestFileDirectValidatesItsInput(t *testing.T) {
	opener := &fakeFileOpener{}
	if got := serveFiles(t, fileServer(nil, opener), fileManagerBase+"/direct").Code; got != http.StatusBadRequest {
		t.Fatalf("missing id = %d, want 400", got)
	}

	// Not-found and not-owned collapse to the same 404 so the response cannot be used to
	// probe which keys exist in somebody else's bucket.
	failing := &fakeFileOpener{err: errors.New("no such key")}
	if got := serveFiles(t, fileServer(nil, failing), fileManagerBase+"/direct?id=x").Code; got != http.StatusNotFound {
		t.Fatalf("unreadable object = %d, want 404", got)
	}
}
