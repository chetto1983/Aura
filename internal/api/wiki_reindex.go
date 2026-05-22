package api

import (
	"context"
	"net/http"
	"time"

	"github.com/aura/aura/internal/storage/search"
)

// wikiReindexResponse is the JSON shape for POST /api/wiki/reindex.
// Field names match the acceptance criteria (US-WIKI-FIX-04).
type wikiReindexResponse struct {
	Collection      string             `json:"collection"`
	PointsIndexed   int                `json:"points_indexed"`
	PagesIndexed    int                `json:"pages_indexed"`
	Skipped         []search.SkippedDoc `json:"skipped,omitempty"`
	ElapsedMS       int64              `json:"elapsed_ms"`
	VectorSize      int                `json:"vector_size"`
	PriorVectorSize int                `json:"prior_vector_size,omitempty"`
	DryRun          bool               `json:"dry_run"`
}

// handleWikiReindex serves POST /api/wiki/reindex. Triggers a full Qdrant
// collection rebuild and returns the report as JSON.
//
// Auth: admin-gated (deps.SkillsAdmin), like skill install/delete.
// Optional ?dry_run=1 returns current state without modifying anything.
func handleWikiReindex(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !deps.SkillsAdmin {
			writeError(w, deps.Logger, http.StatusForbidden, "admin access required")
			return
		}
		if deps.WikiIndexRebuilder == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable,
				"wiki index rebuild unavailable (QDRANT_URL / EMBEDDING_API_KEY missing?)")
			return
		}
		dryRun := r.URL.Query().Get("dry_run") == "1"
		// Allow up to 10 minutes for a full rebuild of a large wiki.
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		start := time.Now()
		report, err := deps.WikiIndexRebuilder.RebuildWikiIndex(ctx, dryRun)
		elapsedMS := time.Since(start).Milliseconds()
		if err != nil {
			deps.Logger.Warn("api: wiki reindex failed", "dry_run", dryRun, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "wiki reindex failed: "+err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, wikiReindexResponse{
			Collection:      report.Collection,
			PointsIndexed:   report.DocsIndexed,
			PagesIndexed:    report.PagesIndexed,
			Skipped:         report.SkippedDocs,
			ElapsedMS:       elapsedMS,
			VectorSize:      report.VectorSize,
			PriorVectorSize: report.PriorVectorSize,
			DryRun:          dryRun,
		})
	}
}
