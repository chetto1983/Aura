package arcadedb

import (
	"strings"
	"testing"
)

func documentCardFixture(id, fileName, card string) map[string]any {
	return map[string]any{
		"search_document_id": id, "source_kind": "s3",
		"source_key": "contabilita/" + fileName, "file_name": fileName,
		"raw_sha256": strings.Repeat("a", 64), "size_bytes": 26740,
		"passage_count": 13, "card": card, "card_score": 1.0,
	}
}

// TestDocumentCardsAnswerAnUnscopedQuery carries forward, against ArcadeDB, the guarantee a
// deleted PostgreSQL integration test used to hold.
//
// That test pinned measured operator damage: an absent document scope means "every
// document", it reached the SQL as a NIL slice, pgx encoded nil as SQL NULL rather than an
// empty array, and under three-valued logic the predicate filtered every row out. Every
// unscoped call returned ZERO cards. On 2026-08-06 document_search("Clienti") found nothing
// for a ready Clienti.xlsx while document_search("Clienti.xlsx") found it.
//
// The SQL is gone, so that exact defect cannot recur -- but the guarantee it protected is
// the same one the card leg still owes, and the degraded path is still where it bites: when
// the vector leg is down, Retrieve falls back to cards alone, so a card leg that answers
// nothing is a total retrieval failure reporting "degraded_card_only".
func TestDocumentCardsAnswerAnUnscopedQuery(t *testing.T) {
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: resultBody([]any{
			documentCardFixture("doc_"+strings.Repeat("a", 32), "clienti_complesso.xlsx",
				"spreadsheet, 3 sheets. Citta: most common Torino (98)."),
		})}
	}, true)

	cards, err := index.DocumentCards(t.Context(), documentTestIdentity, "Torino", 3)
	if err != nil {
		t.Fatalf("DocumentCards: %v", err)
	}
	if len(cards) != 1 || cards[0].FileName != "clienti_complesso.xlsx" {
		t.Fatalf("cards = %+v", cards)
	}
	if cards[0].SourceKey != "contabilita/clienti_complesso.xlsx" || cards[0].SourceKind != "s3" {
		t.Fatalf("card lost the object coordinates, so a card-only answer is unopenable: %+v", cards[0])
	}
	statement, _ := (*requests)[0].Payload["command"].(string)
	// Both legs, OR'd: the card matches what a document CONTAINS, the split name matches
	// what it is CALLED. Dropping either silently narrows recall.
	if !strings.Contains(statement, "SEARCH_INDEX('IndexedDocument[card]'") ||
		!strings.Contains(statement, "SEARCH_INDEX('IndexedDocument[file_name_words]'") {
		t.Fatalf("statement = %s", statement)
	}
}

// A file that could not be described still exists, is still findable by name and can still
// be opened, so an empty card must not fail the decode -- filecard returns one for a
// truncated zip by design.
func TestDocumentCardsAcceptADocumentWithoutACard(t *testing.T) {
	var index *DocumentIndex
	index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
		row := documentCardFixture("doc_"+strings.Repeat("b", 32), "rotto.zip", "")
		return testResponse{Body: resultBody([]any{row})}
	}, true)
	cards, err := index.DocumentCards(t.Context(), documentTestIdentity, "rotto", 3)
	if err != nil || len(cards) != 1 || cards[0].Card != "" {
		t.Fatalf("cards = %+v, err = %v", cards, err)
	}
}

func TestDocumentCardsRejectMalformedRows(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"bad sha":       func(row map[string]any) { row["raw_sha256"] = "nope" },
		"missing key":   func(row map[string]any) { delete(row, "source_key") },
		"missing name":  func(row map[string]any) { delete(row, "file_name") },
		"missing count": func(row map[string]any) { delete(row, "passage_count") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var index *DocumentIndex
			index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
				row := documentCardFixture("doc_"+strings.Repeat("c", 32), "x.pdf", "PDF")
				mutate(row)
				return testResponse{Body: resultBody([]any{row})}
			}, true)
			if _, err := index.DocumentCards(t.Context(), documentTestIdentity, "x", 3); err == nil {
				t.Fatal("malformed card accepted")
			}
		})
	}
}

// The same document must not arrive twice: the two SEARCH_INDEX legs are OR'd in one
// statement, so a duplicate would silently double its weight in the ranking above.
func TestDocumentCardsRejectDuplicates(t *testing.T) {
	var index *DocumentIndex
	index, _ = testDocumentIndex(t, func(recordedRequest) testResponse {
		row := documentCardFixture("doc_"+strings.Repeat("d", 32), "due.xlsx", "spreadsheet")
		return testResponse{Body: resultBody([]any{row, row})}
	}, true)
	if _, err := index.DocumentCards(t.Context(), documentTestIdentity, "due", 3); err == nil {
		t.Fatal("duplicate card accepted")
	}
}

func TestDocumentCardsValidateTheRequestBeforeIO(t *testing.T) {
	for name, run := range map[string]func(*DocumentIndex) error{
		"empty query": func(i *DocumentIndex) error {
			_, err := i.DocumentCards(t.Context(), documentTestIdentity, "  ", 3)
			return err
		},
		"long query": func(i *DocumentIndex) error {
			_, err := i.DocumentCards(t.Context(), documentTestIdentity, strings.Repeat("x", 41), 3)
			return err
		},
		"limit above cap": func(i *DocumentIndex) error {
			_, err := i.DocumentCards(t.Context(), documentTestIdentity, "x", 5000)
			return err
		},
		"empty identity": func(i *DocumentIndex) error {
			_, err := i.DocumentCards(t.Context(), "", "x", 5)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
				t.Fatal("invalid request reached ArcadeDB")
				return testResponse{}
			}, true)
			if err := run(index); err == nil {
				t.Fatal("invalid request accepted")
			}
			if len(*requests) != 0 {
				t.Fatalf("%d request(s) issued", len(*requests))
			}
		})
	}
}

// An empty id list is how a caller spells "do not filter", so it must return an empty scope
// WITHOUT a query -- turning it into a lookup of nothing is what made the unscoped path
// return no documents at all.
func TestResolveDocumentScopeSkipsTheQueryWhenNothingIsNamed(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		t.Fatal("unscoped resolve issued a query")
		return testResponse{}
	}, true)
	scope, err := index.ResolveDocumentScope(t.Context(), documentTestIdentity, nil)
	if err != nil || len(scope) != 0 {
		t.Fatalf("scope = %#v, err = %v", scope, err)
	}
	if len(*requests) != 0 {
		t.Fatalf("%d request(s) issued", len(*requests))
	}
}

func TestResolveDocumentScopeReturnsOnlyWhatTheIdentityHas(t *testing.T) {
	known := "doc_" + strings.Repeat("e", 32)
	index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: resultBody([]any{map[string]any{"search_document_id": known}})}
	}, true)
	scope, err := index.ResolveDocumentScope(t.Context(), documentTestIdentity,
		[]string{known, "doc_" + strings.Repeat("f", 32), known})
	if err != nil {
		t.Fatalf("ResolveDocumentScope: %v", err)
	}
	if len(scope) != 1 || scope[0] != known {
		t.Fatalf("scope = %#v, want only the document this identity has", scope)
	}
}
