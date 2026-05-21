package ingest

import (
	"fmt"
	"regexp"
	"strings"

	source "github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/wiki"
)

const structuredPromptVersion = "ingest_v3"

var structuredHeadingRe = regexp.MustCompile(`(?m)^###\s+(.+?)\s*$`)

type structuredExtract struct {
	Sections []structuredSection
}

type structuredSection struct {
	Index   int
	Heading string
	Body    string
	Title   string
	Slug    string
}

func parseStructuredExtract(raw, parentTitle, parentSlug string) *structuredExtract {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	frontmatter, ok := leadingFrontmatter(normalized)
	if !ok || !hasStructuredFrontmatter(frontmatter) {
		return nil
	}
	matches := structuredHeadingRe.FindAllStringSubmatchIndex(normalized, -1)
	if len(matches) < 3 {
		return nil
	}
	sections := make([]structuredSection, 0, len(matches))
	for i, match := range matches {
		heading := strings.TrimSpace(normalized[match[2]:match[3]])
		if heading == "" {
			heading = fmt.Sprintf("Section %d", i+1)
		}
		bodyStart := match[1]
		bodyEnd := len(normalized)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		body := strings.TrimSpace(normalized[bodyStart:bodyEnd])
		title := fmt.Sprintf("%s - %03d %s", parentTitle, i+1, heading)
		sections = append(sections, structuredSection{
			Index:   i + 1,
			Heading: heading,
			Body:    body,
			Title:   title,
			Slug:    structuredSectionSlug(parentSlug, i+1, heading),
		})
	}
	return &structuredExtract{Sections: sections}
}

func leadingFrontmatter(markdown string) (string, bool) {
	body := strings.TrimPrefix(markdown, "\ufeff")
	if !strings.HasPrefix(body, "---\n") {
		return "", false
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func hasStructuredFrontmatter(frontmatter string) bool {
	for _, marker := range []string{
		"sheet_row_counts:",
		"slide_count:",
		"chapter_count:",
		"heading_counts:",
		"section_count:",
	} {
		if strings.Contains(frontmatter, marker) {
			return true
		}
	}
	return false
}

func structuredSectionSlug(parentSlug string, index int, heading string) string {
	prefixBase := parentSlug
	if len(prefixBase) > 60 {
		prefixBase = strings.Trim(prefixBase[:60], "-")
	}
	headingSlug := wiki.Slug(heading)
	prefix := fmt.Sprintf("%s-%03d-", prefixBase, index)
	maxHeading := 100 - len(prefix)
	if maxHeading < len("section") {
		return strings.Trim(fmt.Sprintf("%s%03d", prefixBase, index), "-")
	}
	if len(headingSlug) > maxHeading {
		headingSlug = strings.Trim(headingSlug[:maxHeading], "-")
		if headingSlug == "" {
			headingSlug = "section"
		}
	}
	return prefix + headingSlug
}

func buildStructuredSummaryBody(src *source.Source, markdownName string, delta ExtractionDelta, sections []structuredSection) string {
	body := strings.TrimRight(buildSummaryBody(src, markdownName, delta), "\n")
	var sb strings.Builder
	sb.WriteString(body)
	sb.WriteString("\n\n## Materialized Sections\n\n")
	sb.WriteString("Structured extraction was materialized into dedicated wiki pages for direct retrieval.\n\n")
	for _, section := range sections {
		fmt.Fprintf(&sb, "- [[%s]] - %s\n", section.Slug, section.Heading)
	}
	return sb.String()
}

func buildStructuredSectionPage(src *source.Source, parentSlug string, section structuredSection, now string) *wiki.Page {
	body := structuredSectionBody(src, parentSlug, section)
	page := &wiki.Page{
		Title:         section.Title,
		Body:          body,
		Category:      "source-section",
		Tags:          []string{"source-section", string(src.Kind)},
		Sources:       []string{"source:" + src.ID},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: structuredPromptVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if len(parentSlug) <= 100 {
		page.Related = wiki.RelatedFromSlugs([]string{parentSlug})
	}
	return page
}

func structuredSectionBody(src *source.Source, parentSlug string, section structuredSection) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", section.Heading)
	fmt.Fprintf(&sb, "Parent source page: [[%s]].\n\n", parentSlug)
	sb.WriteString("## Section Metadata\n\n")
	fmt.Fprintf(&sb, "- Source ID: `%s`\n", src.ID)
	fmt.Fprintf(&sb, "- Parent slug: `%s`\n", parentSlug)
	fmt.Fprintf(&sb, "- Heading: %s\n", section.Heading)
	fmt.Fprintf(&sb, "- Heading index: %d\n", section.Index)
	sb.WriteString("\n## Extracted Section\n\n")
	if strings.TrimSpace(section.Body) == "" {
		sb.WriteString("_No extracted body text._\n")
	} else {
		sb.WriteString(section.Body)
		if !strings.HasSuffix(section.Body, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
