package api

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
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
			Sandbox:   deps.Sandbox,
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

		// Slice 12i: compounding-rate metric.
		rollup.CompoundingRate = computeCompoundingRate(deps.Wiki.Dir(), rollup.Wiki.Pages)

		writeJSON(w, deps.Logger, http.StatusOK, rollup)
	}
}

// computeCompoundingRate counts [auto-sum] entries in wiki/log.md from the
// last 7 days and computes the rate as a percentage of total pages.
func computeCompoundingRate(wikiDir string, totalPages int) CompoundingRate {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	logPath := filepath.Join(wikiDir, "log.md")

	f, err := os.Open(logPath)
	if err != nil {
		return CompoundingRate{TotalPages: totalPages}
	}
	defer f.Close()

	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Log table rows: | timestamp | action | page |
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		action := strings.TrimSpace(parts[2])
		if !strings.HasPrefix(action, "auto-sum") {
			continue
		}
		tsStr := strings.TrimSpace(parts[1])
		ts, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			continue
		}
		if ts.UTC().After(cutoff) {
			count++
		}
	}

	rate := 0.0
	if totalPages > 0 {
		rate = float64(count) / float64(totalPages) * 100
	}
	return CompoundingRate{
		AutoAdded7d: count,
		TotalPages:  totalPages,
		RatePct:     rate,
	}
}
