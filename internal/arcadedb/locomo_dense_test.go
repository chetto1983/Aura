//go:build arcadedb_integration

// The embedding question, settled on conversation instead of on documents.
//
// The 2026-07-31 handoff dropped vectors on a measurement: on a 3221-chunk
// document corpus, dense+rerank answered at rank 1 in 1234-2045 ms where
// full-text alone answered at 28 ms with the right passage in the top 3. That
// measurement is real and this does not dispute it -- it disputes its SCOPE.
// Document retrieval is a case where the query reuses the text's own words. A
// question about a conversation is a paraphrase of something someone said, and
// every LOCOMO miss inspected so far has been exactly that: "What did Caroline
// research?" against a turn that never says "research".
//
// So the same 1536 questions and the same turns compare the two signals the
// production retrieval path actually uses:
//
//	A  BM25 only            lexical baseline, in process
//	B  dense only           cosine over EmbeddingGemma-300M, 768-d
//	C  dense + BM25         additive fusion, no entity leg
//
// Embeddings are brute-forced per conversation rather than indexed: ~600 vectors
// per conversation is a dot product, and an HNSW index would measure ArcadeDB's
// vector implementation rather than whether the signal helps at all.
//
// Run: go test -tags arcadedb_integration -run LocomoDense -v -timeout 60m ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/bm25"
)

const embedBatch = 64

type denseCorpus struct {
	diaIDs  []string
	texts   []string
	vectors [][]float64
	index   *bm25.Index
}

type candidate struct {
	diaID string
	fused float64
}

func embedAll(t *testing.T, client interface {
	Embed(context.Context, []string) ([][]float64, error)
}, texts []string) [][]float64 {
	t.Helper()
	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatch {
		end := min(start+embedBatch, len(texts))
		vectors, err := client.Embed(context.Background(), texts[start:end])
		if err != nil {
			t.Fatalf("embed %d-%d: %v", start, end, err)
		}
		if len(vectors) != end-start {
			t.Fatalf("embed returned %d vectors for %d texts", len(vectors), end-start)
		}
		out = append(out, vectors...)
	}
	return out
}

// normalise in place so the similarity is a plain dot product.
func normalise(vectors [][]float64) {
	for _, vector := range vectors {
		sum := 0.0
		for _, value := range vector {
			sum += value * value
		}
		if sum == 0 {
			continue
		}
		norm := math.Sqrt(sum)
		for i := range vector {
			vector[i] /= norm
		}
	}
}

func dot(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func normalizeBM25(raw, midpoint, steepness float64) float64 {
	return 1.0 / (1.0 + math.Exp(-steepness*(raw-midpoint)))
}

func mem0BM25Params(query string) (midpoint, steepness float64) {
	switch terms := len(strings.Fields(query)); {
	case terms <= 3:
		return 5.0, 0.7
	case terms <= 6:
		return 7.0, 0.6
	case terms <= 9:
		return 9.0, 0.5
	case terms <= 15:
		return 10.0, 0.5
	default:
		return 12.0, 0.5
	}
}

func TestLocomoDenseVersusLexical(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)

	embedder := locomoEmbedder()
	if _, err := embedder.Embed(ctx, []string{"probe"}); err != nil {
		t.Skipf("embedding sidecar unavailable: %v", err)
	}

	// Build one corpus per conversation: turns, their vectors, and a BM25 index.
	corpora := map[string]*denseCorpus{}
	started := time.Now()
	embedded := 0
	for index := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		rows, err := client.Query(ctx,
			"SELECT dia_id, text FROM "+locomoEnglishType+
				" WHERE conversation = :c LIMIT 5000", map[string]any{"c": conversation})
		if err != nil {
			t.Fatalf("read %s: %v", conversation, err)
		}
		if len(rows) == 0 {
			t.Skip("LOCOMO turns not loaded; run TestLocomoLoadsEveryConversation first")
		}
		corpus := &denseCorpus{}
		for _, row := range rows {
			corpus.diaIDs = append(corpus.diaIDs, rowString(row, "dia_id"))
			corpus.texts = append(corpus.texts, rowString(row, "text"))
		}
		corpus.vectors = embedAll(t, embedder, withTask(taskDocumentPrefix, corpus.texts))
		normalise(corpus.vectors)
		corpus.index = bm25.New(corpus.texts)
		corpora[conversation] = corpus
		embedded += len(corpus.texts)
	}
	t.Logf("embedded %d turns in %s (%.1f ms each)", embedded,
		time.Since(started).Round(time.Second),
		float64(time.Since(started).Milliseconds())/float64(max(embedded, 1)))

	variants := []struct {
		name           string
		dense, lexical bool
		// cascade > 0 means: dense owns the first N slots and the lexical ranking
		// only fills what is left. Fusion mixes two opinions into one score and
		// dilutes the stronger -- measured, adding BM25 to EmbeddingGemma cost two
		// points on the same 100 questions. A
		// cascade never lets the weaker leg displace a confident answer; it gets
		// the seats left over.
		cascade int
	}{
		{"A BM25 only", false, true, 0},
		{"B dense only", true, false, 0},
		{"C dense + BM25", true, true, 0},
		{"D dense top-3, lexical fills", true, true, 3},
		{"E dense top-5, lexical fills", true, true, 5},
	}
	scores := make([]*recallScore, len(variants))
	for i := range scores {
		scores[i] = &recallScore{}
	}

	limit := 0
	if raw := envOr("AURA_LOCOMO_MAX_QUESTIONS", ""); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	queried := 0
	queryStart := time.Now()
	for index, sample := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		corpus := corpora[conversation]
		for _, qa := range sample.QA {
			if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
				continue
			}
			if limit > 0 && queried >= limit {
				break
			}
			queried++

			vectors, err := embedder.Embed(ctx, withTask(taskQueryPrefix, []string{qa.Question}))
			if err != nil || len(vectors) != 1 {
				t.Fatalf("embed question: %v", err)
			}
			normalise(vectors)
			question := vectors[0]

			terms := bm25.Tokenize(qa.Question)
			midpoint, steepness := mem0BM25Params(qa.Question)

			for v, variant := range variants {
				if variant.cascade > 0 {
					scores[v].add(cascadeRank(corpus, question, terms,
						midpoint, steepness, variant.cascade, qa.Evidence))
					continue
				}
				ranked := make([]candidate, 0, len(corpus.diaIDs))
				maxPossible := 0.0
				if variant.dense {
					maxPossible += 1.0
				}
				if variant.lexical {
					maxPossible += 1.0
				}
				for i, diaID := range corpus.diaIDs {
					total := 0.0
					if variant.dense {
						total += dot(question, corpus.vectors[i])
					}
					if variant.lexical {
						total += normalizeBM25(corpus.index.ScoreTerms(i, terms), midpoint, steepness)
					}
					ranked = append(ranked, candidate{diaID: diaID, fused: total / maxPossible})
				}
				sort.Slice(ranked, func(a, b int) bool {
					if ranked[a].fused != ranked[b].fused {
						return ranked[a].fused > ranked[b].fused
					}
					return ranked[a].diaID < ranked[b].diaID
				})
				rank := -1
				for position, entry := range ranked {
					if position >= 10 {
						break
					}
					if slices.Contains(qa.Evidence, entry.diaID) {
						rank = position
						break
					}
				}
				scores[v].add(rank)
			}
		}
	}

	t.Logf("%d questions in %s (%.0f ms each, embedding included)", queried,
		time.Since(queryStart).Round(time.Second),
		float64(time.Since(queryStart).Milliseconds())/float64(max(queried, 1)))
	t.Logf("%-26s %8s %8s %8s", "variant", "R@1", "R@5", "R@10")
	for v, variant := range variants {
		score := scores[v]
		t.Logf("%-26s %7.1f%% %7.1f%% %7.1f%%", variant.name,
			pct(score.hit1, score.asked), pct(score.hit5, score.asked),
			pct(score.hit10, score.asked))
	}
}

// cascadeRank ranks by dense alone for the first `top` slots, then fills the
// remaining ten with the lexical ranking, skipping anything dense already
// placed. Nothing is summed, so the strong leg is never diluted by the weak one.
func cascadeRank(
	corpus *denseCorpus,
	question []float64,
	terms []string,
	midpoint, steepness float64,
	top int,
	evidence []string,
) int {
	byDense := make([]candidate, 0, len(corpus.diaIDs))
	byLexical := make([]candidate, 0, len(corpus.diaIDs))
	for i, diaID := range corpus.diaIDs {
		byDense = append(byDense, candidate{diaID: diaID, fused: dot(question, corpus.vectors[i])})
		byLexical = append(byLexical, candidate{diaID: diaID,
			fused: normalizeBM25(corpus.index.ScoreTerms(i, terms), midpoint, steepness)})
	}
	order := func(list []candidate) {
		sort.Slice(list, func(a, b int) bool {
			if list[a].fused != list[b].fused {
				return list[a].fused > list[b].fused
			}
			return list[a].diaID < list[b].diaID
		})
	}
	order(byDense)
	order(byLexical)

	final := make([]string, 0, 10)
	taken := map[string]bool{}
	for _, entry := range byDense[:min(top, len(byDense))] {
		final = append(final, entry.diaID)
		taken[entry.diaID] = true
	}
	for _, entry := range byLexical {
		if len(final) >= 10 {
			break
		}
		if !taken[entry.diaID] {
			final = append(final, entry.diaID)
			taken[entry.diaID] = true
		}
	}
	// Any slot the lexical leg could not fill goes back to dense, so the cascade
	// is never shorter than the plain dense ranking.
	for _, entry := range byDense {
		if len(final) >= 10 {
			break
		}
		if !taken[entry.diaID] {
			final = append(final, entry.diaID)
			taken[entry.diaID] = true
		}
	}
	for position, diaID := range final {
		if slices.Contains(evidence, diaID) {
			return position
		}
	}
	return -1
}
