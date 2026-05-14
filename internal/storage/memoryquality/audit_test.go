package memoryquality

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/wiki"
	_ "modernc.org/sqlite"
)

var (
	_ WikiRepository      = (*fakeAuditWiki)(nil)
	_ WikiFileLister      = fakeWikiFiles(nil)
	_ IndexManifestReader = fakeIndexDocs(nil)
)

type fakeAuditWiki struct {
	pages  map[string]*wiki.Page
	lint   []wiki.LintIssue
	broken []wiki.BrokenLink
}

func (f *fakeAuditWiki) Lint(context.Context) ([]wiki.LintIssue, error) {
	return append([]wiki.LintIssue(nil), f.lint...), nil
}

func (f *fakeAuditWiki) CleanMemory(context.Context, wiki.MemoryHygieneOptions) (*wiki.MemoryHygieneReport, error) {
	return &wiki.MemoryHygieneReport{Pages: len(f.pages), BrokenLinks: append([]wiki.BrokenLink(nil), f.broken...)}, nil
}

func (f *fakeAuditWiki) ListPages() ([]string, error) {
	slugs := make([]string, 0, len(f.pages))
	for slug := range f.pages {
		slugs = append(slugs, slug)
	}
	return slugs, nil
}

func (f *fakeAuditWiki) ReadPage(slug string) (*wiki.Page, error) {
	if page, ok := f.pages[slug]; ok {
		cp := *page
		return &cp, nil
	}
	return nil, os.ErrNotExist
}

type fakeWikiFiles []WikiFile

func (f fakeWikiFiles) ListWikiFiles(context.Context) ([]WikiFile, error) {
	return append([]WikiFile(nil), f...), nil
}

type fakeIndexDocs []IndexDocument

func (f fakeIndexDocs) ListIndexDocuments(context.Context) ([]IndexDocument, error) {
	return append([]IndexDocument(nil), f...), nil
}

func TestAuditWithDependenciesAcceptsRepositoryInterfaces(t *testing.T) {
	deps := AuditDependencies{
		WikiDir: "mem://wiki",
		DBPath:  "mem://index",
		Wiki: &fakeAuditWiki{pages: map[string]*wiki.Page{
			"aura-memory":                  {Title: "Aura Memory", Category: "project", Body: "Clean page."},
			"source-4-5942613039617418204": {Title: "Source: opaque", Category: "sources", Body: "Compact source anchor."},
		}},
		WikiFiles: fakeWikiFiles{
			{Name: "aura-memory.md"},
			{Name: "source-4-5942613039617418204.md"},
			{Name: "legacy.yaml"},
		},
		Index: fakeIndexDocs{
			{ID: "aura-memory", Content: "Aura Memory\nClean page.", Metadata: `{"kind":"wiki_page"}`, Title: "Aura Memory"},
			{ID: "raw:src_bad", Content: "raw OCR garbage", Metadata: `{"kind":"raw"}`, Title: "Raw"},
		},
	}

	report, err := AuditWithDependencies(context.Background(), deps)
	if err != nil {
		t.Fatalf("AuditWithDependencies: %v", err)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false")
	}
	if report.Manifest.WikiPages != 2 || report.Manifest.ActualIndexDocs != 2 {
		t.Fatalf("manifest = %+v", report.Manifest)
	}
	for _, want := range []struct {
		kind IssueKind
		ref  string
	}{
		{KindSuspiciousPage, "source-4-5942613039617418204"},
		{KindLegacyYAML, "legacy"},
		{KindUnexpectedIndexDoc, "raw:src_bad"},
	} {
		if !hasIssue(report, want.kind, want.ref) {
			t.Fatalf("missing issue kind=%s ref=%s: %+v", want.kind, want.ref, report.Issues)
		}
	}
}

func writeAuditPage(t *testing.T, store *wiki.Store, page *wiki.Page) string {
	t.Helper()
	if page.SchemaVersion == 0 {
		page.SchemaVersion = wiki.CurrentSchemaVersion
	}
	if page.PromptVersion == "" {
		page.PromptVersion = "v1"
	}
	if page.CreatedAt == "" {
		page.CreatedAt = "2026-05-06T00:00:00Z"
	}
	if page.UpdatedAt == "" {
		page.UpdatedAt = "2026-05-06T00:00:00Z"
	}
	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage(%q): %v", page.Title, err)
	}
	return wiki.Slug(page.Title)
}

func TestAuditFailsWhenCompiledWikiContainsOCRPreview(t *testing.T) {
	wikiDir := t.TempDir()
	store, err := wiki.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	writeAuditPage(t, store, &wiki.Page{
		Title:    "Source: noisy upload",
		Category: "sources",
		Body:     "## Extracted Markdown\n\nFull text pointer.\n\n## Preview\n\nOCR LINE OCR LINE OCR LINE OCR LINE OCR LINE",
	})

	report, err := Audit(context.Background(), Options{WikiDir: wikiDir})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false: %+v", report)
	}
	if !hasIssue(report, KindRawLeak, "source-noisy-upload") {
		t.Fatalf("raw leak issue missing: %+v", report.Issues)
	}
}

func TestAuditFailsOnUnexpectedFTSDocument(t *testing.T) {
	wikiDir := t.TempDir()
	store, err := wiki.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	writeAuditPage(t, store, &wiki.Page{Title: "Aura Memory", Category: "project", Body: "Clean memory."})

	dbPath := filepath.Join(t.TempDir(), "aura.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIRTUAL TABLE wiki_documents USING fts5(id, content, metadata, title)`); err != nil {
		t.Fatalf("create fts: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO wiki_documents(id, content, metadata, title) VALUES (?, ?, ?, ?)`, "raw:src_bad", "raw OCR garbage", `{"kind":"raw","slug":"raw:src_bad"}`, "raw"); err != nil {
		t.Fatalf("insert fts: %v", err)
	}

	report, err := Audit(context.Background(), Options{WikiDir: wikiDir, DBPath: dbPath})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false")
	}
	if !hasIssue(report, KindUnexpectedIndexDoc, "raw:src_bad") {
		t.Fatalf("unexpected index doc issue missing: %+v", report.Issues)
	}
}

func TestAuditPassesForCleanWikiAndExpectedFTSManifest(t *testing.T) {
	wikiDir := t.TempDir()
	store, err := wiki.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	slug := writeAuditPage(t, store, &wiki.Page{Title: "Aura Memory", Category: "project", Body: "Clean memory."})

	dbPath := filepath.Join(t.TempDir(), "aura.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIRTUAL TABLE wiki_documents USING fts5(id, content, metadata, title)`); err != nil {
		t.Fatalf("create fts: %v", err)
	}
	for _, row := range []struct{ id, content, metadata, title string }{
		{slug, "Aura Memory\nClean memory.", `{"kind":"wiki_page","slug":"aura-memory","title":"Aura Memory"}`, "Aura Memory"},
		{"graph:node:" + slug, "Graph node [[aura-memory]]", `{"kind":"graph_node","slug":"aura-memory","title":"Aura Memory"}`, "Aura Memory"},
		{"graph:index:category:project", "Graph index category: project", `{"kind":"graph_index","slug":"index:category:project","title":"Index: project"}`, "Index: project"},
		{"graph:index:all", "Graph index overview", `{"kind":"graph_index","slug":"index:all","title":"Index: all wiki categories"}`, "Index: all"},
	} {
		if _, err := db.Exec(`INSERT INTO wiki_documents(id, content, metadata, title) VALUES (?, ?, ?, ?)`, row.id, row.content, row.metadata, row.title); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	report, err := Audit(context.Background(), Options{WikiDir: wikiDir, DBPath: dbPath})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !report.OK {
		t.Fatalf("report.OK = false: %+v", report.Issues)
	}
	if report.Manifest.WikiPages != 1 || report.Manifest.ExpectedIndexDocs != 4 || report.Manifest.ActualIndexDocs != 4 {
		t.Fatalf("manifest = %+v", report.Manifest)
	}
}

func hasIssue(report Report, kind IssueKind, ref string) bool {
	for _, issue := range report.Issues {
		if issue.Kind == kind && issue.Ref == ref {
			return true
		}
	}
	return false
}
