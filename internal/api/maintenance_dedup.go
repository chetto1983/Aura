package api

import (
	"context"
	"net/http"

	"github.com/aura/aura/internal/wiki"
)

// entityDeduplicator is the optional capability satisfied by *wiki.Store.
// The interface is local to api so a stripped wiki fake in tests can satisfy
// wiki.Repository without also implementing the dedup pipeline.
type entityDeduplicator interface {
	DeduplicateEntities(ctx context.Context, opts wiki.DedupOptions) (*wiki.DedupReport, error)
}

// handleMaintenanceDedup triggers wiki.Store.DeduplicateEntities on demand
// from the dashboard. Accepts ?dry_run=1 to preview planned merges without
// writing; omitting it performs the full merge pipeline.
//
// MAINTENANCE WINDOW ONLY: DeduplicateEntities acquires a store-level lock
// that prevents concurrent dedup runs but does NOT block concurrent WritePage
// calls. Running dedup while the LLM agent is actively ingesting sources may
// produce inconsistent alias merges or dangling links. Schedule this endpoint
// during maintenance windows only — for example, the same window the nightly
// cron maintenance job runs at 03:00.
//
// Returns 503 when the wiki store does not implement the deduplication
// pipeline (stripped fakes or Qdrant not configured).
func handleMaintenanceDedup(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deduper, ok := deps.Wiki.(entityDeduplicator)
		if !ok || deduper == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "entity dedup unavailable")
			return
		}
		dryRun := r.URL.Query().Get("dry_run") == "1"
		report, err := deduper.DeduplicateEntities(r.Context(), wiki.DedupOptions{DryRun: dryRun})
		if err != nil {
			deps.Logger.Warn("api: entity dedup failed", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "entity dedup failed: "+err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, report)
	}
}
