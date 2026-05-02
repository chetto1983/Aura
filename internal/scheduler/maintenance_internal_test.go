package scheduler

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// openTestDBInternal opens a raw SQLite DB with the scheduler + issues migrations
// applied, without going through the scheduler.NewTestDB exported helper (which
// would be a circular call from within the same package).
func openTestDBInternal(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("openTestDBInternal: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("migrate scheduler: %v", err)
	}
	if _, err := db.Exec(conversationsSchemaSQL); err != nil {
		t.Fatalf("migrate conversations: %v", err)
	}
	if _, err := db.Exec(wikiIssuesSchemaSQL); err != nil {
		t.Fatalf("migrate wiki_issues: %v", err)
	}
	return db
}

// TestIssuesStore_Get_ClosedDB covers the non-ErrNoRows error path in Get.
func TestIssuesStore_Get_ClosedDB(t *testing.T) {
	db := openTestDBInternal(t)
	store := &IssuesStore{db: db}
	db.Close()

	_, err := store.Get(context.Background(), 1)
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
}

// TestIssuesList_ScanError covers the scanIssue error path inside the List loop
// by inserting a row with an unparseable created_at via raw SQL.
func TestIssuesList_ScanError(t *testing.T) {
	db := openTestDBInternal(t)
	store := &IssuesStore{db: db}

	_, err := db.Exec(`INSERT INTO wiki_issues (kind, severity, slug, broken_link, message, status, created_at)
		VALUES ('orphan', 'medium', 'p', '', 'msg', 'open', 'BADTIME')`)
	if err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	_, err = store.List(context.Background(), "open")
	if err == nil {
		t.Fatal("want error from unparseable created_at in scan, got nil")
	}
}

// TestClassifyKind_BrokenRelated covers the "broken related" branch in classifyKind.
func TestClassifyKind_BrokenRelated(t *testing.T) {
	got := classifyKind("broken related: [[slug]]")
	if got != "broken_link" {
		t.Fatalf("want broken_link, got %q", got)
	}
}

// TestClassifyKind_MissingCategory covers the missing_category branch.
func TestClassifyKind_MissingCategory(t *testing.T) {
	got := classifyKind("missing category")
	if got != "missing_category" {
		t.Fatalf("want missing_category, got %q", got)
	}
}

// TestClassifyKind_Default covers the orphan default branch.
func TestClassifyKind_Default(t *testing.T) {
	got := classifyKind("orphan page")
	if got != "orphan" {
		t.Fatalf("want orphan, got %q", got)
	}
}

// TestParseBrokenLink_NoClosingBrackets covers the slug==rest (no "]]") path.
func TestParseBrokenLink_NoClosingBrackets(t *testing.T) {
	_, ok := parseBrokenLink("broken link: [[slug-without-close")
	if ok {
		t.Fatal("want false for missing closing ]], got true")
	}
}

// TestParseBrokenLink_NotBrokenLink covers the prefix-mismatch path.
func TestParseBrokenLink_NotBrokenLink(t *testing.T) {
	_, ok := parseBrokenLink("missing category")
	if ok {
		t.Fatal("want false for non-broken-link message, got true")
	}
}

// TestLevenshtein_EmptyA covers the la==0 early return.
func TestLevenshtein_EmptyA(t *testing.T) {
	got := levenshtein("", "abc")
	if got != 3 {
		t.Fatalf("want 3 (len of b), got %d", got)
	}
}

// TestLevenshtein_EmptyB covers the lb==0 early return.
func TestLevenshtein_EmptyB(t *testing.T) {
	got := levenshtein("abc", "")
	if got != 3 {
		t.Fatalf("want 3 (len of a), got %d", got)
	}
}

// TestMin3_BLessThanC covers the b<c return path (b is minimum).
func TestMin3_BLessThanC(t *testing.T) {
	// a=5, b=2, c=3 → b wins (a>b, b<c)
	got := min3(5, 2, 3)
	if got != 2 {
		t.Fatalf("want 2, got %d", got)
	}
}

// TestMin3_CWins covers the c return when b>=c.
func TestMin3_CWins(t *testing.T) {
	// a=5, b=4, c=3 → c wins (a>b, b>=c)
	got := min3(5, 4, 3)
	if got != 3 {
		t.Fatalf("want 3, got %d", got)
	}
}

// TestScanIssue_BadTimestamp covers the parse-failure branch in scanIssue.
func TestScanIssue_BadTimestamp(t *testing.T) {
	row := &fakeIssueScanner{
		values: []any{
			int64(1), "broken_link", "high", "page", "slug", "msg", "open",
			"not-a-timestamp", sql.NullString{Valid: false},
		},
	}
	_, err := scanIssue(row)
	if err == nil {
		t.Fatal("want error for unparseable created_at, got nil")
	}
}

// TestScanIssue_RFC3339CreatedAt covers the RFC3339 fallback branch in scanIssue.
func TestScanIssue_RFC3339CreatedAt(t *testing.T) {
	row := &fakeIssueScanner{
		values: []any{
			int64(1), "orphan", "medium", "page", "", "msg", "open",
			"2026-05-02T10:00:00Z", sql.NullString{Valid: false},
		},
	}
	issue, err := scanIssue(row)
	if err != nil {
		t.Fatalf("scanIssue with RFC3339 created_at: %v", err)
	}
	if issue.CreatedAt.Year() != 2026 {
		t.Fatalf("unexpected year: %d", issue.CreatedAt.Year())
	}
}

// TestScanIssue_ResolvedAt covers the resolvedAt.Valid path in scanIssue.
func TestScanIssue_ResolvedAt(t *testing.T) {
	row := &fakeIssueScanner{
		values: []any{
			int64(2), "orphan", "low", "page", "", "msg", "resolved",
			"2026-05-02 10:00:00",
			sql.NullString{Valid: true, String: "2026-05-02T11:00:00Z"},
		},
	}
	issue, err := scanIssue(row)
	if err != nil {
		t.Fatalf("scanIssue with resolved_at: %v", err)
	}
	if issue.ResolvedAt == nil {
		t.Fatal("want non-nil ResolvedAt")
	}
	if issue.ResolvedAt.Year() != 2026 {
		t.Fatalf("unexpected resolved year: %d", issue.ResolvedAt.Year())
	}
}

// fakeIssueScanner implements issueScanner from a pre-populated slice.
type fakeIssueScanner struct {
	values []any
}

func (f *fakeIssueScanner) Scan(dest ...any) error {
	for i, d := range dest {
		if i >= len(f.values) {
			break
		}
		switch ptr := d.(type) {
		case *int64:
			switch v := f.values[i].(type) {
			case int64:
				*ptr = v
			case int:
				*ptr = int64(v)
			}
		case *string:
			*ptr = f.values[i].(string)
		case *sql.NullString:
			*ptr = f.values[i].(sql.NullString)
		}
	}
	return nil
}
