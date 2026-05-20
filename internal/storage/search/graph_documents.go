package search

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

const graphNodeBodyLimit = 700

type indexedWikiPage struct {
	Slug          string
	Title         string
	Body          string
	Category      string
	Tags          []string
	Related       []string
	Sources       []string
	Outbound      []string
	Updated       string
	Created       string
	SchemaVersion int
	PromptVersion string
	Unversioned   bool
}

func parseIndexedWikiPage(slug, ext string, data []byte) (indexedWikiPage, error) {
	return parseIndexedWikiPageOpts(slug, ext, data, false)
}

// parseIndexedWikiPageOpts is parseIndexedWikiPage with a confidence filter.
// includeAmbiguous=true surfaces AMBIGUOUS edges (e.g. for the review queue).
func parseIndexedWikiPageOpts(slug, ext string, data []byte, includeAmbiguous bool) (indexedWikiPage, error) {
	var page *wiki.Page
	var err error
	if ext == ".md" {
		page, err = wiki.ParseMD(data)
	} else {
		page, err = wiki.ParseYAML(data)
	}
	if err != nil {
		return indexedWikiPage{}, err
	}
	if strings.TrimSpace(page.Title) == "" {
		page.Title = slug
	}
	relatedSlugs := filteredRelatedSlugs(page.Related, includeAmbiguous)
	outbound := mergeSlugs(wiki.ExtractWikiLinks(page.Body), relatedSlugs)
	return indexedWikiPage{
		Slug:          slug,
		Title:         page.Title,
		Body:          page.Body,
		Category:      page.Category,
		Tags:          cleanGraphValues(page.Tags),
		Related:       cleanGraphValues(relatedSlugs),
		Sources:       cleanGraphValues(page.Sources),
		Outbound:      outbound,
		Updated:       page.UpdatedAt,
		Created:       page.CreatedAt,
		SchemaVersion: page.SchemaVersion,
		PromptVersion: strings.TrimSpace(page.PromptVersion),
		Unversioned:   page.Unversioned,
	}, nil
}

// filteredRelatedSlugs returns related slugs filtered by confidence.
// AMBIGUOUS edges are excluded unless includeAmbiguous is true.
// INFERRED edges are always included (semantically "downweighted" but real).
func filteredRelatedSlugs(refs []wiki.RelatedRef, includeAmbiguous bool) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Confidence == "AMBIGUOUS" && !includeAmbiguous {
			continue
		}
		s := strings.TrimSpace(ref.Slug)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func buildGraphDocuments(pages map[string]indexedWikiPage) []Document {
	if len(pages) == 0 {
		return nil
	}
	backlinks := map[string][]string{}
	byCategory := map[string][]indexedWikiPage{}
	for slug, page := range pages {
		category := strings.TrimSpace(page.Category)
		if category == "" {
			category = "uncategorized"
		}
		byCategory[category] = append(byCategory[category], page)
		for _, target := range page.Outbound {
			if target == slug {
				continue
			}
			if _, ok := pages[target]; ok {
				backlinks[target] = append(backlinks[target], slug)
			}
		}
	}

	slugs := make([]string, 0, len(pages))
	for slug := range pages {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	docs := make([]Document, 0, len(pages)+len(byCategory)+1)
	for _, slug := range slugs {
		page := pages[slug]
		inbound := mergeSlugs(backlinks[slug], nil)
		content := graphNodeCard(page, inbound)
		meta := graphNodeMetadata(page)
		docs = append(docs, Document{
			ID:       "graph:node:" + slug,
			Content:  content,
			Metadata: meta,
		})
	}

	categories := make([]string, 0, len(byCategory))
	for category := range byCategory {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		pages := byCategory[category]
		sort.Slice(pages, func(i, j int) bool { return pages[i].Slug < pages[j].Slug })
		content := graphIndexCard(category, pages)
		docs = append(docs, Document{
			ID:      "graph:index:category:" + safeGraphID(category),
			Content: content,
			Metadata: map[string]string{
				"kind":  "graph_index",
				"slug":  "index:category:" + category,
				"title": "Index: " + category,
			},
		})
	}

	docs = append(docs, Document{
		ID:      "graph:index:all",
		Content: graphIndexOverview(categories, byCategory),
		Metadata: map[string]string{
			"kind":  "graph_index",
			"slug":  "index:all",
			"title": "Index: all wiki categories",
		},
	})
	return docs
}

func graphNodeCard(page indexedWikiPage, backlinks []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Graph node [[%s]]\n", page.Slug)
	fmt.Fprintf(&sb, "Title: %s\n", page.Title)
	writeGraphLine(&sb, "Category", page.Category)
	writeGraphList(&sb, "Tags", page.Tags)
	writeGraphList(&sb, "Sources", page.Sources)
	writeGraphList(&sb, "Outbound links", page.Outbound)
	writeGraphList(&sb, "Backlinks", backlinks)
	if page.SchemaVersion > 0 {
		fmt.Fprintf(&sb, "Schema version: %d\n", page.SchemaVersion)
	}
	writeGraphLine(&sb, "Prompt version", page.PromptVersion)
	writeGraphLine(&sb, "Created", page.Created)
	writeGraphLine(&sb, "Updated", page.Updated)
	if page.Unversioned {
		sb.WriteString("Unversioned: true\n")
	}
	if summary := truncateExcerpt(page.Body, graphNodeBodyLimit); summary != "" {
		fmt.Fprintf(&sb, "Summary: %s\n", summary)
	}
	return sb.String()
}

func graphNodeMetadata(page indexedWikiPage) map[string]string {
	meta := map[string]string{
		"kind":           "graph_node",
		"slug":           page.Slug,
		"title":          page.Title,
		"category":       strings.TrimSpace(page.Category),
		"tags":           strings.Join(page.Tags, ","),
		"related":        strings.Join(page.Related, ","),
		"sources":        strings.Join(page.Sources, ","),
		"schema_version": strconv.Itoa(page.SchemaVersion),
		"prompt_version": strings.TrimSpace(page.PromptVersion),
		"created_at":     strings.TrimSpace(page.Created),
		"updated_at":     strings.TrimSpace(page.Updated),
	}
	if page.Unversioned {
		meta["unversioned"] = "true"
	}
	for k, v := range meta {
		if v == "" || v == "0" && k == "schema_version" {
			delete(meta, k)
		}
	}
	return meta
}

func graphIndexCard(category string, pages []indexedWikiPage) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Graph index category: %s\n", category)
	fmt.Fprintf(&sb, "Page count: %d\n", len(pages))
	for _, page := range pages {
		fmt.Fprintf(&sb, "- [[%s]] %s", page.Slug, page.Title)
		if len(page.Tags) > 0 {
			fmt.Fprintf(&sb, " tags=%s", strings.Join(page.Tags, ", "))
		}
		if len(page.Outbound) > 0 {
			fmt.Fprintf(&sb, " links=%s", strings.Join(page.Outbound, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func graphIndexOverview(categories []string, byCategory map[string][]indexedWikiPage) string {
	var sb strings.Builder
	sb.WriteString("Graph index overview\n")
	total := 0
	for _, category := range categories {
		total += len(byCategory[category])
	}
	fmt.Fprintf(&sb, "Total pages: %d\n", total)
	for _, category := range categories {
		fmt.Fprintf(&sb, "- %s: %d page(s)\n", category, len(byCategory[category]))
	}
	return sb.String()
}

func writeGraphLine(sb *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		fmt.Fprintf(sb, "%s: %s\n", label, value)
	}
}

func writeGraphList(sb *strings.Builder, label string, values []string) {
	values = cleanGraphValues(values)
	if len(values) > 0 {
		fmt.Fprintf(sb, "%s: %s\n", label, strings.Join(values, ", "))
	}
}

func mergeSlugs(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, values := range [][]string{a, b} {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func cleanGraphValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func safeGraphID(value string) string {
	id := wiki.Slug(value)
	if id == "" {
		return "uncategorized"
	}
	return id
}
