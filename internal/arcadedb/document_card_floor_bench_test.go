//go:build retrieval_eval

// Where document cards sit against the relevance floor. The floor (relevance_floors.go, 0.32)
// was measured on 2026-09-09 over PASSAGES, and documentCardStatement applies the same number to
// cards, whose text is a file's description rather than its prose. For every question this
// prints the reranked cosine of each gold document's card and of the best other card; a question
// with no gold is out of corpus, and its best card is the false match the floor must reject.
// TestCardTextVariants asks the same of cards re-embedded with some of their lines dropped.
//
// Run with the same environment as TestDenseFilterCost:
//
//	go test -tags retrieval_eval ./internal/arcadedb/ -run 'TestCardCosineBand|TestCardTextVariants' -v
package arcadedb

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
)

// cardTextVariants are the texts a card's vector could be computed from, each derived from
// the stored card by dropping lines. "full" is what the ingest embeds today.
var cardTextVariants = []struct {
	name string
	keep func(line string) bool
}{
	{"full", func(string) bool { return true }},
	{"no boilerplate", func(line string) bool { return !cardBoilerplate(line) }},
	{"no sample rows", func(line string) bool {
		return !cardBoilerplate(line) && !strings.HasPrefix(strings.TrimSpace(line), "row:")
	}},
	{"schema only", func(line string) bool { return !cardBoilerplate(line) && !strings.HasPrefix(line, "  ") }},
}

// cardBoilerplate is a line filecard writes on every card of its kind, the same for every file.
func cardBoilerplate(line string) bool {
	return strings.HasPrefix(line, "Written by Aura") || strings.Contains(line, "Aura did not read this file")
}

// TestCardTextVariants measures the band TestCardCosineBand measures, once per card text: for
// each variant, the lowest cosine of a gold card and the highest card cosine an out-of-corpus
// question reaches. "full" re-embeds the stored text and must reproduce the stored vector.
func TestCardTextVariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	identity := filterBenchEnv(t, "AURA_BENCH_IDENTITY")
	questions := filterBenchQuestions(t, filterBenchEnv(t, "AURA_BENCH_PILOT"))
	client, embedder := filterBenchClient(ctx, t, identity)
	rows, err := client.Query(ctx, "SELECT source_key, passage_count, card, "+documentEmbeddingProperty+
		" FROM "+IndexedDocumentType+" WHERE card IS NOT NULL AND card <> ''", nil)
	if err != nil {
		t.Fatalf("cards: %v", err)
	}
	// Only a file with no passage is answered by its card alone, so only its gold is scored.
	keys, cardOnly := make([]string, len(rows)), make([]bool, len(rows))
	for i, row := range rows {
		keys[i], _ = row["source_key"].(string)
		count, _ := finiteFloat(row["passage_count"])
		cardOnly[i] = count == 0
	}
	queries := make([][]float64, len(questions))
	for i, q := range questions {
		vectors, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{q.Query}))
		if err != nil || len(vectors) != 1 {
			t.Fatalf("embed %q: %v", q.QID, err)
		}
		queries[i] = vectors[0]
	}

	floor := (DocumentIndexConfig{}).documentFloors(embeddingGemmaSpace).minRelevance
	for _, variant := range cardTextVariants {
		texts := make([]string, len(rows))
		for i, row := range rows {
			card, _ := row["card"].(string)
			var kept []string
			for line := range strings.SplitSeq(card, "\n") {
				if variant.keep(line) {
					kept = append(kept, line)
				}
			}
			texts[i] = strings.Join(kept, "\n")
		}
		vectors, err := embedder.Embed(ctx, embeddings.Prefix(embeddings.UntitledDocumentPrefix, texts))
		if err != nil || len(vectors) != len(rows) {
			t.Fatalf("embed %s cards: %v", variant.name, err)
		}
		if variant.name == "full" {
			worst := 1.0
			for i, row := range rows {
				worst = min(worst, cosine(vectors[i], floats(row[documentEmbeddingProperty])))
			}
			t.Logf("full re-embedded against the stored vectors: lowest cosine %.4f", worst)
		}
		lowestGold, highestOut, passing, golds := math.Inf(1), math.Inf(-1), 0, 0
		var lines []string
		for qi, q := range questions {
			for ci, key := range keys {
				score := cosine(queries[qi], vectors[ci])
				switch {
				case len(q.GoldDocs) == 0:
					highestOut = max(highestOut, score)
				case cardOnly[ci] && slices.Contains(q.GoldDocs, key):
					golds++
					lowestGold = min(lowestGold, score)
					if score >= floor {
						passing++
					}
					lines = append(lines, fmt.Sprintf("%s %s %.3f", q.QID, key, score))
				}
			}
		}
		t.Logf("%-15s gold cards >= %.2f: %d/%d; lowest gold %.3f; highest out-of-corpus %.3f",
			variant.name, floor, passing, golds, lowestGold, highestOut)
		t.Logf("%-15s %s", "", strings.Join(lines, "; "))
	}
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot, na, nb = dot+a[i]*b[i], na+a[i]*a[i], nb+b[i]*b[i]
	}
	return dot / math.Sqrt(na*nb)
}

func floats(value any) []float64 {
	list, _ := value.([]any)
	out := make([]float64, 0, len(list))
	for _, item := range list {
		number, _ := finiteFloat(item)
		out = append(out, number)
	}
	return out
}

func TestCardCosineBand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	identity := filterBenchEnv(t, "AURA_BENCH_IDENTITY")
	questions := filterBenchQuestions(t, filterBenchEnv(t, "AURA_BENCH_PILOT"))
	client, embedder := filterBenchClient(ctx, t, identity)
	statement := "SELECT source_key, `vector.cosineSimilarity`(" + documentEmbeddingProperty +
		", :embedding) AS cosine FROM " + IndexedDocumentType + " WHERE card IS NOT NULL AND card <> ''" +
		" ORDER BY cosine DESC"

	floor := (DocumentIndexConfig{}).documentFloors(embeddingGemmaSpace).minRelevance
	t.Logf("floor %.2f; %-20s %-44s %s", floor, "question", "gold card cosine (rank)", "best other card")
	for _, q := range questions {
		vectors, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{q.Query}))
		if err != nil || len(vectors) != 1 {
			t.Fatalf("embed %q: %v", q.QID, err)
		}
		rows, err := client.Query(ctx, statement, map[string]any{"embedding": vectors[0]})
		if err != nil {
			t.Fatalf("cosines %q: %v", q.QID, err)
		}
		var gold []string
		other := ""
		for rank, row := range rows {
			key, _ := row["source_key"].(string)
			cosine, _ := finiteFloat(row["cosine"])
			if slices.Contains(q.GoldDocs, key) {
				gold = append(gold, fmt.Sprintf("%s %.3f (%d)", key, cosine, rank+1))
			} else if other == "" {
				other = fmt.Sprintf("%s %.3f", key, cosine)
			}
		}
		t.Logf("%-20s %-44s %s", q.QID, strings.Join(gold, ", "), other)
	}
}
