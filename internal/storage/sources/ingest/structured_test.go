package ingest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	source "github.com/aura/aura/internal/storage/sources/store"
)

func TestStructuredExtractMaterializesSectionPages(t *testing.T) {
	env := newTestPipeline(t)
	extractMD := `---
workbook_title: "customers.xlsx"
sheets:
  - "Customers"
sheet_row_counts:
  "Customers": 5
author: "Aura Test"
---

## Sheet: Customers

**Columns**: Customer, City, Plan

### Row 1
- Customer: Ada Rossi
- City: Milano
- Plan: Gold

### Row 2
- Customer: Bruno Verdi
- City: Torino
- Plan: Silver

### Row 3
- Customer: Carla Bianchi
- City: Roma
- Plan: Bronze

### Row 4
- Customer: Dino Neri
- City: Napoli
- Plan: Gold

### Row 5
- Customer: Elena Gialli
- City: Bari
- Plan: Silver
`

	src, _, err := env.sources.Put(context.Background(), source.PutInput{
		Kind:     source.KindXLSX,
		Filename: "customers.xlsx",
		MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Bytes:    []byte("fake workbook bytes"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := source.WriteExtractionFiles(env.sources, src, source.ExtractResult{
		Markdown: extractMD,
		Metadata: source.ExtractionMeta{ExtractorName: "markitdown_xlsx", RowCount: 5},
	}); err != nil {
		t.Fatalf("WriteExtractionFiles: %v", err)
	}
	updated, err := env.sources.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusExtractComplete
		s.Extract = &source.ExtractionMeta{ExtractorName: "markitdown_xlsx", RowCount: 5}
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	res, err := env.pipeline.Compile(context.Background(), updated.ID)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if res.Slug != "source-customers" {
		t.Fatalf("slug = %q, want source-customers", res.Slug)
	}
	if len(res.MaterializedPages) != 5 {
		t.Fatalf("materialized pages = %v, want 5", res.MaterializedPages)
	}
	wantFirst := "source-customers-001-row-1"
	if res.MaterializedPages[0] != wantFirst {
		t.Fatalf("first materialized page = %q, want %q", res.MaterializedPages[0], wantFirst)
	}

	main, err := env.wiki.ReadPage(res.Slug)
	if err != nil {
		t.Fatalf("ReadPage(main): %v", err)
	}
	if !strings.Contains(main.Body, "[["+wantFirst+"]]") {
		t.Fatalf("main page missing materialized link %q:\n%s", wantFirst, main.Body)
	}

	rowPage, err := env.wiki.ReadPage(wantFirst)
	if err != nil {
		t.Fatalf("ReadPage(%s): %v", wantFirst, err)
	}
	for _, want := range []string{
		"Parent source page: [[source-customers]]",
		"- Source ID: `" + updated.ID + "`",
		"- Heading: Row 1",
		"- Customer: Ada Rossi",
		"- City: Milano",
	} {
		if !strings.Contains(rowPage.Body, want) {
			t.Fatalf("row page missing %q:\n%s", want, rowPage.Body)
		}
	}

	post, err := env.sources.Get(updated.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(post.WikiPages) != 6 || post.WikiPages[0] != res.Slug || post.WikiPages[1] != wantFirst {
		t.Fatalf("wiki_pages = %v, want main + 5 row pages", post.WikiPages)
	}

	again, err := env.pipeline.Compile(context.Background(), updated.ID)
	if err != nil {
		t.Fatalf("Compile again: %v", err)
	}
	if again.Created {
		t.Fatal("second Compile Created = true, want false")
	}
	if len(again.MaterializedPages) != 5 {
		t.Fatalf("second materialized pages = %v, want 5", again.MaterializedPages)
	}
	if _, err := os.Stat(env.sources.Path(updated.ID, source.ExtractMarkdownFile)); err != nil {
		t.Fatalf("extract.md should remain in raw source store: %v", err)
	}
}

// TestStructuredExtractCapsAtMaxSections is the safety net guarding
// against the xlsx row-explosion failure mode (2026-05-23): when a
// converter emits more than MaxStructuredSections '### heading'
// sections, parseStructuredExtract returns nil so the source becomes
// one wiki page with H2/H3 subnodes instead of N small fragments.
func TestStructuredExtractCapsAtMaxSections(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\nworkbook_title: \"big.xlsx\"\nsheet_row_counts:\n  \"S\": ")
	fmt.Fprintf(&b, "%d", MaxStructuredSections+1)
	b.WriteString("\n---\n\n## Sheet: S\n\n")
	for i := 1; i <= MaxStructuredSections+1; i++ {
		fmt.Fprintf(&b, "### Row %d\n- col: v%d\n\n", i, i)
	}
	got := parseStructuredExtract(b.String(), "Big", "source-big")
	if got != nil {
		t.Fatalf("expected nil when section count %d exceeds cap %d, got %d sections",
			MaxStructuredSections+1, MaxStructuredSections, len(got.Sections))
	}
}

// TestStructuredExtractMaterializesAtCapLimit confirms the cap is
// inclusive — exactly MaxStructuredSections sections still materialize
// (only the strictly-larger case is rejected).
func TestStructuredExtractMaterializesAtCapLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\nslide_count: ")
	fmt.Fprintf(&b, "%d", MaxStructuredSections)
	b.WriteString("\n---\n\n")
	for i := 1; i <= MaxStructuredSections; i++ {
		fmt.Fprintf(&b, "### Slide %d\nbody %d\n\n", i, i)
	}
	got := parseStructuredExtract(b.String(), "Deck", "source-deck")
	if got == nil {
		t.Fatalf("expected non-nil at section count = cap (%d)", MaxStructuredSections)
	}
	if len(got.Sections) != MaxStructuredSections {
		t.Fatalf("sections = %d, want %d", len(got.Sections), MaxStructuredSections)
	}
}
