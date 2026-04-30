//go:build live_ingest

// Live catch-up test for sources that finished OCR before slice 6 wired the
// AfterOCR auto-ingest hook. Opt-in via build tag:
//
//	INGEST_SOURCE_IDS="src_a,src_b" \
//	  go test -tags=live_ingest -run TestLiveIngest -v ./internal/ingest/...
//
// Reads WIKI_PATH from .env (or LIVE_WIKI_PATH override). For each ID it
// asserts the source exists with status=ocr_complete (or already ingested),
// runs Pipeline.Compile, and verifies the wiki page is on disk and reports
// "Status: ingested" in the body. Side effects are intentional — this is the
// production catch-up recipe.
package ingest

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/wiki"
)

func TestLiveIngest(t *testing.T) {
	loadDotEnvForLiveIngest(t)

	idsCSV := strings.TrimSpace(os.Getenv("INGEST_SOURCE_IDS"))
	if idsCSV == "" {
		t.Skip("INGEST_SOURCE_IDS not set (comma-separated source IDs)")
	}
	wikiPath := strings.TrimSpace(os.Getenv("LIVE_WIKI_PATH"))
	if wikiPath == "" {
		wikiPath = strings.TrimSpace(os.Getenv("WIKI_PATH"))
	}
	if wikiPath == "" {
		t.Skip("WIKI_PATH not set (and LIVE_WIKI_PATH unset)")
	}

	wikiStore, err := wiki.NewStore(wikiPath, nil)
	if err != nil {
		t.Fatalf("wiki.NewStore: %v", err)
	}
	srcStore, err := source.NewStore(wikiPath, nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}
	pipeline, err := New(Config{Sources: srcStore, Wiki: wikiStore})
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}

	for _, id := range strings.Split(idsCSV, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		t.Run(id, func(t *testing.T) {
			pre, err := srcStore.Get(id)
			if err != nil {
				t.Fatalf("get %s: %v", id, err)
			}
			t.Logf("pre  status=%s page_count=%d filename=%s", pre.Status, pre.PageCount, pre.Filename)

			res, err := pipeline.Compile(context.Background(), id)
			if err != nil {
				t.Fatalf("Compile %s: %v", id, err)
			}
			t.Logf("compile created=%v slug=%s note=%s", res.Created, res.Slug, res.PageNote)

			pageBytes, err := os.ReadFile(filepath.Join(wikiPath, res.Slug+".md"))
			if err != nil {
				t.Fatalf("read wiki page: %v", err)
			}
			body := string(pageBytes)
			if !strings.Contains(body, "Status: ingested") {
				t.Errorf("body should report Status: ingested; got:\n%s", body)
			}
			if !strings.Contains(body, "Source ID: `"+id+"`") {
				t.Errorf("body missing source id %q", id)
			}

			post, err := srcStore.Get(id)
			if err != nil {
				t.Fatalf("get %s post-compile: %v", id, err)
			}
			if post.Status != source.StatusIngested {
				t.Errorf("post status = %s, want ingested", post.Status)
			}
			if len(post.WikiPages) != 1 || post.WikiPages[0] != res.Slug {
				t.Errorf("post wiki_pages = %v, want [%s]", post.WikiPages, res.Slug)
			}
		})
	}
}

// loadDotEnvForLiveIngest mirrors the loader in internal/ocr/live_test.go so
// tests can read WIKI_PATH from the project's .env without shell sourcing.
// Never logs values; only the file path it found.
func loadDotEnvForLiveIngest(t *testing.T) {
	t.Helper()
	for _, candidate := range []string{".env", "../.env", "../../.env"} {
		f, err := os.Open(candidate)
		if err != nil {
			continue
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if k != "" && os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
		t.Logf(".env loaded from %s", candidate)
		return
	}
	t.Logf(".env not found (tried .env, ../.env, ../../.env)")
}
