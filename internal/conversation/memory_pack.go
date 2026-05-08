package conversation

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const DefaultMemoryPackMaxBytes = 10 * 1024

type MemoryPackInput struct {
	UserText      string
	SearchContext string
	GraphContext  string
	RecentLog     string
	MaxBytes      int
}

func ComposeMemoryPack(input MemoryPackInput) string {
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMemoryPackMaxBytes
	}

	searchContext := strings.TrimSpace(input.SearchContext)
	graphContext := strings.TrimSpace(input.GraphContext)
	recentLog := strings.TrimSpace(input.RecentLog)
	if searchContext == "" && graphContext == "" && recentLog == "" {
		return ""
	}

	var sections []string
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
		sections = append(sections, "### Search Context\n"+truncateBytes(searchContext, maxBytes/3))
	}
	if graphContext != "" {
		sections = append(sections, "### Graph Context\n"+stripHeading(truncateBytes(graphContext, maxBytes/3), "Wiki Graph Context"))
	}
	if recentLog != "" {
		sections = append(sections, "### Recent Wiki Log\n"+truncateBytes(recentLog, maxBytes/4))
	}

	pack := "## Memory Pack\n\n" + strings.Join(sections, "\n\n")
	return truncateBytes(pack, maxBytes)
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

func stripHeading(text, heading string) string {
	text = strings.TrimSpace(text)
	prefix := "# " + heading
	if strings.HasPrefix(text, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	return text
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
