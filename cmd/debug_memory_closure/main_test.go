package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/memoryquality"
	"github.com/aura/aura/internal/wiki"
)

func TestFormatTextShowsPassAndManifest(t *testing.T) {
	report := memoryquality.Report{
		OK:       true,
		WikiDir:  "wiki",
		DBPath:   "aura.db",
		Manifest: memoryquality.Manifest{WikiPages: 2, ExpectedIndexDocs: 6, ActualIndexDocs: 6},
	}
	out := formatText(report)
	for _, want := range []string{"PASS", "wiki_pages=2", "expected_index_docs=6", "actual_index_docs=6"} {
		if !contains(out, want) {
			t.Fatalf("formatText missing %q in %q", want, out)
		}
	}
}

func TestFormatTextShowsIssues(t *testing.T) {
	report := memoryquality.Report{Issues: []memoryquality.Issue{{Kind: memoryquality.KindRawLeak, Severity: "critical", Ref: "source-paper", Message: "raw leak"}}}
	out := formatText(report)
	for _, want := range []string{"FAIL", "critical", "raw_leak", "source-paper", "raw leak"} {
		if !contains(out, want) {
			t.Fatalf("formatText missing %q in %q", want, out)
		}
	}
}

func TestApplyWikiMemoryHygieneLetsAuditPassAfterOpaqueSourceRename(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceID := "src_1234567890abcdef"
	oldSlug := "source-4-5942613039617418204"
	rawDir := filepath.Join(dir, "raw", sourceID)
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "ocr.md"), []byte("# Source OCR: opaque.pdf\n\n## Page 1\n\n# TEST 1 - Verifica operativa gestione richiesta offerta cliente (PMS)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{"id": sourceID, "wiki_pages": []string{oldSlug}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "source.json"), meta, 0644); err != nil {
		t.Fatal(err)
	}
	page := &wiki.Page{
		Title:         "Source: 4_5942613039617418204",
		Body:          "Compact source anchor.",
		Category:      "sources",
		Tags:          []string{"source", "pdf"},
		Sources:       []string{"source:" + sourceID},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     "2026-05-06T00:00:00Z",
		UpdatedAt:     "2026-05-06T00:00:00Z",
	}
	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatal(err)
	}

	before, err := memoryquality.Audit(context.Background(), memoryquality.Options{WikiDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if before.OK {
		t.Fatal("audit passed before auto-clean; want opaque source failure")
	}
	cleanReport, err := applyWikiMemoryHygiene(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleanReport.RenamedPages) != 1 {
		t.Fatalf("RenamedPages = %#v, want one rename", cleanReport.RenamedPages)
	}
	dbPath := filepath.Join(dir, "aura.db")
	docs, pages, err := rebuildSQLiteWikiIndex(context.Background(), dir, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if docs == 0 || pages == 0 {
		t.Fatalf("rebuild index docs=%d pages=%d, want non-zero", docs, pages)
	}
	after, err := memoryquality.Audit(context.Background(), memoryquality.Options{WikiDir: dir, DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if !after.OK {
		t.Fatalf("audit after auto-clean failed: %#v", after.Issues)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
