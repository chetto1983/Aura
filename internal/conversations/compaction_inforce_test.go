// Unit tier (no build tag): a compaction that already exists is a STATE of the branch, so
// the ladder replays it whether or not the budget asks for one. That rule is what gives
// `/compact` its effect — without it an operator-requested compaction would write a row
// nothing reads until the conversation grew back over the trigger on its own.
package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestApplyStoredCompaction_ReplacesOnlyTheCoveredPrefix(t *testing.T) {
	cache := &fakeCompactionCache{present: true, stored: Compaction{
		Summary: "what happened earlier", CoversThroughSeq: 4, SourceTurns: 3,
	}}
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleUser, Content: "first question"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "first answer"},
		{Seq: 4, Role: llm.RoleUser, Content: "second question"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "second answer"},
		{Seq: 6, Role: llm.RoleUser, Content: "the active round"},
	}

	out, ok := applyStoredCompaction(context.Background(), cache, "conv", "", turns)
	if !ok {
		t.Fatal("a stored summary covering seq<=4 must apply")
	}
	// system head, the summary, the one uncovered historical turn, the active round.
	if len(out) != 4 {
		t.Fatalf("turns = %d, want 4: %+v", len(out), out)
	}
	if out[0].Seq != 1 || out[0].Role != llm.RoleSystem {
		t.Errorf("the protected head must survive verbatim, got %+v", out[0])
	}
	if !isCompaction(out[1]) || !strings.Contains(out[1].Content, "what happened earlier") {
		t.Errorf("turn 1 must be the summary turn, got %+v", out[1])
	}
	if out[1].Seq != 4 {
		t.Errorf("the summary turn must carry the watermark as its seq, got %d", out[1].Seq)
	}
	// seq 5 is PAST the watermark: the summary does not speak for it, so it stays verbatim.
	if out[2].Seq != 5 || out[2].Content != "second answer" {
		t.Errorf("an uncovered turn must stay verbatim, got %+v", out[2])
	}
	if out[3].Content != "the active round" {
		t.Errorf("the active round must survive verbatim, got %+v", out[3])
	}
}

func TestApplyStoredCompaction_LeavesTheTurnsAloneWhenNothingIsCovered(t *testing.T) {
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 8, Role: llm.RoleUser, Content: "old"},
		{Seq: 9, Role: llm.RoleAssistant, Content: "answer"},
		{Seq: 10, Role: llm.RoleUser, Content: "active"},
	}
	for name, cache := range map[string]compactionCache{
		"no cache wired":  nil,
		"never compacted": &fakeCompactionCache{},
		// A branch replayed from a point the watermark has run past: the row exists but
		// speaks for turns this path does not contain.
		"watermark behind every turn": &fakeCompactionCache{present: true, stored: Compaction{
			Summary: "stale", CoversThroughSeq: 4,
		}},
		"blank summary": &fakeCompactionCache{present: true, stored: Compaction{
			Summary: "   ", CoversThroughSeq: 9,
		}},
		"unreadable": &fakeCompactionCache{loadErr: errors.New("boom")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := applyStoredCompaction(context.Background(), cache, "conv", "", turns); ok {
				t.Fatal("want no compaction applied")
			}
		})
	}
}

// The rule the command depends on: nothing is over budget, no summarizer is called, and the
// covered turns are STILL replaced by the summary the operator already paid for.
func TestLadder_KeepsAStoredCompactionInForceUnderTheBudget(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}
	sum := &fakeSummarizer{summary: "must not be called"}
	cache := &fakeCompactionCache{present: true, stored: Compaction{
		Summary: "the earlier turns, condensed", CoversThroughSeq: 3, SourceTurns: 2,
	}}
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleUser, Content: "first question"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "first answer"},
		{Seq: 4, Role: llm.RoleUser, Content: "the active round"},
	}
	// A window nothing here comes close to filling: the ladder has no budget reason to act.
	cfg := ContextConfig{
		ContextWindow: 200000, MaxOutputTokens: 1000, ToolEvictAfterTurns: 10,
		CompactionTriggerPercent: 50, Summarizer: sum, compactionCache: cache,
	}

	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if sum.calls != 0 {
		t.Fatalf("summarizer called %d times, want 0: the stored summary is already in hand", sum.calls)
	}
	if cache.saves != 0 {
		t.Fatalf("cache written %d times, want 0: nothing new was summarized", cache.saves)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want sys + summary + active: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[1].Content, "the earlier turns, condensed") {
		t.Fatalf("the summary must replace the covered turns, got %q", msgs[1].Content)
	}
	if msgs[1].Role != llm.RoleUser {
		t.Fatalf("the summary turn must reach the wire as a plain user message, got %q", msgs[1].Role)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "first question") || strings.Contains(m.Content, "first answer") {
			t.Fatalf("a covered turn was replayed verbatim: %q", m.Content)
		}
	}
	if msgs[2].Content != "the active round" {
		t.Fatalf("the active round must survive verbatim, got %q", msgs[2].Content)
	}
}

// Compaction disabled means disabled: no summarizer, no request, no row.
func TestCompact_WithoutASummarizerIsUnavailable(t *testing.T) {
	var store *Store
	_, err := store.Compact(context.Background(), "conv", ContextConfig{})
	if !errors.Is(err, ErrCompactionUnavailable) {
		t.Fatalf("err = %v, want ErrCompactionUnavailable", err)
	}
}
