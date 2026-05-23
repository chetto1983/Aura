package wiki

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// LintIssue represents a problem found by Lint.
type LintIssue struct {
	Slug     string
	Message  string
	Kind     string
	Severity string
}

// Lint checks the wiki for broken links, missing categories, and memory decay.
func (s *Store) Lint(ctx context.Context) ([]LintIssue, error) {
	return s.lintAt(ctx, time.Now().UTC())
}

func (s *Store) lintAt(_ context.Context, now time.Time) ([]LintIssue, error) {
	slugs, err := s.ListPages()
	if err != nil {
		return nil, err
	}

	slugSet := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		slugSet[s] = true
	}

	var issues []LintIssue
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			issues = append(issues, LintIssue{Slug: slug, Message: "failed to read page", Kind: "read_error", Severity: "medium"})
			continue
		}

		if page.Category == "" {
			issues = append(issues, LintIssue{Slug: slug, Message: "missing category", Kind: "missing_category", Severity: "low"})
		}

		if issue, ok := memoryDecayIssue(slug, page.UpdatedAt, now); ok {
			issues = append(issues, issue)
		}

		for _, link := range ExtractWikiLinks(page.Body) {
			if !slugSet[link] {
				issues = append(issues, LintIssue{
					Slug:     slug,
					Message:  fmt.Sprintf("broken link: [[%s]]", link),
					Kind:     "broken_link",
					Severity: "high",
				})
			}
		}

		for _, ref := range page.Related {
			if !slugSet[ref.Slug] {
				issues = append(issues, LintIssue{
					Slug:     slug,
					Message:  fmt.Sprintf("broken related ref: %s", ref.Slug),
					Kind:     "broken_link",
					Severity: "high",
				})
			}
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Slug != issues[j].Slug {
			return issues[i].Slug < issues[j].Slug
		}
		return issues[i].Message < issues[j].Message
	})

	return issues, nil
}

func memoryDecayIssue(slug, updatedAt string, now time.Time) (LintIssue, bool) {
	updated, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return LintIssue{
			Slug:     slug,
			Message:  "invalid updated_at for decay check",
			Kind:     "invalid_metadata",
			Severity: "medium",
		}, true
	}
	age := now.Sub(updated)
	if age < memoryDecayMediumAge {
		return LintIssue{}, false
	}
	days := int(age.Hours() / 24)
	severity := "medium"
	if age >= memoryDecayHighAge {
		severity = "high"
	}
	decay := age.Hours() / memoryDecayHighAge.Hours()
	if decay > 1 {
		decay = 1
	}
	return LintIssue{
		Slug:     slug,
		Message:  fmt.Sprintf("memory decay: updated_at %s is %d days old (decay=%.2f)", updated.UTC().Format(time.RFC3339), days, decay),
		Kind:     "memory_decay",
		Severity: severity,
	}, true
}
