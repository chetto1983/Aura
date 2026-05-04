package search

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aura/aura/internal/wiki"
	"github.com/philippgille/chromem-go"
)

const graphNodeBodyLimit = 700

type indexedWikiPage struct {
	Slug     string
	Title    string
	Body     string
	Category string
	Tags     []string
	Related  []string
	Sources  []string
	Outbound []string
	Updated  string
}

func parseIndexedWikiPage(slug, ext string, data []byte) (indexedWikiPage, error) {
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
	outbound := mergeSlugs(wiki.ExtractWikiLinks(page.Body), page.Related)
	return indexedWikiPage{
		Slug:     slug,
		Title:    page.Title,
		Body:     page.Body,
		Category: page.Category,
		Tags:     cleanGraphValues(page.Tags),
		Related:  cleanGraphValues(page.Related),
		Sources:  cleanGraphValues(page.Sources),
		Outbound: outbound,
		Updated:  page.UpdatedAt,
	}, nil
}

func buildGraphDocuments(pages map[string]indexedWikiPage) []chromem.Document {
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

	docs := make([]chromem.Document, 0, len(pages)+len(byCategory)+1)
	for _, slug := range slugs {
		page := pages[slug]
		inbound := mergeSlugs(backlinks[slug], nil)
		content := graphNodeCard(page, inbound)
		docs = append(docs, chromem.Document{
			ID:      "graph:node:" + slug,
			Content: content,
			Metadata: map[string]string{
				"kind":  "graph_node",
				"slug":  slug,
				"title": page.Title,
			},
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
		docs = append(docs, chromem.Document{
			ID:      "graph:index:category:" + safeGraphID(category),
			Content: content,
			Metadata: map[string]string{
				"kind":  "graph_index",
				"slug":  "index:category:" + category,
				"title": "Index: " + category,
			},
		})
	}

	docs = append(docs, chromem.Document{
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
	writeGraphLine(&sb, "Updated", page.Updated)
	if summary := truncateExcerpt(page.Body, graphNodeBodyLimit); summary != "" {
		fmt.Fprintf(&sb, "Summary: %s\n", summary)
	}
	return sb.String()
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
