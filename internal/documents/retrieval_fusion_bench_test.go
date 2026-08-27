//go:build retrieval_eval

// Measurement harness for the two ways this codebase can rank a hybrid retrieval:
// the Go tier ladder in retrieval_rank.go, and ArcadeDB's own `vector.fuse`.
//
// It exists because the ranking has never been measured against a corpus with
// distractors. It drives the REAL code for the Go arm -- LexicalCandidates,
// DenseCandidates, the admission filters and rankDocuments, exactly as
// HostRetriever.Retrieve calls them -- so what is scored is the shipped path and not
// a reimplementation that could flatter or libel it.
//
// The card leg is absent by construction: the benchmark identity has no Postgres
// catalog rows, which isolates the lexical+dense fusion that is the thing under test.
//
// Run (needs the stack up and a corpus ingested for AURA_BENCH_IDENTITY):
//
//	go test -tags retrieval_eval ./internal/documents/ -run TestFusionBenchmark -v
package documents

import (
	"context"
	"crypto/sha256"
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

type benchQuestion struct {
	QID      string   `json:"qid"`
	Query    string   `json:"query"`
	GoldDocs []string `json:"gold_docs"`
	Category string   `json:"category,omitempty"`
}

type benchPilot struct {
	SchemaID          string          `json:"schema_id"`
	Questions         []benchQuestion `json:"questions"`
	AbstentionQueries []benchQuestion `json:"abstention_queries"`
}

type benchMetrics struct {
	Queries   int     `json:"queries"`
	RecallAt1 float64 `json:"recall_at_1"`
	RecallAt3 float64 `json:"recall_at_3"`
	MRR       float64 `json:"mrr"`
}

type benchRun struct {
	Arm        string              `json:"arm"`
	Production bool                `json:"production"`
	Ranking    map[string][]string `json:"ranking"`
	Metrics    benchMetrics        `json:"metrics"`
	Passed     bool                `json:"passed"`
	Floor      *float64            `json:"recall_at_1_floor,omitempty"`
}

type benchReport struct {
	SchemaID     string     `json:"schema_id"`
	CandidateSHA string     `json:"candidate_sha"`
	QrelsSHA256  string     `json:"qrels_sha256"`
	Runs         []benchRun `json:"runs"`
}

const productionRecallAt1Floor = 0.75

func benchEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s must be set for the fusion benchmark", name)
	}
	return value
}

// benchDocName maps a passage's object key back to the benchmark's ground-truth unit.
// The corpus is uploaded under one prefix, so the document IS the base name.
func benchDocName(sourceKey string) string { return path.Base(sourceKey) }

func TestFusionBenchmark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	identity := benchEnv(t, "AURA_BENCH_IDENTITY")
	baseURL := benchEnv(t, "AURA_ARCADEDB_URL")
	adminUser := benchEnv(t, "AURA_ARCADEDB_ADMIN_USER")
	adminPassword := benchEnv(t, "AURA_ARCADEDB_ADMIN_PASSWORD")
	pilotPath := benchEnv(t, "AURA_BENCH_PILOT")
	outPath := benchEnv(t, "AURA_BENCH_OUT")
	embedURL := benchEnv(t, "AURA_EMBED_BASE_URL")

	raw, err := os.ReadFile(pilotPath)
	if err != nil {
		t.Fatalf("read pilot: %v", err)
	}
	var pilot benchPilot
	if err := json.Unmarshal(raw, &pilot); err != nil {
		t.Fatalf("decode pilot: %v", err)
	}
	if pilot.SchemaID != "aura.document-retrieval-eval/v1" || len(pilot.Questions) == 0 {
		t.Fatalf("pilot contract is missing or empty: schema=%q questions=%d", pilot.SchemaID, len(pilot.Questions))
	}
	for _, question := range pilot.Questions {
		if question.QID == "" || question.Query == "" || len(question.GoldDocs) == 0 {
			t.Fatalf("invalid scored question: %+v", question)
		}
	}

	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	// Database is required by the client even though provisioning only uses the server
	// endpoints; the tenant resolver overrides it per identity.
	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL: baseURL, Database: benchEnv(t, "AURA_ARCADEDB_DATABASE"),
		User: adminUser, Password: adminPassword,
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	embedder := arcadedb.NewSidecarEmbedder(embedURL, "embeddinggemma", "", 2*time.Minute)
	tenants := arcadedb.NewTenantClients(arcadedb.Config{BaseURL: baseURL}, admin, embedder, credentials)
	index, err := arcadedb.NewDocumentIndex(tenants, arcadedb.DocumentIndexConfig{Dimensions: 768})
	if err != nil {
		t.Fatalf("document index: %v", err)
	}
	cfg := normalizedRetrievalConfig(RetrievalConfig{})
	const topK = 10

	strategies := []arcadedb.FusionStrategy{arcadedb.FusionRRF, arcadedb.FusionDBSF, arcadedb.FusionLINEAR}
	runs := []benchRun{
		{Arm: "fuse-RRF", Ranking: map[string][]string{}},
		{Arm: "fuse-DBSF", Ranking: map[string][]string{}},
		{Arm: "fuse-LINEAR", Ranking: map[string][]string{}},
	}
	for i, strategy := range strategies {
		runs[i].Production = strategy == cfg.FusionStrategy
	}

	for _, question := range pilot.Questions {
		vectors, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{question.Query}))
		if err != nil || len(vectors) != 1 {
			t.Fatalf("embed %q: %v", question.QID, err)
		}
		filter := arcadedb.CandidateFilter{IdentityID: identity, Limit: cfg.CandidateLimit}

		// One arm per strategy, all through the SHIPPED FusedCandidates. The Go tier
		// ladder these were first measured against no longer exists.
		for armIndex, fusion := range strategies {
			fused, err := index.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
				CandidateFilter: filter, Query: question.Query,
				Embedding: vectors[0], Strategy: fusion,
			})
			if err != nil {
				t.Fatalf("fuse %s %q: %v", fusion, question.QID, err)
			}
			// nil names: this arm ranks passages only, and the bench resolves its own
			// display name from SourceKey below, so the lookup map rankDocuments uses
			// for card enrichment has nothing to contribute here.
			for _, doc := range rankDocuments(nil, fused, nil, topK, cfg.TopPassages, false) {
				runs[armIndex].Ranking[question.QID] = append(
					runs[armIndex].Ranking[question.QID], benchDocName(doc.SourceKey),
				)
			}
		}
	}

	productionPassed := false
	for i := range runs {
		runs[i].Metrics = scoreBenchmark(pilot.Questions, runs[i].Ranking)
		runs[i].Passed = true
		if runs[i].Production {
			floor := productionRecallAt1Floor
			runs[i].Floor = &floor
			runs[i].Passed = runs[i].Metrics.RecallAt1 >= floor
			productionPassed = runs[i].Passed
		}
	}
	report := benchReport{
		SchemaID: "aura.document-retrieval-eval-report/v1", CandidateSHA: benchEnv(t, "AURA_BENCH_CANDIDATE"),
		QrelsSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Runs: runs,
	}
	encoded, err := json.MarshalIndent(report, "", " ")
	if err != nil {
		t.Fatalf("encode runs: %v", err)
	}
	if err := os.WriteFile(outPath, encoded, 0o600); err != nil {
		t.Fatalf("write runs: %v", err)
	}
	for _, run := range runs {
		t.Logf("%s production=%t R@1=%.3f R@3=%.3f MRR=%.3f", run.Arm, run.Production,
			run.Metrics.RecallAt1, run.Metrics.RecallAt3, run.Metrics.MRR)
	}
	if !productionPassed {
		t.Fatalf("production fusion recall@1 is below %.2f; report=%s", productionRecallAt1Floor, outPath)
	}
	t.Logf("wrote %d scored arms x %d questions to %s", len(runs), len(pilot.Questions), outPath)
}

func scoreBenchmark(questions []benchQuestion, ranking map[string][]string) benchMetrics {
	metrics := benchMetrics{Queries: len(questions)}
	for _, question := range questions {
		gold := make(map[string]struct{}, len(question.GoldDocs))
		for _, document := range question.GoldDocs {
			gold[document] = struct{}{}
		}
		for rank, document := range ranking[question.QID] {
			if _, ok := gold[document]; !ok {
				continue
			}
			if rank == 0 {
				metrics.RecallAt1++
			}
			if rank < 3 {
				metrics.RecallAt3++
			}
			metrics.MRR += 1 / float64(rank+1)
			break
		}
	}
	if metrics.Queries > 0 {
		denominator := float64(metrics.Queries)
		metrics.RecallAt1 /= denominator
		metrics.RecallAt3 /= denominator
		metrics.MRR /= denominator
	}
	return metrics
}

func TestBenchmarkMetricsSupportMultipleGoldDocsAndExposeRegression(t *testing.T) {
	questions := []benchQuestion{{QID: "q1", GoldDocs: []string{"a", "b"}}, {QID: "q2", GoldDocs: []string{"c"}}}
	good := scoreBenchmark(questions, map[string][]string{"q1": {"b"}, "q2": {"x", "c"}})
	if good.RecallAt1 != 0.5 || good.RecallAt3 != 1 || good.MRR != 0.75 {
		t.Fatalf("multi-gold metrics = %+v", good)
	}
	degraded := scoreBenchmark(questions, map[string][]string{"q1": {"x"}, "q2": {"x"}})
	if degraded.RecallAt1 >= productionRecallAt1Floor {
		t.Fatalf("degraded ranking %.3f unexpectedly clears %.2f", degraded.RecallAt1, productionRecallAt1Floor)
	}
}
