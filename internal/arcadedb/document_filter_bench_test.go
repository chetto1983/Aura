//go:build retrieval_eval

// What the dense leg's candidate filter costs inside the fused document query, on the real
// corpus. With no document filter it selects every Passage of the query's space, so on a
// one-model corpus it narrows nothing, yet a synthetic run on 2026-10-15 put it at 4-5x the
// ANN search itself. This measures the shipped statement against the same statement with the
// filter dropped, question by question, and checks whether the answers change.
//
// Run against the live stack, with the same environment as TestProductionRetrievalRecall:
//
//	go test -tags retrieval_eval ./internal/arcadedb/ -run TestDenseFilterCost -v
package arcadedb

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

const filterBenchReps = 7

func TestDenseFilterCost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	identity := filterBenchEnv(t, "AURA_BENCH_IDENTITY")
	questions := filterBenchQuestions(t, filterBenchEnv(t, "AURA_BENCH_PILOT"))
	client, embedder := filterBenchClient(ctx, t, identity)
	space, err := embedder.Space(ctx)
	if err != nil {
		t.Fatalf("embedding space: %v", err)
	}
	t.Logf("embedding space %s", space.ID)

	where, params := candidateWhere(CandidateFilter{IdentityID: identity})
	if where, err = (DocumentIndexConfig{}).bindDense(where, params, space.ID); err != nil {
		t.Fatal(err)
	}
	const limit = 20
	params["fetch"] = fusedDenseNeighbours
	params["candidates"] = min(max(limit*4, 20), defaultRetrievalCandidateCap)
	shipped := fusedStatement(where, FusionRRF, limit)
	filterClause := "{ filter: (SELECT @rid FROM " + documentPassageType + " WHERE " + where +
		").@rid, maxDistance: :max_distance }"
	if strings.Count(shipped, filterClause) != 1 {
		t.Fatalf("the shipped statement no longer carries the filter this bench drops:\n%s", shipped)
	}
	unfiltered := strings.Replace(shipped, filterClause, "{ maxDistance: :max_distance }", 1)
	subquery := "SELECT count(*) AS c FROM (SELECT @rid FROM " + documentPassageType + " WHERE " + where + ")"

	counted, err := client.Query(ctx, subquery, params)
	if err != nil || len(counted) != 1 {
		t.Fatalf("count the filter's passages: %v", err)
	}
	t.Logf("the filter selects %v passages", counted[0]["c"])

	var allShipped, allUnfiltered, allSubquery []float64
	changed := 0
	t.Logf("%-28s %10s %10s %10s %8s  same answers", "question", "shipped", "no filter", "subquery", "results")
	for _, q := range questions {
		vectors, err := embedder.Embed(ctx, embeddings.RetrievalQueries([]string{q.Query}))
		if err != nil || len(vectors) != 1 {
			t.Fatalf("embed %q: %v", q.QID, err)
		}
		run := map[string]any{"embedding": vectors[0], "query": escapeLucene(q.Query)}
		maps.Copy(run, params)
		shippedMS, shippedRows := timeStatement(ctx, t, client, shipped, run)
		unfilteredMS, unfilteredRows := timeStatement(ctx, t, client, unfiltered, run)
		subqueryMS, _ := timeStatement(ctx, t, client, subquery, run)
		shippedKeys := columnValues(shippedRows, "passage_key")
		same := slices.Equal(shippedKeys, columnValues(unfilteredRows, "passage_key"))
		if !same {
			changed++
		}
		allShipped, allUnfiltered, allSubquery = append(allShipped, shippedMS),
			append(allUnfiltered, unfilteredMS), append(allSubquery, subqueryMS)
		// Two empty answers are equal too, so the count says whether "same" compared anything.
		t.Logf("%-28s %8.1fms %8.1fms %8.1fms %8d  %v", q.QID, shippedMS, unfilteredMS, subqueryMS, len(shippedKeys), same)
	}
	t.Logf("median over %d questions (each the median of %d runs): shipped %.1fms, no filter %.1fms, "+
		"filter subquery %.1fms; answers changed on %d", len(questions), filterBenchReps,
		median(allShipped), median(allUnfiltered), median(allSubquery), changed)
}

// timeStatement runs statement once to warm it, then filterBenchReps times, and returns the
// median latency and the rows of the last run, in order.
func timeStatement(ctx context.Context, t *testing.T, client *Client, statement string, params map[string]any) (float64, []map[string]any) {
	t.Helper()
	var rows []map[string]any
	var err error
	samples := make([]float64, 0, filterBenchReps)
	for rep := 0; rep <= filterBenchReps; rep++ {
		start := time.Now()
		if rows, err = client.Query(ctx, statement, params); err != nil {
			t.Fatalf("query: %v\n%s", err, statement)
		}
		if rep > 0 {
			samples = append(samples, float64(time.Since(start).Microseconds())/1000)
		}
	}
	return median(samples), rows
}

// columnValues is one string column of rows, in their order.
func columnValues(rows []map[string]any, column string) []string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		if value, ok := row[column].(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func median(values []float64) float64 {
	sorted := slices.Clone(values)
	sort.Float64s(sorted)
	if len(sorted) == 0 {
		return 0
	}
	return sorted[len(sorted)/2]
}

type filterBenchQuestion struct {
	QID      string   `json:"qid"`
	Query    string   `json:"query"`
	GoldDocs []string `json:"gold_docs"`
}

func filterBenchQuestions(t *testing.T, file string) []filterBenchQuestion {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read pilot: %v", err)
	}
	var pilot struct {
		SchemaID  string                `json:"schema_id"`
		Questions []filterBenchQuestion `json:"questions"`
	}
	if err := json.Unmarshal(raw, &pilot); err != nil {
		t.Fatalf("decode pilot: %v", err)
	}
	if pilot.SchemaID != "aura.document-retrieval-eval/v1" || len(pilot.Questions) == 0 {
		t.Fatalf("pilot contract is missing or empty: schema=%q", pilot.SchemaID)
	}
	return pilot.Questions
}

// filterBenchClient resolves the identity's tenant database the way production does.
func filterBenchClient(ctx context.Context, t *testing.T, identity string) (*Client, DenseEmbedder) {
	t.Helper()
	credentials, err := NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	admin, err := New(Config{
		BaseURL: filterBenchEnv(t, "AURA_ARCADEDB_URL"), Database: filterBenchEnv(t, "AURA_ARCADEDB_DATABASE"),
		User: filterBenchEnv(t, "AURA_ARCADEDB_ADMIN_USER"), Password: filterBenchEnv(t, "AURA_ARCADEDB_ADMIN_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	embedder := NewMemoryEmbedder(config.EmbedConfig{BaseURL: filterBenchEnv(t, "AURA_EMBED_BASE_URL")}, nil)
	tenants := NewTenantClients(Config{BaseURL: filterBenchEnv(t, "AURA_ARCADEDB_URL")}, admin, embedder, credentials)
	index, err := NewDocumentIndex(tenants, DocumentIndexConfig{Dimensions: vectorDimensions})
	if err != nil {
		t.Fatalf("document index: %v", err)
	}
	client, err := index.tenantClient(ctx, identity)
	if err != nil {
		t.Fatalf("tenant client: %v", err)
	}
	return client, embedder
}

func filterBenchEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s must be set for the filter benchmark", name)
	}
	return value
}
