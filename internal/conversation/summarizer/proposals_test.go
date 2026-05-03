package summarizer

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newProposalStore(t *testing.T) (*sql.DB, *SummariesStore) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "proposals.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	const schema = `CREATE TABLE proposed_updates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		fact TEXT NOT NULL,
		action TEXT NOT NULL,
		target_slug TEXT NOT NULL DEFAULT '',
		similarity REAL NOT NULL DEFAULT 0,
		source_turn_ids TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '',
		related_slugs TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db, NewSummariesStore(db)
}

func insertProposal(t *testing.T, db *sql.DB, status string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO proposed_updates (chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, status)
		 VALUES (42, 'fact', 'patch', 'target', 0.7, '[1,2]', 'project', '["aura"]', ?)`,
		status)
	if err != nil {
		t.Fatalf("insert proposal: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

func TestSummariesStorePropose_New(t *testing.T) {
	_, store := newProposalStore(t)

	got, err := store.Propose(context.Background(), ProposalInput{
		ChatID:        42,
		Fact:          "Create a page about proactive AuraBot proposals.",
		Action:        "new",
		TargetSlug:    "ignored-for-new",
		Similarity:    1.4,
		SourceTurnIDs: []int64{10, 11},
		Category:      "project",
		RelatedSlugs:  []string{"aurabot", "aurabot", " "},
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if got.Status != "pending" || got.Action != "new" || got.TargetSlug != "" {
		t.Fatalf("proposal = %#v", got)
	}
	if got.ChatID != 42 || got.Category != "project" || got.Similarity != 1 {
		t.Fatalf("proposal fields = %#v", got)
	}
	if len(got.SourceTurnIDs) != 2 || got.SourceTurnIDs[0] != 10 || got.SourceTurnIDs[1] != 11 {
		t.Fatalf("source turn ids = %#v", got.SourceTurnIDs)
	}
	if len(got.RelatedSlugs) != 1 || got.RelatedSlugs[0] != "aurabot" {
		t.Fatalf("related slugs = %#v", got.RelatedSlugs)
	}
}

func TestSummariesStorePropose_PatchRequiresTarget(t *testing.T) {
	_, store := newProposalStore(t)

	_, err := store.Propose(context.Background(), ProposalInput{
		Fact:   "Append this to an existing page.",
		Action: "patch",
	})
	if err == nil {
		t.Fatal("expected target_slug validation error")
	}
}

func TestSummariesStorePropose_RejectsInvalidInput(t *testing.T) {
	_, store := newProposalStore(t)

	if _, err := store.Propose(context.Background(), ProposalInput{Action: "new"}); err == nil {
		t.Fatal("expected fact validation error")
	}
	if _, err := store.Propose(context.Background(), ProposalInput{Fact: "x", Action: "delete"}); err == nil {
		t.Fatal("expected action validation error")
	}
}

func TestSummariesStoreSetStatus_UpdatesPending(t *testing.T) {
	db, store := newProposalStore(t)
	id := insertProposal(t, db, "pending")

	updated, err := store.SetStatus(context.Background(), id, "approved")
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if updated.Status != "approved" {
		t.Fatalf("status = %q, want approved", updated.Status)
	}
	if updated.ID != id || updated.Fact != "fact" || updated.Category != "project" {
		t.Fatalf("updated proposal lost fields: %#v", updated)
	}
}

func TestSummariesStoreSetStatus_RejectsAlreadyDecided(t *testing.T) {
	db, store := newProposalStore(t)
	id := insertProposal(t, db, "approved")

	_, err := store.SetStatus(context.Background(), id, "rejected")
	if !errors.Is(err, ErrProposalConflict) {
		t.Fatalf("SetStatus error = %v, want ErrProposalConflict", err)
	}

	got, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("status = %q, want approved", got.Status)
	}
}

func TestSummariesStoreSetStatus_NotFound(t *testing.T) {
	_, store := newProposalStore(t)

	_, err := store.SetStatus(context.Background(), 999, "approved")
	if !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("SetStatus error = %v, want ErrProposalNotFound", err)
	}
}
