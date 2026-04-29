//go:build live_ocr

// Live smoke tests against the real Mistral OCR API. Opt-in via build tag:
//
//	go test -tags=live_ocr -run TestLive -v ./internal/ocr/...
//
// Two tests live here:
//   - TestLiveOCR — single Mistral round-trip on the PDF in LIVE_OCR_PDF.
//   - TestLiveE2E — full PDF → source.Store.Put → ocr.Client.Process →
//     RenderMarkdown → write ocr.md/ocr.json → flip status to
//     ocr_complete. Verifies the four-file layout (PDR §4) on disk.
//
// Reads MISTRAL_API_KEY from .env (no shell exposure). Prints summary
// stats only — never the full OCR markdown — per PDR §9 logging rules.
package ocr

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/source"
)

func TestLiveOCR(t *testing.T) {
	loadDotEnvForLive(t)

	apiKey := os.Getenv("MISTRAL_API_KEY")
	if apiKey == "" {
		t.Skip("MISTRAL_API_KEY not set")
	}
	pdfPath := os.Getenv("LIVE_OCR_PDF")
	if pdfPath == "" {
		t.Skip("LIVE_OCR_PDF not set")
	}

	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	t.Logf("pdf=%s bytes=%d", filepath.Base(pdfPath), len(pdf))

	c := New(Config{
		APIKey:        apiKey,
		BaseURL:       envOr("MISTRAL_OCR_BASE_URL", "https://api.mistral.ai/v1"),
		Model:         envOr("MISTRAL_OCR_MODEL", "mistral-ocr-latest"),
		TableFormat:   envOr("MISTRAL_OCR_TABLE_FORMAT", "markdown"),
		ExtractHeader: false,
		ExtractFooter: false,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := c.Process(ctx, ProcessInput{PDFBytes: pdf})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	t.Logf("response model=%s pages=%d raw_json_bytes=%d",
		res.Response.Model, len(res.Response.Pages), len(res.RawJSON))
	if res.Response.UsageInfo != nil {
		t.Logf("usage: pages_processed=%d doc_size_bytes=%d",
			res.Response.UsageInfo.PagesProcessed,
			res.Response.UsageInfo.DocSizeBytes)
	}
	for i, p := range res.Response.Pages {
		first := strings.ReplaceAll(p.Markdown, "\n", " ")
		if len(first) > 160 {
			first = first[:160] + "..."
		}
		t.Logf("page[%d] index=%d md_len=%d sample=%q",
			i, p.Index, len(p.Markdown), first)
	}

	rendered := RenderMarkdown(RenderMeta{
		SourceID: "src_live_smoke___",
		Filename: filepath.Base(pdfPath),
	}, res.Response)
	t.Logf("rendered ocr.md bytes=%d (first 200 chars: %q)",
		len(rendered),
		strings.ReplaceAll(firstChars(rendered, 200), "\n", " "))
}

func firstChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TestRerenderFromOCRJSON re-renders ocr.md from an existing ocr.json
// (driven by RERENDER_DIRS, comma-separated absolute paths to source dirs
// like wiki/raw/src_xxx). Use after fixing render.go to refresh on-disk
// ocr.md files without paying for another Mistral OCR call.
func TestRerenderFromOCRJSON(t *testing.T) {
	dirs := os.Getenv("RERENDER_DIRS")
	if dirs == "" {
		t.Skip("RERENDER_DIRS not set")
	}
	for _, dir := range strings.Split(dirs, ",") {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "ocr.json"))
		if err != nil {
			t.Errorf("%s: read ocr.json: %v", dir, err)
			continue
		}
		var resp OCRResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Errorf("%s: parse ocr.json: %v", dir, err)
			continue
		}
		var meta struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		}
		if mb, err := os.ReadFile(filepath.Join(dir, "source.json")); err == nil {
			_ = json.Unmarshal(mb, &meta)
		}
		md := RenderMarkdown(RenderMeta{
			SourceID: meta.ID,
			Filename: meta.Filename,
			Model:    resp.Model,
		}, resp)
		mdPath := filepath.Join(dir, "ocr.md")
		if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
			t.Errorf("%s: write ocr.md: %v", dir, err)
			continue
		}
		t.Logf("re-rendered %s: %d bytes (tables=%d header=%d)",
			mdPath, len(md),
			countTables(resp.Pages),
			countHeaders(resp.Pages),
		)
	}
}

func countTables(pages []Page) int {
	n := 0
	for _, p := range pages {
		n += len(p.Tables)
	}
	return n
}

func countHeaders(pages []Page) int {
	n := 0
	for _, p := range pages {
		if strings.TrimSpace(p.Header) != "" {
			n++
		}
	}
	return n
}

// TestLiveE2E runs the full PDF → source → OCR → ocr.md/ocr.json pipeline
// that slices 4 (Telegram handler) and 5 (source tools) will wire up. This
// is the canonical end-to-end check that the slice 1+2+3 stack composes
// correctly without any glue code.
func TestLiveE2E(t *testing.T) {
	loadDotEnvForLive(t)

	apiKey := os.Getenv("MISTRAL_API_KEY")
	if apiKey == "" {
		t.Skip("MISTRAL_API_KEY not set")
	}
	pdfPath := os.Getenv("LIVE_OCR_PDF")
	if pdfPath == "" {
		t.Skip("LIVE_OCR_PDF not set")
	}

	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}

	wikiDir := t.TempDir()
	store, err := source.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}

	// Step 1: store as immutable source
	src, dup, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: filepath.Base(pdfPath),
		MimeType: "application/pdf",
		Bytes:    pdf,
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if dup {
		t.Errorf("first Put returned dup=true on a fresh tempdir")
	}
	t.Logf("step 1 source: id=%s status=%s size=%d", src.ID, src.Status, src.SizeBytes)

	// Step 2: OCR
	c := New(Config{
		APIKey:      apiKey,
		BaseURL:     envOr("MISTRAL_OCR_BASE_URL", "https://api.mistral.ai/v1"),
		Model:       envOr("MISTRAL_OCR_MODEL", "mistral-ocr-latest"),
		TableFormat: envOr("MISTRAL_OCR_TABLE_FORMAT", "markdown"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := c.Process(ctx, ProcessInput{PDFBytes: pdf})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	pageCount := len(res.Response.Pages)
	if res.Response.UsageInfo != nil && res.Response.UsageInfo.PagesProcessed > 0 {
		pageCount = res.Response.UsageInfo.PagesProcessed
	}
	t.Logf("step 2 ocr: model=%s pages=%d raw_json=%d", res.Response.Model, pageCount, len(res.RawJSON))

	// Step 3: render markdown and write ocr.md / ocr.json next to the source
	md := RenderMarkdown(RenderMeta{
		SourceID: src.ID,
		Filename: src.Filename,
		Model:    res.Response.Model,
	}, res.Response)

	mdPath := store.Path(src.ID, "ocr.md")
	if mdPath == "" {
		t.Fatal("source.Store.Path returned empty for ocr.md")
	}
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatalf("write ocr.md: %v", err)
	}
	jsonPath := store.Path(src.ID, "ocr.json")
	if err := os.WriteFile(jsonPath, res.RawJSON, 0o644); err != nil {
		t.Fatalf("write ocr.json: %v", err)
	}
	t.Logf("step 3 wrote: %s (%d bytes), %s (%d bytes)", filepath.Base(mdPath), len(md), filepath.Base(jsonPath), len(res.RawJSON))

	// Step 4: flip status + record OCR metadata
	updated, err := store.Update(src.ID, func(rec *source.Source) error {
		rec.Status = source.StatusOCRComplete
		rec.OCRModel = res.Response.Model
		rec.PageCount = pageCount
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	t.Logf("step 4 status: %s ocr_model=%s page_count=%d", updated.Status, updated.OCRModel, updated.PageCount)

	// Verify on-disk layout matches PDR §4
	for _, name := range []string{"original.pdf", "source.json", "ocr.md", "ocr.json"} {
		p := store.Path(src.ID, name)
		if p == "" {
			t.Errorf("Path(%s) empty", name)
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		t.Logf("verify %s: %d bytes", name, st.Size())
	}

	// Re-read source.json from disk to confirm Update persisted
	reread, err := store.Get(src.ID)
	if err != nil {
		t.Fatalf("Get reread: %v", err)
	}
	if reread.Status != source.StatusOCRComplete {
		t.Errorf("reread status = %q, want ocr_complete", reread.Status)
	}
	if reread.PageCount != pageCount {
		t.Errorf("reread page_count = %d, want %d", reread.PageCount, pageCount)
	}

	// Sanity-check ocr.md shape
	if !strings.HasPrefix(md, "# Source OCR: ") {
		t.Errorf("ocr.md missing PDR §4 header")
	}
	if !strings.Contains(md, "Source ID: "+src.ID) {
		t.Errorf("ocr.md missing source ID line")
	}
	if !strings.Contains(md, "## Page 1") {
		t.Errorf("ocr.md missing first page heading")
	}

	preview := strings.ReplaceAll(firstChars(md, 200), "\n", " ")
	t.Logf("ocr.md preview: %q", preview)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// loadDotEnvForLive parses ../../.env (relative to internal/ocr/) into
// process env. Does not log values; never returns the file content.
func loadDotEnvForLive(t *testing.T) {
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
