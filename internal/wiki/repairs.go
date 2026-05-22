package wiki

import (
	"sort"
	"strings"
)

type graphRef struct {
	target   string
	location string
}

func organizeMemoryPages(pages []memoryPage) (map[string]*Page, map[string][]string) {
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
	return known, byCategory
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
	for _, rref := range page.Related {
		rel := strings.TrimSpace(rref.Slug)
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

func replaceRelated(page *Page, broken, fixed string) bool {
	changed := false
	next := make([]RelatedRef, 0, len(page.Related))
	seen := make(map[string]bool)
	for _, ref := range page.Related {
		s := ref.Slug
		if s == broken {
			s = fixed
			changed = true
		}
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		next = append(next, RelatedRef{Slug: s, Confidence: ref.Confidence})
	}
	RelatedSortBySlugs(next)
	page.Related = next
	return changed
}

func pageReferencesSlug(page *Page, slug string) bool {
	if strings.Contains(page.Body, "[["+slug+"]]") {
		return true
	}
	return RelatedContainsSlug(page.Related, slug)
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

func firstSemanticHeading(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if text == "" {
			continue
		}
		lower := strings.ToLower(text)
		if strings.HasPrefix(lower, "source ocr:") ||
			strings.HasPrefix(lower, "source extract:") ||
			strings.HasPrefix(lower, "source:") ||
			strings.HasPrefix(lower, "page ") {
			continue
		}
		return strings.Trim(text, " .\t")
	}
	return ""
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
