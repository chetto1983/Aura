//go:build retrieval_eval

// Which of ArcadeDB's own levers change what the fused document query answers, or what it costs.
// Each variant is the shipped statement with one lever changed, and every variant is judged
// against the shipped one, question by question: whether its ordered answers are the same, and
// its recall@k and MRR against the question set's gold documents (Garage source keys).
// An index-level lever such as quantization is measured by running this against a copy of the
// database whose index was rebuilt, so the statements stay the same.
//
// Run against the live stack, with the same environment as TestDenseFilterCost:
//
//	go test -tags retrieval_eval ./internal/arcadedb/ -run TestFusedQueryVariants -v
package arcadedb

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
)

type queryVariant struct {
	name      string
	statement string
	params    map[string]any
	// bind adds the parameters a variant derives from the question itself.
	bind func(query string) map[string]any
}

type variantTally struct {
	latencies []float64
	identical int
	ranks     []int
}

func TestFusedQueryVariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	identity := filterBenchEnv(t, "AURA_BENCH_IDENTITY")
	questions := filterBenchQuestions(t, filterBenchEnv(t, "AURA_BENCH_PILOT"))
	client, embedder := filterBenchClient(ctx, t, identity)
	space, err := embedder.Space(ctx)
	if err != nil {
		t.Fatalf("embedding space: %v", err)
	}

	where, params := candidateWhere(CandidateFilter{IdentityID: identity})
	if where, err = (DocumentIndexConfig{}).bindDense(where, params, space.ID); err != nil {
		t.Fatal(err)
	}
	const limit = 20
	params["fetch"] = fusedDenseNeighbours
	params["candidates"] = min(max(limit*4, 20), defaultRetrievalCandidateCap)
	variants := fusedQueryVariants(t, where, limit)

	vectors := make([][]float64, len(questions))
	for i, q := range questions {
		embedded, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{q.Query}))
		if err != nil || len(embedded) != 1 {
			t.Fatalf("embed %q: %v", q.QID, err)
		}
		vectors[i] = embedded[0]
	}

	tallies := make([]variantTally, len(variants))
	for i, q := range questions {
		var shippedKeys []string
		for v, variant := range variants {
			run := map[string]any{"embedding": vectors[i], "query": escapeLucene(q.Query)}
			maps.Copy(run, params)
			maps.Copy(run, variant.params)
			if variant.bind != nil {
				maps.Copy(run, variant.bind(q.Query))
			}
			ms, rows := timeStatement(ctx, t, client, variant.statement, run)
			keys := columnValues(rows, "passage_key")
			if v == 0 {
				shippedKeys = keys
			}
			tally := &tallies[v]
			tally.latencies = append(tally.latencies, ms)
			if slices.Equal(keys, shippedKeys) {
				tally.identical++
			}
			sources := columnValues(rows, "source_key")
			rank := goldRank(sources, q.GoldDocs)
			tally.ranks = append(tally.ranks, rank)
			if v == 0 {
				t.Logf("%-28s gold at %2d of %2d; ahead: %s", q.QID, rank, len(sources),
					strings.Join(sources[:min(len(sources), max(rank-1, 3))], ", "))
			}
		}
	}

	for i, q := range questions {
		ranks := make([]string, len(variants))
		for v := range variants {
			ranks[v] = strconv.Itoa(tallies[v].ranks[i])
		}
		t.Logf("%-28s gold rank by variant: %s", q.QID, strings.Join(ranks, " "))
	}
	t.Logf("%-26s %10s %10s %6s %6s %7s %6s", "variant", "median", "identical", "r@1", "r@3", "r@10", "MRR")
	for v, variant := range variants {
		tally := tallies[v]
		t.Logf("%-26s %8.1fms %6d/%-3d %6.3f %6.3f %7.3f %6.3f", variant.name, median(tally.latencies),
			tally.identical, len(questions), recallAt(tally.ranks, 1), recallAt(tally.ranks, 3),
			recallAt(tally.ranks, 10), meanReciprocalRank(tally.ranks))
	}
}

// fusedQueryVariants is the shipped statement and each lever applied to it alone. It fails if
// the shipped statement stops carrying the exact clauses the levers replace.
func fusedQueryVariants(t *testing.T, where string, limit int) []queryVariant {
	t.Helper()
	shipped := fusedStatement(where, FusionRRF, limit)
	filter := "{ filter: (SELECT @rid FROM " + documentPassageType + " WHERE " + where + ").@rid, " +
		"maxDistance: :max_distance }"
	floor := "WHERE score >= :min_relevance"
	for _, clause := range []string{filter, floor} {
		if strings.Count(shipped, clause) != 1 {
			t.Fatalf("the shipped statement no longer carries %q once:\n%s", clause, shipped)
		}
	}
	// The rerank wrapper, split where the fusion's own options end, so the ranking levers below
	// replace exactly the clauses they name.
	rerankHead := "SELECT expand(`vector.rerank`((SELECT expand(`vector.fuse`("
	rerankTail := "))), :embedding, '" + documentEmbeddingProperty + "', :candidates)))"
	groupOptions := "groupSize: " + strconv.Itoa(fusedGroupSize) + " }"
	for _, clause := range []string{rerankHead, rerankTail, groupOptions} {
		if strings.Count(shipped, clause) != 1 {
			t.Fatalf("the shipped statement no longer carries %q once:\n%s", clause, shipped)
		}
	}
	// Ordered by the fusion's own score, the cosine kept as the floor so the same passages
	// qualify: what 2026-09-09 measured on the office corpus, where it changed nothing.
	fusionOrder := strings.NewReplacer(rerankHead, "SELECT expand(`vector.fuse`(", rerankTail, ")))",
		floor, "WHERE `vector.cosineSimilarity`("+documentEmbeddingProperty+", :embedding) >= :min_relevance "+
			"ORDER BY score DESC").Replace(shipped)
	// The full-text leg as memory recall shapes it (memory_recall.go): the query's words less
	// their stopwords, the BM25 floor lexical mode already applies (document_lexical.go), and
	// only the leg's best rows -- it is there to bring exact words into the pool, not to fill it.
	lexicalLeg := "SEARCH_INDEX('" + documentPassageType + "[text]', :query) = true AND " + where + ")"
	if strings.Count(shipped, lexicalLeg) != 1 {
		t.Fatalf("the shipped statement no longer carries %q once:\n%s", lexicalLeg, shipped)
	}
	stopwordsOut := func(query string) map[string]any {
		terms, _ := lexicalQuery(query)
		return map[string]any{"query": escapeLucene(terms)}
	}
	memoryShaped := func(query string) map[string]any {
		terms, floor := lexicalQuery(query)
		return map[string]any{"query": escapeLucene(terms), "min_lexical_score": floor}
	}
	withFilter := func(replacement string) string { return strings.Replace(shipped, filter, replacement, 1) }
	postFiltered := strings.Replace(withFilter("{ maxDistance: :max_distance }"), floor,
		floor+" AND embed_space = :space", 1)
	// ArcadeDB's maintainers proposed three edits on 2026-10-16: drop the dense filter, keep the
	// full-text leg's best :fetch rows, and rerank the fusion's top :candidates (the variant above
	// of that name). SEARCH_INDEX returns matches by descending $score (manual, full-text index,
	// "Result Order, LIMIT and Cancellation"), so the LIMIT keeps the leg's best rows.
	noFilter := withFilter("{ maxDistance: :max_distance }")
	textLimit := func(statement string) string {
		return strings.Replace(statement, lexicalLeg, strings.TrimSuffix(lexicalLeg, ")")+" LIMIT :fetch)", 1)
	}
	rerankTop := func(statement string) string {
		return strings.Replace(statement, groupOptions, "groupSize: "+strconv.Itoa(fusedGroupSize)+", limit: :candidates }", 1)
	}
	return []queryVariant{
		{name: "shipped", statement: shipped},
		{name: "no filter", statement: noFilter},
		{name: "full-text LIMIT :fetch", statement: textLimit(shipped)},
		{name: "no filter, rerank top 80", statement: rerankTop(noFilter)},
		{name: "maintainers' three edits", statement: rerankTop(textLimit(noFilter))},
		{name: "lexical: stopwords out", statement: shipped, bind: stopwordsOut},
		{name: "lexical: memory's shape", bind: memoryShaped, statement: strings.Replace(shipped, lexicalLeg,
			"SEARCH_INDEX('"+documentPassageType+"[text]', :query) = true AND $score >= :min_lexical_score AND "+
				where+" LIMIT :candidates)", 1)},
		{name: "fusion order, cosine floor", statement: fusionOrder},
		{name: "rerank the fusion's top 80", statement: rerankTop(shipped)},
		{name: "post-filter", statement: postFiltered},
		{name: "filter from the index", statement: withFilter("{ filter: (SELECT rid FROM INDEX:`" +
			documentPassageType + "[embed_space]` WHERE key = :space).rid, maxDistance: :max_distance }")},
		{name: "filter as one list", statement: withFilter("{ filter: (SELECT list(@rid) AS l FROM " +
			documentPassageType + " WHERE " + where + ").l, maxDistance: :max_distance }")},
		{name: "post-filter, fetch 100", statement: postFiltered, params: map[string]any{"fetch": 100}},
	}
}

// goldRank is the 1-based position of the first answer from a gold document, or 0 when none is
// among the answers.
func goldRank(sources, gold []string) int {
	for i, source := range sources {
		if slices.Contains(gold, source) {
			return i + 1
		}
	}
	return 0
}

func recallAt(ranks []int, k int) float64 {
	found := 0
	for _, rank := range ranks {
		if rank > 0 && rank <= k {
			found++
		}
	}
	return float64(found) / float64(len(ranks))
}

func meanReciprocalRank(ranks []int) float64 {
	sum := 0.0
	for _, rank := range ranks {
		if rank > 0 {
			sum += 1 / float64(rank)
		}
	}
	return sum / float64(len(ranks))
}
