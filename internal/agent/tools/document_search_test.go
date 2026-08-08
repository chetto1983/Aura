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
	request  documents.RetrievalRequest
	response documents.RetrievalResponse
	err      error
}

func (f *fakeLibrary) Retrieve(
	_ context.Context,
	request documents.RetrievalRequest,
) (documents.RetrievalResponse, error) {
	f.request = request
	return f.response, f.err
}

func searchPayload(t *testing.T, result ToolResult) documents.RetrievalResponse {
	t.Helper()
	var payload documents.RetrievalResponse
	if err := json.Unmarshal([]byte(result.Preview), &payload); err != nil {
		t.Fatalf("result is not JSON (%q): %v", result.Preview, err)
	}
	return payload
}

func TestDocumentSearchReturnsProvenanceBearingPassages(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{response: documents.RetrievalResponse{
		Profile: documents.ProductionRetrievalProfile,
		Status:  documents.RetrievalComplete,
		Documents: []documents.RetrievalDocument{{
			DocumentID: "doc_9f2c", Title: "Clienti.xlsx",
			SourceKind: "s3", SourceKey: "contabilita/Clienti.xlsx",
			OriginalSHA256: strings.Repeat("a", 64),
			Passages: []documents.RetrievalPassage{{
				Text:          "Il codice cliente di WPT SRL è C-1042.",
				CitationToken: "document:doc_9f2c@2#sheet=Clienti;cell=A42",
			}},
		}},
	}}
	tool := &DocumentSearch{Library: library}

	result, err := tool.Execute(toolTestContext(t), json.RawMessage(
		`{"query":"codice cliente WPT","document_ids":["doc_9f2c"]}`,
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := searchPayload(t, result)
	if len(payload.Documents) != 1 || len(payload.Documents[0].Passages) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Documents[0].Passages[0].CitationToken == "" ||
		payload.Documents[0].OriginalSHA256 == "" {
		t.Fatalf("provenance missing: %#v", payload.Documents[0])
	}
	if library.request.Query != "codice cliente WPT" || library.request.Limit != 8 ||
		len(library.request.DocumentIDs) != 1 {
		t.Fatalf("library request = %#v", library.request)
	}
	if library.request.IdentityID == "" {
		t.Fatal("the owning identity did not reach the library")
	}
	if result.Provenance == nil || result.Provenance.Source != "document_search" ||
		result.Provenance.Trust != TrustUntrusted {
		t.Fatalf("provenance = %#v", result.Provenance)
	}
}

func TestDocumentSearchRejectsBlankMalformedAndUnknownArgs(t *testing.T) {
	t.Parallel()
	tool := &DocumentSearch{Library: &fakeLibrary{}}
	for _, raw := range []string{`{}`, `{"query":"   "}`, ``, `{"document_id":"doc_1"}`} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err == nil {
			t.Fatalf("args %q: expected fail-closed rejection", raw)
		}
	}
}

func TestDocumentSearchLimit(t *testing.T) {
	t.Parallel()
	library := &fakeLibrary{}
	tool := &DocumentSearch{Library: library}
	for _, tc := range []struct{ in, want int }{{0, 8}, {3, 3}, {500, 50}} {
		raw, err := json.Marshal(map[string]any{"query": "fatture clienti", "limit": tc.in})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tool.Execute(toolTestContext(t), raw); err != nil {
			t.Fatalf("limit %d: %v", tc.in, err)
		}
		if library.request.Limit != tc.want {
			t.Errorf("limit %d -> %d, want %d", tc.in, library.request.Limit, tc.want)
		}
	}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"query":"fatture","limit":-1}`)); err == nil {
		t.Error("a negative limit was accepted")
	}
}

func TestDocumentSearchUnconfiguredAndBackendFailure(t *testing.T) {
	t.Parallel()
	if _, err := (&DocumentSearch{}).Execute(toolTestContext(t), json.RawMessage(`{}`)); err == nil {
		t.Error("a tool with no library reported success")
	}
	failing := &DocumentSearch{Library: &fakeLibrary{err: errors.New("pool is closed")}}
	_, err := failing.Execute(toolTestContext(t), json.RawMessage(`{"query":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "pool is closed") {
		t.Errorf("error = %v, want the backend reason preserved", err)
	}
}

func TestDocumentSearchSpecNamesCitationAndOpenContract(t *testing.T) {
	t.Parallel()
	spec := (&DocumentSearch{}).Spec()
	if spec.Name != "document_search" || spec.Deferred {
		t.Fatalf("spec = %#v", spec)
	}
	for _, want := range []string{"document_open", "/workspace", "how many", "citation_token", "requires_open"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("description never mentions %q", want)
		}
	}
}

func toolTestContext(t *testing.T) context.Context {
	t.Helper()
	return WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
}

func TestDocumentSearchThreadsOwnerIdentity(t *testing.T) {
	t.Run("empty principal resolves to local UUID", func(t *testing.T) {
		library := &fakeLibrary{}
		if _, err := (&DocumentSearch{Library: library}).Execute(
			toolTestContext(t), json.RawMessage(`{"query":"hello"}`),
		); err != nil {
			t.Fatal(err)
		}
		if library.request.IdentityID != localOwnerID {
			t.Fatalf("identity = %q, want %q", library.request.IdentityID, localOwnerID)
		}
	})
	t.Run("web principal threaded verbatim", func(t *testing.T) {
		const principal = "00000000-0000-0000-0000-0000000000ab"
		library := &fakeLibrary{}
		ctx := identityctx.WithIdentityID(toolTestContext(t), principal)
		if _, err := (&DocumentSearch{Library: library}).Execute(
			ctx, json.RawMessage(`{"query":"hello"}`),
		); err != nil {
			t.Fatal(err)
		}
		if library.request.IdentityID != principal {
			t.Fatalf("identity = %q, want %q", library.request.IdentityID, principal)
		}
	})
}
