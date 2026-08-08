//go:build retrieval_bench

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
//	go test -tags retrieval_bench ./internal/documents/ -run TestFusionBenchmark -v
package documents

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

type benchQuestion struct {
	QID     string `json:"qid"`
	Query   string `json:"query"`
	GoldDoc string `json:"gold_doc"`
}

type benchPilot struct {
	Questions []benchQuestion `json:"questions"`
}

type benchRun struct {
	Arm     string              `json:"arm"`
	Ranking map[string][]string `json:"ranking"`
}

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
	client, err := tenants.For(ctx, identity)
	if err != nil {
		t.Fatalf("tenant client: %v", err)
	}

	cfg := normalizedRetrievalConfig(RetrievalConfig{})
	const topK = 10

	runs := []benchRun{
		{Arm: "go-ladder", Ranking: map[string][]string{}},
		{Arm: "fuse-RRF", Ranking: map[string][]string{}},
		{Arm: "fuse-DBSF", Ranking: map[string][]string{}},
		{Arm: "fuse-LINEAR", Ranking: map[string][]string{}},
	}

	for _, question := range pilot.Questions {
		vectors, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{question.Query}))
		if err != nil || len(vectors) != 1 {
			t.Fatalf("embed %q: %v", question.QID, err)
		}
		filter := arcadedb.CandidateFilter{IdentityID: identity, Limit: cfg.CandidateLimit}

		// Arm A: the shipped Go path, called the way HostRetriever.Retrieve calls it.
		lexical, err := index.LexicalCandidates(ctx, arcadedb.LexicalCandidateQuery{
			CandidateFilter: filter, Query: question.Query,
		})
		if err != nil {
			t.Fatalf("lexical %q: %v", question.QID, err)
		}
		dense, err := index.DenseCandidates(ctx, arcadedb.DenseCandidateQuery{
			CandidateFilter: filter, Embedding: vectors[0],
		})
		if err != nil {
			t.Fatalf("dense %q: %v", question.QID, err)
		}
		candidates := append(
			admittedLexical(lexical, question.Query, cfg.LexicalMinScore),
			admittedDense(dense, cfg.DenseMaxDistance)...,
		)
		for _, document := range rankDocuments(nil, candidates, topK, cfg.TopPassages, false) {
			runs[0].Ranking[question.QID] = append(runs[0].Ranking[question.QID], benchDocName(document.SourceKey))
		}

		// Arm B: one native query per fusion strategy. Raw SQL on purpose -- the point of
		// the measurement is to decide which strategy to implement, so nothing is
		// implemented yet.
		for armIndex, fusion := range []string{"RRF", "DBSF", "LINEAR"} {
			rows, err := client.Query(ctx, fusedBenchStatement(fusion, topK), map[string]any{
				"embedding": vectors[0],
				"query":     question.Query,
			})
			if err != nil {
				t.Fatalf("fuse %s %q: %v", fusion, question.QID, err)
			}
			for _, row := range rows {
				key, _ := row["source_key"].(string)
				if key != "" {
					runs[armIndex+1].Ranking[question.QID] = append(
						runs[armIndex+1].Ranking[question.QID], benchDocName(key),
					)
				}
			}
		}
	}

	encoded, err := json.MarshalIndent(runs, "", " ")
	if err != nil {
		t.Fatalf("encode runs: %v", err)
	}
	if err := os.WriteFile(outPath, encoded, 0o600); err != nil {
		t.Fatalf("write runs: %v", err)
	}
	t.Logf("wrote %d arms x %d questions to %s", len(runs), len(pilot.Questions), outPath)
}

// fusedBenchStatement is the documented `vector.fuse` shape (ArcadeDB manual,
// "Hybrid Search with vector.fuse"): the dense and full-text legs as sub-pipelines,
// groupBy collapsing to the best passage per document so the ranked unit is the
// document, which is what the Go arm also emits.
func fusedBenchStatement(fusion string, limit int) string {
	return "SELECT source_key, score FROM (" +
		"SELECT expand(`vector.fuse`(" +
		"`vector.neighbors`('Passage[embedding]', :embedding, 200)," +
		"(SELECT @rid, $score FROM Passage WHERE SEARCH_INDEX('Passage[text]', :query) = true)," +
		"{ fusion: '" + fusion + "', groupBy: 'document_id', groupSize: 1 }" +
		"))) ORDER BY score DESC LIMIT " + strconv.Itoa(limit)
}
