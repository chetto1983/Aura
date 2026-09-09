package documents

import (
	"errors"
	"strconv"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// A chunk boundary can cut a table or a definition in half, and the half that answers the
// question is by construction the passage that did NOT match: no rephrasing of the query
// ranks it. Measured 2026-09-09 with only document_search and document_open, the ArcadeDB
// manual's vector.fuse options table came back starting on its last row, four rephrasings
// and a document_ids filter all returned the identical chunk, and chasing the missing header
// surfaced a DIFFERENT API's option name -- a plausible wrong answer beside the right one.
// Each passage owns its character span, which is what makes its citation token its own.
func neighbourAt(ordinal int64, text string) arcadedb.PassageCandidate {
	candidate := retrievalCandidate(arcadedb.RetrievalLegFused)
	candidate.PassageID = "doc_9f2c:" + strconv.FormatInt(ordinal, 10)
	candidate.Ordinal, candidate.Text, candidate.FusedScore = ordinal, text, nil
	candidate.CharacterSpan = &arcadedb.CharacterSpan{Start: ordinal * 100, End: ordinal*100 + 80}
	candidate.HeadingPath = []string{"Documentation", "6.4. Vector"}
	return candidate
}

func neighbourRetriever(t *testing.T, index *fakePassageIndex) *HostRetriever {
	t.Helper()
	score := 0.7
	hit := retrievalCandidate(arcadedb.RetrievalLegFused)
	hit.FusedScore = &score
	index.fused = []arcadedb.PassageCandidate{hit}
	return &HostRetriever{
		ControlPlane: &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		PassageIndex: index,
		Embedder:     &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}
}

func TestNeighboursSurroundTheHitAndCarryTheirOwnCitation(t *testing.T) {
	index := &fakePassageIndex{at: []arcadedb.PassageCandidate{
		neighbourAt(41, "intestazione della tabella"),
		neighbourAt(43, "riga successiva"),
	}}
	response, err := neighbourRetriever(t, index).Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "vector.fuse", Neighbours: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	passage := response.Documents[0].Passages[0]
	if len(passage.ContextBefore) != 1 || passage.ContextBefore[0].Ordinal != 41 {
		t.Fatalf("context_before = %+v, want ordinal 41", passage.ContextBefore)
	}
	if len(passage.ContextAfter) != 1 || passage.ContextAfter[0].Ordinal != 43 {
		t.Fatalf("context_after = %+v, want ordinal 43", passage.ContextAfter)
	}
	// Quoting a neighbour under the hit's token would cite text the hit does not contain.
	if passage.ContextBefore[0].CitationToken == passage.CitationToken {
		t.Fatal("a neighbour was given the citation token of the passage it neighbours")
	}
	if passage.ContextBefore[0].CitationToken == "" {
		t.Fatal("a neighbour arrived with no citation of its own")
	}
	// document_search orders its caller to cite the citation_token AND the locator's
	// heading_path. A neighbour that carried only the token was being asked for a citation
	// it had been handed half of -- and the neighbour is precisely where the answer is when
	// a table is split, so the missing half is the one needed.
	before := passage.ContextBefore[0]
	if len(before.Locator.HeadingPath) == 0 {
		t.Fatal("a neighbour arrived with no heading_path to cite")
	}
	if before.Locator.CharStart == nil || *before.Locator.CharStart != 4100 {
		t.Fatalf("a neighbour's character span is not its own: %+v", before.Locator)
	}
}

// The context must be a contiguous run: returning ordinal+2 when ordinal+1 is missing
// presents two disjoint fragments as though one ran on from the other.
func TestNeighboursStopAtTheFirstGap(t *testing.T) {
	index := &fakePassageIndex{at: []arcadedb.PassageCandidate{neighbourAt(44, "due dopo")}}
	response, err := neighbourRetriever(t, index).Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "vector.fuse", Neighbours: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if after := response.Documents[0].Passages[0].ContextAfter; len(after) != 0 {
		t.Fatalf("context_after = %+v, want nothing across the gap at 43", after)
	}
}

// Zero is the default and it must cost nothing: no lookup, no field.
func TestNeighboursAreNotFetchedWhenNotAsked(t *testing.T) {
	index := &fakePassageIndex{at: []arcadedb.PassageCandidate{neighbourAt(43, "mai chiesto")}}
	response, err := neighbourRetriever(t, index).Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "vector.fuse",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(index.atRefs) != 0 {
		t.Fatalf("an unasked-for lookup was issued for %+v", index.atRefs)
	}
	if len(response.Documents[0].Passages[0].ContextAfter) != 0 {
		t.Fatal("context arrived without being asked for")
	}
}

// The passages are intact and the answer correct without their surroundings, so losing the
// whole response over a convenience would be the worse answer, not the safer one.
func TestNeighbourFailureLeavesTheAnswerStanding(t *testing.T) {
	index := &fakePassageIndex{atErr: errors.New("server unavailable")}
	response, err := neighbourRetriever(t, index).Retrieve(t.Context(), RetrievalRequest{
		IdentityID: retrievalIdentity, Query: "vector.fuse", Neighbours: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != RetrievalComplete || len(response.Documents[0].Passages) != 1 {
		t.Fatalf("a failed context lookup damaged the answer: %#v", response)
	}
}

func TestNeighboursAreBounded(t *testing.T) {
	for _, count := range []int{-1, MaxRetrievalNeighbours + 1} {
		_, err := neighbourRetriever(t, &fakePassageIndex{}).Retrieve(t.Context(), RetrievalRequest{
			IdentityID: retrievalIdentity, Query: "vector.fuse", Neighbours: count,
		})
		if !errors.Is(err, ErrInvalidRetrievalRequest) {
			t.Fatalf("neighbours %d was accepted: %v", count, err)
		}
	}
}
