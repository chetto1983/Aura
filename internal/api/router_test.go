package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/wiki"
)

// testEnv builds an isolated wiki + source + scheduler trio under t.TempDir
// so each test gets a clean slate.
type testEnv struct {
	t       *testing.T
	dir     string
	wiki    *wiki.Store
	sources *source.Store
	sched   *scheduler.Store
	router  http.Handler
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wikiStore, err := wiki.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	sourceStore, err := source.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("source store: %v", err)
	}
	schedStore, err := scheduler.OpenStore(filepath.Join(dir, "sched.db"))
	if err != nil {
		t.Fatalf("scheduler store: %v", err)
	}
	t.Cleanup(func() { schedStore.Close() })

	router := NewRouter(Deps{
		Wiki:      wikiStore,
		Sources:   sourceStore,
		Scheduler: schedStore,
	})
	return &testEnv{
		t:       t,
		dir:     dir,
		wiki:    wikiStore,
		sources: sourceStore,
		sched:   schedStore,
		router:  router,
	}
}

func (e *testEnv) seedPage(title, body, category string, related []string) *wiki.Page {
	e.t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         title,
		Body:          body,
		Category:      category,
		Related:       related,
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := e.wiki.WritePage(context.Background(), page); err != nil {
		e.t.Fatalf("seed page %q: %v", title, err)
	}
	return page
}

func (e *testEnv) seedSource(content []byte, kind source.Kind, filename string) *source.Source {
	e.t.Helper()
	rec, _, err := e.sources.Put(context.Background(), source.PutInput{
		Kind:     kind,
		Filename: filename,
		MimeType: "application/pdf",
		Bytes:    content,
	})
	if err != nil {
		e.t.Fatalf("seed source: %v", err)
	}
	return rec
}

func (e *testEnv) seedTask(name string, kind scheduler.TaskKind, status scheduler.Status, nextRun time.Time) *scheduler.Task {
	e.t.Helper()
	t := &scheduler.Task{
		Name:         name,
		Kind:         kind,
		ScheduleKind: scheduler.ScheduleAt,
		ScheduleAt:   nextRun.UTC(),
		NextRunAt:    nextRun.UTC(),
		Status:       status,
	}
	got, err := e.sched.Upsert(context.Background(), t)
	if err != nil {
		e.t.Fatalf("seed task: %v", err)
	}
	return got
}

func (e *testEnv) do(method, path string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func TestHealthRollup_EmptyStores(t *testing.T) {
	e := newTestEnv(t)
	rr := e.do("GET", "/health")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got HealthRollup
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Wiki.Pages != 0 {
		t.Errorf("wiki.pages = %d, want 0", got.Wiki.Pages)
	}
	if got.Scheduler.NextRun != nil {
		t.Errorf("next_run = %v, want nil", got.Scheduler.NextRun)
	}
	// by_status maps must be present (non-nil) even when empty so the
	// frontend can iterate without a null check.
	if got.Sources.ByStatus == nil {
		t.Error("sources.by_status is nil")
	}
	if got.Tasks.ByStatus == nil {
		t.Error("tasks.by_status is nil")
	}
}

func TestHealthRollup_AggregatesAcrossStores(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Alpha", "body alpha", "notes", nil)
	e.seedPage("Beta", "body beta", "sources", nil)
	e.seedSource([]byte("pdf-bytes-a"), source.KindPDF, "a.pdf")
	e.seedSource([]byte("pdf-bytes-b"), source.KindPDF, "b.pdf")

	now := time.Now().UTC()
	e.seedTask("future-task", scheduler.KindReminder, scheduler.StatusActive, now.Add(time.Hour))
	e.seedTask("past-task", scheduler.KindReminder, scheduler.StatusDone, now.Add(-time.Hour))

	rr := e.do("GET", "/health")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var got HealthRollup
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Wiki.Pages != 2 {
		t.Errorf("wiki.pages = %d, want 2", got.Wiki.Pages)
	}
	if got.Sources.ByStatus[string(source.StatusStored)] != 2 {
		t.Errorf("sources stored = %d, want 2", got.Sources.ByStatus[string(source.StatusStored)])
	}
	if got.Tasks.ByStatus[string(scheduler.StatusActive)] != 1 {
		t.Errorf("tasks active = %d, want 1", got.Tasks.ByStatus[string(scheduler.StatusActive)])
	}
	if got.Scheduler.NextRun == nil {
		t.Fatal("next_run is nil; want future task")
	}
	// Done tasks must not contribute to next_run.
	if got.Scheduler.NextRun.Before(now) {
		t.Errorf("next_run %v is in the past", got.Scheduler.NextRun)
	}
}

func TestWikiPages_SortedByCategoryThenSlug(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Zeta", "z", "notes", nil)
	e.seedPage("Alpha", "a", "notes", nil)
	e.seedPage("Mid", "m", "ideas", nil)

	rr := e.do("GET", "/wiki/pages")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var got []WikiPageSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d pages, want 3", len(got))
	}
	wantOrder := []string{"mid", "alpha", "zeta"} // ideas < notes; alpha < zeta within notes
	for i, w := range wantOrder {
		if got[i].Slug != w {
			t.Errorf("position %d: got %q, want %q", i, got[i].Slug, w)
		}
	}
}

func TestWikiPage_HappyPath(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Hello World", "the body **bold**", "notes", []string{"alpha"})

	rr := e.do("GET", "/wiki/page?slug=hello-world")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got WikiPage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Hello World" {
		t.Errorf("title = %q", got.Title)
	}
	if !strings.Contains(got.BodyMD, "**bold**") {
		t.Errorf("body missing markdown, got %q", got.BodyMD)
	}
	if got.Frontmatter["category"] != "notes" {
		t.Errorf("frontmatter.category = %v", got.Frontmatter["category"])
	}
}

func TestWikiPage_BadInputs(t *testing.T) {
	e := newTestEnv(t)
	cases := []struct {
		name   string
		path   string
		status int
	}{
		{"missing slug", "/wiki/page", http.StatusBadRequest},
		{"empty slug", "/wiki/page?slug=", http.StatusBadRequest},
		{"invalid characters", "/wiki/page?slug=BAD/PATH", http.StatusBadRequest},
		{"path traversal", "/wiki/page?slug=../etc", http.StatusBadRequest},
		{"unknown slug", "/wiki/page?slug=does-not-exist", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.do("GET", tc.path)
			if rr.Code != tc.status {
				t.Errorf("status %d, want %d, body %s", rr.Code, tc.status, rr.Body)
			}
		})
	}
}

func TestWikiGraph_BuildsEdgesAndDropsDangling(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Alpha", "see also [[beta]] and [[ghost]]", "notes", []string{"beta"})
	e.seedPage("Beta", "back to [[alpha]] and [[alpha]]", "notes", nil)
	// "Ghost" intentionally not seeded — edge should be dropped.

	rr := e.do("GET", "/wiki/graph")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var got Graph
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(got.Nodes))
	}
	// Expected edges: alpha->beta wikilink, alpha->beta related (deduped
	// by alpha's seen set so only one survives — first wins is wikilink),
	// beta->alpha wikilink (deduped from two occurrences). Dangling ghost
	// edge dropped.
	wantEdges := map[string]string{
		"alpha->beta": "wikilink",
		"beta->alpha": "wikilink",
	}
	if len(got.Edges) != len(wantEdges) {
		t.Fatalf("edges = %d, want %d: %+v", len(got.Edges), len(wantEdges), got.Edges)
	}
	for _, e := range got.Edges {
		key := e.Source + "->" + e.Target
		if want, ok := wantEdges[key]; !ok {
			t.Errorf("unexpected edge %s", key)
		} else if e.Type != want {
			t.Errorf("edge %s type %s, want %s", key, e.Type, want)
		}
	}
}

func TestWikiGraph_SkipsSelfLoops(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Alpha", "see [[alpha]]", "notes", []string{"alpha"})

	rr := e.do("GET", "/wiki/graph")
	var got Graph
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Edges) != 0 {
		t.Errorf("self-loops not filtered: %+v", got.Edges)
	}
}

func TestSourceList_FilterAndDTO(t *testing.T) {
	e := newTestEnv(t)
	a := e.seedSource([]byte("pdf-a-content"), source.KindPDF, "a.pdf")
	_ = e.seedSource([]byte("pdf-b-content"), source.KindPDF, "b.pdf")

	rr := e.do("GET", "/sources")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var all []SourceSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("got %d, want 2", len(all))
	}

	rr = e.do("GET", "/sources?status=stored")
	var stored []SourceSummary
	_ = json.Unmarshal(rr.Body.Bytes(), &stored)
	if len(stored) != 2 {
		t.Errorf("stored filter %d, want 2", len(stored))
	}

	rr = e.do("GET", "/sources?status=ingested")
	var ingested []SourceSummary
	_ = json.Unmarshal(rr.Body.Bytes(), &ingested)
	if len(ingested) != 0 {
		t.Errorf("ingested filter %d, want 0", len(ingested))
	}

	// DTO sanity check: id round-trips and high-volume fields are stripped.
	hasA := false
	for _, s := range all {
		if s.ID == a.ID {
			hasA = true
			if s.Filename != "a.pdf" {
				t.Errorf("filename = %q", s.Filename)
			}
		}
	}
	if !hasA {
		t.Error("seeded source missing from list")
	}
}

func TestSourceList_RejectsBadFilter(t *testing.T) {
	e := newTestEnv(t)
	rr := e.do("GET", "/sources?status=bogus")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rr.Code)
	}
	rr = e.do("GET", "/sources?kind=video")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("kind status %d, want 400", rr.Code)
	}
}

func TestSourceGet_HappyAndNotFound(t *testing.T) {
	e := newTestEnv(t)
	rec := e.seedSource([]byte("pdf-c-content"), source.KindPDF, "c.pdf")

	rr := e.do("GET", "/sources/"+rec.ID)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var got SourceDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SHA256 == "" || got.SizeBytes == 0 {
		t.Errorf("detail missing high-volume fields: %+v", got)
	}

	rr = e.do("GET", "/sources/src_0000000000000000")
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown id status %d, want 404", rr.Code)
	}
	rr = e.do("GET", "/sources/not-a-source-id")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed id status %d, want 400", rr.Code)
	}
}

func TestSourceOCR_PresentAndMissing(t *testing.T) {
	e := newTestEnv(t)
	rec := e.seedSource([]byte("pdf-d-content"), source.KindPDF, "d.pdf")

	// Without ocr.md the endpoint must 404 even though the source exists.
	rr := e.do("GET", "/sources/"+rec.ID+"/ocr")
	if rr.Code != http.StatusNotFound {
		t.Errorf("missing ocr status %d, want 404", rr.Code)
	}

	// Plant an ocr.md and re-fetch.
	if err := os.WriteFile(e.sources.Path(rec.ID, "ocr.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rr = e.do("GET", "/sources/"+rec.ID+"/ocr")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got SourceOCR
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Markdown, "hello") {
		t.Errorf("ocr body = %q", got.Markdown)
	}
}

func TestSourceRaw_PDFOnly(t *testing.T) {
	e := newTestEnv(t)
	pdf := e.seedSource([]byte("%PDF-1.4 fake content"), source.KindPDF, "doc.pdf")
	txt := e.seedSource([]byte("hello text"), source.KindText, "note.txt")

	rr := e.do("GET", "/sources/"+pdf.ID+"/raw")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("content-type = %q, want application/pdf", ct)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.HasPrefix(string(body), "%PDF") {
		t.Errorf("body did not stream PDF: %q", string(body[:min(20, len(body))]))
	}

	// Text source must not expose a raw PDF endpoint.
	rr = e.do("GET", "/sources/"+txt.ID+"/raw")
	if rr.Code != http.StatusNotFound {
		t.Errorf("text raw status %d, want 404", rr.Code)
	}
}

func TestTaskList_AndFilter(t *testing.T) {
	e := newTestEnv(t)
	now := time.Now().UTC()
	e.seedTask("active-1", scheduler.KindReminder, scheduler.StatusActive, now.Add(time.Hour))
	e.seedTask("done-1", scheduler.KindReminder, scheduler.StatusDone, now.Add(-time.Hour))

	rr := e.do("GET", "/tasks")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var all []Task
	_ = json.Unmarshal(rr.Body.Bytes(), &all)
	if len(all) != 2 {
		t.Errorf("got %d, want 2", len(all))
	}

	rr = e.do("GET", "/tasks?status=active")
	var active []Task
	_ = json.Unmarshal(rr.Body.Bytes(), &active)
	if len(active) != 1 {
		t.Errorf("active filter %d, want 1", len(active))
	}

	rr = e.do("GET", "/tasks?status=bogus")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bogus status code %d, want 400", rr.Code)
	}
}

func TestTaskGet_HappyAndNotFound(t *testing.T) {
	e := newTestEnv(t)
	now := time.Now().UTC()
	e.seedTask("hello", scheduler.KindReminder, scheduler.StatusActive, now.Add(time.Hour))

	rr := e.do("GET", "/tasks/hello")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var got Task
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "hello" {
		t.Errorf("name = %q", got.Name)
	}

	rr = e.do("GET", "/tasks/missing")
	if rr.Code != http.StatusNotFound {
		t.Errorf("missing status %d, want 404", rr.Code)
	}

	rr = e.do("GET", "/tasks/bad@name")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed name status %d, want 400", rr.Code)
	}
}

func TestUnknownPath_Returns404(t *testing.T) {
	e := newTestEnv(t)
	rr := e.do("GET", "/does/not/exist")
	if rr.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rr.Code)
	}
}

func TestMethodNotAllowed_OnReadOnly(t *testing.T) {
	e := newTestEnv(t)
	rr := e.do("POST", "/health")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", rr.Code)
	}
}
