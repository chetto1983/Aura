package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeDescriber struct {
	identityID  string
	documentID  string
	description string
	calls       int
	err         error
}

func (f *fakeDescriber) SetDigest(_ context.Context, identityID, documentID, description string) error {
	f.identityID, f.documentID, f.description = identityID, documentID, description
	f.calls++
	return f.err
}

func TestDocumentDescribeWritesTheDescription(t *testing.T) {
	t.Parallel()
	store := &fakeDescriber{}
	tool := &DocumentDescribe{Documents: store}

	result, err := tool.Execute(toolTestContext(t), json.RawMessage(
		`{"document_id":"doc_9f2c","description":"Elenco clienti, 5889 righe. Colonne: ragione sociale, località, venditore."}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.documentID != "doc_9f2c" {
		t.Fatalf("document id = %q", store.documentID)
	}
	if !strings.Contains(store.description, "ragione sociale") {
		t.Fatalf("description = %q", store.description)
	}
	// The write is identity-scoped downstream, so the identity has to arrive.
	if store.identityID == "" {
		t.Fatal("the owning identity did not reach the store")
	}
	if !strings.Contains(result.Preview, `"described":true`) {
		t.Fatalf("result = %q", result.Preview)
	}
	if result.Provenance == nil || result.Provenance.Source != "document_describe" {
		t.Fatalf("provenance = %#v", result.Provenance)
	}
}

func TestDocumentDescribeRefusesToBlankADescription(t *testing.T) {
	t.Parallel()
	store := &fakeDescriber{}
	tool := &DocumentDescribe{Documents: store}
	for _, raw := range []string{
		`{"document_id":"doc_1"}`,
		`{"document_id":"doc_1","description":"   "}`,
	} {
		if _, err := tool.Execute(toolTestContext(t), json.RawMessage(raw)); err == nil {
			t.Errorf("args %s were accepted", raw)
		}
	}
	// Silently unfindable is the worst outcome here, so nothing may reach the store.
	if store.calls != 0 {
		t.Fatalf("the store was written %d time(s) with an empty description", store.calls)
	}
}

func TestDocumentDescribeValidatesInput(t *testing.T) {
	t.Parallel()
	tool := &DocumentDescribe{Documents: &fakeDescriber{}}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"description":"x"}`)); err == nil {
		t.Error("a missing document_id was accepted")
	}
	if _, err := tool.Execute(toolTestContext(t), json.RawMessage(`{"document_id":`)); err == nil {
		t.Error("malformed args were accepted")
	}
	if _, err := (&DocumentDescribe{}).Execute(toolTestContext(t),
		json.RawMessage(`{"document_id":"d","description":"x"}`)); err == nil {
		t.Error("a tool with no store reported success")
	}
	// A description is a label. Past the cap the model is summarizing the file,
	// which is what the file is for.
	long, err := json.Marshal(map[string]string{
		"document_id": "doc_1", "description": strings.Repeat("a", maxDescriptionChars+1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(toolTestContext(t), long); err == nil {
		t.Error("an over-long description was accepted")
	}
}

func TestDocumentDescribePropagatesStoreError(t *testing.T) {
	t.Parallel()
	tool := &DocumentDescribe{Documents: &fakeDescriber{err: errors.New("document is not catalogued")}}
	_, err := tool.Execute(toolTestContext(t),
		json.RawMessage(`{"document_id":"doc_x","description":"anything"}`))
	if err == nil || !strings.Contains(err.Error(), "not catalogued") {
		t.Fatalf("error = %v, want the store's reason preserved", err)
	}
}

func TestDocumentDescribeSpec(t *testing.T) {
	t.Parallel()
	spec := (&DocumentDescribe{}).Spec()
	if spec.Name != "document_describe" || !spec.Deferred || !spec.Mutating {
		t.Fatalf("spec = %q deferred=%v mutating=%v", spec.Name, spec.Deferred, spec.Mutating)
	}
	// It only pays off if the model reaches for it after opening a file, so the
	// description has to say exactly that.
	for _, want := range []string{"document_search", "open", "search by"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("description never mentions %q", want)
		}
	}
}

// The loop only closes if document_open tells the model to write back what it
// saw. Without this the library never learns and every question about a
// badly-named file starts from zero, forever.
func TestDocumentOpenPointsAtDocumentDescribe(t *testing.T) {
	t.Parallel()
	if !strings.Contains((&DocumentOpen{}).Spec().Description, "document_describe") {
		t.Error("document_open no longer tells the model to record what it found")
	}
}
