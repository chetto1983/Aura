package wiki

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MemoryHygieneOptions controls the automated memory graph cleanup pass.
// Dry-run is the default; Apply=true writes repairs and generated hub pages.
type MemoryHygieneOptions struct {
	Apply bool
}

// MemoryHygieneReport summarizes what the pass found and, when Apply=true,
// what it changed.
type MemoryHygieneReport struct {
	Pages         int          `json:"pages"`
	BrokenLinks   []BrokenLink `json:"broken_links,omitempty"`
	Orphans       []string     `json:"orphans,omitempty"`
	PlannedHubs   []string     `json:"planned_hubs,omitempty"`
	CreatedHubs   []string     `json:"created_hubs,omitempty"`
	RepairedLinks []LinkRepair `json:"repaired_links,omitempty"`
	TouchedPages  []string     `json:"touched_pages,omitempty"`
}

// BrokenLink is a missing [[slug]] or related frontmatter reference.
type BrokenLink struct {
	From     string `json:"from"`
	Target   string `json:"target"`
	Location string `json:"location"`
}

// LinkRepair records a broken reference rewritten to an existing canonical page.
type LinkRepair struct {
	From        string `json:"from"`
	Target      string `json:"target"`
	Replacement string `json:"replacement"`
	Location    string `json:"location"`
}

type memoryPage struct {
	slug string
	page *Page
}

// CleanMemory audits and optionally repairs wiki graph hygiene. It keeps
// operational files out of memory, repairs obvious alias links, creates shared
// category hub pages for repeated missing concepts, and connects isolated pages
// to those hubs.
func (s *Store) CleanMemory(ctx context.Context, opts MemoryHygieneOptions) (*MemoryHygieneReport, error) {
	pages, err := s.memoryPages()
	if err != nil {
		return nil, err
	}

	known := make(map[string]*Page, len(pages))
	byCategory := make(map[string][]string)
	for _, item := range pages {
		known[item.slug] = item.page
		category := cleanCategory(item.page.Category)
		byCategory[category] = append(byCategory[category], item.slug)
	}
	for category := range byCategory {
		sort.Strings(byCategory[category])
	}

	report := &MemoryHygieneReport{Pages: len(pages)}
	incoming := make(map[string]int)
	outgoing := make(map[string]int)
	missingByTarget := make(map[string][]BrokenLink)
	missingByTargetCategory := make(map[string]map[string]int)

	for _, item := range pages {
		targets := pageGraphRefs(item.page)
		for _, ref := range targets {
			if _, ok := known[ref.target]; ok {
				incoming[ref.target]++
				outgoing[item.slug]++
				continue
			}
			broken := BrokenLink{From: item.slug, Target: ref.target, Location: ref.location}
			report.BrokenLinks = append(report.BrokenLinks, broken)
			missingByTarget[ref.target] = append(missingByTarget[ref.target], broken)
			category := cleanCategory(item.page.Category)
			if missingByTargetCategory[ref.target] == nil {
				missingByTargetCategory[ref.target] = make(map[string]int)
			}
			missingByTargetCategory[ref.target][category]++
		}
	}

	for _, item := range pages {
		if incoming[item.slug] == 0 && outgoing[item.slug] == 0 {
			report.Orphans = append(report.Orphans, item.slug)
		}
	}
	sort.Strings(report.Orphans)

	hubCategory := make(map[string]string)
	for target, categoryCounts := range missingByTargetCategory {
		category, count := dominantCategory(categoryCounts)
		if count >= 2 {
			hubCategory[target] = category
		}
	}
	for category, slugs := range byCategory {
		if len(slugs) < 2 {
			continue
		}
		if existingCategoryHub(category, slugs, known) != "" {
			continue
		}
		needsHub := false
		for _, slug := range slugs {
			if incoming[slug] == 0 && outgoing[slug] == 0 {
				needsHub = true
				break
			}
		}
		if needsHub {
			hubCategory[categoryHubSlug(category)] = category
		}
	}

	replacements := make(map[string]string)
	for target := range missingByTarget {
		if _, createsHub := hubCategory[target]; createsHub {
			continue
		}
		if replacement := bestAliasMatch(target, known); replacement != "" {
			replacements[target] = replacement
		}
	}

	report.PlannedHubs = sortedHubSlugs(hubCategory, known)
	if !opts.Apply {
		report.RepairedLinks = plannedRepairs(missingByTarget, replacements)
		return report, nil
	}

	for _, hubSlug := range report.PlannedHubs {
		category := hubCategory[hubSlug]
		if err := s.writeMemoryHub(ctx, hubSlug, category, byCategory[category], known); err != nil {
			return report, err
		}
		report.CreatedHubs = append(report.CreatedHubs, hubSlug)
	}

	touched := make(map[string]bool)
	for _, item := range pages {
		changed := false
		if stripAutoSourcePreview(item.page) {
			changed = true
		}
		for broken, fixed := range replacements {
			old := "[[" + broken + "]]"
			next := "[[" + fixed + "]]"
			if strings.Contains(item.page.Body, old) {
				item.page.Body = strings.ReplaceAll(item.page.Body, old, next)
				report.RepairedLinks = append(report.RepairedLinks, LinkRepair{From: item.slug, Target: broken, Replacement: fixed, Location: "body"})
				changed = true
			}
			if replaceRelated(item.page, broken, fixed) {
				report.RepairedLinks = append(report.RepairedLinks, LinkRepair{From: item.slug, Target: broken, Replacement: fixed, Location: "related"})
				changed = true
			}
		}

		category := cleanCategory(item.page.Category)
		for _, hubSlug := range report.CreatedHubs {
			if hubCategory[hubSlug] != category || hubSlug == item.slug {
				continue
			}
			if !pageReferencesSlug(item.page, hubSlug) {
				item.page.Related = appendUniqueSorted(item.page.Related, hubSlug)
				changed = true
			}
		}

		if !changed {
			continue
		}
		item.page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.WritePage(ctx, item.page); err != nil {
			return report, fmt.Errorf("clean memory write %s: %w", item.slug, err)
		}
		touched[item.slug] = true
	}

	report.TouchedPages = sortedStringSet(touched)
	s.RebuildIndex(ctx)
	s.AppendLog(ctx, "memory-hygiene", "")
	return report, nil
}

type graphRef struct {
	target   string
	location string
}

func (s *Store) memoryPages() ([]memoryPage, error) {
	slugs, err := s.ListPages()
	if err != nil {
		return nil, fmt.Errorf("clean memory list pages: %w", err)
	}
	sort.Strings(slugs)
	pages := make([]memoryPage, 0, len(slugs))
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			return nil, fmt.Errorf("clean memory read %s: %w", slug, err)
		}
		pages = append(pages, memoryPage{slug: slug, page: page})
	}
	return pages, nil
}

func pageGraphRefs(page *Page) []graphRef {
	seen := make(map[string]bool)
	var refs []graphRef
	for _, link := range ExtractWikiLinks(page.Body) {
		if seen["body:"+link] {
			continue
		}
		seen["body:"+link] = true
		refs = append(refs, graphRef{target: link, location: "body"})
	}
	for _, rel := range page.Related {
		rel = strings.TrimSpace(rel)
		if rel == "" || seen["related:"+rel] {
			continue
		}
		seen["related:"+rel] = true
		refs = append(refs, graphRef{target: rel, location: "related"})
	}
	return refs
}

func cleanCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "uncategorized"
	}
	return category
}

func dominantCategory(counts map[string]int) (string, int) {
	var bestCategory string
	var bestCount int
	for category, count := range counts {
		if count > bestCount || count == bestCount && category < bestCategory {
			bestCategory = category
			bestCount = count
		}
	}
	return bestCategory, bestCount
}

func categoryHubSlug(category string) string {
	return Slug(category)
}

func existingCategoryHub(category string, slugs []string, known map[string]*Page) string {
	want := categoryHubSlug(category)
	for _, slug := range slugs {
		page := known[slug]
		if slug == want || hasString(page.Tags, "hub") {
			return slug
		}
	}
	return ""
}

func sortedHubSlugs(hubs map[string]string, known map[string]*Page) []string {
	out := make([]string, 0, len(hubs))
	for slug := range hubs {
		if _, exists := known[slug]; exists || IsOperationalSlug(slug) {
			continue
		}
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

func plannedRepairs(missing map[string][]BrokenLink, replacements map[string]string) []LinkRepair {
	var repairs []LinkRepair
	for broken, fixed := range replacements {
		for _, link := range missing[broken] {
			repairs = append(repairs, LinkRepair{From: link.From, Target: broken, Replacement: fixed, Location: link.Location})
		}
	}
	sort.Slice(repairs, func(i, j int) bool {
		if repairs[i].From != repairs[j].From {
			return repairs[i].From < repairs[j].From
		}
		if repairs[i].Target != repairs[j].Target {
			return repairs[i].Target < repairs[j].Target
		}
		return repairs[i].Location < repairs[j].Location
	})
	return repairs
}

func bestAliasMatch(target string, known map[string]*Page) string {
	targetTokens := slugTokens(target)
	if len(targetTokens) == 0 {
		return ""
	}
	type candidate struct {
		slug  string
		score float64
	}
	var best candidate
	for slug := range known {
		candidateTokens := tokenSet(slugTokens(slug))
		hits := 0
		for _, token := range targetTokens {
			if candidateTokens[token] {
				hits++
			}
		}
		coverage := float64(hits) / float64(len(targetTokens))
		if coverage < 0.75 {
			continue
		}
		score := coverage + (float64(hits) / float64(len(candidateTokens)+1) / 10)
		if score > best.score || score == best.score && slug < best.slug {
			best = candidate{slug: slug, score: score}
		}
	}
	return best.slug
}

func slugTokens(slug string) []string {
	parts := strings.FieldsFunc(strings.ToLower(slug), func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	var tokens []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

func tokenSet(tokens []string) map[string]bool {
	out := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		out[token] = true
	}
	return out
}

func (s *Store) writeMemoryHub(ctx context.Context, hubSlug, category string, slugs []string, known map[string]*Page) error {
	now := time.Now().UTC().Format(time.RFC3339)
	title := titleFromSlug(hubSlug)
	var body strings.Builder
	fmt.Fprintf(&body, "%s hub for pages in the %s category.\n\n", title, category)
	body.WriteString("## Pages\n\n")
	for _, slug := range slugs {
		if slug == hubSlug {
			continue
		}
		title := slug
		if page := known[slug]; page != nil && page.Title != "" {
			title = page.Title
		}
		fmt.Fprintf(&body, "- [[%s]] %s\n", slug, title)
	}
	page := &Page{
		Title:         title,
		Body:          strings.TrimSpace(body.String()),
		Category:      category,
		Tags:          []string{"hub", "memory-quality"},
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if got := Slug(page.Title); got != hubSlug {
		page.Title = hubSlug
	}
	return s.WritePage(ctx, page)
}

func titleFromSlug(slug string) string {
	parts := strings.Split(slug, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func replaceRelated(page *Page, broken, fixed string) bool {
	changed := false
	next := make([]string, 0, len(page.Related))
	seen := make(map[string]bool)
	for _, rel := range page.Related {
		if rel == broken {
			rel = fixed
			changed = true
		}
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		next = append(next, rel)
	}
	sort.Strings(next)
	page.Related = next
	return changed
}

func pageReferencesSlug(page *Page, slug string) bool {
	if strings.Contains(page.Body, "[["+slug+"]]") {
		return true
	}
	return hasString(page.Related, slug)
}

func stripAutoSourcePreview(page *Page) bool {
	if page == nil || !hasString(page.Tags, "source") || len(page.Sources) == 0 {
		return false
	}
	marker := "\n## Preview\n"
	idx := strings.Index(page.Body, marker)
	if idx < 0 {
		return false
	}
	page.Body = strings.TrimRight(page.Body[:idx], " \n\t") + "\n"
	return true
}

func appendUniqueSorted(values []string, value string) []string {
	if value == "" || hasString(values, value) {
		return values
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sortedStringSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
