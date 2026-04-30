package api

import (
	"net/http"

	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
)

func handleHealth(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rollup := HealthRollup{
			Sources:   SourcesHealth{ByStatus: map[string]int{}},
			Tasks:     TasksHealth{ByStatus: map[string]int{}},
			Scheduler: SchedulerHealth{},
		}

		// Wiki rollup
		slugs, err := deps.Wiki.ListPages()
		if err != nil {
			deps.Logger.Warn("api: health wiki list", "error", err)
		} else {
			rollup.Wiki.Pages = len(slugs)
		}
		if mtime, err := latestWikiMTime(deps.Wiki.Dir()); err == nil {
			rollup.Wiki.LastUpdate = mtime
		}

		// Sources rollup
		if records, err := deps.Sources.List(source.ListFilter{}); err == nil {
			for _, rec := range records {
				rollup.Sources.ByStatus[string(rec.Status)]++
			}
		} else {
			deps.Logger.Warn("api: health sources list", "error", err)
		}

		// Tasks rollup + next-run
		if records, err := deps.Scheduler.List(ctx, ""); err == nil {
			for _, rec := range records {
				rollup.Tasks.ByStatus[string(rec.Status)]++
				if rec.Status == scheduler.StatusActive {
					next := rec.NextRunAt.UTC()
					if rollup.Scheduler.NextRun == nil || next.Before(*rollup.Scheduler.NextRun) {
						rollup.Scheduler.NextRun = &next
					}
				}
			}
		} else {
			deps.Logger.Warn("api: health tasks list", "error", err)
		}

		writeJSON(w, deps.Logger, http.StatusOK, rollup)
	}
}
