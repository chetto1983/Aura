package documents

import (
	"testing"

	"github.com/chetto1983/aura/internal/rerank"
)

// The numbers here are measured, not invented. On the live deployment 2026-07-30 the
// reranker scored a customer-spreadsheet chunk 0.00079 against a question about a B&R
// servo motor — its own verdict of "irrelevant" — and the pipeline returned the chunk
// anyway, because no stage was permitted to drop anything. Against a question the corpus
// can actually answer, the same shape of chunk scored 0.0586, and the row carrying the
// answer scored 0.99. Those three numbers are the fixtures below.
func TestDropBelowRelevanceFloor(t *testing.T) {
	t.Parallel()

	seeds := []SearchHit{
		{ChunkID: "chunk_a", Text: "customer rows"},
		{ChunkID: "chunk_b", Text: "more customer rows"},
	}
	irrelevant := []rerank.Scored{{Index: 0, Score: 0.00079}, {Index: 1, Score: 0.000057}}
	relevant := []rerank.Scored{{Index: 1, Score: 0.9933}, {Index: 0, Score: 0.0586}}

	t.Run("an irrelevant set becomes empty", func(t *testing.T) {
		got := dropBelowRelevanceFloor(seeds, seeds, irrelevant, 0.5)
		if len(got) != 0 {
			t.Fatalf("kept %d hit(s) scored at most 0.00079 under a 0.5 floor: retrieval "+
				"must be able to answer \"I have nothing\"", len(got))
		}
	})

	t.Run("the hit carrying the answer survives", func(t *testing.T) {
		ordered := []SearchHit{seeds[1], seeds[0]}
		got := dropBelowRelevanceFloor(ordered, seeds, relevant, 0.5)
		if len(got) != 1 || got[0].ChunkID != "chunk_b" {
			t.Fatalf("got %+v, want only chunk_b (0.9933); chunk_a scored 0.0586, which is "+
				"what a chunk looks like when the answer is outside the reranker's window", got)
		}
	})

	t.Run("zero disables it, and so does a missing reranker", func(t *testing.T) {
		if got := dropBelowRelevanceFloor(seeds, seeds, irrelevant, 0); len(got) != 2 {
			t.Fatalf("floor 0 dropped %d hit(s); it must be a no-op so a deployment "+
				"without the knob keeps today's behaviour", 2-len(got))
		}
		if got := dropBelowRelevanceFloor(seeds, seeds, nil, 0.5); len(got) != 2 {
			t.Fatal("no rerank scores means no opinion: dropping here would gate seed " +
				"scores (Lucene, cosine, term counts) that share no scale with [0,1]")
		}
	})

	t.Run("a length or index mismatch never drops", func(t *testing.T) {
		short := []rerank.Scored{{Index: 0, Score: 0.001}}
		if got := dropBelowRelevanceFloor(seeds, seeds, short, 0.5); len(got) != 2 {
			t.Fatal("a mismatched rerank result must leave the set untouched, not empty it")
		}
		bad := []rerank.Scored{{Index: 7, Score: 0.99}, {Index: 1, Score: 0.99}}
		got := dropBelowRelevanceFloor(seeds, seeds, bad, 0.5)
		if len(got) != 1 || got[0].ChunkID != "chunk_b" {
			t.Fatalf("got %+v: an out-of-range index must be skipped, not panic or admit", got)
		}
	})
}
