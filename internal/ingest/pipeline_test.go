package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/wiki"
)

// testEnv bundles the temp dir + stores so individual tests can poke at side
// files (index.md, log.md) without re-deriving paths.
type testEnv struct {
	dir      string
	pipeline *Pipeline
	sources  *source.Store
	wiki     *wiki.Store
}

// newTestPipeline wires a fresh source.Store + wiki.Store rooted in t.TempDir().
// We reuse the same temp dir for both — the wiki is at <tmp> and sources at
// <tmp>/raw — matching the production layout (PDR §4).
func newTestPipeline(t *testing.T) testEnv {
	t.Helper()
	wikiDir := t.TempDir()

	wikiStore, err := wiki.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("wiki.NewStore: %v", err)
	}
	srcStore, err := source.NewStore(wikiDir, nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}

	p, err := New(Config{
		Sources: srcStore,
		Wiki:    wikiStore,
		Now:     func() time.Time { return time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}
	return testEnv{dir: wikiDir, pipeline: p, sources: srcStore, wiki: wikiStore}
}

// putOCRComplete sets up a PDF source with an ocr.md and status=ocr_complete,
// mimicking what the slice-4 Telegram pipeline produces on disk.
func putOCRComplete(t *testing.T, store *source.Store, ocrBody string) *source.Source {
	t.Helper()
	return putOCRCompleteAs(t, store, "paper.pdf", "%PDF-1.4 fake "+t.Name(), ocrBody)
}

// putOCRCompleteAs lets a test pin both filename and content so collision
// scenarios (same filename, different bytes → different source IDs, same
// candidate slug) are reproducible.
func putOCRCompleteAs(t *testing.T, store *source.Store, filename, content, ocrBody string) *source.Source {
	t.Helper()
	src, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: filename,
		MimeType: "application/pdf",
		Bytes:    []byte(content),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.WriteFile(store.Path(src.ID, "ocr.md"), []byte(ocrBody), 0o644); err != nil {
		t.Fatalf("write ocr.md: %v", err)
	}
	updated, err := store.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		s.OCRModel = "mistral-ocr-latest"
		s.PageCount = 1
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	return updated
}

func TestNewValidatesDeps(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("expected error when sources nil")
	}
	srcStore, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("source.NewStore: %v", err)
	}
	if _, err := New(Config{Sources: srcStore}); err == nil {
		t.Error("expected error when wiki nil")
	}
}

func TestCompile_HappyPath(t *testing.T) {
	env := newTestPipeline(t)
	p, srcStore, wikiStore := env.pipeline, env.sources, env.wiki

	ocrBody := "# Source OCR: paper.pdf\n\nSource ID: src_xxx\nModel: mistral-ocr-latest\n\n## Page 1\n\nThe quick brown fox jumps over the lazy dog. This is the first paragraph of the OCR'd content."
	src := putOCRComplete(t, srcStore, ocrBody)

	res, err := p.Compile(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !res.Created {
		t.Errorf("Created = false, want true on first compile")
	}
	// Slug derives from the display filename (sans extension) so the wiki
	// graph view shows readable nodes, not opaque source IDs.
	wantSlug := "source-paper"
	if res.Slug != wantSlug {
		t.Errorf("Slug = %q, want %q", res.Slug, wantSlug)
	}
	if !strings.Contains(res.PageNote, wantSlug) {
		t.Errorf("PageNote should reference slug: %q", res.PageNote)
	}

	// Wiki page on disk.
	page, err := wikiStore.ReadPage(wantSlug)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.Title != "Source: paper" {
		t.Errorf("title = %q, want %q", page.Title, "Source: paper")
	}
	if page.Category != "sources" {
		t.Errorf("category = %q, want sources", page.Category)
	}
	if len(page.Sources) != 1 || page.Sources[0] != "source:"+src.ID {
		t.Errorf("sources = %v, want [source:%s]", page.Sources, src.ID)
	}
	for _, want := range []string{
		"# Source: paper.pdf",
		"Source ID: `" + src.ID + "`",
		"Filename: paper.pdf",
		"Pages: 1",
		"OCR model: mistral-ocr-latest",
		// Body must reflect the post-flip status, not the pre-flip value — a
		// re-read of an ingested source page should never say ocr_complete.
		"Status: ingested",
		"## Raw OCR",
		"## Preview",
		"The quick brown fox",
	} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("body missing %q in:\n%s", want, page.Body)
		}
	}
	// Preview must NOT contain the OCR file's own header lines (PDR §8 — don't
	// dump full OCR text into the wiki page; surface body only).
	for _, leak := range []string{"# Source OCR:", "Source ID: src_xxx", "Model: mistral-ocr-latest", "## Page 1"} {
		if strings.Contains(page.Body, leak) {
			t.Errorf("preview leaked OCR header %q in:\n%s", leak, page.Body)
		}
	}

	// Source flipped to ingested with wiki_pages set.
	updated, err := srcStore.Get(src.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != source.StatusIngested {
		t.Errorf("status = %s, want ingested", updated.Status)
	}
	if len(updated.WikiPages) != 1 || updated.WikiPages[0] != wantSlug {
		t.Errorf("wiki_pages = %v, want [%s]", updated.WikiPages, wantSlug)
	}
}

func TestCompile_Idempotent(t *testing.T) {
	env := newTestPipeline(t)
	p, srcStore := env.pipeline, env.sources
	src := putOCRComplete(t, srcStore, "# Source OCR: x\n\n## Page 1\n\nbody")

	first, err := p.Compile(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("first Compile: %v", err)
	}
	if !first.Created {
		t.Errorf("first Created = false, want true")
	}

	second, err := p.Compile(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("second Compile: %v", err)
	}
	if second.Created {
		t.Errorf("second Created = true, want false (idempotent)")
	}
	if second.Slug != first.Slug {
		t.Errorf("second slug = %q, want %q", second.Slug, first.Slug)
	}
	if !strings.Contains(second.PageNote, "already compiled") {
		t.Errorf("second PageNote should signal idempotent skip: %q", second.PageNote)
	}
}

func TestCompile_MissingOCR(t *testing.T) {
	env := newTestPipeline(t)
	p, srcStore := env.pipeline, env.sources
	// Status = ocr_complete but no ocr.md on disk → error pointing at the
	// recovery path (run ocr_source first).
	src, _, err := srcStore.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "no-ocr.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF noocr"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := srcStore.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if _, err := p.Compile(context.Background(), src.ID); err == nil {
		t.Fatal("expected ocr.md-missing error")
	} else if !strings.Contains(err.Error(), "ocr.md missing") || !strings.Contains(err.Error(), "ocr_source") {
		t.Errorf("error should hint ocr_source: %v", err)
	}
}

func TestCompile_WrongStatus(t *testing.T) {
	env := newTestPipeline(t)
	p, srcStore := env.pipeline, env.sources
	src, _, err := srcStore.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "stored.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF stored"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := p.Compile(context.Background(), src.ID); err == nil {
		t.Fatal("expected wrong-status error")
	} else if !strings.Contains(err.Error(), "want ocr_complete") {
		t.Errorf("error should mention required status: %v", err)
	}
}

func TestCompile_UnknownAndInvalid(t *testing.T) {
	p := newTestPipeline(t).pipeline

	if _, err := p.Compile(context.Background(), "src_0000000000000000"); err == nil {
		t.Error("expected unknown-id error")
	}
	if _, err := p.Compile(context.Background(), "../../etc/passwd"); err == nil {
		t.Error("expected invalid-id error")
	}
}

func TestAfterOCR_Adapter(t *testing.T) {
	env := newTestPipeline(t)
	p, srcStore := env.pipeline, env.sources
	src := putOCRComplete(t, srcStore, "# Source OCR: x\n\n## Page 1\n\nhello")

	note, err := p.AfterOCR(context.Background(), src)
	if err != nil {
		t.Fatalf("AfterOCR: %v", err)
	}
	if !strings.Contains(note, "compiled as [[") {
		t.Errorf("note should reference compiled slug: %q", note)
	}
}

func TestBuildPreview(t *testing.T) {
	cases := []struct {
		name string
		md   string
		max  int
		want string
	}{
		{
			"strips ocr header and finds page body",
			"# Source OCR: foo.pdf\n\nSource ID: src_x\nModel: m\n\n## Page 1\n\nhello world",
			100,
			"hello world",
		},
		{
			"truncates at maxChars",
			"## Page 1\n\nabcdefghij",
			5,
			"abcde…",
		},
		{
			"empty when no body after page header",
			"## Page 1\n\n",
			100,
			"",
		},
		{
			"no page header → whole body kept",
			"freeform notes\nwithout OCR header",
			100,
			"freeform notes\nwithout OCR header",
		},
		{
			"zero maxChars returns empty",
			"## Page 1\n\nbody",
			0,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPreview(tc.md, tc.max)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildPreview_UTF8Boundary(t *testing.T) {
	// Italian / accented chars across the cut point — must not slice mid-rune.
	body := "## Page 1\n\nperché perché perché perché"
	got := buildPreview(body, 12)
	// Output must be valid UTF-8 (Go strings are bytes; check by re-decoding).
	for _, r := range got {
		if r == 0xFFFD {
			t.Errorf("UTF-8 replacement char in cut output: %q", got)
		}
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix: %q", got)
	}
}

func TestCompile_WikiFilesOnDisk(t *testing.T) {
	env := newTestPipeline(t)
	src := putOCRComplete(t, env.sources, "## Page 1\n\nhello")

	if _, err := env.pipeline.Compile(context.Background(), src.ID); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// wiki.Store.WritePage produces index.md + log.md side files at the
	// wiki root (PDR §4 layout).
	for _, name := range []string{"index.md", "log.md"} {
		if _, err := os.Stat(filepath.Join(env.dir, name)); err != nil {
			t.Errorf("wiki %s missing: %v", name, err)
		}
	}
}

// TestCompile_FilenameCollision: two sources with the same display
// filename (different bytes → different IDs) must end up at different
// slugs. The second source gets a short-id suffix so it doesn't silently
// overwrite the first page.
func TestCompile_FilenameCollision(t *testing.T) {
	env := newTestPipeline(t)
	a := putOCRCompleteAs(t, env.sources, "uta.pdf", "%PDF-A", "## Page 1\n\nfirst")
	b := putOCRCompleteAs(t, env.sources, "uta.pdf", "%PDF-B", "## Page 1\n\nsecond")

	first, err := env.pipeline.Compile(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Compile a: %v", err)
	}
	if first.Slug != "source-uta" {
		t.Errorf("first slug = %q, want source-uta", first.Slug)
	}

	second, err := env.pipeline.Compile(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("Compile b: %v", err)
	}
	if second.Slug == first.Slug {
		t.Fatalf("colliding slug: a=%s b=%s", first.Slug, second.Slug)
	}
	if !strings.HasPrefix(second.Slug, "source-uta-") {
		t.Errorf("second slug = %q, want source-uta-<short> prefix", second.Slug)
	}
	// Both pages must remain readable — the disambiguator must not have
	// rewritten the first page in place.
	if _, err := env.wiki.ReadPage(first.Slug); err != nil {
		t.Errorf("first page lost: %v", err)
	}
	if _, err := env.wiki.ReadPage(second.Slug); err != nil {
		t.Errorf("second page missing: %v", err)
	}
}

// TestCompile_MigratesStaleSlug: a source already ingested at an old slug
// (e.g. before the renderer slug rule changed) gets rewritten at the new
// slug on next Compile, and the old wiki page is deleted so the wiki
// doesn't accumulate orphan slugs.
func TestCompile_MigratesStaleSlug(t *testing.T) {
	env := newTestPipeline(t)
	src := putOCRCompleteAs(t, env.sources, "report.pdf", "%PDF-mig", "## Page 1\n\nbody")

	// Plant a stale page at the old "Source <id>" slug rule.
	staleSlug := wiki.Slug("Source " + src.ID)
	stalePage := &wiki.Page{
		Title:         "Source " + src.ID,
		Body:          "stale",
		Category:      "sources",
		Tags:          []string{"source", "pdf"},
		Sources:       []string{"source:" + src.ID},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     "2026-04-29T00:00:00Z",
		UpdatedAt:     "2026-04-29T00:00:00Z",
	}
	if err := env.wiki.WritePage(context.Background(), stalePage); err != nil {
		t.Fatalf("seed stale page: %v", err)
	}
	if _, err := env.sources.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusIngested
		s.WikiPages = []string{staleSlug}
		return nil
	}); err != nil {
		t.Fatalf("seed source state: %v", err)
	}

	res, err := env.pipeline.Compile(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if res.Slug != "source-report" {
		t.Errorf("new slug = %q, want source-report", res.Slug)
	}
	if !res.Created {
		t.Errorf("Created = false, want true on slug rewrite")
	}

	// New page exists, old page deleted.
	if _, err := env.wiki.ReadPage("source-report"); err != nil {
		t.Errorf("new page missing: %v", err)
	}
	if _, err := env.wiki.ReadPage(staleSlug); err == nil {
		t.Errorf("stale page %q still exists; should have been deleted", staleSlug)
	}

	// source.json now points only at the new slug.
	post, err := env.sources.Get(src.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(post.WikiPages) != 1 || post.WikiPages[0] != "source-report" {
		t.Errorf("wiki_pages = %v, want [source-report]", post.WikiPages)
	}
}

// TestCompile_EmptyFilenameFallback: missing filename → title falls back
// to "Source: <id>" so we still produce a valid slug.
func TestCompile_EmptyFilenameFallback(t *testing.T) {
	env := newTestPipeline(t)
	src := putOCRCompleteAs(t, env.sources, "  ", "%PDF-noname", "## Page 1\n\nbody")

	res, err := env.pipeline.Compile(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if res.Slug != wiki.Slug("Source: "+src.ID) {
		t.Errorf("slug = %q, want fallback derived from id", res.Slug)
	}
}

func TestBuildTitle(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string
	}{
		{"strips .pdf", "uta.pdf", "Source: uta"},
		{"trims whitespace", "  uta.pdf  ", "Source: uta"},
		{"keeps spaces inside name", "MARCHETTO DAVIDE_DDT N. 90.pdf", "Source: MARCHETTO DAVIDE_DDT N. 90"},
		{"empty falls back to id", "", "Source: src_FALLBACK"},
		{"whitespace falls back to id", "   ", "Source: src_FALLBACK"},
		{"strips only final extension", "report.v2.pdf", "Source: report.v2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildTitle(&source.Source{Filename: tc.filename, ID: "src_FALLBACK"}, "src_FALLBACK")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	cases := map[string]string{
		"src_24abf740febd9eac": "24abf7",
		"src_abc":              "abc",
		"src_":                 "",
		"not_a_source_id":      "",
		"":                     "",
	}
	for in, want := range cases {
		if got := shortID(in); got != want {
			t.Errorf("shortID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStaleSlugsToDelete(t *testing.T) {
	cases := []struct {
		name    string
		prev    []string
		current string
		want    []string
	}{
		{"none stale when matches", []string{"source-uta"}, "source-uta", []string{}},
		{"all stale when differs", []string{"source-old"}, "source-uta", []string{"source-old"}},
		{"empty entries dropped", []string{"", "source-old", ""}, "source-uta", []string{"source-old"}},
		{"nil prev", nil, "source-uta", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := staleSlugsToDelete(tc.prev, tc.current)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
