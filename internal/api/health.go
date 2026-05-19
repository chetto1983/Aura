package api

import (
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/tokenjuice"
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
			Sandbox:   deps.Sandbox,
		}

		// Process rollup
		rollup.Process.Version = deps.Version
		rollup.Process.GitRevision = gitRevision()
		rollup.Process.Commit = deps.Commit
		rollup.Process.BuildDate = deps.BuildDate
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
				if rec.Status == cron.StatusActive {
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
		if deps.CompactMemory != nil {
			rollup.CompactMemory = deps.CompactMemory.Snapshot()
		}

		// Phase 2 D-16: reindex worker telemetry. Always present (zero value
		// when callback is nil) so TypeScript strict types stay stable.
		// WARNING 12 of 2026-05-10 plan revision (closed without Phase 3 deferral).
		if deps.ReindexHealth != nil {
			rollup.Reindex = reindexHealthFromHealth(deps.ReindexHealth())
		}

		// Phase-TJ: process-level token-juice compaction stats.
		snap := tokenjuice.SnapshotStats()
		rollup.TokenJuice.TotalCalls = snap.TotalCalls
		rollup.TokenJuice.TotalBytesSaved = snap.TotalBytesSaved
		rollup.TokenJuice.AvgRatio = snap.AvgRatio
		for _, r := range snap.TopRules {
			rollup.TokenJuice.TopRules = append(rollup.TokenJuice.TopRules, RuleSaving{
				RuleID:     r.RuleID,
				BytesSaved: r.BytesSaved,
			})
		}
		if deps.RuntimeConfig != nil {
			rollup.TokenJuice.Enabled = deps.RuntimeConfig.TokenJuiceEnabled
		}

		writeJSON(w, deps.Logger, http.StatusOK, rollup)
	}
}
