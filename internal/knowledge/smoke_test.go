//go:build smoke

// Italian retrieval acceptance harness (ROADMAP Phase 1 SC#5): recall@5 = 5/5 +
// p95 vector-search latency <= 30ms on the deterministic 5-doc fixture corpus.
//
// Why Go instead of the original bash+jq script: (1) the host need not have jq;
// (2) measuring p95 by wrapping `aura neo4j cypher` per query timed Go process
// startup + a fresh MCP subprocess spawn + bolt connect (~seconds) rather than
// the HNSW search — structurally impossible to land <=30ms. Here we hold ONE
// persistent driver connection and read the server-side query time from
// ResultSummary.ResultAvailableAfter(), which is the actual HNSW search latency
// SC#5 targets (matches the Phase 6b spike's 22-30ms p95 basis).
//
// Run via: scripts/neo4j_smoke.sh (sets env, invokes `go test -tags smoke`).
package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const fixturesDir = "../../scripts/fixtures/neo4j-smoke"

// smokeHTTP disables keep-alives so no idle connection goroutines linger past
// the test (goleak in the shared TestMain would otherwise flag them).
var smokeHTTP = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true},
}

func smokeConfig(t *testing.T) *Config {
	t.Helper()
	pw := envOrSkipCI(t, "NEO4J_PASSWORD", "smoke")
	envOr := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	return &Config{
		BoltURL:         envOr("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
		User:            envOr("NEO4J_USER", "neo4j"),
		Password:        pw,
		Database:        envOr("AURA_NEO4J_DATABASE", "neo4j"),
		EmbedURL:        envOr("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
		EmbedDimensions: 768,
	}
}

// embed POSTs text to the sidecar and returns the embedding vector.
func embedText(ctx context.Context, baseURL, text string) ([]float64, error) {
	payload, _ := json.Marshal(map[string]string{"input": text, "model": "embedding"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := smokeHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Data) == 0 {
		return nil, fmt.Errorf("embed sidecar returned no data for %q", text)
	}
	return body.Data[0].Embedding, nil
}

func TestKnowledgeSmoke(t *testing.T) {
	ctx := context.Background()
	cfg := smokeConfig(t)

	driver, err := neo4j.NewDriverWithContext(cfg.BoltURL, neo4j.BasicAuth(cfg.User, cfg.Password, ""))
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	defer driver.Close(ctx)
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("connectivity: %v", err)
	}
	run := func(cypher string, params map[string]any) (neo4j.ResultWithContext, error) {
		s := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: cfg.Database})
		defer s.Close(ctx)
		res, err := s.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		_, err = res.Collect(ctx)
		return res, err
	}

	// 1. Embed + upsert each fixture as a :Chunk node.
	files, err := filepath.Glob(filepath.Join(fixturesDir, "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: glob err=%v count=%d", err, len(files))
	}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".md")
		text, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		vec, err := embedText(ctx, cfg.EmbedURL, string(text))
		if err != nil {
			t.Fatalf("embed %s: %v", id, err)
		}
		if len(vec) != cfg.EmbedDimensions {
			t.Fatalf("embed dim=%d, want %d", len(vec), cfg.EmbedDimensions)
		}
		if _, err := run(
			"MERGE (c:Chunk {id: $id}) SET c.text = $text, c.embedding = $emb",
			map[string]any{"id": id, "text": string(text), "emb": vec},
		); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	// 2. Run known-answer queries; recall + server-side latency (p95).
	queries, err := os.ReadFile(filepath.Join(fixturesDir, "queries.txt"))
	if err != nil {
		t.Fatalf("queries.txt: %v", err)
	}

	// Warm-up: the first vector queries pay the HNSW index / page-cache cold load
	// (worse when a prior step DROP+recreated the index). p95 targets steady-state
	// search latency (the Phase 6b spike basis), so issue several untimed queries
	// to fully warm the index before measuring (standard benchmark hygiene).
	if warm, err := embedText(ctx, cfg.EmbedURL, "warmup"); err == nil {
		for range 5 {
			ws := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: cfg.Database})
			if wr, err := ws.Run(ctx, "CALL db.index.vector.queryNodes('chunk_embedding', 5, $q) YIELD node RETURN node.id", map[string]any{"q": warm}); err == nil {
				_, _ = wr.Consume(ctx)
			}
			ws.Close(ctx)
		}
	}

	hits, total := 0, 0
	var latencies []time.Duration
	for line := range strings.SplitSeq(strings.TrimSpace(string(queries)), "\n") {
		query, expected, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		total++
		qvec, err := embedText(ctx, cfg.EmbedURL, query)
		if err != nil {
			t.Fatalf("embed query %q: %v", query, err)
		}
		sess := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: cfg.Database})
		res, err := sess.Run(ctx,
			"CALL db.index.vector.queryNodes('chunk_embedding', 5, $q) YIELD node, score RETURN node.id AS id ORDER BY score DESC LIMIT 1",
			map[string]any{"q": qvec})
		if err != nil {
			sess.Close(ctx)
			t.Fatalf("vector query %q: %v", query, err)
		}
		var topID any
		if res.Next(ctx) {
			topID, _ = res.Record().Get("id")
		}
		summary, err := res.Consume(ctx)
		if err != nil {
			sess.Close(ctx)
			t.Fatalf("consume %q: %v", query, err)
		}
		latencies = append(latencies, summary.ResultAvailableAfter())
		sess.Close(ctx)

		if topID == expected {
			hits++
		} else {
			t.Logf("MISS: query=%q expected=%s top1=%v", query, expected, topID)
		}
	}

	if hits != total || total != 5 {
		t.Fatalf("recall@5 = %d/%d (want 5/5)", hits, total)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := latencies[len(latencies)-1] // p95 of 5 samples = max
	p95ms := p95.Milliseconds()
	// recall@5 is a hardware-independent correctness gate (asserted above, always
	// strict). p95 is a latency gate that depends on CPU + HNSW index warmth, so
	// the budget is tunable via AURA_SMOKE_P95_MS — default 30ms on the operator's
	// known hardware (SC#5), relaxed on shared CI runners where a cold index can
	// spike (measured 57ms cold vs 1-8ms warm). Correctness never relaxes.
	maxP95 := int64(30)
	if v := os.Getenv("AURA_SMOKE_P95_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxP95 = int64(n)
		}
	}
	if p95ms > maxP95 {
		t.Fatalf("p95 = %d ms (want <= %d ms)", p95ms, maxP95)
	}
	fmt.Printf("ok: recall@5 = 5/5, p95 = %d ms (budget %d ms)\n", p95ms, maxP95)
}
