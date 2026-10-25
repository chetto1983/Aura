//go:build arcadedb_integration

// Exact terms over a live ArcadeDB (spec 2026-10-25-document-exactness-and-judge, decision 1): a
// font named once inside a long passage, far from the query's vector, is found as a phrase by the
// full-text indexes and reaches the agent first, where the reranked cosine and its floor drop it.
//
// Run: ARCADEDB_URL=http://127.0.0.1:2480 ARCADEDB_PASSWORD=… go test -tags arcadedb_integration \
//
//	-run DocumentExactLive ./internal/arcadedb/
package arcadedb_test

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/documents"
)

// cardTrailerForTest is what filecard writes at the end of every card; the index holds it too.
const cardTrailerForTest = "Written by Aura at ingest from the file itself: it describes the file and is not the file — " +
	"open it with document_open to read or compute on it."

// exactLibrary is the lab VM's case in miniature, every vector in liveSpace so the dense legs run:
// the font sits in one long passage pointing away from the query, the slides sit on the query's
// vector, and the Markdown file names Aura in its own text.
func exactLibrary() []liveDocument {
	far, near := []float64{0, 1, 0}, []float64{0.95, 0.05, 0}
	fonts := strings.Repeat("calibre converts documents between formats and keeps the layout. ", 30) +
		"Fun with fonts. This document has embedded the Ubuntu font family. The body text is in the " +
		"Ubuntu typeface, here is some text in the Ubuntu Mono typeface, notice how every letter has " +
		"the same width, even i and m. " + strings.Repeat("Tables and lists convert as well. ", 30)
	return []liveDocument{
		{id: "doc_calibre", fileName: "calibre_demo.docx", words: "calibre_demo.docx calibre demo docx",
			card: "calibre_demo.docx — document, 12 KB.\nTitled: calibre demo\n" + cardTrailerForTest, content: "calibre",
			passages: []livePassage{{text: fonts, vector: far, space: liveSpace}}},
		{id: "doc_slides", fileName: "SampleShow.pptx", words: "SampleShow.pptx SampleShow pptx",
			card: "SampleShow.pptx — presentation, 3 slides.\n" + cardTrailerForTest, content: "slides",
			passages: []livePassage{{text: "Which font family does the document embed? The slides compare " +
				"monospaced and proportional fonts in general.", vector: near, space: liveSpace}}},
		{id: "doc_reversed", fileName: "note.txt", words: "note.txt note txt",
			card: "note.txt — text file.\n" + cardTrailerForTest, content: "note",
			passages: []livePassage{{text: "Mono is a typeface style; Ubuntu is a Linux distribution.", vector: far, space: liveSpace}}},
		{id: "doc_markdown", fileName: "esempio.md", words: "esempio.md esempio md",
			card: "esempio.md — Markdown.\n" + cardTrailerForTest, content: "markdown",
			passages: []livePassage{{text: "Questo è un file di esempio creato da Aura.", vector: far, space: liveSpace}}},
	}
}

func exactLiveIndex(t *testing.T) (*documents.HostRetriever, *arcadedb.DocumentIndex) {
	t.Helper()
	client, _ := liveDocumentDatabase(t)
	seedLibrary(t, client, liveSchema, exactLibrary())
	return liveRetriever(t, client)
}

func TestDocumentExactLivePhraseFindsTheTermInOrderOnly(t *testing.T) {
	_, index := exactLiveIndex(t)
	filter := arcadedb.CandidateFilter{IdentityID: liveIdentity, Limit: 10}
	term := arcadedb.QueryExactTerms(`quale font mostra "Ubuntu Mono"?`)[0]
	hits, err := index.ExactTermDocuments(context.Background(), filter, term)
	if err != nil {
		t.Fatalf("ExactTermDocuments: %v", err)
	}
	if len(hits.Passages) != 1 || hits.Passages[0].SearchDocumentID != "doc_calibre" ||
		hits.Passages[0].Leg != arcadedb.RetrievalLegExact {
		t.Fatalf("passages = %+v, want doc_calibre alone: the phrase, not its words in another order", hits.Passages)
	}
	if !term.CarriedBy(hits.Passages[0].Text) {
		t.Fatal("the returned passage does not carry the term it was found for")
	}

	file := arcadedb.QueryExactTerms("apri calibre_demo.docx")[0]
	hits, err = index.ExactTermDocuments(context.Background(), filter, file)
	if err != nil {
		t.Fatalf("ExactTermDocuments(file): %v", err)
	}
	if len(hits.Cards) != 1 || hits.Cards[0].SearchDocumentID != "doc_calibre" {
		t.Fatalf("cards = %+v, want the file's own card", hits.Cards)
	}

	// The trailer is indexed in every card, so the phrase finds them all; only the document that
	// names Aura in its own text carries it.
	aura := arcadedb.QueryExactTerms(`"Aura"`)[0]
	hits, err = index.ExactTermDocuments(context.Background(), filter, aura)
	if err != nil {
		t.Fatalf("ExactTermDocuments(Aura): %v", err)
	}
	if len(hits.Cards) != 0 || len(hits.Passages) != 1 || hits.Passages[0].SearchDocumentID != "doc_markdown" {
		t.Fatalf("hits = %d cards, passages %+v; want the Markdown passage alone", len(hits.Cards), hits.Passages)
	}
}
