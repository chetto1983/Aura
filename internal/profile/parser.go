package profile

import (
	"fmt"
	"strings"
)

// Section is a parsed Agent.md H2 section for display.
type Section struct {
	Title string
	Items []string
	Body  []string
}

// ParseSections returns Agent.md H2 sections in file order.
func ParseSections(agentMD string) []Section {
	lines := splitLines(agentMD)
	sections := []Section{}
	current := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			sections = append(sections, Section{Title: title})
			current = len(sections) - 1
			continue
		}
		if current == -1 || trimmed == "" || strings.HasPrefix(trimmed, "# ") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			sections[current].Items = append(sections[current].Items, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		sections[current].Body = append(sections[current].Body, trimmed)
	}
	return sections
}

// FormatSectionTree renders a compact ASCII tree for CLI display.
func FormatSectionTree(agentMD string) string {
	sections := ParseSections(agentMD)
	var b strings.Builder
	b.WriteString("Agent.md\n")
	for _, section := range sections {
		fmt.Fprintf(&b, "- %s\n", section.Title)
		for _, item := range section.Items {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
		for _, line := range section.Body {
			fmt.Fprintf(&b, "  - %s\n", line)
		}
	}
	return b.String()
}
