//go:build db_integration

// Live-Postgres tests for the durable compaction row (migration 0096). They exist for the
// one thing a unit test with a fake cache cannot prove: the watermark invariant lives in
// SQL, in the WHERE on the upsert's DO UPDATE, and is the only thing standing between two
// concurrent turns and a summary that silently un-covers forty turns.
package conversations

import "testing"

func TestCompactionRoundTripAndWatermarkOnlyMovesForward(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	// ownerCtx carries app.current_identity: without it the fail-closed policy from 0096
	// refuses the write, which is the policy doing its job.
	ctx := ownerCtx()
	conversationID := newConversation(t, store)

	if _, ok, err := store.LoadCompaction(ctx, conversationID, ""); err != nil || ok {
		t.Fatalf("LoadCompaction on a fresh conversation = (%v, %v), want (false, nil)", ok, err)
	}

	first := Compaction{Summary: "first summary", CoversThroughSeq: 20, SourceTurns: 8, Model: "test-model"}
	if err := store.SaveCompaction(ctx, conversationID, "", first); err != nil {
		t.Fatalf("SaveCompaction: %v", err)
	}
	got, ok, err := store.LoadCompaction(ctx, conversationID, "")
	if err != nil || !ok {
		t.Fatalf("LoadCompaction after save = (%v, %v)", ok, err)
	}
	if got.Summary != first.Summary || got.CoversThroughSeq != 20 || got.SourceTurns != 8 {
		t.Fatalf("loaded = %+v, want %+v", got, first)
	}

	ahead := Compaction{Summary: "second summary", CoversThroughSeq: 40, SourceTurns: 5}
	if err := store.SaveCompaction(ctx, conversationID, "", ahead); err != nil {
		t.Fatalf("SaveCompaction forward: %v", err)
	}

	// The loser of a race: an older, shorter summary. It must not win, and it must not
	// error either -- the turn that produced it is otherwise correct and has nothing to
	// apologise for.
	behind := Compaction{Summary: "stale summary", CoversThroughSeq: 25, SourceTurns: 3}
	if err := store.SaveCompaction(ctx, conversationID, "", behind); err != nil {
		t.Fatalf("SaveCompaction backward returned an error, want a silent no-op: %v", err)
	}
	got, _, err = store.LoadCompaction(ctx, conversationID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.CoversThroughSeq != 40 || got.Summary != "second summary" {
		t.Fatalf("loaded = %+v, want the watermark to stay at 40 with its own summary", got)
	}
}

// A summary is per branch: two branches of one conversation are two different histories,
// so one's summary must never be served for the other.
func TestCompactionIsScopedPerBranch(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	ctx := ownerCtx()
	conversationID := newConversation(t, store)
	const otherBranch = "11111111-1111-4111-8111-111111111111"

	if err := store.SaveCompaction(ctx, conversationID, "",
		Compaction{Summary: "canonical", CoversThroughSeq: 10}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.LoadCompaction(ctx, conversationID, otherBranch); err != nil || ok {
		t.Fatalf("the other branch read the canonical summary: (%v, %v)", ok, err)
	}
	if err := store.SaveCompaction(ctx, conversationID, otherBranch,
		Compaction{Summary: "branch", CoversThroughSeq: 3}); err != nil {
		t.Fatal(err)
	}
	canonical, _, err := store.LoadCompaction(ctx, conversationID, "")
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Summary != "canonical" || canonical.CoversThroughSeq != 10 {
		t.Fatalf("canonical branch = %+v after the other branch wrote its own", canonical)
	}
}

// The row belongs to the conversation: deleting the parent must take it with it, or a
// purged conversation leaves its summary behind as an orphan nobody can reach or erase.
func TestCompactionIsDeletedWithItsConversation(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	ctx := ownerCtx()
	conversationID := newConversation(t, store)

	if err := store.SaveCompaction(ctx, conversationID, "",
		Compaction{Summary: "doomed", CoversThroughSeq: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.conversations WHERE id = $1", conversationID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM aura.conversation_compactions WHERE conversation_id = $1",
		conversationID).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d compaction row(s) outlived the conversation", remaining)
	}
}
