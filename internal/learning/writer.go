package learning

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// Proposal is the subset of a proposed_updates row needed by the writer functions.
// For operational_memory proposals: ToolName is stored in target_slug and
// ErrorClass is stored in category by the SQLProposalStore adapter.
// For user_memory proposals: Category holds the triage category (person/preference/fact/todo).
type Proposal struct {
	Kind          string
	Fact          string
	SignatureHash string
	ToolName      string
	ErrorClass    string
	Category      string
}

// WriteApprovedLesson writes an approved operational_memory proposal to
// compact_memory_documents with Kind=operational. Returns nil without writing
// when proposal.Kind is not "operational_memory". The write is idempotent:
// the document ID is derived from SignatureHash so re-approving the same
// proposal overwrites the existing row rather than creating a duplicate.
func WriteApprovedLesson(ctx context.Context, store *memoryindex.Store, proposal Proposal) error {
	if proposal.Kind != "operational_memory" {
		return nil
	}
	sig := proposal.SignatureHash
	title := fmt.Sprintf("Lesson: %s %s", proposal.ToolName, proposal.ErrorClass)
	body := proposal.Fact
	contentHash := memoryindex.ContentHash("operational", body, "")
	doc := memoryindex.Document{
		ID:          "operational:" + sig,
		Kind:        memoryindex.KindOperational,
		Title:       title,
		Body:        body,
		Handle:      "operational:" + sig,
		Status:      "active",
		UpdatedAt:   time.Now().UTC(),
		ContentHash: contentHash,
	}
	return store.Upsert(ctx, doc)
}

// MigrateOperationalProposals promotes all pending operational_memory rows in
// proposed_updates to compact_memory_documents (auto-accept migration for US-OP01).
// Each promoted row has its status set to "accepted" in proposed_updates.
// Returns the count of promoted rows.
func MigrateOperationalProposals(ctx context.Context, db *sql.DB, store *memoryindex.Store) (int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, fact, target_slug, category, signature_hash
		 FROM proposed_updates
		 WHERE kind = 'operational_memory' AND status = 'pending'`)
	if err != nil {
		return 0, fmt.Errorf("learning: migrate operational proposals: query: %w", err)
	}
	defer rows.Close()

	type pending struct {
		id            int64
		fact          string
		toolName      string
		errorClass    string
		signatureHash string
	}
	var items []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.fact, &p.toolName, &p.errorClass, &p.signatureHash); err != nil {
			return 0, fmt.Errorf("learning: migrate operational proposals: scan: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("learning: migrate operational proposals: iterate: %w", err)
	}

	promoted := 0
	for _, p := range items {
		lp := Proposal{
			Kind:          "operational_memory",
			Fact:          p.fact,
			SignatureHash: p.signatureHash,
			ToolName:      p.toolName,
			ErrorClass:    p.errorClass,
		}
		if err := WriteApprovedLesson(ctx, store, lp); err != nil {
			return promoted, fmt.Errorf("learning: migrate operational proposals: write id=%d: %w", p.id, err)
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE proposed_updates SET status = 'accepted' WHERE id = ?`, p.id); err != nil {
			return promoted, fmt.Errorf("learning: migrate operational proposals: accept id=%d: %w", p.id, err)
		}
		promoted++
	}
	return promoted, nil
}
