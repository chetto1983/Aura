package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	source "github.com/aura/aura/internal/storage/sources/store"
)

func cascadeTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recordingPurger captures PurgeSource invocations so tests can assert
// the cascade calls the memoryindex purger exactly once.
type recordingPurger struct {
	ids []string
	err error
}

func (r *recordingPurger) PurgeSource(_ context.Context, id string) error {
	r.ids = append(r.ids, id)
	return r.err
}

// seedWikiPage writes a .md file with the given frontmatter sources list.
func seedWikiPage(t *testing.T, e *testEnv, slug string, sources []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\ntitle: " + slug + "\nsources:\n")
	for _, s := range sources {
		b.WriteString("  - ")
		b.WriteString(s)
		b.WriteByte('\n')
	}
	b.WriteString("schema_version: 3\n---\n# " + slug + "\nbody\n")
	if err := os.WriteFile(filepath.Join(e.wiki.Dir(), slug+".md"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("seed page %s: %v", slug, err)
	}
}

func TestHandleSourceDelete_CascadesTrackedWikiPages(t *testing.T) {
	e := newTestEnv(t)
	rec := e.seedSource([]byte("workbook"), source.KindXLSX, "Programma base.xlsx")

	// Materialise a fake "section page" set and pin them to the source's
	// WikiPages so the cascade has something to delete.
	tracked := []string{"source-programma-base", "source-programma-base-001-row-1", "source-programma-base-002-row-2"}
	for _, slug := range tracked {
		seedWikiPage(t, e, slug, []string{"source:" + rec.ID})
	}
	if _, err := e.sources.Update(rec.ID, func(s *source.Source) error {
		s.WikiPages = tracked
		s.Status = source.StatusIngested
		return nil
	}); err != nil {
		t.Fatalf("update wiki_pages: %v", err)
	}

	purger := &recordingPurger{}
	deps := Deps{
		Wiki:         e.wiki,
		Sources:      e.sources,
		SourcePurger: purger,
		Logger:       cascadeTestLogger(),
	}

	req := httptest.NewRequest(http.MethodDelete, "/sources/"+rec.ID, nil)
	req.SetPathValue("id", rec.ID)
	w := httptest.NewRecorder()
	handleSourceDelete(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}

	var resp DeleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.MemoryPurged {
		t.Fatalf("memory_purged = false, response = %+v", resp)
	}
	if len(resp.WikiPagesDeleted) != len(tracked) {
		t.Fatalf("wiki_pages_deleted = %v, want %v", resp.WikiPagesDeleted, tracked)
	}
	if len(resp.WikiPagesFailed) != 0 {
		t.Fatalf("wiki_pages_failed = %v, want none", resp.WikiPagesFailed)
	}
	if len(purger.ids) != 1 || purger.ids[0] != rec.ID {
		t.Fatalf("purger calls = %v, want [%s]", purger.ids, rec.ID)
	}

	// Pages are truly gone from disk.
	for _, slug := range tracked {
		if _, err := os.Stat(filepath.Join(e.wiki.Dir(), slug+".md")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("page %s still on disk after cascade: err=%v", slug, err)
		}
	}
}

func TestHandleSourceDelete_ReportsOrphanReferences(t *testing.T) {
	e := newTestEnv(t)
	rec := e.seedSource([]byte("pdf"), source.KindPDF, "thesis.pdf")

	// One tracked page (cascade-deleted) + one concept page that still
	// references the source from frontmatter but is NOT in WikiPages —
	// must surface as an orphan reference, not be deleted.
	seedWikiPage(t, e, "source-thesis", []string{"source:" + rec.ID})
	seedWikiPage(t, e, "shared-concept", []string{"source:" + rec.ID, "source:src_other"})

	if _, err := e.sources.Update(rec.ID, func(s *source.Source) error {
		s.WikiPages = []string{"source-thesis"}
		s.Status = source.StatusIngested
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	deps := Deps{
		Wiki:    e.wiki,
		Sources: e.sources,
		Logger:  cascadeTestLogger(),
	}
	req := httptest.NewRequest(http.MethodDelete, "/sources/"+rec.ID, nil)
	req.SetPathValue("id", rec.ID)
	w := httptest.NewRecorder()
	handleSourceDelete(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp DeleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.WikiPagesDeleted) != 1 || resp.WikiPagesDeleted[0] != "source-thesis" {
		t.Fatalf("wiki_pages_deleted = %v, want [source-thesis]", resp.WikiPagesDeleted)
	}
	if len(resp.OrphanReferences) != 1 || resp.OrphanReferences[0] != "shared-concept" {
		t.Fatalf("orphan_references = %v, want [shared-concept]", resp.OrphanReferences)
	}

	// shared-concept must survive — operator decides whether to prune it
	// since it still references src_other.
	if _, err := os.Stat(filepath.Join(e.wiki.Dir(), "shared-concept.md")); err != nil {
		t.Fatalf("shared concept page should still exist: %v", err)
	}
}

func TestHandleSourceDelete_NotFoundReturns404(t *testing.T) {
	e := newTestEnv(t)
	deps := Deps{
		Wiki:    e.wiki,
		Sources: e.sources,
		Logger:  cascadeTestLogger(),
	}
	// Use a syntactically valid src_<16-hex> id that simply doesn't exist
	// in the store, so the regex check passes and the Get-before-delete
	// path hits the not-found branch.
	ghost := "src_dead0000beef0001"
	req := httptest.NewRequest(http.MethodDelete, "/sources/"+ghost, nil)
	req.SetPathValue("id", ghost)
	w := httptest.NewRecorder()
	handleSourceDelete(deps)(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s, want 404", w.Code, w.Body.String())
	}
}

func TestHandleSourceDerived_ReturnsTrackedAndReferencing(t *testing.T) {
	e := newTestEnv(t)
	rec := e.seedSource([]byte("workbook"), source.KindXLSX, "data.xlsx")

	seedWikiPage(t, e, "source-data", []string{"source:" + rec.ID})
	seedWikiPage(t, e, "source-data-001-row-1", []string{"source:" + rec.ID})
	seedWikiPage(t, e, "shared-concept", []string{"source:" + rec.ID})

	if _, err := e.sources.Update(rec.ID, func(s *source.Source) error {
		s.WikiPages = []string{"source-data", "source-data-001-row-1"}
		s.Status = source.StatusIngested
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	deps := Deps{Wiki: e.wiki, Sources: e.sources, Logger: cascadeTestLogger()}

	req := httptest.NewRequest(http.MethodGet, "/sources/"+rec.ID+"/derived", nil)
	req.SetPathValue("id", rec.ID)
	w := httptest.NewRecorder()
	handleSourceDerived(deps)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp DerivedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != rec.ID || resp.Filename != "data.xlsx" {
		t.Fatalf("response identity = %+v, want id=%s file=data.xlsx", resp, rec.ID)
	}
	if len(resp.TrackedPages) != 2 {
		t.Fatalf("tracked_pages = %v, want 2", resp.TrackedPages)
	}
	// referencing_pages is the full scan — includes the tracked ones AND
	// the orphan-shared one.
	found := map[string]bool{}
	for _, slug := range resp.ReferencingPages {
		found[slug] = true
	}
	for _, want := range []string{"source-data", "source-data-001-row-1", "shared-concept"} {
		if !found[want] {
			t.Fatalf("referencing_pages missing %q: got %v", want, resp.ReferencingPages)
		}
	}
}

func TestHandleFilesDelete_WikiMarkdownGoesThroughStoreDeletePage(t *testing.T) {
	e := newTestEnv(t)
	seedWikiPage(t, e, "to-be-deleted", nil)

	// Sanity: the wiki store sees the page first.
	if _, err := e.wiki.ReadPage("to-be-deleted"); err != nil {
		t.Fatalf("ReadPage pre-delete: %v", err)
	}

	deps := Deps{
		Wiki:    e.wiki,
		WikiDir: e.wiki.Dir(),
		Logger:  cascadeTestLogger(),
	}
	req := httptest.NewRequest(http.MethodDelete, "/files/wiki/file?path=to-be-deleted.md", nil)
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesDelete(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}

	// Through Store.DeletePage: file is gone AND the read returns ErrNotExist
	// shaped through the wiki Store (not raw os.ErrNotExist), so the FTS5
	// + GraphIndex + Qdrant + git + TOC hooks all fired.
	if _, err := os.Stat(filepath.Join(e.wiki.Dir(), "to-be-deleted.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("page still on disk: err=%v", err)
	}
	if _, err := e.wiki.ReadPage("to-be-deleted"); err == nil {
		t.Fatal("wiki.ReadPage should error for deleted page")
	}
}

func TestHandleFilesBulkDelete_WikiMarkdownGoesThroughStoreDeletePage(t *testing.T) {
	e := newTestEnv(t)
	pages := []string{"alpha-page", "beta-page", "gamma-page"}
	for _, slug := range pages {
		seedWikiPage(t, e, slug, nil)
	}

	deps := Deps{
		Wiki:    e.wiki,
		WikiDir: e.wiki.Dir(),
		Logger:  cascadeTestLogger(),
	}
	body, _ := json.Marshal(fileBulkDeleteRequest{Paths: []string{"alpha-page.md", "beta-page.md", "gamma-page.md"}})
	req := httptest.NewRequest(http.MethodPost, "/files/wiki/delete-many", io.NopCloser(strings.NewReader(string(body))))
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesBulkDelete(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp fileBulkDeleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deleted) != len(pages) {
		t.Fatalf("deleted = %v, want %v", resp.Deleted, pages)
	}
	if len(resp.Failed) != 0 {
		t.Fatalf("failed = %v, want none", resp.Failed)
	}
	for _, slug := range pages {
		if _, err := os.Stat(filepath.Join(e.wiki.Dir(), slug+".md")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("page %s still on disk: err=%v", slug, err)
		}
		if _, err := e.wiki.ReadPage(slug); err == nil {
			t.Fatalf("wiki.ReadPage(%s) succeeded after bulk delete", slug)
		}
	}
}

func TestHandleFilesBulkDelete_ReportsPartialFailure(t *testing.T) {
	e := newTestEnv(t)
	seedWikiPage(t, e, "real-page", nil)

	deps := Deps{
		Wiki:    e.wiki,
		WikiDir: e.wiki.Dir(),
		Logger:  cascadeTestLogger(),
	}
	body, _ := json.Marshal(fileBulkDeleteRequest{
		Paths: []string{"real-page.md", "ghost-page.md", "another-ghost.md"},
	})
	req := httptest.NewRequest(http.MethodPost, "/files/wiki/delete-many", io.NopCloser(strings.NewReader(string(body))))
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesBulkDelete(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp fileBulkDeleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deleted) != 1 || resp.Deleted[0] != "real-page.md" {
		t.Fatalf("deleted = %v, want [real-page.md]", resp.Deleted)
	}
	if len(resp.Failed) != 2 {
		t.Fatalf("failed = %v, want 2 entries", resp.Failed)
	}
}

func TestHandleFilesBulkDelete_RejectsEmptyAndOversize(t *testing.T) {
	e := newTestEnv(t)
	deps := Deps{Wiki: e.wiki, WikiDir: e.wiki.Dir(), Logger: cascadeTestLogger()}

	// Empty paths array.
	body, _ := json.Marshal(fileBulkDeleteRequest{Paths: []string{}})
	req := httptest.NewRequest(http.MethodPost, "/files/wiki/delete-many", io.NopCloser(strings.NewReader(string(body))))
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesBulkDelete(deps)(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty paths: status = %d, want 400", w.Code)
	}

	// Over the cap.
	over := make([]string, fileBulkDeleteMax+1)
	for i := range over {
		over[i] = fmt.Sprintf("page-%d.md", i)
	}
	body2, _ := json.Marshal(fileBulkDeleteRequest{Paths: over})
	req2 := httptest.NewRequest(http.MethodPost, "/files/wiki/delete-many", io.NopCloser(strings.NewReader(string(body2))))
	req2.SetPathValue("root", "wiki")
	w2 := httptest.NewRecorder()
	handleFilesBulkDelete(deps)(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("oversize paths: status = %d, want 400", w2.Code)
	}
}

func TestHandleFilesDelete_WikiNonMarkdownStillUsesOsRemove(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.wiki.Dir(), "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write scratch: %v", err)
	}

	deps := Deps{
		Wiki:    e.wiki,
		WikiDir: e.wiki.Dir(),
		Logger:  cascadeTestLogger(),
	}
	req := httptest.NewRequest(http.MethodDelete, "/files/wiki/file?path=scratch.txt", nil)
	req.SetPathValue("root", "wiki")
	w := httptest.NewRecorder()
	handleFilesDelete(deps)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(e.wiki.Dir(), "scratch.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch.txt still on disk: err=%v", err)
	}
}

