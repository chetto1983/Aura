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

// OwnerNotifier is called when high-severity issues are found. Passed as a
// function to avoid an import cycle between scheduler and telegram packages.
type OwnerNotifier func(ctx context.Context, msg string)

// MaintenanceJob runs the nightly wiki maintenance pass.
type MaintenanceJob struct {
	wiki     WikiMaintainer
	issues   *IssuesStore  // nil → skip enqueue (pre-12h behaviour)
	notifier OwnerNotifier // nil → skip notifications
	logger   *slog.Logger
}

// NewMaintenanceJob returns a MaintenanceJob. logger, issues, and notifier may
// be nil (issues/notifier = no DB enqueue / no Telegram notification).
func NewMaintenanceJob(w WikiMaintainer, logger *slog.Logger) *MaintenanceJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &MaintenanceJob{wiki: w, logger: logger}
}

// WithIssuesStore configures the IssuesStore used to persist deferred issues.
func (j *MaintenanceJob) WithIssuesStore(s *IssuesStore) *MaintenanceJob {
	j.issues = s
	return j
}

// WithOwnerNotifier configures the callback for high-severity notifications.
func (j *MaintenanceJob) WithOwnerNotifier(n OwnerNotifier) *MaintenanceJob {
	j.notifier = n
	return j
}

// Run executes the maintenance pass and returns (fixed, deferred, err).
// fixed: number of broken links auto-repaired.
// deferred: number of issues persisted to wiki_issues (or just logged when
// no IssuesStore is wired).
func (j *MaintenanceJob) Run(ctx context.Context) (fixed, deferred int, err error) {
	lintIssues, err := j.wiki.Lint(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("maintenance lint: %w", err)
	}

	slugs, err := j.wiki.ListPages()
	if err != nil {
		return 0, 0, fmt.Errorf("maintenance list pages: %w", err)
	}

	var highCount int

	for _, li := range lintIssues {
		brokenSlug, ok := parseBrokenLink(li.Message)
		if !ok {
			// Non-broken-link issue (missing category, orphan, etc.)
			severity := classifyNonLink(li.Message)
			j.logger.Info("maintenance: non-link issue deferred",
				"page", li.Slug, "msg", li.Message, "severity", severity)
			j.enqueue(ctx, Issue{
				Kind:     classifyKind(li.Message),
				Severity: severity,
				Slug:     li.Slug,
				Message:  li.Message,
			}, &highCount)
			deferred++
			continue
		}

		candidates := levenshteinCandidates(brokenSlug, slugs, 2)
		if len(candidates) == 1 {
			if repErr := j.wiki.RepairLink(ctx, brokenSlug, candidates[0]); repErr != nil {
				j.logger.Warn("maintenance: repair failed",
					"broken", brokenSlug, "candidate", candidates[0], "error", repErr)
				j.enqueue(ctx, Issue{
					Kind: "broken_link", Severity: "high",
					Slug: li.Slug, BrokenLink: brokenSlug, Message: li.Message,
				}, &highCount)
				deferred++
			} else {
				j.logger.Info("maintenance: auto-fixed broken link",
					"page", li.Slug, "broken", brokenSlug, "fixed", candidates[0])
				fixed++
			}
		} else {
			j.logger.Info("maintenance: ambiguous broken link, deferring",
				"page", li.Slug, "broken", brokenSlug, "candidates", len(candidates))
			j.enqueue(ctx, Issue{
				Kind: "broken_link", Severity: "high",
				Slug: li.Slug, BrokenLink: brokenSlug, Message: li.Message,
			}, &highCount)
			deferred++
		}
	}

	if highCount > 0 && j.notifier != nil {
		msg := fmt.Sprintf("Aura wiki maintenance: %d high-severity issue(s) found. Check the dashboard /maintenance.", highCount)
		j.notifier(ctx, msg)
	}

	return fixed, deferred, nil
}

func (j *MaintenanceJob) enqueue(ctx context.Context, issue Issue, highCount *int) {
	if j.issues != nil {
		if err := j.issues.Enqueue(ctx, issue); err != nil {
			j.logger.Warn("maintenance: enqueue failed", "error", err)
		}
	}
	if issue.Severity == "high" {
		*highCount++
	}
}

// classifyKind maps a lint message to an issue kind string.
func classifyKind(msg string) string {
	switch {
	case strings.Contains(msg, "broken link") || strings.Contains(msg, "broken related"):
		return "broken_link"
	case strings.Contains(msg, "missing category"):
		return "missing_category"
	default:
		return "orphan"
	}
}

// classifyNonLink assigns severity to non-broken-link issues.
func classifyNonLink(msg string) string {
	switch {
	case strings.Contains(msg, "missing category"):
		return "low"
	default:
		return "medium"
	}
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
