package summarizer_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/wiki"
)

// --- fake wiki store ---

type fakeWikiStore struct {
	written  []*wiki.Page
	readPage *wiki.Page // returned by ReadPage if non-nil
	logLines []string
}

func (f *fakeWikiStore) WritePage(_ context.Context, p *wiki.Page) error {
	f.written = append(f.written, p)
	return nil
}

func (f *fakeWikiStore) ReadPage(slug string) (*wiki.Page, error) {
	if f.readPage != nil {
		return f.readPage, nil
	}
	return nil, fmt.Errorf("page not found: %s", slug)
}

func (f *fakeWikiStore) AppendLog(_ context.Context, action, slug string) {
	f.logLines = append(f.logLines, action+":"+slug)
}

// --- helper ---

func newReviewDB(t *testing.T) *sql.DB {
	t.Helper()
	db := scheduler.NewTestDB(t)
	return db
}

func makeDecision(action summarizer.Action, slug string) summarizer.Decision {
	return summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:          "Marco lives in Bologna",
			Score:         0.9,
			Category:      "person",
			RelatedSlugs:  []string{"bologna", "marco"},
			SourceTurnIDs: []int64{1, 2},
		},
		Action:     action,
		TargetSlug: slug,
		Similarity: 0.3,
	}
}

// === AutoApplier tests ===

func TestAutoApplier_ActionNew_WritesPage(t *testing.T) {
	ws := &fakeWikiStore{}
	a := summarizer.NewAutoApplier(ws)

	err := a.Apply(context.Background(), makeDecision(summarizer.ActionNew, ""))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ws.written) != 1 {
		t.Fatalf("want 1 page written, got %d", len(ws.written))
	}
	p := ws.written[0]
	if p.Body == "" {
		t.Fatal("want non-empty body")
	}
	// evidence encoded as sources
	if len(p.Sources) == 0 {
		t.Fatal("want sources (evidence) set")
	}
	if len(p.Related) != 2 || p.Related[0] != "bologna" || p.Related[1] != "marco" {
		t.Fatalf("related = %#v, want [bologna marco]", p.Related)
	}
}

func TestAutoApplier_ActionPatch_AppendsToBody(t *testing.T) {
	existingPage := &wiki.Page{
		Title:    "Marco Info",
		Category: "person",
		Body:     "Marco is a person.",
	}
	ws := &fakeWikiStore{readPage: existingPage}
	a := summarizer.NewAutoApplier(ws)

	err := a.Apply(context.Background(), makeDecision(summarizer.ActionPatch, "marco-info"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ws.written) != 1 {
		t.Fatalf("want page written on patch, got %d", len(ws.written))
	}
	body := ws.written[0].Body
	if !strings.Contains(body, "[auto-sum") {
		t.Fatalf("want [auto-sum] block in body, got: %q", body)
	}
	if len(ws.written[0].Related) != 2 || ws.written[0].Related[0] != "bologna" || ws.written[0].Related[1] != "marco" {
		t.Fatalf("related = %#v, want [bologna marco]", ws.written[0].Related)
	}
}

func TestAutoApplier_ActionSkip_WritesLogOnly(t *testing.T) {
	ws := &fakeWikiStore{}
	a := summarizer.NewAutoApplier(ws)

	err := a.Apply(context.Background(), makeDecision(summarizer.ActionSkip, "existing-page"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ws.written) != 0 {
		t.Fatalf("want 0 pages written for skip, got %d", len(ws.written))
	}
	if len(ws.logLines) != 1 {
		t.Fatalf("want 1 log line for skip, got %d", len(ws.logLines))
	}
	if !strings.Contains(ws.logLines[0], "auto-sum") {
		t.Fatalf("want [auto-sum] in log action, got %q", ws.logLines[0])
	}
}

// === ReviewApplier tests ===

func TestReviewApplier_ActionNew_InsertsProposal(t *testing.T) {
	db := newReviewDB(t)
	a, err := summarizer.NewReviewApplier(db)
	if err != nil {
		t.Fatalf("NewReviewApplier: %v", err)
	}

	if err := a.Apply(context.Background(), makeDecision(summarizer.ActionNew, "")); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	rows, err := db.QueryContext(context.Background(), "SELECT status, category, related_slugs, provenance_json FROM proposed_updates WHERE action='new'")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var count int
	for rows.Next() {
		var status string
		var category string
		var related string
		var provenance string
		rows.Scan(&status, &category, &related, &provenance)
		if status != "pending" {
			t.Fatalf("want status=pending, got %q", status)
		}
		if category != "person" {
			t.Fatalf("want category=person, got %q", category)
		}
		if related != `["bologna","marco"]` {
			t.Fatalf("want related slugs JSON, got %q", related)
		}
		if !strings.Contains(provenance, "conversation_summarizer") || !strings.Contains(provenance, "conversation:1") {
			t.Fatalf("want provenance with summarizer origin and turn evidence, got %q", provenance)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("want 1 row, got %d", count)
	}
}

func TestReviewApplier_MigratesLegacyProposalColumns(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE proposed_updates (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id         INTEGER NOT NULL,
  fact            TEXT    NOT NULL,
  action          TEXT    NOT NULL,
  target_slug     TEXT    NOT NULL DEFAULT '',
  similarity      REAL    NOT NULL DEFAULT 0,
  source_turn_ids TEXT    NOT NULL DEFAULT '',
  status          TEXT    NOT NULL DEFAULT 'pending',
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := summarizer.NewReviewApplier(db); err != nil {
		t.Fatalf("NewReviewApplier: %v", err)
	}
	rows, err := db.Query(`SELECT category, related_slugs, provenance_json FROM proposed_updates LIMIT 0`)
	if err != nil {
		t.Fatalf("new columns not queryable: %v", err)
	}
	rows.Close()
}

func TestReviewApplier_ActionPatch_InsertsProposal(t *testing.T) {
	db := newReviewDB(t)
	a, err := summarizer.NewReviewApplier(db)
	if err != nil {
		t.Fatalf("NewReviewApplier: %v", err)
	}

	if err := a.Apply(context.Background(), makeDecision(summarizer.ActionPatch, "target-slug")); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var count int
	db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM proposed_updates WHERE action='patch' AND target_slug='target-slug'").Scan(&count)
	if count != 1 {
		t.Fatalf("want 1 row, got %d", count)
	}
}

func TestReviewApplier_ActionSkip_NoInsert(t *testing.T) {
	db := newReviewDB(t)
	a, err := summarizer.NewReviewApplier(db)
	if err != nil {
		t.Fatalf("NewReviewApplier: %v", err)
	}

	if err := a.Apply(context.Background(), makeDecision(summarizer.ActionSkip, "existing")); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var count int
	db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM proposed_updates").Scan(&count)
	if count != 0 {
		t.Fatalf("want 0 rows for skip, got %d", count)
	}
}

// === OffApplier tests ===

func TestOffApplier_ActionNew_NoSideEffects(t *testing.T) {
	a := summarizer.NewOffApplier()
	if err := a.Apply(context.Background(), makeDecision(summarizer.ActionNew, "")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// No assertions needed — just must not panic or error
}

func TestOffApplier_ActionPatch_NoSideEffects(t *testing.T) {
	a := summarizer.NewOffApplier()
	if err := a.Apply(context.Background(), makeDecision(summarizer.ActionPatch, "slug")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestOffApplier_ActionSkip_NoSideEffects(t *testing.T) {
	a := summarizer.NewOffApplier()
	if err := a.Apply(context.Background(), makeDecision(summarizer.ActionSkip, "slug")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}
