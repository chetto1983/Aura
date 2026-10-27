//go:build db_integration

// Live-Postgres tests for `/compact` (Store.Compact). They exist for the two things a fake
// cannot prove: that the requested compaction and the ladder that reads it afterwards agree
// on the same durable row, and that a summary which would COST tokens is refused before it
// becomes that row — a refusal that has to happen after the model has already answered, and
// therefore has to be visible in what the store did rather than in what it returned.
package conversations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// compactConfig is a requested-compaction config over a scripted summarizer. The window is
// large on purpose: nothing here is over budget, which is the whole situation `/compact`
// exists for.
func compactConfig(sum Summarizer) ContextConfig {
	return ContextConfig{
		ContextWindow:       200000,
		MaxOutputTokens:     1000,
		ToolEvictAfterTurns: 10,
		HistoryHardCapTurns: 50,
		Summarizer:          sum,
	}
}

func seedTurns(t *testing.T, s *Store, convID string, turns ...AppendTurnParams) {
	t.Helper()
	for _, turn := range turns {
		if err := s.AppendTurn(ownerCtx(), turn); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", turn.Seq, err)
		}
	}
}

// A long question and a long answer, so a short summary is genuinely a reduction. Each round
// is TAGGED with its seq: with one shared body, a covered turn and the active round are
// textually identical and "was this replayed verbatim?" cannot be asked at all.
func verboseRound(convID string, seq int) []AppendTurnParams {
	body := fmt.Sprintf(" frase del turno %d che occupa spazio nel contesto.", seq)
	return []AppendTurnParams{
		{ConversationID: convID, Seq: seq, Role: llm.RoleUser,
			Content: fmt.Sprintf("domanda-%d:", seq) + strings.Repeat(body, 120)},
		{ConversationID: convID, Seq: seq + 1, Role: llm.RoleAssistant,
			Content: fmt.Sprintf("risposta-%d:", seq+1) + strings.Repeat(body, 120)},
	}
}

func TestCompact_StoresTheSummaryTheLadderThenReplays(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	convID := newConversation(t, store)
	var seeded []AppendTurnParams
	for seq := 1; seq <= 5; seq += 2 {
		seeded = append(seeded, verboseRound(convID, seq)...)
	}
	seedTurns(t, store, convID, seeded...)

	sum := &fakeSummarizer{summary: "L'operatore ha fatto tre domande verbose; Aura ha risposto."}
	result, err := store.Compact(ownerCtx(), convID, 0, compactConfig(sum))
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if sum.calls != 1 {
		t.Fatalf("summarizer called %d times, want exactly 1", sum.calls)
	}
	if result.TokensAfter >= result.TokensBefore {
		t.Fatalf("tokens %d -> %d: a compaction that does not reduce must not be reported as one",
			result.TokensBefore, result.TokensAfter)
	}
	// The active round is the last user-led one (seq 5,6), so the summary speaks through 4.
	if result.CoversThroughSeq != 4 || result.SourceTurns != 4 {
		t.Fatalf("result = %+v, want the summary to cover the 4 turns before the active round", result)
	}

	stored, ok, err := store.LoadCompaction(ownerCtx(), convID, "")
	if err != nil || !ok {
		t.Fatalf("LoadCompaction after Compact = (%v, %v), want the row the command wrote", ok, err)
	}
	if stored.Summary != sum.summary || stored.CoversThroughSeq != 4 {
		t.Fatalf("stored = %+v, want the summary the command returned", stored)
	}

	// The point of the whole command: the NEXT turn's history replays the summary instead of
	// the turns, without the ladder being over any budget and without a second model call.
	msgs, err := store.LoadManagedHistory(ownerCtx(), convID, 0, compactConfig(sum))
	if err != nil {
		t.Fatalf("LoadManagedHistory: %v", err)
	}
	if sum.calls != 1 {
		t.Fatalf("summarizer called %d times by the ladder, want the stored summary reused", sum.calls)
	}
	joined := renderAll(msgs)
	if !strings.Contains(joined, sum.summary) {
		t.Fatalf("the replayed history does not carry the summary: %q", truncate(joined))
	}
	for _, covered := range []string{"domanda-1:", "risposta-2:", "domanda-3:", "risposta-4:"} {
		if strings.Contains(joined, covered) {
			t.Fatalf("covered turn %s was replayed verbatim: %q", covered, truncate(joined))
		}
	}
	// The active round is never condensed — it is the turn being answered.
	if !strings.Contains(joined, "domanda-5:") || !strings.Contains(msgs[len(msgs)-1].Content, "risposta-6:") {
		t.Fatalf("the active round must survive verbatim, got %q", truncate(msgs[len(msgs)-1].Content))
	}
}

// The guard the first live run found: on a short thread the summary is BIGGER than the turns
// it would replace, and a stored compaction stays in force forever. It must not be written.
func TestCompact_RefusesASummaryLongerThanTheConversation(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	convID := newConversation(t, store)
	seedTurns(t, store,
		convID,
		AppendTurnParams{ConversationID: convID, Seq: 1, Role: llm.RoleUser, Content: "ciao"},
		AppendTurnParams{ConversationID: convID, Seq: 2, Role: llm.RoleAssistant, Content: "ciao a te"},
		AppendTurnParams{ConversationID: convID, Seq: 3, Role: llm.RoleUser, Content: "e ora?"},
	)

	sum := &fakeSummarizer{summary: strings.Repeat("un riassunto molto piu' lungo dei turni. ", 200)}
	_, err := store.Compact(ownerCtx(), convID, 0, compactConfig(sum))
	if !errors.Is(err, ErrCompactionNotWorthwhile) {
		t.Fatalf("Compact = %v, want ErrCompactionNotWorthwhile", err)
	}
	if _, ok, loadErr := store.LoadCompaction(ownerCtx(), convID, ""); loadErr != nil || ok {
		t.Fatalf("a refused compaction must leave no row: (%v, %v)", ok, loadErr)
	}
}

// Compact reads history through the same identity-scoped path as every other read, so
// without a principal it sees NOTHING and refuses — it never compacts a thread the caller
// cannot see.
//
// This is here because it was found the hard way: all three tests above called Compact with
// context.Background() and passed on the developer's Postgres, where the `aura` role is
// SUPERUSER with BYPASSRLS and row-level security therefore does not apply. The coverage
// gate's disposable Postgres provisions the role without those attributes, RLS engaged, and
// the tests failed with "nothing to compact". A local green against a bypassing role is not
// the same test.
func TestCompact_WithoutAPrincipalSeesNothing(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	convID := newConversation(t, store)
	var seeded []AppendTurnParams
	for seq := 1; seq <= 5; seq += 2 {
		seeded = append(seeded, verboseRound(convID, seq)...)
	}
	seedTurns(t, store, convID, seeded...)

	sum := &fakeSummarizer{summary: "non deve mai essere chiesto"}
	if _, err := store.Compact(context.Background(), convID, 0, compactConfig(sum)); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("Compact with no principal = %v, want ErrNothingToCompact", err)
	}
	if sum.calls != 0 {
		t.Fatalf("summarizer called %d times for a thread the caller cannot see", sum.calls)
	}
	if _, ok, loadErr := store.LoadCompaction(ownerCtx(), convID, ""); loadErr != nil || ok {
		t.Fatalf("a principal-less Compact must leave no row: (%v, %v)", ok, loadErr)
	}
}

func TestCompact_NothingToCompactOnAFreshConversation(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	convID := newConversation(t, store)
	seedTurns(t, store, convID,
		AppendTurnParams{ConversationID: convID, Seq: 1, Role: llm.RoleUser, Content: "prima domanda"})

	sum := &fakeSummarizer{summary: "irrilevante"}
	if _, err := store.Compact(ownerCtx(), convID, 0, compactConfig(sum)); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("Compact on a single-turn conversation = %v, want ErrNothingToCompact", err)
	}
	if sum.calls != 0 {
		t.Fatalf("summarizer called %d times, want 0: there was nothing to summarize", sum.calls)
	}
}

// The operator compacts the branch on screen, which is not always the one written last
// (spec 2026-10-27, D13). Each branch keeps its own row, and a leaf of 0 is the turn written
// last.
func TestCompact_CondensesTheBranchItsLeafNames(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	convID := newConversation(t, store)
	var seeded []AppendTurnParams
	for seq := 1; seq <= 5; seq += 2 {
		seeded = append(seeded, verboseRound(convID, seq)...)
	}
	seedTurns(t, store, convID, seeded...)
	// Question 3 rewritten answers on a branch of its own: 1, 2, 7, 8.
	rewrite := verboseRound(convID, 7)
	forkSeq, err := store.ForkBranch(ownerCtx(), convID, 3, llm.RoleUser, rewrite[0].Content)
	if err != nil || forkSeq != 7 {
		t.Fatalf("ForkBranch = (%d, %v), want seq 7", forkSeq, err)
	}
	seedTurns(t, store, convID, rewrite[1])

	older := &fakeSummarizer{summary: "il ramo originale: tre domande verbose"}
	result, err := store.Compact(ownerCtx(), convID, 6, compactConfig(older))
	if err != nil {
		t.Fatalf("Compact(leaf 6): %v", err)
	}
	if result.CoversThroughSeq != 4 {
		t.Fatalf("Compact(leaf 6) covers through %d, want 4: the round before 5-6 on its own path",
			result.CoversThroughSeq)
	}
	condensed := renderAll(older.gotRounds)
	if !strings.Contains(condensed, "domanda-3:") || strings.Contains(condensed, "domanda-7:") {
		t.Fatalf("the summarizer was not handed the older branch: %q", truncate(condensed))
	}
	if got, ok, err := store.LoadPathCompaction(ownerCtx(), convID, 6); err != nil || !ok || got.Summary != older.summary {
		t.Fatalf("LoadPathCompaction(6) = (%+v, %v, %v), want the older branch's summary", got, ok, err)
	}
	for _, leaf := range []int{8, 0} {
		if got, ok, err := store.LoadPathCompaction(ownerCtx(), convID, leaf); err != nil || ok {
			t.Fatalf("LoadPathCompaction(%d) = (%+v, %v, %v): the branch written last was compacted by a /compact of the other",
				leaf, got, ok, err)
		}
	}

	latest := &fakeSummarizer{summary: "il ramo riscritto"}
	result, err = store.Compact(ownerCtx(), convID, 0, compactConfig(latest))
	if err != nil {
		t.Fatalf("Compact(leaf 0): %v", err)
	}
	if result.CoversThroughSeq != 2 {
		t.Fatalf("Compact(leaf 0) covers through %d, want 2: the path written last is 1, 2, 7, 8",
			result.CoversThroughSeq)
	}
	if got, ok, err := store.LoadPathCompaction(ownerCtx(), convID, 8); err != nil || !ok || got.Summary != latest.summary {
		t.Fatalf("LoadPathCompaction(8) = (%+v, %v, %v), want the rewritten branch's summary", got, ok, err)
	}
	if got, ok, err := store.LoadPathCompaction(ownerCtx(), convID, 6); err != nil || !ok ||
		got.Summary != older.summary || got.CoversThroughSeq != 4 {
		t.Fatalf("LoadPathCompaction(6) = (%+v, %v, %v): compacting one branch moved the other's row", got, ok, err)
	}
	// Turn 1 is on both paths, and its branch is the canonical one, where leaf 6 filed.
	if got, ok, err := store.LoadPathCompaction(ownerCtx(), convID, 1); err != nil || !ok || got.Summary != older.summary {
		t.Fatalf("LoadPathCompaction(1) = (%+v, %v, %v), want the canonical branch's summary", got, ok, err)
	}
	if _, _, err := store.LoadPathCompaction(ownerCtx(), convID, 99); !errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("LoadPathCompaction(99) = %v, want ErrTurnNotFound", err)
	}
}

// A thread with no turn has nothing compacted, which is an answer and not an error; an id
// that is not a uuid is an error, and not a missing turn.
func TestLoadPathCompaction_NoTurnAndAMalformedID(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	convID := newConversation(t, store)

	if got, ok, err := store.LoadPathCompaction(ownerCtx(), convID, 0); err != nil || ok {
		t.Fatalf("LoadPathCompaction on a thread with no turn = (%+v, %v, %v), want none and no error", got, ok, err)
	}
	if _, _, err := store.LoadPathCompaction(ownerCtx(), "not-a-uuid", 3); err == nil || errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("LoadPathCompaction on a malformed id = %v, want a parse error", err)
	}
}

// The row's own guards: a malformed key is an error, and a summary must say something about
// at least one turn. One turn is enough.
func TestCompactionRow_RefusesWhatItCannotFile(t *testing.T) {
	pool := migratedPool(t)
	store := newStore(t, pool)
	convID := newConversation(t, store)

	if _, _, err := store.LoadCompaction(ownerCtx(), "not-a-uuid", ""); err == nil {
		t.Fatal("LoadCompaction on a malformed conversation id succeeded")
	}
	if _, _, err := store.LoadCompaction(ownerCtx(), convID, "not-a-uuid"); err == nil {
		t.Fatal("LoadCompaction on a malformed branch id succeeded")
	}
	if err := store.SaveCompaction(ownerCtx(), "not-a-uuid", "", Compaction{Summary: "s", CoversThroughSeq: 1}); err == nil {
		t.Fatal("SaveCompaction on a malformed conversation id succeeded")
	}
	for name, c := range map[string]Compaction{
		"no turn":    {Summary: "s", CoversThroughSeq: 0},
		"no summary": {Summary: "", CoversThroughSeq: 3},
	} {
		if err := store.SaveCompaction(ownerCtx(), convID, "", c); err == nil {
			t.Fatalf("SaveCompaction with %s succeeded", name)
		}
	}
	if _, ok, err := store.LoadCompaction(ownerCtx(), convID, ""); err != nil || ok {
		t.Fatalf("a refused save left a row: (%v, %v)", ok, err)
	}
	if err := store.SaveCompaction(ownerCtx(), convID, "", Compaction{Summary: "one turn", CoversThroughSeq: 1}); err != nil {
		t.Fatalf("SaveCompaction covering one turn: %v", err)
	}
	if got, ok, err := store.LoadCompaction(ownerCtx(), convID, ""); err != nil || !ok || got.CoversThroughSeq != 1 {
		t.Fatalf("LoadCompaction after a one-turn save = (%+v, %v, %v)", got, ok, err)
	}
}

func renderAll(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func truncate(s string) string {
	if len(s) <= 300 {
		return s
	}
	return s[:300] + "…"
}
