package wiki

import (
	"context"
	"fmt"
	"strings"
	"time"
)

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
		pageTitle := slug
		if page := known[slug]; page != nil && page.Title != "" {
			pageTitle = page.Title
		}
		fmt.Fprintf(&body, "- [[%s]] %s\n", slug, pageTitle)
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
