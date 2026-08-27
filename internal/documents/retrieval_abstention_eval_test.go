//go:build retrieval_eval

// Collects the evidence an abstention decision would have to be made from, per query,
// against the live stack and through the SHIPPED retrieval path.
//
// It exists because `vector.fuse` cannot abstain by construction: RRF ranks by
// position and discards magnitude, DBSF and LINEAR normalise inside the returned set,
// and the fused row carries distance: null. Measured 2026-08-08, an answerable and an
// unanswerable question returned the same document at the identical fused score
// 0.032787 (= 1/61 + 1/61). So an abstention signal has to come from somewhere the
// fusion did not flatten.
//
// The method being fed is arXiv 2402.12997, "Towards Trustworthy Reranking": it does
// not detect unanswerable questions, it estimates whether the RANKING is reliable from
// the score distribution -- max, standard deviation, the top1-top2 gap, and a linear
// regressor over the sorted scores. All four need the whole vector, which is why this
// harness emits every candidate's score and not just the top one.
//
// What it writes, per query, in FUSED order (the order Aura ships):
//   - the document behind each candidate, so ranking quality is scoreable against qrels
//   - the fused score, which is the RRF sum and carries no magnitude
//   - the full-precision similarity of that same candidate, which does
//
// Run (needs the stack up and a corpus ingested for AURA_BENCH_IDENTITY):
//
//	go test -tags retrieval_eval ./internal/documents/ -run TestAbstentionEvidence -v
package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

type abstentionQuery struct {
	QID      string   `json:"qid"`
	Query    string   `json:"query"`
	GoldDocs []string `json:"gold_docs"`
	// GoldDocs is empty for a query with no answer in the corpus. Those are not noise:
	// the paper scores abstention against ranking QUALITY, so an unanswerable query
	// enters as a zero-quality case, and that mapping has to be written down rather
	// than discovered when the numbers look odd.
}

type abstentionRecord struct {
	QID        string    `json:"qid"`
	GoldDocs   []string  `json:"gold_docs"`
	Documents  []string  `json:"documents"`
	FusedScore []float64 `json:"fused_score"`
	Similarity []float64 `json:"similarity"`
}

// abstentionCandidates is how many candidates the score vector carries. The scorers
// need a FIXED width -- the authors' code indexes a (queries x candidates) matrix --
// and a query whose fused result is shorter is dropped rather than padded, because a
// padded score is a number the retriever never produced.
const abstentionCandidates = 20

func TestAbstentionEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	identity := benchEnv(t, "AURA_BENCH_IDENTITY")
	baseURL := benchEnv(t, "AURA_ARCADEDB_URL")
	pilotPath := benchEnv(t, "AURA_ABSTAIN_QUERIES")
	outPath := benchEnv(t, "AURA_ABSTAIN_OUT")

	queries := loadAbstentionQueries(t, pilotPath)
	index, tenants, embedder := abstentionClients(t, baseURL)
	client, err := tenants.For(ctx, identity)
	if err != nil {
		t.Fatalf("tenant client for %s: %v", identity, err)
	}

	cfg := normalizedRetrievalConfig(RetrievalConfig{})
	records := make([]abstentionRecord, 0, len(queries))
	short := 0
	started := time.Now()
	for position, question := range queries {
		vectors, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{question.Query}))
		if err != nil || len(vectors) != 1 {
			t.Fatalf("embed %q: %v", question.QID, err)
		}
		fused, err := index.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
			CandidateFilter: arcadedb.CandidateFilter{IdentityID: identity, Limit: cfg.CandidateLimit},
			Query:           question.Query,
			Embedding:       vectors[0],
			Strategy:        cfg.FusionStrategy,
		})
		if err != nil {
			t.Fatalf("fuse %q: %v", question.QID, err)
		}
		if len(fused) < abstentionCandidates {
			short++
			continue
		}
		fused = fused[:abstentionCandidates]

		similarity, err := passageSimilarity(ctx, client, vectors[0], fused)
		if err != nil {
			t.Fatalf("similarity %q: %v", question.QID, err)
		}
		record := abstentionRecord{QID: question.QID, GoldDocs: question.GoldDocs}
		for _, candidate := range fused {
			score, ok := similarity[candidate.PassageID]
			if !ok {
				t.Fatalf("similarity %q: passage %s absent from the rerank", question.QID, candidate.PassageID)
			}
			record.Documents = append(record.Documents, path.Base(candidate.SourceKey))
			record.FusedScore = append(record.FusedScore, *candidate.FusedScore)
			record.Similarity = append(record.Similarity, score)
		}
		records = append(records, record)
		if (position+1)%50 == 0 {
			t.Logf("%d/%d queries, %s elapsed", position+1, len(queries), time.Since(started).Round(time.Second))
		}
	}

	encoded, err := json.MarshalIndent(records, "", " ")
	if err != nil {
		t.Fatalf("encode records: %v", err)
	}
	if err := os.WriteFile(outPath, encoded, 0o600); err != nil {
		t.Fatalf("write records: %v", err)
	}
	// Reported, never silent: a dropped query is coverage this run does not have, and a
	// harness that hides its own truncation reads as "measured everything" when it did not.
	t.Logf("wrote %d records of %d queries to %s (%d dropped: fewer than %d candidates)",
		len(records), len(queries), outPath, short, abstentionCandidates)
}

func loadAbstentionQueries(t *testing.T, pilotPath string) []abstentionQuery {
	t.Helper()
	raw, err := os.ReadFile(pilotPath)
	if err != nil {
		t.Fatalf("read queries: %v", err)
	}
	var pilot struct {
		Queries []abstentionQuery `json:"abstention_queries"`
	}
	if err := json.Unmarshal(raw, &pilot); err != nil {
		t.Fatalf("decode queries: %v", err)
	}
	if len(pilot.Queries) == 0 {
		t.Fatalf("%s carries no queries", pilotPath)
	}
	return pilot.Queries
}

func abstentionClients(t *testing.T, baseURL string) (*arcadedb.DocumentIndex, *arcadedb.TenantClients, *arcadedb.SidecarEmbedder) {
	t.Helper()
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL:  baseURL,
		Database: benchEnv(t, "AURA_ARCADEDB_DATABASE"),
		User:     benchEnv(t, "AURA_ARCADEDB_ADMIN_USER"),
		Password: benchEnv(t, "AURA_ARCADEDB_ADMIN_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	embedder := arcadedb.NewSidecarEmbedder(benchEnv(t, "AURA_EMBED_BASE_URL"), "embeddinggemma", "", 2*time.Minute)
	tenants := arcadedb.NewTenantClients(arcadedb.Config{BaseURL: baseURL}, admin, embedder, credentials)
	index, err := arcadedb.NewDocumentIndex(tenants, arcadedb.DocumentIndexConfig{Dimensions: 768})
	if err != nil {
		t.Fatalf("document index: %v", err)
	}
	return index, tenants, embedder
}

// passageSimilarity re-scores the fused candidates against the full-precision vectors
// with the engine's own `vector.rerank`, keyed by passage so the FUSED order survives:
// rerank returns its own descending order, and adopting it would replace the ranking
// that measured 0.850 recall@1 with the dense one that did not.
//
// The signature is the server's, not the javadoc's -- the doc comment implies an
// options map and the server rejects it with the real four-argument form. Verified
// live 2026-08-09 against the 125-document Italian corpus, where it returns exactly
// 1 - distance on every row because the passage index is NONE-quantized, so the
// rerank recomputes the same cosine it stored.
func passageSimilarity(
	ctx context.Context,
	client *arcadedb.Client,
	embedding []float64,
	candidates []arcadedb.PassageCandidate,
) (map[string]float64, error) {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.PassageID)
	}
	rows, err := client.Query(ctx,
		"SELECT passage_id, score FROM (SELECT expand(`vector.rerank`("+
			"(SELECT FROM Passage WHERE passage_id IN :ids), :embedding, 'embedding', :k)))",
		map[string]any{"ids": ids, "embedding": embedding, "k": len(ids)})
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	scores := make(map[string]float64, len(rows))
	for _, row := range rows {
		passageID, _ := row["passage_id"].(string)
		score, ok := numeric(row["score"])
		if passageID == "" || !ok {
			return nil, fmt.Errorf("rerank row %v carries no passage_id/score pair", row)
		}
		scores[passageID] = score
	}
	if len(scores) != len(ids) {
		return nil, fmt.Errorf("rerank returned %d scores for %d candidates", len(scores), len(ids))
	}
	return scores, nil
}

func numeric(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		var parsed float64
		_, err := fmt.Sscanf(strings.TrimSpace(typed), "%g", &parsed)
		return parsed, err == nil
	}
	return 0, false
}
