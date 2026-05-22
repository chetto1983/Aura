package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aura/aura/internal/storage/memoryindex"
)

type fakeCompactRebuilder struct {
	called bool
	report memoryindex.CompactRebuildReport
	err    error
}

func (f *fakeCompactRebuilder) RebuildCompact(_ context.Context) (memoryindex.CompactRebuildReport, error) {
	f.called = true
	return f.report, f.err
}

func TestHandleCompactReindexAdminGate(t *testing.T) {
	handler := handleCompactReindex(Deps{
		SkillsAdmin:      false,
		CompactRebuilder: &fakeCompactRebuilder{},
	})
	r := httptest.NewRequest(http.MethodPost, "/compact/reindex", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleCompactReindexNoRebuilder(t *testing.T) {
	handler := handleCompactReindex(Deps{
		SkillsAdmin:      true,
		CompactRebuilder: nil,
	})
	r := httptest.NewRequest(http.MethodPost, "/compact/reindex", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleCompactReindexCallsRebuilder(t *testing.T) {
	fake := &fakeCompactRebuilder{
		report: memoryindex.CompactRebuildReport{
			Collection:      "aura_memory_v1_compact",
			PointsIndexed:   15,
			DocsInSQL:       20,
			VectorSize:      256,
			PriorVectorSize: 128,
		},
	}
	handler := handleCompactReindex(Deps{
		SkillsAdmin:      true,
		CompactRebuilder: fake,
	})
	r := httptest.NewRequest(http.MethodPost, "/compact/reindex", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !fake.called {
		t.Fatal("expected RebuildCompact to be called")
	}
	var resp compactReindexResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Collection != "aura_memory_v1_compact" {
		t.Errorf("collection = %q, want aura_memory_v1_compact", resp.Collection)
	}
	if resp.PointsIndexed != 15 {
		t.Errorf("points_indexed = %d, want 15", resp.PointsIndexed)
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
}

func TestHandleCompactReindexRebuilderError(t *testing.T) {
	fake := &fakeCompactRebuilder{err: errors.New("qdrant unavailable")}
	handler := handleCompactReindex(Deps{
		SkillsAdmin:      true,
		CompactRebuilder: fake,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	r := httptest.NewRequest(http.MethodPost, "/compact/reindex", nil)
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}
