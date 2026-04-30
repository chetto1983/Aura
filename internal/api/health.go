package api

import (
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
)

// gitRevision is read once via debug.ReadBuildInfo. The result depends
// only on the binary so caching is safe across goroutines.
var (
	gitRevisionOnce sync.Once
	gitRevisionVal  string
)

func gitRevision() string {
	gitRevisionOnce.Do(func() {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				// Short hash for human display; full SHA available via
				// `git rev-parse HEAD` if anyone needs it.
				gitRevisionVal = s.Value[:7]
				return
			}
		}
	})
	return gitRevisionVal
}

func handleHealth(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rollup := HealthRollup{
			Sources:   SourcesHealth{ByStatus: map[string]int{}},
			Tasks:     TasksHealth{ByStatus: map[string]int{}},
			Scheduler: SchedulerHealth{},
		}

		// Process rollup
		rollup.Process.Version = deps.Version
		rollup.Process.GitRevision = gitRevision()
		if !deps.StartedAt.IsZero() {
			started := deps.StartedAt.UTC()
			rollup.Process.StartedAt = started
			rollup.Process.UptimeSeconds = int64(time.Since(started).Seconds())
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

		// Slice 11j: embed cache stats. Stays at zero when no cache is
		// wired (e.g. EMBEDDING_API_KEY unset).
		if deps.EmbedCache != nil {
			hits, misses := deps.EmbedCache.Stats()
			rollup.EmbedCache.Hits = hits
			rollup.EmbedCache.Misses = misses
		}

		writeJSON(w, deps.Logger, http.StatusOK, rollup)
	}
}
