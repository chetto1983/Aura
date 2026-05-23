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
// (lowercase-hyphen). Aliases carries all surface forms from the page's
// frontmatter aliases list (US-GRAPH-01), so injection matches any of
// them in addition to the canonical Title.
type EntityCandidate struct {
	Title   string
	Slug    string
	Aliases []string
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

// matchEntry is an internal (matchString, targetSlug) pair built from a
// candidate's Title plus each of its Aliases.
type matchEntry struct {
	str  string
	slug string
}

// matchOccurrence records a single match position in the masked text.
type matchOccurrence struct {
	start, end int
	slug       string
}

// InjectEntityLinks scans body for unlinked occurrences of each
// candidate Title and all candidate Aliases, and wraps them in
// [[Slug]]. The match is case-insensitive and uses word boundaries
// where the match string begins/ends with a word character. Existing
// [[...]] spans and code fences are protected. selfSlug, when
// non-empty, excludes the page's own slug from candidates so a page
// never auto-links to itself.
//
// All match strings (titles + aliases) are sorted by length DESC before
// scanning so longer strings win over shorter ones at any given position
// (e.g. "Codice Civile" wins over "Codice" in "Codice Civile").
// Matches are found in the original masked text first, then applied
// non-overlappingly — this prevents a shorter alias from matching
// inside an already-injected [[slug]] span.
//
// Returns the rewritten body and the slugs that were injected (in
// stable sort order).
func InjectEntityLinks(body string, candidates []EntityCandidate, selfSlug string) (string, []string) {
	if len(body) == 0 || len(candidates) == 0 {
		return body, nil
	}

	// Expand each candidate into (matchString, slug) pairs: title + each alias.
	expanded := make([]matchEntry, 0, len(candidates)*3)
	for _, cand := range candidates {
		slug := strings.TrimSpace(cand.Slug)
		if slug == "" || slug == selfSlug {
			continue
		}
		title := strings.TrimSpace(cand.Title)
		if title != "" {
			expanded = append(expanded, matchEntry{str: title, slug: slug})
		}
		for _, alias := range cand.Aliases {
			alias = strings.TrimSpace(alias)
			if alias != "" {
				expanded = append(expanded, matchEntry{str: alias, slug: slug})
			}
		}
	}
	if len(expanded) == 0 {
		return body, nil
	}

	// Sort by match-string length DESC so longer aliases / titles are
	// found first and win at any given text position.
	sort.SliceStable(expanded, func(i, j int) bool {
		return len(expanded[i].str) > len(expanded[j].str)
	})

	// Mask code fences + existing wikilinks with placeholders so the
	// scanner ignores anything inside them.
	masked, restoreMasked := maskProtectedSpans(body)

	// Single-pass: collect ALL match positions across ALL patterns in the
	// original masked text before making any substitutions. This prevents
	// a shorter pattern from matching inside a [[slug]] injected by a
	// longer pattern earlier in the loop.
	var occurrences []matchOccurrence
	for _, e := range expanded {
		pattern := wholeWordPattern(e.str)
		re, err := regexp.Compile(`(?i)` + pattern)
		if err != nil {
			continue
		}
		for _, loc := range re.FindAllStringIndex(masked, -1) {
			occurrences = append(occurrences, matchOccurrence{
				start: loc[0],
				end:   loc[1],
				slug:  e.slug,
			})
		}
	}

	if len(occurrences) == 0 {
		return body, nil
	}

	// Sort occurrences by start position. On same start, prefer the
	// longer match (larger end) — this is guaranteed by the expanded
	// order (length-DESC) preserved via SliceStable, but we re-sort
	// explicitly for correctness when building the output.
	sort.SliceStable(occurrences, func(i, j int) bool {
		if occurrences[i].start != occurrences[j].start {
			return occurrences[i].start < occurrences[j].start
		}
		return occurrences[i].end > occurrences[j].end
	})

	// Apply non-overlapping replacements left to right.
	injected := make(map[string]bool)
	var sb strings.Builder
	cursor := 0
	for _, occ := range occurrences {
		if occ.start < cursor {
			continue // overlaps with an already-applied match — skip
		}
		sb.WriteString(masked[cursor:occ.start])
		sb.WriteString("[[" + occ.slug + "]]")
		injected[occ.slug] = true
		cursor = occ.end
	}
	sb.WriteString(masked[cursor:])
	out := restoreMasked(sb.String())

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

// wholeWordPattern turns a match string into a regex pattern with word
// boundaries where the match string starts/ends with an ASCII word
// character ([0-9A-Za-z_]). Regex metacharacters in the string are
// escaped via regexp.QuoteMeta — no pattern characters are interpreted
// from the input.
//
// Smart boundary: `\b` is added at the start only when the first byte
// of s is a word char, and at the end only when the last byte is a
// word char. This lets aliases ending in punctuation (e.g.
// "art. 1218 c.c.") match correctly without a trailing `\b` that
// would otherwise fail when the next character is also non-word.
func wholeWordPattern(s string) string {
	if s == "" {
		return ""
	}
	escaped := regexp.QuoteMeta(s)
	var prefix, suffix string
	if isASCIIWordChar(s[0]) {
		prefix = `\b`
	}
	if isASCIIWordChar(s[len(s)-1]) {
		suffix = `\b`
	}
	return prefix + escaped + suffix
}

// isASCIIWordChar reports whether b is in the ASCII word-character set
// ([0-9A-Za-z_]) that Go's regex engine uses for \b boundaries.
func isASCIIWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// maskProtectedSpans replaces code fences + inline code + HTML comments
// + existing wikilinks with placeholder strings so the title scanner
// doesn't recurse into them. The returned restore function reverses the
// substitution after the scanner finishes.
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
//
// The candidate list includes each linkable page's Title AND all
// frontmatter Aliases (US-GRAPH-02), so injection triggers on any
// surface form seen in the source corpus. report.Links maps each
// updated page slug to the list of injected entity slugs (len gives
// the per-page injected count).
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
		candidates = append(candidates, EntityCandidate{
			Title:   title,
			Slug:    item.slug,
			Aliases: item.page.Aliases,
		})
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
