package documents

import (
	"strings"
	"testing"
)

// Batching by chunk COUNT says nothing about request size, and that gap took a live
// deployment down silently. Clienti.xlsx chunked into ~8 KB rows; a full 32-chunk batch
// asked the embedding sidecar for ~67k tokens against its 4096, got HTTP 400 five times,
// dead-lettered — and the document stayed "ready" with 118 chunks and zero embeddings.
// Retrieval then had only fulltext to work with and returned rows scoring 0.018.
func TestBatchEndStopsOnTheByteBudgetNotJustTheCount(t *testing.T) {
	t.Parallel()
	wide := func(n int) []Chunk {
		out := make([]Chunk, n)
		for i := range out {
			out[i] = Chunk{Text: strings.Repeat("x", 8000)}
		}
		return out
	}

	t.Run("wide chunks stop well before the count cap", func(t *testing.T) {
		t.Parallel()
		// 32 allowed by count, but 24000 bytes only fits three 8 KB chunks.
		if got := batchEnd(wide(32), 0, 32, 24000); got != 3 {
			t.Fatalf("batchEnd = %d, want 3: the byte budget must bind before the count", got)
		}
	})

	t.Run("narrow chunks still fill the count cap", func(t *testing.T) {
		t.Parallel()
		small := make([]Chunk, 100)
		for i := range small {
			small[i] = Chunk{Text: "short"}
		}
		if got := batchEnd(small, 0, 32, 24000); got != 32 {
			t.Fatalf("batchEnd = %d, want 32: prose must not be penalised by the byte cap", got)
		}
	})

	t.Run("a chunk bigger than the whole budget still advances", func(t *testing.T) {
		t.Parallel()
		// Never return start: the caller loops on it, and a zero-width batch would spin
		// forever. Sent alone, the sidecar rejects it with a message naming the size —
		// a far better failure than a hang.
		huge := []Chunk{{Text: strings.Repeat("x", 100000)}, {Text: "next"}}
		if got := batchEnd(huge, 0, 32, 24000); got != 1 {
			t.Fatalf("batchEnd = %d, want 1: an oversized chunk must still be attempted alone", got)
		}
	})

	t.Run("the tail batch is whatever is left", func(t *testing.T) {
		t.Parallel()
		if got := batchEnd(wide(4), 3, 32, 24000); got != 4 {
			t.Fatalf("batchEnd = %d, want 4", got)
		}
	})
}
