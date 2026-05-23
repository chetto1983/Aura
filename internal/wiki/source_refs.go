package wiki

import (
	"context"
	"fmt"
	"strings"
)

// SourceRefPrefix is the conventional prefix used in wiki frontmatter to
// reference an ingested source. Example value in YAML:
//
//	sources:
//	  - source:src_b6d7837be78ec5a4
const SourceRefPrefix = "source:"

// PagesReferencingSource returns the slugs of every wiki page whose
// frontmatter 'sources:' list contains the given source ID. The match
// is exact against "source:<id>" — partial matches are not accepted to
// avoid false positives between sources with id prefixes that overlap.
//
// The full wiki directory is scanned linearly. This is O(N) in pages
// and runs only when an operator deletes a source, so the cost is
// acceptable. Errors reading individual pages are logged via the
// Store's logger and the page is skipped — one corrupt page must not
// stop the cleanup of the rest.
func (s *Store) PagesReferencingSource(ctx context.Context, sourceID string) ([]string, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("wiki: empty source id")
	}
	ref := SourceRefPrefix + sourceID

	slugs, err := s.ListPages()
	if err != nil {
		return nil, fmt.Errorf("wiki: list pages: %w", err)
	}

	var matches []string
	for _, slug := range slugs {
		if ctx.Err() != nil {
			return matches, ctx.Err()
		}
		page, err := s.ReadPage(slug)
		if err != nil {
			s.logger.Warn("wiki: read page during source-ref scan", "slug", slug, "error", err)
			continue
		}
		if pageReferencesSource(page, ref) {
			matches = append(matches, slug)
		}
	}
	return matches, nil
}

func pageReferencesSource(page *Page, ref string) bool {
	if page == nil {
		return false
	}
	for _, s := range page.Sources {
		if strings.TrimSpace(s) == ref {
			return true
		}
	}
	return false
}
