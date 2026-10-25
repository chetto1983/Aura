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
	"slices"
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
	terms := arcadedb.QueryExactTerms(`quale font mostra "Ubuntu Mono"?`)
	hits, err := index.ExactTermDocuments(context.Background(), filter, terms)
	if err != nil {
		t.Fatalf("ExactTermDocuments: %v", err)
	}
	if len(hits.Passages) != 1 || hits.Passages[0].SearchDocumentID != "doc_calibre" ||
		hits.Passages[0].Leg != arcadedb.RetrievalLegExact {
		t.Fatalf("passages = %+v, want doc_calibre alone: the phrase, not its words in another order", hits.Passages)
	}
	if !arcadedb.CarriesEvery(terms, hits.Passages[0].Text) {
		t.Fatal("the returned passage does not carry the term it was found for")
	}

	file := arcadedb.QueryExactTerms("apri calibre_demo.docx")
	hits, err = index.ExactTermDocuments(context.Background(), filter, file)
	if err != nil {
		t.Fatalf("ExactTermDocuments(file): %v", err)
	}
	if len(hits.Cards) != 1 || hits.Cards[0].SearchDocumentID != "doc_calibre" {
		t.Fatalf("cards = %+v, want the file's own card", hits.Cards)
	}

	// The trailer is indexed in every card, so the phrase finds them all; only the document that
	// names Aura in its own text carries it.
	aura := arcadedb.QueryExactTerms(`"Aura"`)
	hits, err = index.ExactTermDocuments(context.Background(), filter, aura)
	if err != nil {
		t.Fatalf("ExactTermDocuments(Aura): %v", err)
	}
	if len(hits.Cards) != 0 || len(hits.Passages) != 1 || hits.Passages[0].SearchDocumentID != "doc_markdown" {
		t.Fatalf("hits = %d cards, passages %+v; want the Markdown passage alone", len(hits.Cards), hits.Passages)
	}
}

// The lab VM's s04 through the whole cascade (acceptance 1): quoted, the font's document comes
// first and the reversed words do not count; unquoted, the reranked cosine and its floor drop it,
// which is the burial decision 1 ends.
func TestDocumentExactLiveRetrieveRanksTheQuotedFontFirst(t *testing.T) {
	retriever, _ := exactLiveIndex(t)
	for _, query := range []string{`quale font mostra "Ubuntu Mono"?`, `"Ubuntu Mono"`} {
		response, err := retriever.Retrieve(context.Background(), documents.RetrievalRequest{IdentityID: liveIdentity, Query: query})
		if err != nil {
			t.Fatalf("Retrieve(%s): %v", query, err)
		}
		if response.Abstained || len(response.Documents) == 0 || response.Documents[0].DocumentID != "doc_calibre" {
			t.Fatalf("%s: documents = %v abstained %v, want doc_calibre first", query, liveDocumentIDs(response), response.Abstained)
		}
		if leg := response.Documents[0].Passages[0].Evidence[0].Leg; leg != string(arcadedb.RetrievalLegExact) {
			t.Fatalf("%s: carrier's evidence = %s, want exact", query, leg)
		}
		if slices.Contains(liveDocumentIDs(response), "doc_reversed") {
			t.Fatalf("%s: documents = %v, the reversed words carry no phrase", query, liveDocumentIDs(response))
		}
	}
	unquoted, err := retriever.Retrieve(context.Background(), documents.RetrievalRequest{
		IdentityID: liveIdentity, Query: "quale font mostra Ubuntu Mono?",
	})
	if err != nil {
		t.Fatalf("Retrieve(unquoted): %v", err)
	}
	if slices.Contains(liveDocumentIDs(unquoted), "doc_calibre") {
		t.Fatalf("unquoted documents = %v: the floor kept the font, so this library does not reproduce the burial", liveDocumentIDs(unquoted))
	}
}

// Review C1 on the engine: StandardAnalyzer keeps calibre_demo.docx one token, so the identifier
// calibre_demo cut from it is in no index. Every card points away from the query's vector, so no
// leg passes it; the exact lane finds the file by its name, unquoted, and its title carries both.
func TestDocumentExactLiveRetrieveFindsAnUnderscoreFileByName(t *testing.T) {
	retriever, _ := exactLiveIndex(t)
	response, err := retriever.Retrieve(context.Background(), documents.RetrievalRequest{
		IdentityID: liveIdentity, Query: "apri calibre_demo.docx",
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if response.Abstained || len(response.Documents) == 0 || response.Documents[0].DocumentID != "doc_calibre" {
		t.Fatalf("documents = %v abstained %v, want doc_calibre first", liveDocumentIDs(response), response.Abstained)
	}
	exact := slices.ContainsFunc(response.Documents[0].Evidence, func(evidence documents.RetrievalEvidence) bool {
		return evidence.Leg == string(arcadedb.RetrievalLegExact)
	})
	if !exact {
		t.Fatalf("doc_calibre evidence = %+v, want the exact lane's", response.Documents[0].Evidence)
	}
}

func liveDocumentIDs(response documents.RetrievalResponse) []string {
	ids := make([]string, 0, len(response.Documents))
	for _, document := range response.Documents {
		ids = append(ids, document.DocumentID)
	}
	return ids
}
