package learning_test

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/learning"
)

// TestSweepStaleProposals covers the three TTL cases:
//  1. Fresh pending row — not purged.
//  2. 31-day-old pending row — purged.
//  3. 31-day-old approved row — not purged (approved rows are never swept).
func TestSweepStaleProposals(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	now := time.Now().UTC()
	recent := now.Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	stale := now.Add(-31 * 24 * time.Hour).Format("2006-01-02 15:04:05")

	insert := func(status, sigSuffix, createdAt string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO proposed_updates
			  (chat_id, fact, action, target_slug, similarity,
			   source_turn_ids, category, related_slugs, provenance_json,
			   status, kind, signature_hash, created_at)
			VALUES (0, 'test fact', 'new', 'test-slug', 1.0, '[]', '', '[]', '{}',
			        ?, 'wiki', ?, ?)`,
			status, "sig-"+sigSuffix, createdAt,
		)
		if err != nil {
			t.Fatalf("insert proposal (%s): %v", sigSuffix, err)
		}
	}

	insert("pending", "fresh-pending", recent)   // case 1: must survive
	insert("pending", "stale-pending", stale)     // case 2: must be purged
	insert("approved", "stale-approved", stale)   // case 3: must survive

	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposed_updates`).Scan(&total); err != nil {
		t.Fatalf("count pre-sweep: %v", err)
	}
	if total != 3 {
		t.Fatalf("pre-sweep: expected 3 rows, got %d", total)
	}

	purged, err := learning.SweepStaleProposals(ctx, db, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("SweepStaleProposals: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1", purged)
	}

	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposed_updates`).Scan(&remaining); err != nil {
		t.Fatalf("count post-sweep: %v", err)
	}
	if remaining != 2 {
		t.Errorf("post-sweep row count = %d, want 2", remaining)
	}

	var pendingCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposed_updates WHERE status='pending'`).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending post-sweep: %v", err)
	}
	if pendingCount != 1 {
		t.Errorf("pending rows after sweep = %d, want 1 (fresh row must survive)", pendingCount)
	}

	var approvedCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proposed_updates WHERE status='approved'`).Scan(&approvedCount); err != nil {
		t.Fatalf("count approved post-sweep: %v", err)
	}
	if approvedCount != 1 {
		t.Errorf("approved rows after sweep = %d, want 1 (approved rows must never be purged)", approvedCount)
	}
}
