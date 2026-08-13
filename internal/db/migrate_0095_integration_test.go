//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrateTo0094 lands the database on EXACTLY version 94, the state immediately before
// the parent_seq backfill. Same formulation as migrateTo0093 and for the same reason:
// migration numbers are assigned at landing and are NOT contiguous, so stepping by count
// lands somewhere arbitrary. Migrate to head, then step down until the version matches.
func migrateTo0094(t *testing.T, ctx context.Context, migrateURL string, admin *pgxpool.Pool) {
	t.Helper()
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to head: %v", err)
	}
	for step := 0; step <= 32; step++ {
		switch v := currentMigrationVersion(t, ctx, admin); {
		case v == 94:
			return
		case v < 94:
			t.Fatalf("stepped past 0094: landed on version %d", v)
		}
		if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
			t.Fatalf("step down toward 0094: %v", err)
		}
	}
	t.Fatalf("did not reach version 94 within 32 steps (now %d)", currentMigrationVersion(t, ctx, admin))
}

// TestMigrate0095RepairsLegacyNullParentChain covers the data repair AND its two guards.
//
// The repair matters because chaining parent_seq in InsertConversationTurn only fixes
// rows written from now on. Measured on the live deployment before this was written: 690
// turns, all with a NULL parent, 662 of them at seq > 1 and therefore breaking the branch
// walk. Every one of those conversations would still lose its history on an edit.
//
// The guards matter more than the repair. A blind `seq - 1` would FABRICATE topology:
//
//   - a forked turn's parent is its divergence point, not the turn that happens to sit
//     one seq below it. Those rows carry NULL too, and the table no longer knows what
//     their parent was, so they must stay NULL — a missing pointer is honest, a wrong
//     one silently reattaches a branch somewhere it never was.
//   - a seq gap (a deleted turn) would produce a dangling pointer, which ends the walk
//     exactly like the NULL it replaced.
func TestMigrate0095RepairsLegacyNullParentChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0095_backfill")
	migrateTo0094(t, ctx, migrateURL, admin)

	const canonical = "00000000-0000-0000-0000-000000000000"
	forked := "11111111-1111-1111-1111-111111111111"

	var convID string
	if err := admin.QueryRow(ctx, `
INSERT INTO aura.conversations (id, identity_id, model, status)
VALUES (gen_random_uuid(), $1::uuid, 'test-model', 'active') RETURNING id::text`,
		seededOperatorIdentity).Scan(&convID); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	// Seqs 1..4 on the canonical branch, all with the NULL parent the append path wrote.
	// Seq 6 is canonical too but its seq-1 (5) is deliberately absent — the gap case.
	// Seq 7 sits on a forked branch: its true parent is unknowable from here.
	for _, row := range []struct {
		seq    int
		branch string
	}{{1, canonical}, {2, canonical}, {3, canonical}, {4, canonical}, {6, canonical}, {7, forked}} {
		if _, err := admin.Exec(ctx, `
INSERT INTO aura.conversation_turns (conversation_id, seq, role, content, branch_id, parent_seq)
VALUES ($1::uuid, $2, 'user', 'legacy turn', $3::uuid, NULL)`, convID, row.seq, row.branch); err != nil {
			t.Fatalf("seed legacy turn seq %d: %v", row.seq, err)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0095 up: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 95 {
		t.Fatalf("version after up = %d, want 95", got)
	}

	parentOf := func(seq int) *int {
		t.Helper()
		var p *int
		if err := admin.QueryRow(ctx,
			`SELECT parent_seq FROM aura.conversation_turns WHERE conversation_id = $1::uuid AND seq = $2`,
			convID, seq).Scan(&p); err != nil {
			t.Fatalf("read parent_seq of seq %d: %v", seq, err)
		}
		return p
	}

	if p := parentOf(1); p != nil {
		t.Errorf("root seq 1 parent = %d, want NULL (it has no parent)", *p)
	}
	for _, seq := range []int{2, 3, 4} {
		p := parentOf(seq)
		if p == nil {
			t.Errorf("seq %d still has a NULL parent: the walk still dies here", seq)
			continue
		}
		if *p != seq-1 {
			t.Errorf("seq %d parent = %d, want %d", seq, *p, seq-1)
		}
	}
	if p := parentOf(6); p != nil {
		t.Errorf("seq 6 parent = %d, want NULL: seq 5 does not exist, so seq-1 would dangle", *p)
	}
	if p := parentOf(7); p != nil {
		t.Errorf("forked seq 7 parent = %d, want NULL: its divergence point is unrecoverable "+
			"and seq-1 would attach it to a branch it was never on", *p)
	}

	// Idempotent: a second run must match nothing, which is what makes re-applying after a
	// rollback safe (the down migration deliberately cannot un-repair).
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0095 down: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0095 up again: %v", err)
	}
	for _, seq := range []int{2, 3, 4} {
		if p := parentOf(seq); p == nil || *p != seq-1 {
			t.Errorf("seq %d parent after down+up = %v, want %d", seq, p, seq-1)
		}
	}
}
