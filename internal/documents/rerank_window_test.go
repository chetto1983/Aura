package documents

import (
	"fmt"
	"strings"
	"testing"
)

// rerankVisibleRunes mirrors rerankVisibleRunes in internal/rerank/client.go:32, which is
// unexported. The coupling is real and load-bearing — focusWindowRunes is sized against
// it — so it is written down here rather than left implicit. If the rerank client raises
// its cap, this number and the reasoning in focusWindow both have to move.
const rerankVisibleRunes = 480

// clientChunk reproduces the shape that broke retrieval: 50 spreadsheet rows in one
// ~6400-character chunk, with the answering row near the end and the reranker only ever
// reading the first 480 characters.
func clientChunk(answerRow int) string {
	var b strings.Builder
	for i := 5851; i <= 5900; i++ {
		name := fmt.Sprintf("AZIENDA GENERICA %d SRL", i)
		if i == answerRow {
			name = "ZOPPI SRL"
		}
		fmt.Fprintf(&b, "row %d: A%d: DIST.CUNEO | B%d: F031 | C%d: Alba | D%d: %d | E%d: %s | F%d: TREISO | G%d: BERBOTTO PAOLO ",
			i, i, i, i, i, 400000+i, i, name, i, i)
	}
	return b.String()
}

func TestFocusWindowPutsTheAnswerWhereTheRerankerCanSeeIt(t *testing.T) {
	t.Parallel()

	const answerRow = 5882
	chunk := clientChunk(answerRow)
	if len([]rune(chunk)) < 4000 {
		t.Fatalf("fixture is %d runes; it must be big enough to overflow every downstream window", len([]rune(chunk)))
	}
	if strings.Contains(string([]rune(chunk)[:rerankVisibleRunes]), "ZOPPI") {
		t.Fatal("fixture invalid: the answer must NOT already be in the reranker's first 480 runes, " +
			"otherwise this test proves nothing")
	}

	got := focusWindow(chunk, "codice cliente di ZOPPI SRL")

	head := string([]rune(got)[:min(rerankVisibleRunes, len([]rune(got)))])
	if !strings.Contains(head, "ZOPPI SRL") {
		t.Fatalf("the answer is not in the first %d runes the reranker reads; got head %q",
			rerankVisibleRunes, head)
	}
	if !strings.Contains(head, "424410"[:3]) && !strings.Contains(head, fmt.Sprint(400000+answerRow)) {
		t.Errorf("the matching row lost its client code: %q", head)
	}
	if len([]rune(got)) > focusWindowRunes {
		t.Errorf("window is %d runes, over the %d cap that keeps it inside the 2048-byte preview",
			len([]rune(got)), focusWindowRunes)
	}
}

// A query the corpus cannot answer must not be "focused" onto an arbitrary row: with no
// term overlap the chunk keeps its head, scores near zero, and the relevance floor drops
// it. Inventing a window here would manufacture false evidence for a real miss.
func TestFocusWindowLeavesUnmatchedChunksAlone(t *testing.T) {
	t.Parallel()

	chunk := clientChunk(5882)
	got := focusWindow(chunk, "B&R 8LSA35 servomotor specifications")
	if !strings.HasPrefix(got, "row 5851:") {
		t.Fatalf("a chunk with no term overlap must keep its head, got %q", string([]rune(got)[:60]))
	}
}

func TestFocusWindowIsANoOpOnSmallChunks(t *testing.T) {
	t.Parallel()

	const prose = "La sovranità appartiene al popolo, che la esercita nelle forme e nei limiti della Costituzione."
	if got := focusWindow(prose, "sovranità popolo"); got != prose {
		t.Fatalf("a chunk already inside the window must be returned verbatim, got %q", got)
	}
}

// focusSeeds must not mutate its input: the caller still holds the seed slice, and a
// silent in-place rewrite would make the un-focused text unrecoverable.
func TestFocusSeedsDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := clientChunk(5882)
	seeds := []SearchHit{{ChunkID: "c1", Text: original}}
	focused := focusSeeds(seeds, "codice cliente di ZOPPI SRL")
	if seeds[0].Text != original {
		t.Fatal("focusSeeds mutated the caller's slice")
	}
	if focused[0].Text == original {
		t.Fatal("focusSeeds returned the chunk unchanged")
	}
}
