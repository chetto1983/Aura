package conversations

import (
	"context"
	"errors"
	"testing"
)

func TestGenerationFifthForcesRebase(t *testing.T) {
	for generation := 0; generation < 4; generation++ {
		if NeedsCanonicalRebase(RebaseQuality{Generation: generation, Coverage: 1, Entailment: 1, Similarity: 1, Depth: generation}) {
			t.Fatalf("generation %d unexpectedly rebases", generation)
		}
	}
	if !NeedsCanonicalRebase(RebaseQuality{Generation: 4, Coverage: 1, Entailment: 1, Similarity: 1, Depth: 4}) {
		t.Fatal("fifth generation must rebase")
	}
}

func TestRebaseDriftThresholds(t *testing.T) {
	for _, quality := range []RebaseQuality{
		{Coverage: .99, Entailment: 1, Similarity: 1},
		{Coverage: 1, Entailment: .979, Similarity: 1},
		{Coverage: 1, Entailment: 1, Similarity: .899},
	} {
		if !NeedsCanonicalRebase(quality) {
			t.Fatalf("quality %+v must rebase", quality)
		}
	}
}

func TestRebaseChunksAtSixtyPercentAndNoDuplicateTailFacts(t *testing.T) {
	units := []CanonicalSemanticUnit{{Seqs: []int{1}, Tokens: 40}, {Seqs: []int{2}, Tokens: 20}, {Seqs: []int{3}, Tokens: 40}}
	seen := map[int]int{}
	result, err := RebaseCanonical(context.Background(), units, RebaseContract{ModelVersion: "m1", PromptVersion: "p1", ProbeVersion: "q1", ScorerVersion: "s1", SummarizerCapacity: 100}, func(_ context.Context, chunk []CanonicalSemanticUnit) (RebaseChunk, error) {
		tokens := 0
		var seqs []int
		for _, unit := range chunk {
			tokens += unit.Tokens
			seqs = append(seqs, unit.Seqs...)
			for _, seq := range unit.Seqs {
				seen[seq]++
			}
		}
		if tokens > 60 {
			t.Fatalf("chunk tokens=%d", tokens)
		}
		return RebaseChunk{SourceSeqs: seqs, Summary: "bounded"}, nil
	})
	if err != nil || len(result.Chunks) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for seq := 1; seq <= 3; seq++ {
		if seen[seq] != 1 {
			t.Fatalf("seq %d seen %d times", seq, seen[seq])
		}
	}
}

func TestRebaseFailurePreservesLKGAndDisablesAutoGenerations(t *testing.T) {
	lkg := RebaseState{ActiveCheckpointID: "lkg", AutoGenerationsEnabled: true}
	got := ApplyRebase(lkg, RebaseResult{}, errors.New("quality rejected"))
	if got.ActiveCheckpointID != "lkg" || got.AutoGenerationsEnabled || got.QuarantineReason == "" {
		t.Fatalf("state=%+v", got)
	}
}
