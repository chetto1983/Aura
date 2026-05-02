package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

// WikiMaintainer is the wiki surface MaintenanceJob needs.
type WikiMaintainer interface {
	Lint(ctx context.Context) ([]wiki.LintIssue, error)
	ListPages() ([]string, error)
	RepairLink(ctx context.Context, brokenSlug, fixedSlug string) error
}

// MaintenanceJob runs the nightly wiki maintenance pass.
type MaintenanceJob struct {
	wiki   WikiMaintainer
	logger *slog.Logger
}

// NewMaintenanceJob returns a MaintenanceJob. logger may be nil (uses default).
func NewMaintenanceJob(w WikiMaintainer, logger *slog.Logger) *MaintenanceJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &MaintenanceJob{wiki: w, logger: logger}
}

// Run executes the maintenance pass and returns (fixed, deferred, err).
// fixed: number of broken links auto-repaired.
// deferred: number of issues that could not be auto-fixed (queued by 12h).
func (j *MaintenanceJob) Run(ctx context.Context) (fixed, deferred int, err error) {
	issues, err := j.wiki.Lint(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("maintenance lint: %w", err)
	}

	slugs, err := j.wiki.ListPages()
	if err != nil {
		return 0, 0, fmt.Errorf("maintenance list pages: %w", err)
	}

	for _, issue := range issues {
		brokenSlug, ok := parseBrokenLink(issue.Message)
		if !ok {
			// Not a broken-link issue (e.g. missing category, orphan) — defer.
			j.logger.Info("maintenance: non-link issue deferred",
				"page", issue.Slug, "msg", issue.Message)
			deferred++
			continue
		}

		candidates := levenshteinCandidates(brokenSlug, slugs, 2)
		if len(candidates) == 1 {
			if repErr := j.wiki.RepairLink(ctx, brokenSlug, candidates[0]); repErr != nil {
				j.logger.Warn("maintenance: repair failed",
					"broken", brokenSlug, "candidate", candidates[0], "error", repErr)
				deferred++
			} else {
				j.logger.Info("maintenance: auto-fixed broken link",
					"page", issue.Slug, "broken", brokenSlug, "fixed", candidates[0])
				fixed++
			}
		} else {
			j.logger.Info("maintenance: ambiguous broken link, deferring",
				"page", issue.Slug, "broken", brokenSlug, "candidates", len(candidates))
			deferred++
		}
	}
	return fixed, deferred, nil
}

// parseBrokenLink extracts the slug from a LintIssue message like
// "broken link: [[slug]]". Returns ("", false) for other message types.
func parseBrokenLink(msg string) (string, bool) {
	const prefix = "broken link: [["
	if !strings.HasPrefix(msg, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(msg, prefix)
	slug := strings.TrimSuffix(rest, "]]")
	if slug == rest {
		return "", false
	}
	return slug, true
}

// levenshteinCandidates returns all slugs whose Levenshtein distance to
// target is at most maxDist.
func levenshteinCandidates(target string, slugs []string, maxDist int) []string {
	var out []string
	for _, s := range slugs {
		if levenshtein(target, s) <= maxDist {
			out = append(out, s)
		}
	}
	return out
}

// levenshtein computes the edit distance between a and b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	row := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		row[j] = j
	}
	for i := 1; i <= la; i++ {
		prev := row[0]
		row[0] = i
		for j := 1; j <= lb; j++ {
			tmp := row[j]
			if ra[i-1] == rb[j-1] {
				row[j] = prev
			} else {
				row[j] = 1 + min3(prev, row[j], row[j-1])
			}
			prev = tmp
		}
	}
	return row[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
