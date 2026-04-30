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
	dir       string
	pipeline  *Pipeline
	sources   *source.Store
	wiki      *wiki.Store
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
	src, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "paper.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF-1.4 fake " + t.Name()),
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
	wantSlug := wiki.Slug("Source " + src.ID)
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
	if page.Title != "Source "+src.ID {
		t.Errorf("title = %q", page.Title)
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
