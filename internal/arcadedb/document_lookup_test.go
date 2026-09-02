package arcadedb

import (
	"strings"
	"testing"
)

// missingLibraryResponse is what ArcadeDB answers before CocoIndex has created the type:
// the library is not there yet, which is a different fact from "the query failed".
func missingLibraryResponse() testResponse {
	return testResponse{Status: 500, Body: `{"exception":"com.arcadedb.exception.SchemaException",` +
		`"detail":"Type '` + IndexedDocumentType + `' not found"}`}
}

func TestDocumentByIDRefusesAnAmbiguousOrAbsentDocument(t *testing.T) {
	t.Parallel()

	t.Run("malformed identifiers never reach the database", func(t *testing.T) {
		t.Parallel()
		index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{})}
		})
		if _, err := index.DocumentByID(t.Context(), "  ", "doc-a"); err == nil {
			t.Fatal("DocumentByID accepted a blank identity")
		}
		if _, err := index.DocumentByID(t.Context(), documentTestIdentity, " "); err == nil {
			t.Fatal("DocumentByID accepted a blank document id")
		}
		if len(*requests) != 0 {
			t.Fatalf("a malformed lookup reached the database: %d requests", len(*requests))
		}
	})

	t.Run("an empty library reads as not found", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return missingLibraryResponse()
		})
		_, err := index.DocumentByID(t.Context(), documentTestIdentity, "doc-a")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("an absent library did not read as not found: %v", err)
		}
	})

	t.Run("zero rows is not found", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{})}
		})
		_, err := index.DocumentByID(t.Context(), documentTestIdentity, "doc-a")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("a missing document did not read as not found: %v", err)
		}
	})

	t.Run("two rows means the unique index failed, never pick one", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{
				documentCardFixture("doc-a", "first.xlsx", "first"),
				documentCardFixture("doc-a", "second.xlsx", "second"),
			})}
		})
		_, err := index.DocumentByID(t.Context(), documentTestIdentity, "doc-a")
		if err == nil || !strings.Contains(err.Error(), "not unique") {
			t.Fatalf("an ambiguous document was resolved anyway: %v", err)
		}
	})

	t.Run("a transport failure is reported as one", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Status: 503, Body: `{"exception":"Unavailable","detail":"down"}`}
		})
		_, err := index.DocumentByID(t.Context(), documentTestIdentity, "doc-a")
		if err == nil || !strings.Contains(err.Error(), "document by id") {
			t.Fatalf("a query failure was not reported as one: %v", err)
		}
	})

	t.Run("one row decodes into a card", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{documentCardFixture("doc-a", "Clienti.xlsx", "a card")})}
		})
		card, err := index.DocumentByID(t.Context(), documentTestIdentity, " doc-a ")
		if err != nil {
			t.Fatalf("DocumentByID: %v", err)
		}
		if card.SearchDocumentID != "doc-a" || card.FileName != "Clienti.xlsx" {
			t.Fatalf("card = %+v, want the requested document", card)
		}
	})
}

func TestDocumentNamesResolvesOnlyWhatItCan(t *testing.T) {
	t.Parallel()

	t.Run("no ids asks nothing", func(t *testing.T) {
		t.Parallel()
		index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{})}
		})
		names, err := index.DocumentNames(t.Context(), documentTestIdentity, []string{"", "   "})
		if err != nil || names != nil {
			t.Fatalf("DocumentNames(blank ids) = %v, %v; want no lookup", names, err)
		}
		if len(*requests) != 0 {
			t.Fatalf("a nameless lookup reached the database: %d requests", len(*requests))
		}
	})

	t.Run("more filters than the configured ceiling", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{})}
		})
		ids := []string{"doc-a", "doc-b", "doc-c", "doc-d"}
		if _, err := index.DocumentNames(t.Context(), documentTestIdentity, ids); err == nil {
			t.Fatal("DocumentNames accepted more filters than the ceiling allows")
		}
	})

	t.Run("a blank identity never reaches the database", func(t *testing.T) {
		t.Parallel()
		index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{})}
		})
		if _, err := index.DocumentNames(t.Context(), " ", []string{"doc-a"}); err == nil {
			t.Fatal("DocumentNames accepted a blank identity")
		}
		if len(*requests) != 0 {
			t.Fatalf("a malformed lookup reached the database: %d requests", len(*requests))
		}
	})

	t.Run("an empty library resolves no names rather than failing", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return missingLibraryResponse()
		})
		names, err := index.DocumentNames(t.Context(), documentTestIdentity, []string{"doc-a"})
		if err != nil || names != nil {
			t.Fatalf("DocumentNames(no library) = %v, %v; want no names and no error", names, err)
		}
	})

	t.Run("a transport failure is reported as one", func(t *testing.T) {
		t.Parallel()
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Status: 503, Body: `{"exception":"Unavailable","detail":"down"}`}
		})
		if _, err := index.DocumentNames(t.Context(), documentTestIdentity, []string{"doc-a"}); err == nil ||
			!strings.Contains(err.Error(), "document names") {
			t.Fatalf("a query failure was not reported as one: %v", err)
		}
	})

	t.Run("a row without its own id or name is refused", func(t *testing.T) {
		t.Parallel()
		for _, row := range []map[string]any{
			{"file_name": "Clienti.xlsx"},
			{"search_document_id": "doc-a"},
		} {
			index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
				return testResponse{Body: resultBody([]any{row})}
			})
			if _, err := index.DocumentNames(t.Context(), documentTestIdentity, []string{"doc-a"}); err == nil ||
				!strings.Contains(err.Error(), "document name") {
				t.Fatalf("an incomplete name row was accepted: %v", err)
			}
		}
	})

	t.Run("duplicate ids are asked for once", func(t *testing.T) {
		t.Parallel()
		index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: resultBody([]any{
				map[string]any{"search_document_id": "doc-a", "file_name": "Clienti.xlsx"},
			})}
		})
		names, err := index.DocumentNames(t.Context(), documentTestIdentity, []string{"doc-a", " doc-a "})
		if err != nil {
			t.Fatalf("DocumentNames: %v", err)
		}
		if names["doc-a"] != "Clienti.xlsx" {
			t.Fatalf("names = %v, want the resolved display name", names)
		}
		if len(*requests) != 1 {
			t.Fatalf("a duplicated id cost %d statements, want 1", len(*requests))
		}
	})
}
