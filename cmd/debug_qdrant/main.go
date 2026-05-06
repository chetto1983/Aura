package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/search"
	"github.com/philippgille/chromem-go"
)

type output struct {
	OK         bool                        `json:"ok"`
	URL        string                      `json:"url"`
	Collection string                      `json:"collection"`
	Rebuild    *search.QdrantRebuildReport `json:"rebuild,omitempty"`
	Error      string                      `json:"error,omitempty"`
}

func main() {
	qdrantURL := flag.String("url", "", "Qdrant base URL; defaults to QDRANT_URL")
	collection := flag.String("collection", "", "Qdrant collection; defaults to QDRANT_COLLECTION")
	wikiDir := flag.String("wiki", "", "wiki directory; defaults to WIKI_PATH")
	timeout := flag.Duration("timeout", 120*time.Second, "operation timeout")
	rebuild := flag.Bool("rebuild", false, "recreate collection and upsert wiki embeddings")
	jsonOut := flag.Bool("json", false, "print JSON")
	flag.Parse()

	if err := loadDotEnv(config.EnvPathFromEnvironment()); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "warning: could not load env file: %v\n", err)
	}
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

	if !*rebuild {
		if err := search.CheckQdrantReady(ctx, qcfg); err != nil {
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
		if cache, err := search.OpenEmbedCache(cfg.DBPath, cacheNamespace, embedFn, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
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

func createEmbeddingFunc(cfg *config.Config) chromem.EmbeddingFunc {
	normalized := true
	return chromem.NewEmbeddingFuncOpenAICompat(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel, &normalized)
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

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
