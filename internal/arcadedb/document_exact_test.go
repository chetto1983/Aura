package arcadedb

import (
	"strings"
	"testing"
)

const exactCardTrailer = "Written by Aura at ingest from the file itself: it describes the file and is not the file — " +
	"open it with document_open to read or compute on it."

// Spec 2026-10-25 decision 1: an exact term is asked of every full-text index as one phrase, with
// no score floor, inside the caller's scope, and only the texts that carry it come back. The
// engine's phrase is every word in one record, in any order (document_exact.go).
func TestExactTermDocumentsKeepOnlyTheCarriers(t *testing.T) {
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "Passage[text]"):
			reversed := candidateFixture(index, "doc_r:0", "doc_r", 0, "lexical_score", 9.0)
			reversed["text"], reversed["raw_sha256"] = "Mono is a style; Ubuntu is a distribution.", strings.Repeat("c", 64)
			carrierElsewhere := candidateFixture(index, "doc_a:1", "doc_a", 1, "lexical_score", 7.0)
			carrierElsewhere["text"] = "Ubuntu here, Mono there."
			carrier := candidateFixture(index, "doc_a:4", "doc_a", 4, "lexical_score", 6.1)
			carrier["text"] = "some text in the Ubuntu Mono typeface"
			return testResponse{Body: resultBody([]any{reversed, carrierElsewhere, carrier})}
		case strings.Contains(statement, "IndexedDocument[card]"):
			named := documentCardFixture("doc_b", "b.pdf", "b.pdf — PDF. Opens: the Ubuntu Mono sample.\n"+exactCardTrailer)
			named["card_score"] = 1.4
			return testResponse{Body: resultBody([]any{named})}
		default:
			named := documentCardFixture("doc_b", "b.pdf", "b.pdf — PDF. Opens: the Ubuntu Mono sample.\n"+exactCardTrailer)
			named["card_score"] = 2.9
			unnamed := documentCardFixture("doc_c", "c.pdf", "c.pdf — PDF, Ubuntu and then Mono.\n"+exactCardTrailer)
			unnamed["card_score"] = 2.0
			return testResponse{Body: resultBody([]any{named, unnamed})}
		}
	})
	term := QueryExactTerms(`quale documento mostra "Ubuntu Mono"?`)[0]
	hits, err := index.ExactTermDocuments(t.Context(), CandidateFilter{
		IdentityID: documentTestIdentity, Limit: 3, DocumentIDs: []string{"doc_a", "doc_b", "doc_r"},
	}, term)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits.Passages) != 1 || hits.Passages[0].PassageID != "doc_a:4" || hits.Passages[0].Leg != RetrievalLegExact ||
		*hits.Passages[0].Score() != 6.1 {
		t.Fatalf("passages = %+v, want doc_a's carrying passage alone, though another of its passages scored higher", hits.Passages)
	}
	if len(hits.Cards) != 1 || hits.Cards[0].SearchDocumentID != "doc_b" || hits.Cards[0].Score != 2.9 {
		t.Fatalf("cards = %+v, want doc_b once, at its higher score", hits.Cards)
	}
	if len(*requests) != 3 {
		t.Fatalf("requests = %d, want the passage, card and file-name indexes", len(*requests))
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		if params["query"] != `"Ubuntu Mono"` || params["min_lexical_score"] != float64(0) {
			t.Fatalf("params = %v, want the quoted phrase and no floor", params)
		}
		if !strings.Contains(statement, "search_document_id IN :document_ids") || strings.Contains(statement, "vector.") {
			t.Fatalf("statement leaves the scope or ranks by vector:\n%s", statement)
		}
	}
}

// The trailer is indexed in every card and names Aura: a card carries "Aura" only in its own text.
func TestExactTermDocumentsIgnoreTheCardTrailer(t *testing.T) {
	index, _ := testDocumentIndex(t, func(request recordedRequest) testResponse {
		if statement, _ := request.Payload["command"].(string); strings.Contains(statement, "FROM Passage") {
			return testResponse{Body: resultBody([]any{})}
		}
		return testResponse{Body: resultBody([]any{
			documentCardFixture("doc_trailer", "x.pdf", "x.pdf — PDF.\n"+exactCardTrailer),
			documentCardFixture("doc_named", "y.md", "y.md — Markdown. Opens: creato da Aura.\n"+exactCardTrailer),
		})}
	})
	hits, err := index.ExactTermDocuments(t.Context(), CandidateFilter{IdentityID: documentTestIdentity, Limit: 3},
		QueryExactTerms(`"Aura"`)[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(hits.Cards) != 1 || hits.Cards[0].SearchDocumentID != "doc_named" {
		t.Fatalf("cards = %+v, want only the card that names Aura itself", hits.Cards)
	}
}

func TestExactTermDocumentsTreatAMissingTypeAsAnEmptyLibrary(t *testing.T) {
	term := QueryExactTerms(`"Ubuntu Mono"`)[0]
	filter := CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}
	index, _ := testDocumentIndex(t, func(request recordedRequest) testResponse {
		if statement, _ := request.Payload["command"].(string); strings.Contains(statement, "FROM Passage") {
			return testResponse{Status: 500, Body: missingPassageBody}
		}
		return testResponse{Status: 500, Body: missingTypeBody}
	})
	hits, err := index.ExactTermDocuments(t.Context(), filter, term)
	if err != nil || len(hits.Passages) != 0 || len(hits.Cards) != 0 {
		t.Fatalf("hits = %+v err = %v, want an empty library", hits, err)
	}
}
