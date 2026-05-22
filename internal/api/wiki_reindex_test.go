package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aura/aura/internal/storage/search"
)

type fakeWikiIndexRebuilder struct {
	called  bool
	dryRun  bool
	report  search.QdrantRebuildReport
	err     error
}

func (f *fakeWikiIndexRebuilder) RebuildWikiIndex(_ context.Context, dryRun bool) (search.QdrantRebuildReport, error) {
	f.called = true
	f.dryRun = dryRun
	return f.report, f.err
}

func TestHandleWikiReindexAdminGate(t *testing.T) {
	handler := handleWikiReindex(Deps{
		SkillsAdmin:        false,
		WikiIndexRebuilder: &fakeWikiIndexRebuilder{},
	})
	r := httptest.NewRequest(http.MethodPost, "/wiki/reindex", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleWikiReindexNoRebuilder(t *testing.T) {
	handler := handleWikiReindex(Deps{
		SkillsAdmin:        true,
		WikiIndexRebuilder: nil,
	})
	r := httptest.NewRequest(http.MethodPost, "/wiki/reindex", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleWikiReindexCallsRebuilder(t *testing.T) {
	fake := &fakeWikiIndexRebuilder{
		report: search.QdrantRebuildReport{
			Collection:      "aura_memory_v1",
			DocsIndexed:     42,
			PagesIndexed:    20,
			VectorSize:      256,
			PriorVectorSize: 128,
		},
	}
	handler := handleWikiReindex(Deps{
		SkillsAdmin:        true,
		WikiIndexRebuilder: fake,
	})
	r := httptest.NewRequest(http.MethodPost, "/wiki/reindex", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !fake.called {
		t.Fatal("expected RebuildWikiIndex to be called")
	}
	if fake.dryRun {
		t.Fatal("expected dryRun=false for plain request")
	}
	var resp wikiReindexResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Collection != "aura_memory_v1" {
		t.Errorf("collection = %q, want aura_memory_v1", resp.Collection)
	}
	if resp.PointsIndexed != 42 {
		t.Errorf("points_indexed = %d, want 42", resp.PointsIndexed)
	}
	if resp.PagesIndexed != 20 {
		t.Errorf("pages_indexed = %d, want 20", resp.PagesIndexed)
	}
	if resp.VectorSize != 256 {
		t.Errorf("vector_size = %d, want 256", resp.VectorSize)
	}
	if resp.PriorVectorSize != 128 {
		t.Errorf("prior_vector_size = %d, want 128", resp.PriorVectorSize)
	}
	if resp.DryRun {
		t.Error("dry_run should be false")
	}
}

func TestHandleWikiReindexDryRun(t *testing.T) {
	fake := &fakeWikiIndexRebuilder{
		report: search.QdrantRebuildReport{
			Collection:      "aura_memory_v1",
			DocsIndexed:     10,
			PagesIndexed:    5,
			VectorSize:      256,
			PriorVectorSize: 256,
		},
	}
	handler := handleWikiReindex(Deps{
		SkillsAdmin:        true,
		WikiIndexRebuilder: fake,
	})
	r := httptest.NewRequest(http.MethodPost, "/wiki/reindex?dry_run=1", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !fake.dryRun {
		t.Fatal("expected dryRun=true for ?dry_run=1")
	}
	var resp wikiReindexResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.DryRun {
		t.Error("dry_run should be true in response")
	}
}
