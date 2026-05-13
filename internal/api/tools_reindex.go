package api

import (
	"context"
	"net/http"
	"time"

	"github.com/aura/aura/internal/toolindex"
)

// handleToolsReindex serves POST /api/tools/reindex. Synchronously runs
// the reconciler and returns its Report as JSON. Wave 2.10.b — primary
// use case is debugging tool-index drift, dashboard "refresh index"
// buttons, and CI smoke tests after a tool description edit.
//
// Auth: Bearer-gated by the surrounding RequireBearer wrapper in
// router.go. No additional admin gate — the operation is read-mostly
// (worst case it re-embeds 60 tools and writes 60 Qdrant points, all
// idempotent against the wanted state).
func handleToolsReindex(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.ToolReconciler == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable,
				"tool reconciler not configured (QDRANT_URL / EMBEDDING_API_KEY missing?)")
			return
		}
		// Cap the synchronous wait so a wedged Qdrant doesn't pin the
		// HTTP server forever; 30 s is generous for a 60-tool reindex.
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		report := deps.ToolReconciler.Reconcile(ctx, toolindex.ReasonManual)
		writeJSON(w, deps.Logger, http.StatusOK, report)
	}
}
