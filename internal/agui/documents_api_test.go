package agui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/docfields"
)

const documentsOwner = "44444444-4444-4444-8444-444444444444"

// fakeDocumentsStore records what each route asked of the store, under which identity. Another
// identity's document is not in its library: any id in missing answers ErrNotFound.
type fakeDocumentsStore struct {
	identity  string
	calls     []string
	query     docfields.ListQuery
	exclude   []string
	ids       []string
	diff      docfields.Diff
	label     docfields.Label
	field     docfields.CustomField
	selection []string
	missing   []string
}

func (f *fakeDocumentsStore) note(identity, call string, ids ...string) error {
	f.identity = identity
	f.calls = append(f.calls, call)
	for _, id := range ids {
		if slices.Contains(f.missing, id) {
			return fmt.Errorf("%s: %w", call, docfields.ErrNotFound)
		}
	}
	return nil
}

func (f *fakeDocumentsStore) List(_ context.Context, id string, q docfields.ListQuery) (docfields.ListResult, error) {
	f.query = q
	return docfields.ListResult{Rows: []docfields.DocumentRow{{ID: "d1", Values: []docfields.Value{}}}, Total: 1,
		Facets: docfields.Facets{"type": {"t1": 1}}}, f.note(id, "list")
}

func (f *fakeDocumentsStore) Select(_ context.Context, id string, q docfields.ListQuery, exclude []string) ([]string, error) {
	f.query, f.exclude = q, exclude
	return f.selection, f.note(id, "select")
}

func (f *fakeDocumentsStore) Document(_ context.Context, id, documentID string) (docfields.DocumentDetail, error) {
	return docfields.DocumentDetail{ID: documentID}, f.note(id, "document", documentID)
}

func (f *fakeDocumentsStore) ApplyToDocuments(_ context.Context, id string, ids []string, d docfields.Diff) error {
	f.ids, f.diff = ids, d
	return f.note(id, "apply", ids...)
}

func (f *fakeDocumentsStore) ReviewDocuments(_ context.Context, id string, ids []string) error {
	f.ids = ids
	return f.note(id, "review", ids...)
}

func (f *fakeDocumentsStore) Labels(_ context.Context, id string, kind docfields.Kind) ([]docfields.Label, error) {
	return []docfields.Label{{ID: "l1", Kind: kind, Name: "Fattura"}}, f.note(id, "labels "+string(kind))
}

func (f *fakeDocumentsStore) SaveLabel(_ context.Context, id string, in docfields.Label) (docfields.Label, error) {
	f.label = in
	if in.ID == "" {
		in.ID = "l-new"
	}
	return in, f.note(id, "save label", in.ID)
}

func (f *fakeDocumentsStore) DeleteLabel(_ context.Context, id, labelID string) error {
	return f.note(id, "delete label "+labelID, labelID)
}

func (f *fakeDocumentsStore) RunRule(_ context.Context, id, labelID string) (docfields.PassResult, error) {
	return docfields.PassResult{Documents: 40, Suggested: 3}, f.note(id, "run "+labelID, labelID)
}

func (f *fakeDocumentsStore) CustomFields(_ context.Context, id string) ([]docfields.CustomField, error) {
	return []docfields.CustomField{{ID: "cf1", Name: "Stato", DataType: docfields.CustomText}}, f.note(id, "custom fields")
}

func (f *fakeDocumentsStore) SaveCustomField(_ context.Context, id string, in docfields.CustomField) (docfields.CustomField, error) {
	f.field = in
	if in.ID == "" {
		in.ID = "cf-new"
	}
	return in, f.note(id, "save custom field", in.ID)
}

func (f *fakeDocumentsStore) DeleteCustomField(_ context.Context, id, fieldID string) error {
	return f.note(id, "delete custom field "+fieldID, fieldID)
}

func documentsServer(store documentsStore) *Server {
	s := NewServer(nil, nil, ServerConfig{})
	if store != nil {
		s.SetDocumentFieldsStore(store)
	}
	return s
}

func documentsRequest(t *testing.T, s *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := policyRequest(t, s, method, path, body, documentsOwner)
	return rec.Code, decodeBoard[map[string]any](t, rec)
}

func TestDocumentsRoutesAnswer503Unwired(t *testing.T) {
	s := documentsServer(nil)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/documents", ""},
		{http.MethodGet, "/api/documents/d1", ""},
		{http.MethodPatch, "/api/documents/d1", `{}`},
		{http.MethodPost, "/api/documents/d1/review", ""},
		{http.MethodPost, "/api/documents/bulk", `{}`},
		{http.MethodGet, "/api/documents/labels?kind=type", ""},
		{http.MethodPost, "/api/documents/labels", `{}`},
		{http.MethodPut, "/api/documents/labels/l1", `{}`},
		{http.MethodDelete, "/api/documents/labels/l1", ""},
		{http.MethodGet, "/api/documents/custom-fields", ""},
		{http.MethodPost, "/api/documents/custom-fields", `{}`},
		{http.MethodPut, "/api/documents/custom-fields/cf1", `{}`},
		{http.MethodDelete, "/api/documents/custom-fields/cf1", ""},
	} {
		if code, body := documentsRequest(t, s, c.method, c.path, c.body); code != http.StatusServiceUnavailable || body["error"] != "documents_unavailable" {
			t.Errorf("%s %s = %d %v, want 503 documents_unavailable", c.method, c.path, code, body)
		}
	}
}

// The library is the principal's own: no route takes an identity, and the filters arrive as the
// list's query parameters.
func TestDocumentsListReadsTheFilterAndThePrincipal(t *testing.T) {
	store := &fakeDocumentsStore{}
	code, body := documentsRequest(t, documentsServer(store), http.MethodGet,
		"/api/documents?type=t1&type=t2&tags=g1&tags_mode=all&date_from=2026-01-01&include_unreviewed=false"+
			"&needs_review=true&sort=-indexed_at&offset=50&limit=25&identity=someone-else", "")
	if code != http.StatusOK || body["total"] != float64(1) {
		t.Fatalf("list = %d %v", code, body)
	}
	q := store.query
	if store.identity != documentsOwner || !slices.Equal(q.Fields.Types, []string{"t1", "t2"}) || !slices.Equal(q.Fields.Tags, []string{"g1"}) ||
		!q.Fields.TagsAll || q.Fields.DateFrom != "2026-01-01" || q.Fields.IncludeUnreviewed || !q.NeedsReview ||
		q.Sort != "-indexed_at" || q.Offset != 50 || q.Limit != 25 {
		t.Fatalf("identity %s, query %+v", store.identity, q)
	}
	if code, _ := documentsRequest(t, documentsServer(&fakeDocumentsStore{}), http.MethodGet, "/api/documents?limit=many", ""); code != http.StatusBadRequest {
		t.Fatalf("a limit that is no number = %d, want 400", code)
	}
	unfiltered := &fakeDocumentsStore{}
	documentsRequest(t, documentsServer(unfiltered), http.MethodGet, "/api/documents", "")
	if !unfiltered.query.Fields.IncludeUnreviewed {
		t.Fatal("include_unreviewed did not default to true")
	}
}

func TestDocumentsPatchAppliesTheDiffToThatDocument(t *testing.T) {
	store := &fakeDocumentsStore{}
	code, _ := documentsRequest(t, documentsServer(store), http.MethodPatch, "/api/documents/d1",
		`{"set":[{"field":"type","value_key":"t1"}],"remove":[{"field":"tag","value_key":"g1"}]}`)
	if code != http.StatusOK || !slices.Equal(store.ids, []string{"d1"}) || len(store.diff.Set) != 1 ||
		store.diff.Set[0].ValueKey != "t1" || store.diff.Remove[0].ValueKey != "g1" {
		t.Fatalf("patch = %d, ids %v, diff %+v", code, store.ids, store.diff)
	}
	if code, _ := documentsRequest(t, documentsServer(store), http.MethodPatch, "/api/documents/d1", `{"set":[],"owner":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("an unknown field = %d, want 400", code)
	}
	if code, _ := documentsRequest(t, documentsServer(store), http.MethodPost, "/api/documents/d1/review", ""); code != http.StatusOK || store.calls[len(store.calls)-1] != "review" {
		t.Fatalf("review = %d, calls %v", code, store.calls)
	}
}

// Another identity's document is not found, never forbidden: the answer must not tell the two
// apart.
func TestDocumentsAnotherIdentitysDocumentIs404(t *testing.T) {
	store := &fakeDocumentsStore{missing: []string{"theirs"}}
	s := documentsServer(store)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/documents/theirs", ""},
		{http.MethodPatch, "/api/documents/theirs", `{"set":[{"field":"tag","value_key":"g"}]}`},
		{http.MethodPost, "/api/documents/theirs/review", ""},
		{http.MethodPost, "/api/documents/bulk", `{"selection":{"ids":["mine","theirs"]},"diff":{"set":[{"field":"tag","value_key":"g"}]}}`},
	} {
		if code, body := documentsRequest(t, s, c.method, c.path, c.body); code != http.StatusNotFound || body["error"] != "not_found" {
			t.Errorf("%s %s = %d %v, want 404 not_found", c.method, c.path, code, body)
		}
	}
}

// "Select all in the filter", then Apply after the result changed: the store's count at apply
// time differs from the one the operator confirmed, so nothing is written and the new count comes
// back (Review Focus 2).
func TestDocumentsBulkRefusesAChangedCount(t *testing.T) {
	store := &fakeDocumentsStore{selection: []string{"d1", "d2", "d3", "d4"}}
	s := documentsServer(store)
	bulk := `{"selection":{"filter":{"type":["t1"],"include_unreviewed":false},"exclude":["d9"],"expected_count":%d},` +
		`"diff":{"set":[{"field":"tag","value_key":"g1"}]}}`
	code, body := documentsRequest(t, s, http.MethodPost, "/api/documents/bulk", fmt.Sprintf(bulk, 3))
	if code != http.StatusConflict || body["error"] != "count_changed" || body["count"] != float64(4) || slices.Contains(store.calls, "apply") {
		t.Fatalf("stale bulk = %d %v, calls %v", code, body, store.calls)
	}
	if !slices.Equal(store.query.Fields.Types, []string{"t1"}) || store.query.Fields.IncludeUnreviewed || !slices.Equal(store.exclude, []string{"d9"}) {
		t.Fatalf("selection query %+v, exclude %v", store.query, store.exclude)
	}
	code, body = documentsRequest(t, s, http.MethodPost, "/api/documents/bulk", fmt.Sprintf(bulk, 4))
	if code != http.StatusOK || body["applied"] != float64(4) || !slices.Equal(store.ids, store.selection) {
		t.Fatalf("bulk = %d %v, applied to %v", code, body, store.ids)
	}
	for _, raw := range []string{
		`{"selection":{"ids":["d1"],"filter":{}},"diff":{}}`,
		`{"selection":{},"diff":{}}`,
		`{"selection":{"filter":{}},"diff":{}}`,
	} {
		if code, _ := documentsRequest(t, s, http.MethodPost, "/api/documents/bulk", raw); code != http.StatusBadRequest {
			t.Errorf("bulk %s = %d, want 400", raw, code)
		}
	}
}

func TestDocumentsLabelsAndCustomFields(t *testing.T) {
	store := &fakeDocumentsStore{}
	s := documentsServer(store)
	if code, _ := documentsRequest(t, s, http.MethodGet, "/api/documents/labels?kind=counterparty", ""); code != http.StatusOK ||
		store.calls[0] != "labels counterparty" {
		t.Fatalf("list labels = %d, calls %v", code, store.calls)
	}
	code, body := documentsRequest(t, s, http.MethodPost, "/api/documents/labels",
		`{"kind":"type","name":"Fattura","match_algorithm":"any","match_pattern":"fattura","case_insensitive":true}`)
	if code != http.StatusOK || store.label.Pattern != "fattura" || body["suggested"] != float64(3) || !slices.Contains(store.calls, "run l-new") {
		t.Fatalf("save label with a rule = %d %v, calls %v", code, body, store.calls)
	}
	if code, _ := documentsRequest(t, s, http.MethodPut, "/api/documents/labels/l7", `{"id":"l3","kind":"tag","name":"x"}`); code != http.StatusOK || store.label.ID != "l7" {
		t.Fatalf("update label = %d, saved %+v", code, store.label)
	}
	before := len(store.calls)
	documentsRequest(t, s, http.MethodPut, "/api/documents/labels/l8", `{"kind":"tag","name":"senza regola"}`)
	if strings.HasPrefix(store.calls[len(store.calls)-1], "run") || len(store.calls) != before+1 {
		t.Fatalf("a label without a rule ran one: %v", store.calls[before:])
	}
	if code, _ := documentsRequest(t, s, http.MethodDelete, "/api/documents/labels/l7", ""); code != http.StatusOK {
		t.Fatalf("delete label = %d", code)
	}
	if code, _ := documentsRequest(t, s, http.MethodPost, "/api/documents/custom-fields", `{"name":"Stato","data_type":"select","options":["pagata"]}`); code != http.StatusOK ||
		store.field.Name != "Stato" || store.field.Options[0] != "pagata" {
		t.Fatalf("save custom field = %d, saved %+v", code, store.field)
	}
	if code, _ := documentsRequest(t, s, http.MethodPut, "/api/documents/custom-fields/cf9", `{"name":"Numero","data_type":"text"}`); code != http.StatusOK || store.field.ID != "cf9" {
		t.Fatalf("update custom field = %d, saved %+v", code, store.field)
	}
	if code, _ := documentsRequest(t, s, http.MethodDelete, "/api/documents/custom-fields/cf9", ""); code != http.StatusOK {
		t.Fatalf("delete custom field = %d", code)
	}
	if code, body := documentsRequest(t, s, http.MethodGet, "/api/documents/custom-fields", ""); code != http.StatusOK || body["fields"] == nil {
		t.Fatalf("list custom fields = %d %v", code, body)
	}
}

func TestDocumentsStoreErrorsAnswerByKind(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
		name string
	}{
		{docfields.ErrNotFound, http.StatusNotFound, "not_found"},
		{fmt.Errorf("wrapped: %w", docfields.ErrInvalid), http.StatusBadRequest, "invalid"},
		{errors.New("arcadedb down"), http.StatusInternalServerError, "internal"},
	} {
		rec := httptest.NewRecorder()
		writeDocumentsError(rec, c.err)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), `"error":"`+c.name+`"`) {
			t.Errorf("%v = %d %s", c.err, rec.Code, rec.Body.String())
		}
	}
}

// Exclude, "Non assegnata" and the search box ride the same filter, in the list's query and in a
// bulk selection's body alike.
func TestDocumentsFilterCarriesExclusionsAndSearch(t *testing.T) {
	store := &fakeDocumentsStore{}
	s := documentsServer(store)
	documentsRequest(t, s, http.MethodGet, "/api/documents?type_not=t9&counterparty_not=c9&tags_not=g9&unassigned=tags&unassigned=type&q=fattura", "")
	q := store.query
	if !slices.Equal(q.Exclude.Types, []string{"t9"}) || !slices.Equal(q.Exclude.Counterparties, []string{"c9"}) ||
		!slices.Equal(q.Exclude.Tags, []string{"g9"}) || !slices.Equal(q.Unassigned, []string{"tags", "type"}) || q.Search != "fattura" {
		t.Fatalf("query = %+v", q)
	}
	store.selection = []string{"d1"}
	documentsRequest(t, s, http.MethodPost, "/api/documents/bulk",
		`{"selection":{"filter":{"tags_not":["g9"],"unassigned":["type"],"q":"nota"},"expected_count":1},"diff":{}}`)
	if q := store.query; !slices.Equal(q.Exclude.Tags, []string{"g9"}) || !slices.Equal(q.Unassigned, []string{"type"}) || q.Search != "nota" {
		t.Fatalf("bulk query = %+v", q)
	}
}
