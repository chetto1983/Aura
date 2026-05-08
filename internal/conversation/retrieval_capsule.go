package conversation

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const DefaultRetrievalCapsuleMaxBytes = 10 * 1024

type RetrievalCapsuleInput struct {
	UserText      string
	SearchContext string
	Route         string
	MaxBytes      int
}

func ComposeRetrievalCapsule(input RetrievalCapsuleInput) string {
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultRetrievalCapsuleMaxBytes
	}

	searchContext := strings.TrimSpace(input.SearchContext)
	if searchContext == "" {
		return ""
	}

	var sections []string
	route := strings.TrimSpace(input.Route)
	if route != "" {
		sections = append(sections, "### Route\n"+truncateBytes(route, 80))
	}
	if userText := strings.TrimSpace(input.UserText); userText != "" {
		sections = append(sections, "### User Request\n"+truncateBytes(userText, 500))
	}
	if pages := relevantPageList(searchContext); len(pages) > 0 {
		var sb strings.Builder
		sb.WriteString("### Relevant Pages\n")
		for _, page := range pages {
			fmt.Fprintf(&sb, "- [[%s]]\n", page)
		}
		sections = append(sections, strings.TrimRight(sb.String(), "\n"))
	}
	if searchContext != "" {
		sections = append(sections, "### Evidence\n"+truncateBytes(searchContext, maxBytes/2))
	}

	capsule := "## Retrieval Capsule\n\n" + strings.Join(sections, "\n\n")
	return truncateBytes(capsule, maxBytes)
}

var wikiLinkRE = regexp.MustCompile(`\[\[([a-z0-9][a-z0-9-]{0,199})\]\]`)

func relevantPageList(searchContext string) []string {
	matches := wikiLinkRE.FindAllStringSubmatch(searchContext, -1)
	seen := map[string]bool{}
	pages := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		slug := strings.TrimSpace(match[1])
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		pages = append(pages, slug)
	}
	sort.Strings(pages)
	if len(pages) > 8 {
		return pages[:8]
	}
	return pages
}

func truncateBytes(text string, maxBytes int) string {
	text = strings.TrimSpace(text)
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	if maxBytes <= len("...") {
		return text[:maxBytes]
	}
	return strings.TrimSpace(text[:maxBytes-len("...")]) + "..."
}
