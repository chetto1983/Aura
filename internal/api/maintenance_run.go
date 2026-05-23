package api

import (
	"context"
	"net/http"

	"github.com/aura/aura/internal/wiki"
)

// MaintenanceReport is the JSON body returned by POST /maintenance/run.
// Mirrors wiki.MemoryHygieneReport but flattens nested types to keep the
// frontend payload small (the operator sees a summary + counts; details
// are best-effort recorded in /maintenance/issues by the cron job).
//
// LinksInjected counts pages that gained at least one auto-injected
// [[wiki-link]] from the post-hygiene cross-wiki link-injection pass.
// This is what fixes the "two sources both about Siemens but graph
// shows disconnected clusters" failure mode reported 2026-05-23.
type MaintenanceReport struct {
	Pages           int      `json:"pages"`
	Orphans         []string `json:"orphans,omitempty"`
	BrokenLinkCount int      `json:"broken_link_count"`
	RenameCount     int      `json:"rename_count"`
	HubsCreated     int      `json:"hubs_created"`
	LinksInjected   int      `json:"links_injected"`
	DedupClusters   int      `json:"dedup_clusters"`
	DedupMerges     int      `json:"dedup_merges"`
}

// linkInjector is the optional capability satisfied by *wiki.Store. The
// interface is local to api so a stripped wiki fake in tests can
// satisfy MemoryCleaner without also implementing the injection pass.
type linkInjector interface {
	InjectAcrossWiki(ctx context.Context) (*wiki.InjectionReport, error)
}

// handleMaintenanceRun triggers wiki.Store.CleanMemory({Apply: true}) on
// demand from the dashboard. The cron-driven maintenance job runs the
// same routine periodically; this endpoint exposes a manual lever so the
// operator can re-run after a batch ingest (the cron tick may be hours
// away) or to surface the report without waiting.
//
// Returns 503 when the wiki store does not implement the MemoryCleaner
// boundary (test wiring with a stripped fake, or wiki disabled).
func handleMaintenanceRun(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cleaner, ok := deps.Wiki.(wiki.MemoryCleaner)
		if !ok || cleaner == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "wiki maintenance unavailable")
			return
		}
		report, err := cleaner.CleanMemory(r.Context(), wiki.MemoryHygieneOptions{Apply: true})
		if err != nil {
			deps.Logger.Warn("api: maintenance run failed", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "maintenance run failed: "+err.Error())
			return
		}
		resp := MaintenanceReport{
			Pages:           report.Pages,
			Orphans:         report.Orphans,
			BrokenLinkCount: len(report.BrokenLinks),
			RenameCount:     len(report.RenamedPages),
			HubsCreated:     len(report.CreatedHubs),
		}

		// Cross-wiki link-injection pass: scans every page body for
		// occurrences of any entity/concept title and wraps unlinked
		// mentions with [[slug]]. Necessary because the per-source LLM
		// extractor only sees entities that existed at ingest time —
		// later-ingested sources sharing entities with earlier sources
		// stay disconnected in the graph until this runs.
		if injector, ok := deps.Wiki.(linkInjector); ok && injector != nil {
			if ir, err := injector.InjectAcrossWiki(r.Context()); err != nil {
				deps.Logger.Warn("api: link-injection pass failed", "error", err)
			} else if ir != nil {
				resp.LinksInjected = ir.Updated
			}
		}

		// Entity-dedup pass: merges near-duplicate entity/concept pages
		// detected by embedding cosine similarity + optional LLM tiebreaker.
		// Runs last so alias + link injection is already in place before
		// candidates are evaluated. Skipped silently when the dedup backend
		// is not configured (Qdrant absent).
		if deduper, ok := deps.Wiki.(entityDeduplicator); ok && deduper != nil {
			if dr, dedupErr := deduper.DeduplicateEntities(r.Context(), wiki.DedupOptions{}); dedupErr != nil {
				deps.Logger.Warn("api: dedup step failed", "error", dedupErr)
			} else if dr != nil {
				resp.DedupClusters = len(dr.Clusters)
				resp.DedupMerges = dr.MergesApplied
			}
		}

		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}
