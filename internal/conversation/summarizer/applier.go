package summarizer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// WikiWriter is the write surface of wiki.Store needed by AutoApplier.
type WikiWriter interface {
	WritePage(ctx context.Context, page *wiki.Page) error
	ReadPage(slug string) (*wiki.Page, error)
	AppendLog(ctx context.Context, action, slug string)
}

// Applier applies a single Decision.
type Applier interface {
	Apply(ctx context.Context, d Decision) error
}

// ---- AutoApplier ----

// AutoApplier writes directly to the wiki store.
type AutoApplier struct {
	wiki WikiWriter
}

// NewAutoApplier returns an AutoApplier backed by the given WikiWriter.
func NewAutoApplier(w WikiWriter) *AutoApplier {
	return &AutoApplier{wiki: w}
}

func (a *AutoApplier) Apply(ctx context.Context, d Decision) error {
	switch d.Action {
	case ActionNew:
		return a.applyNew(ctx, d)
	case ActionPatch:
		return a.applyPatch(ctx, d)
	case ActionSkip:
		a.wiki.AppendLog(ctx, "auto-sum skip", d.TargetSlug)
		return nil
	default:
		return fmt.Errorf("auto applier: unknown action %q", d.Action)
	}
}

func (a *AutoApplier) applyNew(ctx context.Context, d Decision) error {
	title := d.Candidate.Fact
	if len(title) > 80 {
		title = title[:80]
	}
	sources := make([]string, len(d.Candidate.SourceTurnIDs))
	for i, id := range d.Candidate.SourceTurnIDs {
		sources[i] = fmt.Sprintf("turn:%d", id)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "summarizer_v1",
		Title:         title,
		Category:      d.Candidate.Category,
		Related:       uniqueNonEmpty(d.Candidate.RelatedSlugs),
		Tags:          []string{"auto-added"},
		Sources:       sources,
		CreatedAt:     now,
		UpdatedAt:     now,
		Body:          fmt.Sprintf("%s\n\n*Auto-extracted by Aura summarizer.*", d.Candidate.Fact),
	}
	if err := a.wiki.WritePage(ctx, page); err != nil {
		return fmt.Errorf("auto applier new: %w", err)
	}
	a.wiki.AppendLog(ctx, "auto-sum new", wiki.Slug(title))
	return nil
}

func (a *AutoApplier) applyPatch(ctx context.Context, d Decision) error {
	page, err := a.wiki.ReadPage(d.TargetSlug)
	if err != nil {
		return fmt.Errorf("auto applier patch read: %w", err)
	}
	date := time.Now().UTC().Format("2006-01-02")
	block := fmt.Sprintf("\n\n> [auto-sum %s] %s\n", date, d.Candidate.Fact)
	page.Body = strings.TrimRight(page.Body, "\n") + block
	// Append new source turn IDs.
	for _, id := range d.Candidate.SourceTurnIDs {
		ref := fmt.Sprintf("turn:%d", id)
		if !containsStr(page.Sources, ref) {
			page.Sources = append(page.Sources, ref)
		}
	}
	for _, slug := range d.Candidate.RelatedSlugs {
		if slug != "" && !containsStr(page.Related, slug) {
			page.Related = append(page.Related, slug)
		}
	}
	page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.wiki.WritePage(ctx, page); err != nil {
		return fmt.Errorf("auto applier patch write: %w", err)
	}
	a.wiki.AppendLog(ctx, "auto-sum patch", d.TargetSlug)
	return nil
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// ---- ReviewApplier ----

const reviewMigrationSQL = `
CREATE TABLE IF NOT EXISTS proposed_updates (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id         INTEGER NOT NULL,
  fact            TEXT    NOT NULL,
  action          TEXT    NOT NULL,
  target_slug     TEXT    NOT NULL DEFAULT '',
  similarity      REAL    NOT NULL DEFAULT 0,
  source_turn_ids TEXT    NOT NULL DEFAULT '',
  category        TEXT    NOT NULL DEFAULT '',
  related_slugs   TEXT    NOT NULL DEFAULT '',
  provenance_json TEXT    NOT NULL DEFAULT '{}',
  status          TEXT    NOT NULL DEFAULT 'pending',
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// ReviewApplier inserts proposals into proposed_updates; no wiki mutation.
type ReviewApplier struct {
	db *sql.DB
}

// NewReviewApplier returns a ReviewApplier, applying the migration if needed.
func NewReviewApplier(db *sql.DB) (*ReviewApplier, error) {
	if _, err := db.Exec(reviewMigrationSQL); err != nil {
		return nil, fmt.Errorf("review applier migrate: %w", err)
	}
	if err := ensureReviewColumns(db); err != nil {
		return nil, fmt.Errorf("review applier migrate columns: %w", err)
	}
	return &ReviewApplier{db: db}, nil
}

func (r *ReviewApplier) Apply(ctx context.Context, d Decision) error {
	if d.Action == ActionSkip {
		return nil
	}
	ids, _ := json.Marshal(d.Candidate.SourceTurnIDs)
	related, _ := json.Marshal(d.Candidate.RelatedSlugs)
	provenance, _ := json.Marshal(Provenance{OriginTool: "conversation_summarizer", Evidence: turnEvidenceRefs(d.Candidate.SourceTurnIDs)})
	const q = `INSERT INTO proposed_updates (chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status)
		VALUES (0, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`
	_, err := r.db.ExecContext(ctx, q,
		d.Candidate.Fact, string(d.Action), d.TargetSlug, d.Similarity, string(ids), d.Candidate.Category, string(related), string(provenance))
	if err != nil {
		return fmt.Errorf("review applier insert: %w", err)
	}
	return nil
}

func ensureReviewColumns(db *sql.DB) error {
	cols, err := tableColumns(db, "proposed_updates")
	if err != nil {
		return err
	}
	if !cols["category"] {
		if _, err := db.Exec(`ALTER TABLE proposed_updates ADD COLUMN category TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["related_slugs"] {
		if _, err := db.Exec(`ALTER TABLE proposed_updates ADD COLUMN related_slugs TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["provenance_json"] {
		if _, err := db.Exec(`ALTER TABLE proposed_updates ADD COLUMN provenance_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// ---- OffApplier ----

// OffApplier is a no-op; it neither writes wiki nor logs.
type OffApplier struct{}

// NewOffApplier returns an OffApplier.
func NewOffApplier() *OffApplier { return &OffApplier{} }

func (o *OffApplier) Apply(_ context.Context, _ Decision) error { return nil }

func uniqueNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func turnEvidenceRefs(ids []int64) []EvidenceRef {
	if len(ids) == 0 {
		return []EvidenceRef{}
	}
	out := make([]EvidenceRef, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		out = append(out, EvidenceRef{Kind: "archive", ID: fmt.Sprintf("conversation:%d", id)})
	}
	return out
}
