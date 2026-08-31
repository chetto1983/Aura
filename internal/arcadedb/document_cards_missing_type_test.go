package arcadedb

import (
	"strings"
	"testing"
)

// missingTypeBody is the VERBATIM payload ArcadeDB 26.8.1 returns when a query names a
// vertex type the database does not have. Captured live on 2026-08-31 from
// POST /api/v1/query/<db> after the memory graph was emptied, which is exactly the state a
// tenant is in before its first ingestion run applies services/ingest's DDL.
const missingTypeBody = `{"error":"Error on transaction commit",` +
	`"detail":"Type with name 'IndexedDocument' was not found",` +
	`"exception":"com.arcadedb.exception.SchemaException"}`

func missingTypeIndex(t *testing.T) *DocumentIndex {
	t.Helper()
	index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Status: 500, Body: missingTypeBody}
	})
	return index
}

// An un-ingested library is an EMPTY library, not a broken tool. Before this, every read
// below handed the caller ArcadeDB's 500 verbatim; document_search passed it to the model,
// which read a tool fault where the truth was "nothing is indexed yet".
func TestDocumentReadsTreatAMissingTypeAsAnEmptyLibrary(t *testing.T) {
	t.Parallel()

	t.Run("cards", func(t *testing.T) {
		cards, err := missingTypeIndex(t).DocumentCardsScoped(
			t.Context(), CandidateFilter{IdentityID: documentTestIdentity, Limit: 2}, "anything")
		if err != nil {
			t.Fatalf("DocumentCardsScoped returned an error for an empty library: %v", err)
		}
		if len(cards) != 0 {
			t.Fatalf("cards = %d, want 0", len(cards))
		}
	})

	t.Run("scope", func(t *testing.T) {
		scope, err := missingTypeIndex(t).ResolveDocumentScope(
			t.Context(), documentTestIdentity, []string{"doc_abc"})
		if err != nil {
			t.Fatalf("ResolveDocumentScope returned an error for an empty library: %v", err)
		}
		if len(scope) != 0 {
			t.Fatalf("scope = %v, want empty", scope)
		}
	})

	t.Run("names", func(t *testing.T) {
		names, err := missingTypeIndex(t).DocumentNames(
			t.Context(), documentTestIdentity, []string{"doc_abc"})
		if err != nil {
			t.Fatalf("DocumentNames returned an error for an empty library: %v", err)
		}
		if len(names) != 0 {
			t.Fatalf("names = %v, want empty", names)
		}
	})

	// DocumentByID is the one that still fails, because its caller asked for ONE specific
	// document: an empty library is simply one way for that document to be absent, so it
	// gets the same "not found" the zero-row case already produced -- never a 500.
	t.Run("by id is not found, not a server error", func(t *testing.T) {
		_, err := missingTypeIndex(t).DocumentByID(t.Context(), documentTestIdentity, "doc_abc")
		if err == nil {
			t.Fatal("DocumentByID must still report the document as absent")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("err = %v, want a not-found error", err)
		}
		if strings.Contains(err.Error(), "http 500") {
			t.Fatalf("err = %v, want the 500 translated away", err)
		}
	})
}

// The narrowness is the point: swallowing every SchemaException would hide a genuine schema
// fault behind an empty result, which is the failure mode this whole change exists to stop.
func TestMissingIndexedDocumentTypeIsNarrow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		body string
		want bool
	}{
		{"the real payload", missingTypeBody, true},
		{
			"another type entirely",
			`{"detail":"Type with name 'Passage' was not found","exception":"com.arcadedb.exception.SchemaException"}`,
			false,
		},
		{
			"a different exception naming the type",
			`{"detail":"Type with name 'IndexedDocument' was not found","exception":"com.arcadedb.exception.TimeoutException"}`,
			false,
		},
		{"a plain parse error", `{"detail":"syntax error","exception":"com.arcadedb.exception.CommandSQLParsingException"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
				return testResponse{Status: 500, Body: test.body}
			})
			_, err := index.ResolveDocumentScope(t.Context(), documentTestIdentity, []string{"doc_abc"})
			if got := err == nil; got != test.want {
				t.Fatalf("swallowed = %v, want %v (err = %v)", got, test.want, err)
			}
		})
	}
}
