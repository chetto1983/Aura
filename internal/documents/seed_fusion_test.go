package documents

import (
	"slices"
	"testing"
)

// Retrieval seed gate. The dense seed answers paraphrase; the lexical seed answers
// the exact literal. Production ran dense-only and kept lexical as a FALLBACK — it
// executed solely when the vector leg came back empty — so a query naming one thing
// exactly retrieved seven confident, cited, wrong chunks instead.
//
// Reproduced live against the deployed corpus (Clienti.xlsx, 302 chunks):
//
//	document_search "ZOPPI SRL"  -> 7 hits, top scores .85/.85/.84, NONE containing ZOPPI
//	fulltext        "ZOPPI SRL"  -> chunk ..._000117 at rank 1, score 2.767 (3x margin)
//
// chunk ..._000117 is the only chunk of the 302 that contains the string. These tests
// pin the union: whatever either leg finds, the seed carries into rerank.

// seedRowsFor builds the dense-leg fixture rows.
func fusionSeedRows(ids ...string) []map[string]any {
	rows := make([]map[string]any, 0, len(ids))
	for i, id := range ids {
		rows = append(rows, seedRow("doc-1", id, "text "+id, 0.9-float64(i)*0.01))
	}
	return rows
}

func fusionHits(ids ...string) []SearchHit {
	hits := make([]SearchHit, 0, len(ids))
	for i, id := range ids {
		hits = append(hits, SearchHit{
			DocumentID: "doc-1", ChunkID: id, Text: "text " + id,
			Score: 3.0 - float64(i),
		})
	}
	return hits
}

func seedChunkIDs(hits []SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ChunkID)
	}
	return out
}

// The literal the dense leg cannot reach must still be seeded.
func TestSeedHits_CarriesTheLexicalOnlyChunk(t *testing.T) {
	svc := &Service{
		Knowledge:     &fakeRetrieveKnowledge{seedRows: fusionSeedRows("chunk-a", "chunk-b")},
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Searcher:      &fakeSearchBackend{hits: fusionHits("chunk-z")},
	}

	got, err := svc.seedHits(t.Context(), SearchRequest{Query: "ZOPPI SRL", Limit: 5})
	if err != nil {
		t.Fatalf("seedHits: %v", err)
	}
	ids := seedChunkIDs(got)
	if !slices.Contains(ids, "chunk-z") {
		t.Errorf("lexical-only chunk missing from the seed: %v", ids)
	}
	for _, want := range []string{"chunk-a", "chunk-b"} {
		if !slices.Contains(ids, want) {
			t.Errorf("dense chunk %q dropped from the seed: %v", want, ids)
		}
	}
}

// A chunk both legs return is seeded once, and agreement ranks it first: two
// independent retrievers voting for the same chunk is the strongest signal available
// before the reranker sees anything.
func TestSeedHits_DedupesAndPrefersAgreement(t *testing.T) {
	svc := &Service{
		Knowledge:     &fakeRetrieveKnowledge{seedRows: fusionSeedRows("chunk-a", "chunk-shared")},
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Searcher:      &fakeSearchBackend{hits: fusionHits("chunk-shared", "chunk-z")},
	}

	got, err := svc.seedHits(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatalf("seedHits: %v", err)
	}
	ids := seedChunkIDs(got)
	seen := map[string]int{}
	for _, id := range ids {
		seen[id]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("chunk %q seeded %d times: %v", id, n, ids)
		}
	}
	if len(ids) == 0 || ids[0] != "chunk-shared" {
		t.Errorf("seed order = %v, want the chunk both legs returned first", ids)
	}
}

// Either leg alone still seeds: a corpus with no fulltext match, or an embedder that
// is down, must degrade to the other leg rather than to nothing.
func TestSeedHits_EitherLegAlone(t *testing.T) {
	t.Run("lexical_empty", func(t *testing.T) {
		svc := &Service{
			Knowledge:     &fakeRetrieveKnowledge{seedRows: fusionSeedRows("chunk-a")},
			QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
			Searcher:      &fakeSearchBackend{},
		}
		got, err := svc.seedHits(t.Context(), SearchRequest{Query: "q", Limit: 5})
		if err != nil || !slices.Equal(seedChunkIDs(got), []string{"chunk-a"}) {
			t.Fatalf("dense-only seed = %v (%v), want [chunk-a]", seedChunkIDs(got), err)
		}
	})

	t.Run("dense_unavailable", func(t *testing.T) {
		svc := &Service{
			Knowledge: &fakeRetrieveKnowledge{},
			Searcher:  &fakeSearchBackend{hits: fusionHits("chunk-z")},
		}
		got, err := svc.seedHits(t.Context(), SearchRequest{Query: "q", Limit: 5})
		if err != nil || !slices.Equal(seedChunkIDs(got), []string{"chunk-z"}) {
			t.Fatalf("lexical-only seed = %v (%v), want [chunk-z]", seedChunkIDs(got), err)
		}
	})
}

// Fusion is a pure function of the two rankings: the same pair of legs produces the
// same seed order every time, so a repeated search cannot reshuffle the citations.
func TestSeedHits_IsDeterministic(t *testing.T) {
	newService := func() *Service {
		return &Service{
			Knowledge:     &fakeRetrieveKnowledge{seedRows: fusionSeedRows("chunk-a", "chunk-b", "chunk-c")},
			QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
			Searcher:      &fakeSearchBackend{hits: fusionHits("chunk-c", "chunk-z")},
		}
	}
	first, err := newService().seedHits(t.Context(), SearchRequest{Query: "q", Limit: 5})
	if err != nil {
		t.Fatalf("seedHits: %v", err)
	}
	for i := range 3 {
		got, err := newService().seedHits(t.Context(), SearchRequest{Query: "q", Limit: 5})
		if err != nil {
			t.Fatalf("seedHits: %v", err)
		}
		if !slices.Equal(seedChunkIDs(got), seedChunkIDs(first)) {
			t.Fatalf("run %d = %v, first = %v", i+2, seedChunkIDs(got), seedChunkIDs(first))
		}
	}
}
