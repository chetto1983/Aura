package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
)

type fakeLibrary struct {
	identityID string
	query      string
	limit      int
	hits       []documents.DigestHit
	err        error
}

func (f *fakeLibrary) SearchDigests(
	_ context.Context,
	identityID, query string,
	limit int,
) ([]documents.DigestHit, error) {
	f.identityID, f.query, f.limit = identityID, query, limit
	return f.hits, f.err
}

func searchPayload(t *testing.T, result ToolResult) []map[string]any {
	t.Helper()
	var payload struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.Unmarshal([]byte(result.Preview), &payload); err != nil {
		t.Fatalf("result is not JSON (%q): %v", result.Preview, err)
	}
	return payload.Documents
}

func TestDocumentSearchReturnsDocumentsNotPassages(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{hits: []documents.DigestHit{{
		DocumentID: "doc_9f2c", Title: "Clienti.xlsx",
		Tags:   []string{"clienti", "2026"},
		Digest: "Clienti.xlsx — Tabular data. Clienti (5889 rows): Ragione sociale, Località",
		Rank:   0.42,
	}}}
	tool := &DocumentSearch{Library: library}

	result, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"elenco clienti"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	docs := searchPayload(t, result)
	if len(docs) != 1 {
		t.Fatalf("got %d documents, want 1", len(docs))
	}
	// The id is the whole point: it is what document_open takes.
	if docs[0]["document_id"] != "doc_9f2c" {
		t.Fatalf("document_id = %v", docs[0]["document_id"])
	}
	// Operator-written tags travel: they are the part of the description a PERSON
	// wrote, and the cockpit lets them edit it.
	tags, _ := docs[0]["tags"].([]any)
	if len(tags) != 2 || tags[0] != "clienti" {
		t.Fatalf("tags = %v", docs[0]["tags"])
	}
	if library.query != "elenco clienti" || library.limit != 8 {
		t.Fatalf("library called with query=%q limit=%d", library.query, library.limit)
	}
	if library.identityID == "" {
		t.Fatal("the owning identity did not reach the library")
	}
	if result.Provenance == nil || result.Provenance.Source != "document_search" {
		t.Fatalf("provenance = %#v", result.Provenance)
	}
}

func TestDocumentSearchBlankQueryListsTheLibrary(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{hits: []documents.DigestHit{{DocumentID: "doc_1", Title: "a.pdf"}}}
	tool := &DocumentSearch{Library: library}

	// "the file I just uploaded" carries no search terms, and must still list.
	for _, raw := range []string{`{}`, `{"query":"   "}`, ``} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err != nil {
			t.Fatalf("args %q: %v", raw, err)
		}
		if library.query != "" {
			t.Fatalf("query = %q, want empty", library.query)
		}
	}
}

func TestDocumentSearchLimit(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	tool := &DocumentSearch{Library: library}

	for _, tc := range []struct{ in, want int }{{0, 8}, {3, 3}, {500, 50}} {
		raw, err := json.Marshal(map[string]int{"limit": tc.in})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tool.Execute(toolTestContext(t), raw); err != nil {
			t.Fatalf("limit %d: %v", tc.in, err)
		}
		if library.limit != tc.want {
			t.Errorf("limit %d -> %d, want %d", tc.in, library.limit, tc.want)
		}
	}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"limit":-1}`)); err == nil {
		t.Error("a negative limit was accepted")
	}
}

func TestDocumentSearchUnconfiguredAndMalformed(t *testing.T) {
	t.Parallel()
	if _, err := (&DocumentSearch{}).Execute(toolTestContext(t), json.RawMessage(`{}`)); err == nil {
		t.Error("a tool with no library reported success")
	}
	tool := &DocumentSearch{Library: &fakeLibrary{}}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":`)); err == nil {
		t.Error("malformed args were accepted")
	}
	failing := &DocumentSearch{Library: &fakeLibrary{err: errors.New("pool is closed")}}
	_, err := failing.Execute(toolTestContext(t), json.RawMessage(`{"query":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "pool is closed") {
		t.Errorf("error = %v, want the backend reason preserved", err)
	}
}

// The handoff is the whole design: search names the file, open hands it over.
func TestDocumentSearchSpecPointsAtDocumentOpen(t *testing.T) {
	t.Parallel()
	spec := (&DocumentSearch{}).Spec()
	if spec.Name != "document_search" || !spec.Deferred {
		t.Fatalf("spec = %q deferred=%v", spec.Name, spec.Deferred)
	}
	for _, want := range []string{"document_open", "/workspace", "how many", "NOT their text"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("description never mentions %q", want)
		}
	}
}

func toolTestContext(t *testing.T) context.Context {
	t.Helper()
	return WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
}

// TestDocumentSearchThreadsOwnerIdentity locks ME-01, carried over from the
// retrieval-shaped tool: an empty CLI/no-principal ctx maps to the seeded `local`
// UUID (…001) via ownerFromContext — so the operator's own local-owned documents
// stay reachable with AURA_MUSR_ISOLATION on — and an authenticated web principal
// is threaded verbatim. The library is identity-scoped in SQL, so getting this
// wrong now shows another identity's files rather than none.
func TestDocumentSearchThreadsOwnerIdentity(t *testing.T) {
	t.Run("empty principal resolves to local UUID", func(t *testing.T) {
		library := &fakeLibrary{}
		tool := &DocumentSearch{Library: library}
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"hello"}`)); err != nil {
			t.Fatal(err)
		}
		if library.identityID != localOwnerID {
			t.Fatalf("identity = %q, want the local UUID %q", library.identityID, localOwnerID)
		}
	})
	t.Run("web principal threaded verbatim", func(t *testing.T) {
		const principal = "00000000-0000-0000-0000-0000000000ab"
		library := &fakeLibrary{}
		tool := &DocumentSearch{Library: library}
		ctx := identityctx.WithIdentityID(toolTestContext(t), principal)
		if _, err := tool.Execute(ctx, json.RawMessage(`{"query":"hello"}`)); err != nil {
			t.Fatal(err)
		}
		if library.identityID != principal {
			t.Fatalf("identity = %q, want %q", library.identityID, principal)
		}
	})
}
