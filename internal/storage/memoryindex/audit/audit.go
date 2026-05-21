package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/wiki"
)

type Options struct {
	WikiDir string
	DBPath  string
}

type AuditDependencies struct {
	WikiDir   string
	DBPath    string
	Wiki      WikiRepository
	WikiFiles WikiFileLister
	Index     IndexManifestReader
}

type WikiRepository interface {
	wiki.PageReader
	wiki.Linter
	wiki.MemoryCleaner
}

type WikiFile struct {
	Name  string
	IsDir bool
}

type WikiFileLister interface {
	ListWikiFiles(ctx context.Context) ([]WikiFile, error)
}

type IndexDocument struct {
	ID       string
	Content  string
	Metadata string
	Title    string
}

type IndexManifestReader interface {
	ListIndexDocuments(ctx context.Context) ([]IndexDocument, error)
}

type IssueKind string

const (
	KindWikiLint           IssueKind = "wiki_lint"
	KindBrokenMemoryGraph  IssueKind = "broken_memory_graph"
	KindRawLeak            IssueKind = "raw_leak"
	KindYAMLCompatibility  IssueKind = "yaml_compatibility"
	KindSuspiciousPage     IssueKind = "suspicious_page"
	KindIndexMissing       IssueKind = "index_missing"
	KindUnexpectedIndexDoc IssueKind = "unexpected_index_doc"
	KindMissingIndexDoc    IssueKind = "missing_index_doc"
)

type Issue struct {
	Kind     IssueKind `json:"kind"`
	Severity string    `json:"severity"`
	Ref      string    `json:"ref,omitempty"`
	Message  string    `json:"message"`
}

type Manifest struct {
	WikiPages         int      `json:"wiki_pages"`
	Categories        []string `json:"categories,omitempty"`
	ExpectedIndexDocs int      `json:"expected_index_docs"`
	ActualIndexDocs   int      `json:"actual_index_docs,omitempty"`
}

type Report struct {
	OK       bool     `json:"ok"`
	WikiDir  string   `json:"wiki_dir"`
	DBPath   string   `json:"db_path,omitempty"`
	Manifest Manifest `json:"manifest"`
	Issues   []Issue  `json:"issues,omitempty"`
}

type pageRecord struct {
	slug     string
	title    string
	category string
	body     string
}

var opaqueSourceSlugRe = regexp.MustCompile(`^source-\d+-\d+$`)

func Audit(ctx context.Context, opts Options) (Report, error) {
	opts.WikiDir = strings.TrimSpace(opts.WikiDir)
	if opts.WikiDir == "" {
		return Report{}, errors.New("memory quality: WikiDir required")
	}

	store, err := wiki.NewStore(opts.WikiDir, nil)
	if err != nil {
		return Report{WikiDir: opts.WikiDir, DBPath: strings.TrimSpace(opts.DBPath)}, err
	}
	deps := AuditDependencies{
		WikiDir:   opts.WikiDir,
		DBPath:    strings.TrimSpace(opts.DBPath),
		Wiki:      store,
		WikiFiles: DirectoryWikiFiles{Dir: opts.WikiDir},
	}
	if deps.DBPath != "" {
		deps.Index = SQLiteIndexManifestReader{DBPath: deps.DBPath}
	}
	return AuditWithDependencies(ctx, deps)
}

func AuditWithDependencies(ctx context.Context, deps AuditDependencies) (Report, error) {
	deps.WikiDir = strings.TrimSpace(deps.WikiDir)
	if deps.WikiDir == "" {
		return Report{}, errors.New("memory quality: WikiDir required")
	}
	report := Report{WikiDir: deps.WikiDir, DBPath: strings.TrimSpace(deps.DBPath)}
	if deps.Wiki == nil {
		return report, errors.New("memory quality: wiki repository required")
	}
	if deps.WikiFiles == nil {
		return report, errors.New("memory quality: wiki file lister required")
	}
	pages, issues, err := scanWiki(ctx, deps.Wiki, deps.WikiFiles)
	if err != nil {
		return report, err
	}
	report.Issues = append(report.Issues, issues...)
	report.Manifest = buildManifest(pages)

	if deps.Index != nil {
		docs, err := deps.Index.ListIndexDocuments(ctx)
		if errors.Is(err, errIndexMissing) {
			report.Manifest.ActualIndexDocs = 0
			report.Issues = append(report.Issues, Issue{Kind: KindIndexMissing, Severity: "high", Message: "wiki_documents index table is missing"})
			sortIssues(report.Issues)
			report.OK = len(report.Issues) == 0
			return report, nil
		}
		if err != nil {
			return report, err
		}
		dbIssues, actual := auditIndexDocuments(docs, expectedIndexIDs(pages))
		report.Manifest.ActualIndexDocs = actual
		report.Issues = append(report.Issues, dbIssues...)
	}

	sortIssues(report.Issues)
	report.OK = len(report.Issues) == 0
	return report, nil
}

func scanWiki(ctx context.Context, store WikiRepository, files WikiFileLister) ([]pageRecord, []Issue, error) {
	var issues []Issue
	lintIssues, err := store.Lint(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, issue := range lintIssues {
		if issue.Kind == "memory_decay" || issue.Kind == "missing_category" {
			continue
		}
		issues = append(issues, Issue{
			Kind:     KindWikiLint,
			Severity: issue.Severity,
			Ref:      issue.Slug,
			Message:  issue.Message,
		})
	}

	hygiene, err := store.CleanMemory(ctx, wiki.MemoryHygieneOptions{})
	if err != nil {
		return nil, nil, err
	}
	for _, broken := range hygiene.BrokenLinks {
		issues = append(issues, Issue{
			Kind:     KindBrokenMemoryGraph,
			Severity: "high",
			Ref:      broken.From,
			Message:  "broken " + broken.Location + " reference [[" + broken.Target + "]]",
		})
	}

	entries, err := files.ListWikiFiles(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		name := entry.Name
		slug := strings.TrimSuffix(name, filepath.Ext(name))
		if strings.HasSuffix(name, ".yaml") && !wiki.IsOperationalSlug(slug) {
			issues = append(issues, Issue{Kind: KindYAMLCompatibility, Severity: "high", Ref: slug, Message: ".yaml page must be migrated before memory closure"})
		}
		if strings.HasSuffix(name, ".md") && opaqueSourceSlugRe.MatchString(slug) {
			issues = append(issues, Issue{Kind: KindSuspiciousPage, Severity: "medium", Ref: slug, Message: "opaque source slug should be renamed or quarantined before embedding"})
		}
	}

	slugs, err := store.ListPages()
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(slugs)
	pages := make([]pageRecord, 0, len(slugs))
	for _, slug := range slugs {
		page, err := store.ReadPage(slug)
		if err != nil {
			issues = append(issues, Issue{Kind: KindWikiLint, Severity: "high", Ref: slug, Message: "failed to read page for closure audit: " + err.Error()})
			continue
		}
		pages = append(pages, pageRecord{slug: slug, title: page.Title, category: cleanCategory(page.Category), body: page.Body})
		if rawLeakMessage(page.Body) != "" {
			issues = append(issues, Issue{Kind: KindRawLeak, Severity: "critical", Ref: slug, Message: rawLeakMessage(page.Body)})
		}
	}
	return pages, issues, nil
}

type DirectoryWikiFiles struct {
	Dir string
}

func (d DirectoryWikiFiles) ListWikiFiles(context.Context) ([]WikiFile, error) {
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return nil, err
	}
	files := make([]WikiFile, 0, len(entries))
	for _, entry := range entries {
		files = append(files, WikiFile{Name: entry.Name(), IsDir: entry.IsDir()})
	}
	return files, nil
}

func rawLeakMessage(body string) string {
	if strings.Contains(body, "\n## Preview\n") || strings.HasPrefix(body, "## Preview\n") {
		return "compiled wiki page contains raw OCR/source preview; keep raw text under wiki/raw and source(action=\"read\")"
	}
	if strings.Count(body, "â") >= 3 {
		return "compiled wiki page contains repeated replacement characters, likely bad extraction text"
	}
	return ""
}

func buildManifest(pages []pageRecord) Manifest {
	catSet := map[string]bool{}
	for _, page := range pages {
		catSet[page.category] = true
	}
	cats := make([]string, 0, len(catSet))
	for cat := range catSet {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	return Manifest{
		WikiPages:         len(pages),
		Categories:        cats,
		ExpectedIndexDocs: len(pages)*2 + len(cats) + 1,
	}
}

func expectedIndexIDs(pages []pageRecord) map[string]bool {
	out := map[string]bool{"graph:index:all": true}
	cats := map[string]bool{}
	for _, page := range pages {
		out[page.slug] = true
		out["graph:node:"+page.slug] = true
		cats[page.category] = true
	}
	for cat := range cats {
		out["graph:index:category:"+safeGraphID(cat)] = true
	}
	return out
}

type SQLiteIndexManifestReader struct {
	DBPath string
}

func (r SQLiteIndexManifestReader) ListIndexDocuments(ctx context.Context) ([]IndexDocument, error) {
	db, err := auradb.Open(r.DBPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	if !tableExists(ctx, db, "wiki_documents") {
		return nil, errIndexMissing
	}

	rows, err := db.QueryContext(ctx, `SELECT id, content, metadata, title FROM wiki_documents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []IndexDocument
	for rows.Next() {
		var id, content, metadata, title string
		if err := rows.Scan(&id, &content, &metadata, &title); err != nil {
			return nil, err
		}
		docs = append(docs, IndexDocument{ID: id, Content: content, Metadata: metadata, Title: title})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

var errIndexMissing = errors.New("wiki_documents index table is missing")

func auditIndexDocuments(docs []IndexDocument, expected map[string]bool) ([]Issue, int) {
	actual := map[string]bool{}
	var issues []Issue
	for _, doc := range docs {
		actual[doc.ID] = true
		if !expected[doc.ID] {
			issues = append(issues, Issue{Kind: KindUnexpectedIndexDoc, Severity: "critical", Ref: doc.ID, Message: "index contains document outside current wiki manifest"})
		}
		if rawLeakMessage(doc.Content) != "" {
			issues = append(issues, Issue{Kind: KindRawLeak, Severity: "critical", Ref: doc.ID, Message: "indexed document contains raw OCR/source preview"})
		}
		if indexMetadataKind(doc.Metadata) == "raw" {
			issues = append(issues, Issue{Kind: KindUnexpectedIndexDoc, Severity: "critical", Ref: doc.ID, Message: "index metadata marks document as raw"})
		}
	}
	for id := range expected {
		if !actual[id] {
			issues = append(issues, Issue{Kind: KindMissingIndexDoc, Severity: "high", Ref: id, Message: "expected wiki/graph document is missing from index"})
		}
	}
	return issues, len(actual)
}

func tableExists(ctx context.Context, db *sql.DB, name string) bool {
	var got string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type IN ('table', 'virtual table') AND name = ?`, name).Scan(&got)
	return err == nil && got == name
}

func indexMetadataKind(raw string) string {
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return ""
	}
	if kind, ok := meta["kind"].(string); ok {
		return kind
	}
	return ""
}

func cleanCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "uncategorized"
	}
	return category
}

func safeGraphID(value string) string {
	id := wiki.Slug(value)
	if id == "" {
		return "uncategorized"
	}
	return id
}

func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Severity != issues[j].Severity {
			return severityRank(issues[i].Severity) < severityRank(issues[j].Severity)
		}
		if issues[i].Kind != issues[j].Kind {
			return issues[i].Kind < issues[j].Kind
		}
		return issues[i].Ref < issues[j].Ref
	})
}

func severityRank(sev string) int {
	switch sev {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}
