package ingest

import (
	"context"
	"embed"
	"os"
	"strings"
	"testing"

	source "github.com/aura/aura/internal/storage/sources/store"
)

//go:embed fixtures
var fixtureFS embed.FS

// fixtureCase describes one round-trip fixture: source bytes + pre-computed
// extraction markdown → compiled wiki summary page.
type fixtureCase struct {
	name        string
	filename    string
	kind        source.Kind
	mimeType    string
	srcFile     string   // path in fixtureFS for source bytes
	extractSrc  string   // path in fixtureFS for the extraction golden
	extractFile string   // "ocr.md" or "extract.md" written into source store
	wikiKeywords []string // required substrings in the compiled wiki page body
}

var conversionFixtures = []fixtureCase{
	{
		name:         "PDF",
		filename:     "sample.pdf",
		kind:         source.KindPDF,
		mimeType:     "application/pdf",
		srcFile:      "fixtures/pdf/source.bin",
		extractSrc:   "fixtures/pdf/ocr.md",
		extractFile:  "ocr.md",
		wikiKeywords: []string{
			"Kind: pdf",
			"Status: ingested",
			"## Extracted Markdown",
			"sample.pdf",
		},
	},
	{
		name:         "DOCX",
		filename:     "sample.docx",
		kind:         source.KindDOCX,
		mimeType:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		srcFile:      "fixtures/docx/source.bin",
		extractSrc:   "fixtures/docx/extract.md",
		extractFile:  "extract.md",
		wikiKeywords: []string{
			"Kind: docx",
			"Status: ingested",
			"## Extracted Markdown",
			"sample.docx",
		},
	},
	{
		name:         "XLSX",
		filename:     "sample.xlsx",
		kind:         source.KindXLSX,
		mimeType:     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		srcFile:      "fixtures/xlsx/source.bin",
		extractSrc:   "fixtures/xlsx/extract.md",
		extractFile:  "extract.md",
		wikiKeywords: []string{
			"Kind: xlsx",
			"Status: ingested",
			"## Extracted Markdown",
			"sample.xlsx",
		},
	},
	{
		name:         "PPTX",
		filename:     "sample.pptx",
		kind:         source.KindPPTX,
		mimeType:     "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		srcFile:      "fixtures/pptx/source.bin",
		extractSrc:   "fixtures/pptx/extract.md",
		extractFile:  "extract.md",
		wikiKeywords: []string{
			"Kind: pptx",
			"Status: ingested",
			"## Extracted Markdown",
			"sample.pptx",
		},
	},
	{
		name:         "Markdown",
		filename:     "sample.md",
		kind:         source.KindMarkdown,
		mimeType:     "text/markdown; charset=utf-8",
		srcFile:      "fixtures/md/source.md",
		extractSrc:   "fixtures/md/extract.md",
		extractFile:  "extract.md",
		wikiKeywords: []string{
			"Kind: markdown",
			"Status: ingested",
			"## Extracted Markdown",
			"sample.md",
		},
	},
}

// TestConversionFixtures round-trips each canonical source type through the
// ingest pipeline and asserts the resulting wiki page has the expected
// structural elements. Each sub-test prints the first 200 chars of the
// compiled wiki body so regressions are humanly visible in test output.
func TestConversionFixtures(t *testing.T) {
	for _, fx := range conversionFixtures {
		t.Run(fx.name, func(t *testing.T) {
			// Load source bytes from fixture directory.
			srcBytes, err := fixtureFS.ReadFile(fx.srcFile)
			if err != nil {
				t.Fatalf("load source bytes from %s: %v", fx.srcFile, err)
			}

			// Load pre-computed extraction golden.
			extractMD, err := fixtureFS.ReadFile(fx.extractSrc)
			if err != nil {
				t.Fatalf("load extract golden from %s: %v", fx.extractSrc, err)
			}

			// Fresh isolated pipeline for this sub-test.
			env := newTestPipeline(t)

			// Store the source bytes.
			src, _, err := env.sources.Put(context.Background(), source.PutInput{
				Kind:     fx.kind,
				Filename: fx.filename,
				MimeType: fx.mimeType,
				Bytes:    srcBytes,
			})
			if err != nil {
				t.Fatalf("Put: %v", err)
			}

			// Write pre-computed extraction output alongside the stored source.
			mdPath := env.sources.Path(src.ID, fx.extractFile)
			if err := os.WriteFile(mdPath, extractMD, 0o644); err != nil {
				t.Fatalf("write %s to source store: %v", fx.extractFile, err)
			}

			// Flip source status to the appropriate completion state.
			completedStatus := source.StatusExtractComplete
			if fx.kind == source.KindPDF {
				completedStatus = source.StatusOCRComplete
			}
			if _, err := env.sources.Update(src.ID, func(s *source.Source) error {
				s.Status = completedStatus
				return nil
			}); err != nil {
				t.Fatalf("Update status to %s: %v", completedStatus, err)
			}

			// Compile: source → wiki summary page.
			res, err := env.pipeline.Compile(context.Background(), src.ID)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if !res.Created {
				t.Error("Created = false, want true on first compile")
			}

			// Fetch the artifact: the compiled wiki page is the ground truth.
			page, err := env.wiki.ReadPage(res.Slug)
			if err != nil {
				t.Fatalf("ReadPage(%q): %v", res.Slug, err)
			}

			// Always print first 200 chars so regressions are humanly visible
			// in test output rather than hidden behind a bare PASS status.
			body200 := page.Body
			if len(body200) > 200 {
				body200 = body200[:200]
			}
			t.Logf("[%s] wiki page body (first 200 chars):\n%s", fx.name, body200)

			// Mojibake check: UTF-8 replacement char signals encoding corruption.
			for _, r := range page.Body {
				if r == '�' {
					t.Errorf("[%s] UTF-8 replacement char (mojibake) found in wiki page body", fx.name)
					break
				}
			}

			// Structural assertions: each required keyword must appear in the body.
			for _, kw := range fx.wikiKeywords {
				if !strings.Contains(page.Body, kw) {
					t.Errorf("[%s] wiki body missing %q\nfull body:\n%s", fx.name, kw, page.Body)
				}
			}

			// Ground-truth: source record must be ingested and point at the slug.
			post, err := env.sources.Get(src.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if post.Status != source.StatusIngested {
				t.Errorf("[%s] source status = %s, want ingested", fx.name, post.Status)
			}
			if len(post.WikiPages) != 1 || post.WikiPages[0] != res.Slug {
				t.Errorf("[%s] wiki_pages = %v, want [%s]", fx.name, post.WikiPages, res.Slug)
			}
		})
	}
}
