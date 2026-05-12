// Command probe_ingest_e2e is the Wave 2 multi-page-touch E2E harness.
// It exercises the production pipeline path (with the LLM extractor
// wired) against a temp wiki + temp source store, calling the same
// llm.Client + ingest.Pipeline code Aura uses live. Validates:
//
//   - one source → multiple wiki pages (entities + concepts + summary)
//   - bidirectional backlinks (Wave 2.1)
//   - in-memory graph index sees the new edges (Wave 2.2)
//   - extractor strict validation (Wave 2.3)
//   - merge accumulates citations (Wave 2.4)
//   - `^[src_xxx]` provenance markers on every claim (Wave 2.5)
//
// Reads LLM_API_KEY / LLM_BASE_URL / LLM_MODEL from env (or .env).
// Reuses the temp wiki on disk after exit when --keep-wiki is set so
// the operator can grep wiki/*.md for backlinks and markers.
//
//	go run ./cmd/probe_ingest_e2e
//	go run ./cmd/probe_ingest_e2e --keep-wiki    # don't delete temp wiki
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/ingest"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/wiki"
)

const probeSourceBody = `# Aura: A Self-Hosted Second Brain

Aura is a personal AI agent that runs as a single Go binary. It reaches
the user through Telegram and maintains a Markdown wiki as its persistent
second brain. Every source ingested gets integrated into the existing
wiki rather than retrieved from raw chunks on every query.

## Architecture

The wiki is a directory of interlinked Markdown files using Obsidian-style
[[wiki-link]] syntax. Each page has YAML frontmatter (title, tags,
category, related, sources) plus a Markdown body. Aura — driven by
DeepSeek V4 Flash via OpenRouter — owns this layer entirely: it creates
pages, updates them when new sources arrive, maintains cross-references,
and keeps everything consistent.

## Compounding Memory

The key idea: the wiki is a persistent, compounding artifact. When a new
source arrives, the LLM doesn't just write a summary — it extracts
entities and concepts and integrates them into 10-15 existing pages.
The cross-references are already there. Contradictions get flagged.
The synthesis already reflects everything Aura has read.

## Retrieval

Search uses a hybrid of Qdrant vector search, SQLite FTS5 keyword
search, and an in-memory graph index that traverses [[wiki-link]] +
related: edges in under 10 microseconds for a 1000-node graph. Reciprocal
Rank Fusion combines the three signals.

## Inspiration

The pattern was sketched by Andrej Karpathy in his LLM Wiki gist and
applied broadly via Obsidian. Aura takes it further by automating the
maintenance the LLM is good at and the human finds tedious.
`

func main() {
	keepWiki := flag.Bool("keep-wiki", false, "keep the temporary wiki directory after the run")
	flag.Parse()

	if err := loadDotEnv(envDefault("AURA_ENV_PATH", ".env")); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warning: could not load .env: %v\n", err)
	}
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("FAIL: LLM_API_KEY is required")
		os.Exit(1)
	}
	baseURL := envDefault("LLM_BASE_URL", "https://api.openai.com/v1")
	model := envDefault("LLM_MODEL", "gpt-4")

	wikiDir, err := os.MkdirTemp("", "aura-probe-ingest-e2e-*")
	if err != nil {
		fmt.Printf("FAIL: create temp wiki: %v\n", err)
		os.Exit(1)
	}
	if !*keepWiki {
		defer os.RemoveAll(wikiDir)
	}
	fmt.Printf("== probe ingest E2E ==\nwiki:  %s\nllm:   %s @ %s\n\n", wikiDir, model, baseURL)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	wikiStore, err := wiki.NewStore(wikiDir, logger)
	if err != nil {
		die("wiki.NewStore: %v", err)
	}
	srcStore, err := source.NewStore(wikiDir, logger)
	if err != nil {
		die("source.NewStore: %v", err)
	}
	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})
	extractor := ingest.NewLLMExtractor(client, model)
	pipeline, err := ingest.New(ingest.Config{
		Sources:   srcStore,
		Wiki:      wikiStore,
		Extractor: extractor,
		Logger:    logger,
	})
	if err != nil {
		die("ingest.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Seed a single text source with rich content the extractor can pull entities from.
	src, _, err := srcStore.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "aura-second-brain.md",
		MimeType: "text/markdown",
		Bytes:    []byte(probeSourceBody),
	})
	if err != nil {
		die("seed source: %v", err)
	}
	if err := source.WriteExtractionFiles(srcStore, src, source.ExtractResult{
		Markdown: probeSourceBody,
		Metadata: source.ExtractionMeta{ExtractorName: "probe", TextBytes: len(probeSourceBody)},
	}); err != nil {
		die("write extraction: %v", err)
	}
	if _, err := srcStore.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusExtractComplete
		s.Extract = &source.ExtractionMeta{ExtractorName: "probe", TextBytes: len(probeSourceBody)}
		return nil
	}); err != nil {
		die("flip status: %v", err)
	}
	fmt.Printf("seeded source %s (%d bytes)\n", src.ID, len(probeSourceBody))

	start := time.Now()
	res, err := pipeline.Compile(ctx, src.ID)
	if err != nil {
		die("Compile: %v", err)
	}
	elapsed := time.Since(start)
	fmt.Printf("Compile done in %s  →  source summary slug: %s\n\n", elapsed.Round(time.Millisecond), res.Slug)

	if err := reportWiki(wikiStore, src.ID); err != nil {
		die("report: %v", err)
	}
	if *keepWiki {
		fmt.Printf("\nwiki retained at: %s\n", wikiDir)
	}
}

func reportWiki(store *wiki.Store, sourceID string) error {
	slugs, err := store.ListPages()
	if err != nil {
		return fmt.Errorf("ListPages: %w", err)
	}
	fmt.Printf("== wiki state ==  pages: %d\n", len(slugs))

	idx := store.GraphIndex()
	markerToken := "^[" + sourceID + "]"

	var totalProvenance int
	for _, slug := range slugs {
		page, err := store.ReadPage(slug)
		if err != nil {
			fmt.Printf("  - %s  [READ FAILED: %v]\n", slug, err)
			continue
		}
		in, out := idx.Degree(slug)
		count := strings.Count(page.Body, markerToken)
		totalProvenance += count
		fmt.Printf("  - [%s] %s\n", page.Category, slug)
		fmt.Printf("      title: %q\n", page.Title)
		fmt.Printf("      tags: %v  sources: %v\n", page.Tags, page.Sources)
		fmt.Printf("      related: %v\n", page.Related)
		fmt.Printf("      graph: in=%d out=%d  provenance markers: %d\n", in, out, count)
		if preview := previewLine(page.Body, 100); preview != "" {
			fmt.Printf("      body preview: %s\n", preview)
		}
	}
	fmt.Printf("\nTotal `%s` provenance markers across wiki: %d\n", markerToken, totalProvenance)
	return nil
}

func previewLine(body string, maxLen int) string {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > maxLen {
			line = line[:maxLen] + "…"
		}
		return line
	}
	return ""
}

func die(format string, args ...any) {
	fmt.Printf("FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads KEY=VALUE pairs from a file and sets them in the
// process environment unless already set. Tolerates missing file
// (returns os.ErrNotExist) so callers can decide whether to log.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return scanner.Err()
}

var _ = filepath.Join // future-proof path helper imports
