package api

import (
	"context"
	"net/http"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// CompactMemoryRebuilder triggers a full compact memory vector index rebuild,
// bypassing the warm-cache check. Implemented by *memoryindex.Store.
type CompactMemoryRebuilder interface {
	RebuildCompact(ctx context.Context) (memoryindex.CompactRebuildReport, error)
}

// compactReindexResponse is the JSON shape for POST /api/compact/reindex.
type compactReindexResponse struct {
	Collection      string `json:"collection"`
	PointsIndexed   int    `json:"points_indexed"`
	PagesIndexed    int    `json:"pages_indexed"`    // documents in SQLite compact_memory_documents
	ElapsedMS       int64  `json:"elapsed_ms"`
	VectorSize      int    `json:"vector_size"`
	PriorVectorSize int    `json:"prior_vector_size,omitempty"`
}

// handleCompactReindex serves POST /api/compact/reindex. Forces a full Qdrant
// compact collection rebuild from the SQLite compact_memory_documents table.
// Admin-gated (same as POST /api/wiki/reindex).
func handleCompactReindex(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !deps.SkillsAdmin {
			writeError(w, deps.Logger, http.StatusForbidden, "admin access required")
			return
		}
		if deps.CompactRebuilder == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable,
				"compact memory rebuild unavailable (QDRANT_URL / EMBEDDING_API_KEY missing?)")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		start := time.Now()
		report, err := deps.CompactRebuilder.RebuildCompact(ctx)
		elapsedMS := time.Since(start).Milliseconds()
		if err != nil {
			deps.Logger.Warn("api: compact reindex failed", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "compact reindex failed: "+err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, compactReindexResponse{
			Collection:      report.Collection,
			PointsIndexed:   report.PointsIndexed,
			PagesIndexed:    report.DocsInSQL,
			ElapsedMS:       elapsedMS,
			VectorSize:      report.VectorSize,
			PriorVectorSize: report.PriorVectorSize,
		})
	}
}
