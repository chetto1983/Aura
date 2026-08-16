//go:build db_integration

// The two things the durable summary's correctness rests on that only a live Postgres can
// show: the row is filed under the branch the replay actually walked, and the watermark
// survives two turns compacting at the same time.
//
// hermes-agent holds the equivalent invariant with a lease around its compression commit
// (conversation_compression.py captures MAX(id) of the active rows under the lock). Aura
// holds it in SQL instead — the upsert refuses a backwards watermark — so the test that
// matters is the one that runs the writers concurrently and reads the row afterwards.
package conversations

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// A branch is a different history, so the ladder must file its summary under the branch it
// replayed. Until 2026-08-16 both call sites passed "", so a fork inherited the canonical
// branch's summary — a description of turns it never had — and then buried the canonical
// watermark with its own.
func TestManagedHistoryFilesTheSummaryUnderTheReplayedBranch(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	ctx := ownerCtx()
	conversationID := newConversation(t, store)

	const rounds = 20
	seedCompactableTurns(t, ctx, store, conversationID, rounds)
	// Diverge at the LAST user turn, not an early one: the fork's path is
	// root -> divergeSeq-1 plus the new turn, so forking at seq 6 would replay six turns
	// and never reach the compaction trigger at all.
	forkSeq, branchID, err := store.ForkBranch(ctx, conversationID, 2*rounds, llm.RoleUser, "diverging question")
	if err != nil {
		t.Fatalf("ForkBranch: %v", err)
	}

	cfg := compactingConfig(&fixedSummarizer{text: "branch summary"})
	if _, err := store.LoadManagedHistoryForBranch(ctx, conversationID, forkSeq, cfg); err != nil {
		t.Fatalf("LoadManagedHistoryForBranch: %v", err)
	}

	stored, ok, err := store.LoadCompaction(ctx, conversationID, branchID.String())
	if err != nil || !ok {
		t.Fatalf("the branch has no summary of its own: (%v, %v)", ok, err)
	}
	if stored.Summary != "branch summary" {
		t.Errorf("branch summary = %q", stored.Summary)
	}
	if _, ok, err := store.LoadCompaction(ctx, conversationID, ""); err != nil || ok {
		t.Errorf("the canonical branch was written by a fork's replay: (%v, %v)", ok, err)
	}
}

// And the canonical path keeps writing the canonical row: the fix must not send every
// linear conversation to a branch-shaped key.
func TestManagedHistoryFilesTheCanonicalSummaryUnderTheCanonicalBranch(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	ctx := ownerCtx()
	conversationID := newConversation(t, store)

	seedCompactableTurns(t, ctx, store, conversationID, 20)
	cfg := compactingConfig(&fixedSummarizer{text: "canonical summary"})
	if _, err := store.LoadManagedHistory(ctx, conversationID, cfg); err != nil {
		t.Fatalf("LoadManagedHistory: %v", err)
	}

	stored, ok, err := store.LoadCompaction(ctx, conversationID, "")
	if err != nil || !ok {
		t.Fatalf("the canonical branch has no summary: (%v, %v)", ok, err)
	}
	if stored.Summary != "canonical summary" {
		t.Errorf("canonical summary = %q", stored.Summary)
	}
}

// Two turns compacting at once. Whichever order Postgres commits them in, the surviving
// row must be the FURTHEST watermark with ITS summary — never a mix of the two, and never
// a summary that covers fewer turns than the row it replaced (that is how forty turns
// silently leave the model's view while the ladder believes they were condensed).
func TestConcurrentCompactionsKeepTheFurthestWatermark(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	ctx := ownerCtx()
	conversationID := newConversation(t, store)

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	start := make(chan struct{})
	for i := 1; i <= writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them together, so the writes actually overlap
			err := store.SaveCompaction(ctx, conversationID, "", Compaction{
				Summary:          fmt.Sprintf("summary covering %d", i*10),
				CoversThroughSeq: i * 10,
				SourceTurns:      i,
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a concurrent SaveCompaction failed: %v", err)
	}

	stored, ok, err := store.LoadCompaction(ctx, conversationID, "")
	if err != nil || !ok {
		t.Fatalf("LoadCompaction after the race = (%v, %v)", ok, err)
	}
	if stored.CoversThroughSeq != writers*10 {
		t.Fatalf("watermark = %d, want %d — a loser overwrote the winner",
			stored.CoversThroughSeq, writers*10)
	}
	// The summary must belong to the watermark that survived. A row carrying the highest
	// seq with somebody else's text describes turns it never read.
	if want := fmt.Sprintf("summary covering %d", writers*10); stored.Summary != want {
		t.Fatalf("summary = %q, want %q — the row is a mix of two writers", stored.Summary, want)
	}
}

// A conversation reserved for export-delete must still be able to compact.
//
// This is the measurement behind NOT copying hermes-agent's archive-and-compact. There the
// compaction UPDATEs the message rows (active=0); here that UPDATE would fire migration
// 0047's conversation_turns_snapshot_bump, which raises SQLSTATE 55006 on a reserved
// conversation — so a reserved conversation would lose compaction entirely and fall to the
// L2.5 hard drop precisely while an export is being assembled from it. Aura writes a
// separate row instead, which carries no bump trigger, so the guard has nothing to refuse.
func TestCompactionSurvivesAnExportDeleteReservation(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	ctx := ownerCtx()
	conversationID := newConversation(t, store)

	// The reservation is a SHAPE, not a single column (0048's
	// conversations_delete_lifecycle_shape): a reservation token without a phase and a
	// timestamp is refused outright.
	if _, err := pool.Exec(ctx,
		`UPDATE aura.conversations
		    SET delete_reservation = 'export-1',
		        delete_phase = 'reserved',
		        delete_reserved_at = now()
		  WHERE id = $1`,
		conversationID); err != nil {
		t.Fatalf("reserve conversation: %v", err)
	}

	// The reservation is real: a turn write is refused with 55006.
	err := store.AppendTurn(ctx, AppendTurnParams{
		ConversationID: conversationID, Seq: 1, Role: llm.RoleUser, Content: "blocked",
	})
	if err == nil {
		t.Fatal("a reserved conversation accepted a turn; the guard is not armed and this test proves nothing")
	}

	if err := store.SaveCompaction(ctx, conversationID, "",
		Compaction{Summary: "reserved but compactable", CoversThroughSeq: 12}); err != nil {
		t.Fatalf("SaveCompaction on a reserved conversation: %v", err)
	}
	stored, ok, err := store.LoadCompaction(ctx, conversationID, "")
	if err != nil || !ok || stored.CoversThroughSeq != 12 {
		t.Fatalf("LoadCompaction = (%+v, %v, %v)", stored, ok, err)
	}
}

// seedCompactableTurns writes a system head plus n user/assistant rounds long enough that
// the ladder's early trigger fires on them.
func seedCompactableTurns(t *testing.T, ctx context.Context, store *Store, conversationID string, rounds int) {
	t.Helper()
	if err := store.AppendTurn(ctx, AppendTurnParams{
		ConversationID: conversationID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura",
	}); err != nil {
		t.Fatalf("append system turn: %v", err)
	}
	seq := 2
	for i := range rounds {
		for _, turn := range []struct {
			role, content string
		}{
			{llm.RoleUser, fmt.Sprintf("question %d %s", i, strings.Repeat("lorem ipsum dolor ", 40))},
			{llm.RoleAssistant, fmt.Sprintf("answer %d %s", i, strings.Repeat("sit amet consectetur ", 40))},
		} {
			if err := store.AppendTurn(ctx, AppendTurnParams{
				ConversationID: conversationID, Seq: seq, Role: turn.role, Content: turn.content,
			}); err != nil {
				t.Fatalf("append turn %d: %v", seq, err)
			}
			seq++
		}
	}
}

// compactingConfig is a ladder config whose early trigger fires on the seeded history.
//
// The window is small on purpose. The protected tail is a share of the CAP, not of the
// history, so on a 120k window it is ~13k tokens — more than the whole seeded transcript,
// which leaves nothing older to condense and the compaction never runs. A 40k window puts
// the tail budget at ~3k against ~4k of history, which is the shape a real over-budget
// conversation has.
func compactingConfig(sum Summarizer) ContextConfig {
	return ContextConfig{
		ContextWindow:            40_000,
		MaxOutputTokens:          4096,
		CompactionTriggerPercent: 5,
		HistoryHardCapTurns:      200,
		Summarizer:               sum,
	}
}

// fixedSummarizer answers with one canned summary, so the test asserts on WHERE the
// summary landed rather than on what a model wrote.
type fixedSummarizer struct{ text string }

func (f *fixedSummarizer) Summarize(context.Context, []llm.Message) (string, error) {
	return f.text, nil
}
