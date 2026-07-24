//go:build retrieval_eval

// retrieval_eval.go is the data-loading + scoring-aggregation + report-emitter half of
// Aura's retrieval eval harness (RET-05), split out of retrieval_eval_test.go to keep
// every file <=600 LOC (CLAUDE.md). It carries the retrieval_eval build tag so it is a
// GATED tier — never part of `go test ./...` or the quality-full gate. The pure metrics
// (ndcgAtK/recallAtK/mrr) live in retrieval_metrics.go (no tag, coverage-counted); this
// file owns the judged-set loading, the content-addressable relevance oracle, the
// per-query/aggregate scoring, the identity baseline reranker, and the docs/ report.
//
// The entry point + live wiring + assertions live in retrieval_eval_test.go.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/rerank"
)

// retrievalReportPath is the scored-report destination (docs/, never /tmp — CLAUDE.md).
// It is named distinctly from scoring_cot_eval.go's reportPath so a `-tags "cot_eval
// retrieval_eval"` build has no duplicate-const collision.
const retrievalReportPath = "../../docs/aura-retrieval-eval.md"

// relevantChunkQuery is the content-addressable relevance oracle: a judged chunk is
// "relevant" when its text contains ALL of the judgment's phrases (case-insensitive),
// scoped to the ingested document. Chunk ids are content-hash-derived at ingest time and
// cannot be hard-coded in a static fixture, so the judgments carry stable phrases and the
// gold chunk-id set is resolved live against the freshly-ingested corpus — the spike-070
// methodology ("the gold chunk is the one carrying the answer phrase"). The relevance
// signal is independent of the ranker, so it fairly discriminates vector vs vector+rerank.
const relevantChunkQuery = `
MATCH (c:Chunk)
WHERE c.document_id = $document_id
  AND ALL(p IN $phrases WHERE toLower(c.text) CONTAINS toLower(p))
RETURN c.id AS chunk_id
`

// defaultRegressionNoise is the per-query nDCG@10 drop above which a query is judged to
// have REGRESSED under rerank (the non-monotonic guard verification). Tunable via
// AURA_RERANK_EVAL_NOISE on a known-hardware run.
const defaultRegressionNoise = 0.10

// defaultMinLift is the minimum mean nDCG@10 lift (reranked - vector) required to pass.
// 0.0 = "reranked is at least as good on average" (no mean regression); spike-070
// measured a clear positive lift on G220. Tunable up via AURA_RERANK_EVAL_MIN_LIFT.
const defaultMinLift = 0.0

// defaultMinJudged is the minimum number of judged queries that must actually resolve a
// non-empty relevant set in the ingested corpus, so a near-empty corpus match cannot
// vacuously pass the gate. Tunable via AURA_RERANK_EVAL_MIN_JUDGED.
const defaultMinJudged = 8

// judgment is one labeled query -> relevant-content phrases entry in the judged set.
type judgment struct {
	Query           string   `json:"query"`
	RelevantPhrases []string `json:"relevant_phrases"`
	Note            string   `json:"note"`
}

// queryEval is the scored outcome for one judged query: the vector-only and
// vector+rerank metrics plus whether rerank actually changed the order.
type queryEval struct {
	query        string
	relevant     int
	vectorNDCG   float64
	rerankNDCG   float64
	vectorRecall float64
	rerankRecall float64
	vectorMRR    float64
	rerankMRR    float64
	changed      bool
}

// evalSummary is the aggregate verdict over the evaluated queries.
type evalSummary struct {
	judged          int
	changed         int
	meanVectorNDCG  float64
	meanRerankNDCG  float64
	meanVectorRec   float64
	meanRerankRec   float64
	meanVectorMRR   float64
	meanRerankMRR   float64
	lift            float64
	regressions     []string
	minLift         float64
	regressionNoise float64
}

// loadJudgments reads the judged set from testdata/retrieval_judgments.json.
func loadJudgments(path string) ([]judgment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []judgment
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode judgments %s: %w", path, err)
	}
	return out, nil
}

// resolveRelevant runs the content-addressable oracle against the ingested document and
// returns the gold chunk-id set for one judgment (empty when no chunk carries the
// phrases — the caller skips such judgments for this corpus).
func resolveRelevant(ctx context.Context, k documents.KnowledgeClient, documentID string, phrases []string) (map[string]struct{}, error) {
	rows, err := k.Read(ctx, relevantChunkQuery, map[string]any{
		"document_id": documentID,
		"phrases":     phrases,
	})
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if id, ok := row["chunk_id"].(string); ok && id != "" {
			set[id] = struct{}{}
		}
	}
	return set, nil
}

// rankedChunkIDs projects a retrieval result into the ordered chunk-id list the metrics
// score.
func rankedChunkIDs(hits []documents.SearchHit) []string {
	ids := make([]string, len(hits))
	for i := range hits {
		ids[i] = hits[i].ChunkID
	}
	return ids
}

// equalIDs reports whether two ranked id lists are identical (same order) — used to tell
// whether rerank actually changed the vector-seed order for a query.
func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// summarize aggregates per-query results into the verdict: the mean metrics, the mean
// nDCG@10 lift, and the list of queries that regressed beyond the noise threshold.
func summarize(results []queryEval, minLift, noise float64) evalSummary {
	s := evalSummary{minLift: minLift, regressionNoise: noise}
	for _, r := range results {
		s.judged++
		if r.changed {
			s.changed++
		}
		s.meanVectorNDCG += r.vectorNDCG
		s.meanRerankNDCG += r.rerankNDCG
		s.meanVectorRec += r.vectorRecall
		s.meanRerankRec += r.rerankRecall
		s.meanVectorMRR += r.vectorMRR
		s.meanRerankMRR += r.rerankMRR
		if r.rerankNDCG < r.vectorNDCG-noise {
			s.regressions = append(s.regressions, r.query)
		}
	}
	if s.judged > 0 {
		n := float64(s.judged)
		s.meanVectorNDCG /= n
		s.meanRerankNDCG /= n
		s.meanVectorRec /= n
		s.meanRerankRec /= n
		s.meanVectorMRR /= n
		s.meanRerankMRR /= n
	}
	s.lift = s.meanRerankNDCG - s.meanVectorNDCG
	return s
}

// evalReranker is the IDENTITY reranker used for the vector-only baseline: it returns
// the input order unchanged so Retrieve yields the pure vector-seed order through the
// exact same two-stage pipeline as the rerank arm (isolating the rerank reorder as the
// only difference). It mirrors rerank's internal identity contract.
type evalReranker struct{}

func (evalReranker) Rerank(_ context.Context, _ string, docs []string) ([]rerank.Scored, error) {
	out := make([]rerank.Scored, len(docs))
	for i := range docs {
		out[i] = rerank.Scored{Index: i, Document: docs[i], Score: 0}
	}
	return out, nil
}

// evalEmbedQueue runs the embedding worker synchronously inside IngestPath so the
// chunk_embedding HNSW index is populated before the harness queries (exercising the
// real dense vector path, not the fulltext fallback).
type evalEmbedQueue struct {
	worker   *documents.EmbeddingWorker
	embedded int
	err      error
}

func (q *evalEmbedQueue) Enqueue(ctx context.Context, doc documents.ExtractedDocument) error {
	q.err = q.worker.Process(ctx, doc)
	q.embedded = len(doc.Chunks)
	return q.err
}

// retrievalEnvFloat reads a float override (named distinctly to avoid colliding with any
// cot_eval helper under a multi-tag build).
func retrievalEnvFloat(key string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// retrievalEnvInt reads an int override.
func retrievalEnvInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// writeRetrievalReport emits the scored markdown report to docs/ (never /tmp).
func writeRetrievalReport(path string, results []queryEval, s evalSummary, model string, rerankActive bool) error {
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(&b, "# Aura Retrieval Eval — vector vs vector+rerank — %s\n\n", now)
	fmt.Fprintf(&b, "Reranker: `%s` (active=%v). Gated tier (`-tags retrieval_eval`), NOT in CI/quality-full.\n\n", model, rerankActive)
	b.WriteString("## Aggregate (mean over judged queries)\n\n")
	b.WriteString("| Metric | vector-only | vector+rerank | delta |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| nDCG@10 | %.4f | %.4f | %+.4f |\n", s.meanVectorNDCG, s.meanRerankNDCG, s.lift)
	fmt.Fprintf(&b, "| Recall@5 | %.4f | %.4f | %+.4f |\n", s.meanVectorRec, s.meanRerankRec, s.meanRerankRec-s.meanVectorRec)
	fmt.Fprintf(&b, "| MRR | %.4f | %.4f | %+.4f |\n", s.meanVectorMRR, s.meanRerankMRR, s.meanRerankMRR-s.meanVectorMRR)
	fmt.Fprintf(&b, "\nJudged queries: %d; queries rerank reordered: %d; mean-lift floor: %.4f; per-query regression-noise: %.4f.\n",
		s.judged, s.changed, s.minLift, s.regressionNoise)
	if len(s.regressions) > 0 {
		fmt.Fprintf(&b, "\n**Regressions beyond noise (%d):** %s\n", len(s.regressions), strings.Join(s.regressions, "; "))
	} else {
		b.WriteString("\n**Zero non-monotonic regressions beyond the noise threshold.**\n")
	}

	b.WriteString("\n## Per-query nDCG@10 / Recall@5 / MRR\n\n")
	b.WriteString("| Query | rel | nDCG vec | nDCG rr | Rec@5 vec | Rec@5 rr | MRR vec | MRR rr | reordered |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	sort.Slice(results, func(i, j int) bool { return results[i].query < results[j].query })
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f | %v |\n",
			truncateQuery(r.query), r.relevant, r.vectorNDCG, r.rerankNDCG,
			r.vectorRecall, r.rerankRecall, r.vectorMRR, r.rerankMRR, r.changed)
	}
	b.WriteString("\nAdaptive serving: OUT — this evaluation harness does not mutate runtime policy.\n")
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// truncateQuery keeps the report table readable.
func truncateQuery(q string) string {
	const max = 56
	if len([]rune(q)) <= max {
		return q
	}
	return string([]rune(q)[:max]) + "…"
}
