package wiki

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// EntityCandidate names an entity/concept page that can be auto-linked
// from other pages' bodies. Title is what the writer typed (e.g.
// "Siemens", "PSA di Arese Aldo SAS"); Slug is the wiki page id
// (lowercase-hyphen).
type EntityCandidate struct {
	Title string
	Slug  string
}

// InjectionReport describes which pages gained which links.
type InjectionReport struct {
	Pages   int                 `json:"pages"`
	Updated int                 `json:"updated"`
	Links   map[string][]string `json:"links,omitempty"` // slug → injected entity slugs
}

// linkableCategories are the page categories whose titles we consider
// safe to auto-link from other bodies. Source pages are excluded
// because the file names are noisy and short ("Source: foo.xlsx" would
// false-positive on every "foo" mention). Operational + hub pages are
// also excluded.
var linkableCategories = map[string]bool{
	"entity":  true,
	"concept": true,
}

// IsLinkableCategory exposes the category whitelist so callers (tests
// and pipeline glue) can build candidate lists consistently.
func IsLinkableCategory(category string) bool {
	return linkableCategories[strings.TrimSpace(category)]
}

// fencedBlockRe matches markdown code fences (``` ... ```), inline code
// (`...`), and HTML comments (<!-- ... -->). Mentions inside these
// regions must NOT be auto-linked because they're often verbatim code
// or commentary that would render badly when wrapped in [[ ]].
var fencedBlockRe = regexp.MustCompile("```[\\s\\S]*?```|`[^`\\n]+`|<!--[\\s\\S]*?-->")

// existingWikilinkRe matches an existing [[wiki-link]] so we can skip
// over its full span and avoid double-wrapping a mention that's already
// linked.
var existingWikilinkRe = regexp.MustCompile(`\[\[[^\]]+\]\]`)

// InjectEntityLinks scans body for unlinked occurrences of each
// candidate Title and wraps them in [[Slug]]. The match is
// case-insensitive and word-bounded (matches "Siemens" but not
// "siemensless" or "Siemens's"). Existing [[...]] spans and code
// fences are protected. selfSlug, when non-empty, excludes the page's
// own slug from candidates so a page never auto-links to itself.
//
// Returns the rewritten body and the slugs that were injected (in
// stable sort order).
func InjectEntityLinks(body string, candidates []EntityCandidate, selfSlug string) (string, []string) {
	if len(body) == 0 || len(candidates) == 0 {
		return body, nil
	}

	// Mask code fences + existing wikilinks with placeholders so the
	// title scanner ignores anything inside them. We rebuild the
	// original spans back in place after the rewrites.
	masked, restoreMasked := maskProtectedSpans(body)

	// Order candidates by Title length DESCENDING so the longer phrase
	// wins when one title is a substring of another (e.g. "STEP 7
	// Professional V16" must match before "STEP 7").
	sorted := append([]EntityCandidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].Title) > len(sorted[j].Title)
	})

	injected := make(map[string]bool)
	out := masked
	for _, cand := range sorted {
		title := strings.TrimSpace(cand.Title)
		slug := strings.TrimSpace(cand.Slug)
		if title == "" || slug == "" || slug == selfSlug {
			continue
		}
		pattern := wholeWordPattern(title)
		re, err := regexp.Compile(`(?i)` + pattern)
		if err != nil {
			continue
		}
		replaced := false
		out = re.ReplaceAllStringFunc(out, func(match string) string {
			replaced = true
			return "[[" + slug + "]]"
		})
		if replaced {
			injected[slug] = true
		}
	}

	out = restoreMasked(out)

	if len(injected) == 0 {
		return body, nil
	}

	slugs := make([]string, 0, len(injected))
	for s := range injected {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return out, slugs
}

// wholeWordPattern turns a title into a regex that matches the title
// at word boundaries. Existing regex metacharacters in the title are
// escaped. We accept ASCII word boundaries (\b) and treat hyphens +
// apostrophes as part of the surrounding word so "PSA's" is not a
// match for "PSA".
func wholeWordPattern(title string) string {
	escaped := regexp.QuoteMeta(title)
	return `\b` + escaped + `\b`
}

// maskProtectedSpans replaces code fences + inline code + HTML comments
// + existing wikilinks with placeholder strings of the same length so
// the title scanner doesn't recurse into them. The returned restore
// function reverses the substitution after the scanner finishes.
func maskProtectedSpans(body string) (string, func(string) string) {
	type span struct {
		start, end int
		raw        string
	}
	var spans []span
	for _, m := range fencedBlockRe.FindAllStringIndex(body, -1) {
		spans = append(spans, span{m[0], m[1], body[m[0]:m[1]]})
	}
	for _, m := range existingWikilinkRe.FindAllStringIndex(body, -1) {
		spans = append(spans, span{m[0], m[1], body[m[0]:m[1]]})
	}
	if len(spans) == 0 {
		return body, func(s string) string { return s }
	}

	// Sort by start asc; if two spans overlap (a wikilink inside a
	// fenced block is unlikely but theoretically possible), keep the
	// outer one only.
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := spans[:0]
	last := -1
	for _, sp := range spans {
		if sp.start < last {
			continue
		}
		merged = append(merged, sp)
		last = sp.end
	}

	var sb strings.Builder
	markers := make(map[string]string, len(merged))
	cursor := 0
	for i, sp := range merged {
		sb.WriteString(body[cursor:sp.start])
		placeholder := fmt.Sprintf("\x00MASK_%06d\x00", i)
		sb.WriteString(placeholder)
		markers[placeholder] = sp.raw
		cursor = sp.end
	}
	sb.WriteString(body[cursor:])
	masked := sb.String()

	restore := func(s string) string {
		for k, v := range markers {
			s = strings.ReplaceAll(s, k, v)
		}
		return s
	}
	return masked, restore
}

// InjectAcrossWiki walks every page in the wiki and injects entity
// links from a freshly-computed candidate list. Used by ingest
// post-write and by CleanMemory as a maintenance pass. Returns a
// report describing what changed. Pages are rewritten via
// store.WritePage so FTS5/GraphIndex/Qdrant/git stay in sync.
func (s *Store) InjectAcrossWiki(ctx context.Context) (*InjectionReport, error) {
	pages, err := s.memoryPages()
	if err != nil {
		return nil, err
	}

	candidates := make([]EntityCandidate, 0, len(pages))
	for _, item := range pages {
		if !IsLinkableCategory(item.page.Category) {
			continue
		}
		title := strings.TrimSpace(item.page.Title)
		if title == "" {
			continue
		}
		candidates = append(candidates, EntityCandidate{Title: title, Slug: item.slug})
	}

	report := &InjectionReport{Pages: len(pages), Links: map[string][]string{}}
	for _, item := range pages {
		newBody, injected := InjectEntityLinks(item.page.Body, candidates, item.slug)
		if len(injected) == 0 {
			continue
		}
		updated := *item.page
		updated.Body = newBody
		if err := s.WritePage(ctx, &updated); err != nil {
			s.logger.Warn("wiki: inject backlinks write failed", "slug", item.slug, "error", err)
			continue
		}
		report.Updated++
		report.Links[item.slug] = injected
	}
	return report, nil
}
