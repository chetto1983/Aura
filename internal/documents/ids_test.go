package documents

import (
	"strings"
	"testing"
	"time"
)

func TestDocumentIDStableForSameContentAndSource(t *testing.T) {
	a := DocumentID("hash-a", "source-a")
	b := DocumentID("hash-a", "source-a")
	if a != b {
		t.Fatalf("document id should be stable: %q != %q", a, b)
	}
	if !strings.HasPrefix(a, "doc_") || len(a) != len("doc_")+32 {
		t.Fatalf("unexpected document id format: %q", a)
	}
}

func TestDocumentIDDifferentForDifferentSource(t *testing.T) {
	a := DocumentID("hash-a", "source-a")
	b := DocumentID("hash-a", "source-b")
	if a == b {
		t.Fatalf("document id should include source id: %q", a)
	}
}

func TestChunkHashUsesLocator(t *testing.T) {
	a, err := ChunkHash("Same text", Locator{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ChunkHash("Same   text", Locator{Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("chunk hash should change when locator changes")
	}
	c, err := ChunkHash("Same   text", Locator{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a != c {
		t.Fatal("chunk hash should normalize whitespace")
	}
}

func TestChunkIDIsZeroPaddedAndOrdered(t *testing.T) {
	if got := ChunkID("doc_abc", 7); got != "chunk_doc_abc_000007" {
		t.Fatalf("unexpected chunk id: %q", got)
	}
	if ChunkID("doc_abc", 9) >= ChunkID("doc_abc", 10) {
		t.Fatal("zero-padded chunk ids should sort in chunk order")
	}
}

func TestBuildExtractedDocumentAssignsStableChunkMetadata(t *testing.T) {
	req := IngestRequest{
		SourceID:   "cli",
		SourceKind: "local",
		FileName:   "manual.pdf",
		MIMEType:   "application/pdf",
		SizeBytes:  123,
	}
	resp := &ExtractorResponse{
		Title: "Manual",
		Chunks: []ExtractedChunk{
			{Kind: "page", Text: " hello   world ", Locator: Locator{Page: 1}},
			{Kind: "page", Text: "second", Locator: Locator{Page: 2}},
		},
	}
	doc, err := BuildExtractedDocument(req, "content-hash", resp, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != DocumentID("content-hash", "cli") {
		t.Fatalf("document id = %q", doc.ID)
	}
	if len(doc.Chunks) != 2 {
		t.Fatalf("chunks = %d", len(doc.Chunks))
	}
	if doc.Chunks[0].Text != "hello world" {
		t.Fatalf("chunk text was not normalized: %q", doc.Chunks[0].Text)
	}
	if doc.Chunks[1].ChunkCount != 2 {
		t.Fatalf("chunk count = %d", doc.Chunks[1].ChunkCount)
	}
}
