package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/storage/search"
)

type output struct {
	OK         bool                        `json:"ok"`
	URL        string                      `json:"url"`
	Collection string                      `json:"collection"`
	Rebuild    *search.QdrantRebuildReport `json:"rebuild,omitempty"`
	Query      *queryReport                `json:"query,omitempty"`
	Error      string                      `json:"error,omitempty"`
}

func main() {
	qdrantURL := flag.String("url", "", "Qdrant base URL; defaults to QDRANT_URL")
	collection := flag.String("collection", "", "Qdrant collection; defaults to QDRANT_COLLECTION")
	wikiDir := flag.String("wiki", "", "wiki directory; defaults to WIKI_PATH")
	query := flag.String("q", "", "non-mutating query smoke text")
	topK := flag.Int("top-k", 5, "results per query")
	runs := flag.Int("runs", 3, "measured runs for query smoke")
	warmup := flag.Int("warmup", 1, "warmup runs before measurements")
	compare := flag.Bool("compare", false, "compare Qdrant query latency against local in-memory chromem search")
	timeout := flag.Duration("timeout", 120*time.Second, "operation timeout")
	rebuild := flag.Bool("rebuild", false, "recreate collection and upsert wiki embeddings")
	jsonOut := flag.Bool("json", false, "print JSON")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fail(*jsonOut, output{Error: "load config: " + err.Error()})
	}
	if strings.TrimSpace(*qdrantURL) == "" {
		*qdrantURL = cfg.QdrantURL
	}
	if strings.TrimSpace(*collection) == "" {
		*collection = cfg.QdrantCollection
	}
	if strings.TrimSpace(*wikiDir) == "" {
		*wikiDir = cfg.WikiPath
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	qcfg := search.QdrantConfig{
		BaseURL:    *qdrantURL,
		Collection: *collection,
		APIKey:     cfg.QdrantAPIKey,
	}

	if strings.TrimSpace(*query) != "" {
		if strings.TrimSpace(cfg.EmbeddingAPIKey) == "" {
			fail(*jsonOut, output{URL: *qdrantURL, Collection: *collection, Error: "EMBEDDING_API_KEY is required for Qdrant query smoke"})
		}
		embedFn := createEmbeddingFunc(cfg)
		embedFn, closeCache := maybeWrapQueryEmbeddingWithCache(queryCacheConfig{
			DBPath:           cfg.DBPath,
			EmbeddingBaseURL: cfg.EmbeddingBaseURL,
			EmbeddingModel:   cfg.EmbeddingModel,
		}, embedFn, slog.New(slog.NewTextHandler(io.Discard, nil)), search.OpenEmbedCache)
		defer closeCache()
		report, err := runQuerySmoke(ctx, querySmokeConfig{
			Query:   *query,
			TopK:    *topK,
			Runs:    *runs,
			Warmup:  *warmup,
			Compare: *compare,
			WikiDir: *wikiDir,
			Qdrant:  qcfg,
			EmbedFn: embedFn,
			Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			fail(*jsonOut, output{URL: *qdrantURL, Collection: *collection, Error: err.Error()})
		}
		if !report.OK {
			fail(*jsonOut, output{URL: *qdrantURL, Collection: *collection, Query: &report, Error: "qdrant query smoke failed: " + report.Recommendation})
		}
		printOutput(*jsonOut, output{OK: report.OK, URL: *qdrantURL, Collection: *collection, Query: &report})
		return
	}

	if !*rebuild {
		qclient, err := qdrant.NewClient(qdrant.Config{
			BaseURL: qcfg.BaseURL,
			APIKey:  qcfg.APIKey,
		})
		if err != nil {
			fail(*jsonOut, output{URL: *qdrantURL, Collection: *collection, Error: err.Error()})
		}
		if err := qdrant.WaitForReady(ctx, qclient, *timeout); err != nil {
			fail(*jsonOut, output{URL: *qdrantURL, Collection: *collection, Error: err.Error()})
		}
		printOutput(*jsonOut, output{OK: true, URL: *qdrantURL, Collection: *collection})
		return
	}
	if strings.TrimSpace(cfg.EmbeddingAPIKey) == "" {
		fail(*jsonOut, output{URL: *qdrantURL, Collection: *collection, Error: "EMBEDDING_API_KEY is required for Qdrant rebuild"})
	}

	embedFn := createEmbeddingFunc(cfg)
	if cfg.DBPath != "" {
		cacheNamespace := search.EmbedCacheNamespace(cfg.EmbeddingBaseURL, cfg.EmbeddingModel)
		if cache, err := search.OpenEmbedCache(cfg.DBPath, cacheNamespace, cfg.EmbeddingOutputDim, embedFn, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
			defer cache.Close()
			embedFn = cache.EmbedFunc()
		}
	}
	report, err := search.RebuildQdrantWikiDocuments(ctx, *wikiDir, embedFn, qcfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		fail(*jsonOut, output{URL: *qdrantURL, Collection: *collection, Error: err.Error()})
	}
	printOutput(*jsonOut, output{OK: true, URL: *qdrantURL, Collection: *collection, Rebuild: &report})
}

type queryCacheConfig struct {
	DBPath           string
	EmbeddingBaseURL string
	EmbeddingModel   string
}

type openEmbedCacheFunc func(string, string, int, search.EmbeddingFunction, *slog.Logger) (*search.EmbedCache, error)

func maybeWrapQueryEmbeddingWithCache(cfg queryCacheConfig, embedFn search.EmbeddingFunction, logger *slog.Logger, openCache openEmbedCacheFunc) (search.EmbeddingFunction, func()) {
	// Query/compare smoke is documented as non-mutating. Do not open the
	// SQLite embedding cache here: cache misses would write the live DB.
	return embedFn, func() {}
}

func createEmbeddingFunc(cfg *config.Config) search.EmbeddingFunc {
	return search.NewOpenAICompatEmbeddingFunction(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel, cfg.EmbeddingOutputDim, true, nil)
}

type querySmokeConfig struct {
	Query   string
	TopK    int
	Runs    int
	Warmup  int
	Compare bool
	WikiDir string
	Qdrant  search.QdrantConfig
	EmbedFn search.EmbeddingFunc
	Logger  *slog.Logger
}

type queryReport struct {
	OK             bool        `json:"ok"`
	Query          string      `json:"query"`
	TopK           int         `json:"top_k"`
	Runs           int         `json:"runs"`
	Warmup         int         `json:"warmup"`
	Qdrant         queryProbe  `json:"qdrant"`
	Local          *queryProbe `json:"local,omitempty"`
	Overlap        float64     `json:"overlap_qdrant_local,omitempty"`
	Recommendation string      `json:"recommendation"`
}

type queryProbe struct {
	OK        bool              `json:"ok"`
	Count     int               `json:"count"`
	P50MS     int64             `json:"p50_ms"`
	P95MS     int64             `json:"p95_ms"`
	Results   []queryResultLite `json:"results,omitempty"`
	Error     string            `json:"error,omitempty"`
	Empty     bool              `json:"empty,omitempty"`
	Latencies []int64           `json:"latencies_ms,omitempty"`
}

type queryResultLite struct {
	Kind  string  `json:"kind"`
	Slug  string  `json:"slug"`
	Title string  `json:"title,omitempty"`
	Score float32 `json:"score"`
}

func runQuerySmoke(ctx context.Context, cfg querySmokeConfig) (queryReport, error) {
	query := strings.TrimSpace(cfg.Query)
	if query == "" {
		return queryReport{}, fmt.Errorf("query is required")
	}
	if cfg.EmbedFn == nil {
		return queryReport{}, fmt.Errorf("embedding function is required")
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.Runs <= 0 {
		cfg.Runs = 3
	}
	if cfg.Warmup < 0 {
		cfg.Warmup = 0
	}
	qdrant, err := search.NewQdrantSearcher(cfg.Qdrant, cfg.EmbedFn)
	if err != nil {
		return queryReport{}, err
	}
	report := queryReport{
		Query:  query,
		TopK:   cfg.TopK,
		Runs:   cfg.Runs,
		Warmup: cfg.Warmup,
	}
	report.Qdrant = measureQuery(ctx, qdrant, query, cfg.TopK, cfg.Runs, cfg.Warmup)

	if cfg.Compare {
		// The local in-memory wiki engine (chromem-go) was removed when
		// Qdrant became required infrastructure. -compare is now a no-op;
		// keep the flag wired so callers' scripts don't break, but report
		// the unavailability explicitly.
		report.Local = &queryProbe{OK: false, Error: "local engine removed; Qdrant is the only wiki vector backend"}
	}
	report.Recommendation = queryRecommendation(report)
	report.OK = report.Qdrant.OK && !report.Qdrant.Empty
	if report.Local != nil {
		report.OK = report.OK && report.Local.OK
	}
	return report, nil
}

type querySearcher interface {
	Search(ctx context.Context, query string, topK int) ([]search.Result, error)
}

func measureQuery(ctx context.Context, searcher querySearcher, query string, topK, runs, warmup int) queryProbe {
	for i := 0; i < warmup; i++ {
		if _, err := searcher.Search(ctx, query, topK); err != nil {
			return queryProbe{OK: false, Error: err.Error()}
		}
	}
	latencies := make([]int64, 0, runs)
	var last []search.Result
	for i := 0; i < runs; i++ {
		start := time.Now()
		results, err := searcher.Search(ctx, query, topK)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return queryProbe{OK: false, Error: err.Error(), Latencies: latencies}
		}
		latencies = append(latencies, elapsed)
		last = results
	}
	lite := make([]queryResultLite, 0, len(last))
	for _, result := range last {
		lite = append(lite, queryResultLite{
			Kind:  result.Kind,
			Slug:  result.Slug,
			Title: result.Title,
			Score: result.Score,
		})
	}
	return queryProbe{
		OK:        true,
		Count:     len(last),
		P50MS:     percentile(latencies, 0.50),
		P95MS:     percentile(latencies, 0.95),
		Results:   lite,
		Empty:     len(last) == 0,
		Latencies: latencies,
	}
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func queryRecommendation(report queryReport) string {
	if !report.Qdrant.OK {
		return "check_qdrant"
	}
	if report.Qdrant.Empty || report.Qdrant.Count == 0 {
		if report.Local != nil && report.Local.OK && report.Local.Count > 0 {
			return "rebuild_qdrant"
		}
		return "qdrant_empty"
	}
	if report.Local != nil && report.Local.OK && report.Overlap < 0.5 {
		return "review_quality"
	}
	return "ok"
}

func printOutput(jsonOut bool, out output) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	if out.Rebuild != nil {
		fmt.Printf("PASS qdrant url=%s collection=%s docs=%d pages=%d vector_size=%d\n",
			out.URL,
			out.Collection,
			out.Rebuild.DocsIndexed,
			out.Rebuild.PagesIndexed,
			out.Rebuild.VectorSize,
		)
		return
	}
	if out.Query != nil {
		fmt.Printf("PASS qdrant query url=%s collection=%s q=%q qdrant_p50=%dms qdrant_p95=%dms qdrant_count=%d recommendation=%s\n",
			out.URL,
			out.Collection,
			out.Query.Query,
			out.Query.Qdrant.P50MS,
			out.Query.Qdrant.P95MS,
			out.Query.Qdrant.Count,
			out.Query.Recommendation,
		)
		if out.Query.Local != nil {
			fmt.Printf("compare local_p50=%dms local_p95=%dms local_count=%d overlap=%.0f%%\n",
				out.Query.Local.P50MS,
				out.Query.Local.P95MS,
				out.Query.Local.Count,
				out.Query.Overlap*100,
			)
		}
		return
	}
	fmt.Printf("PASS qdrant url=%s collection=%s ready=true\n", out.URL, out.Collection)
}

func fail(jsonOut bool, out output) {
	if jsonOut {
		out.OK = false
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	} else {
		if out.URL != "" || out.Collection != "" {
			fmt.Fprintf(os.Stderr, "FAIL qdrant url=%s collection=%s error=%s\n", out.URL, out.Collection, out.Error)
		} else {
			fmt.Fprintf(os.Stderr, "FAIL qdrant: %s\n", out.Error)
		}
	}
	os.Exit(2)
}

