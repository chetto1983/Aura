package profile

import (
	"fmt"
	"strings"
)

const MaxAgentMDBytes = 32768

// AgentContent is the structured form rendered into Agent.md.
type AgentContent struct {
	Facts              []string
	Preferences        []string
	Context            []string
	CustomInstructions []string
}

// RenderAgentMD renders Agent.md with a stable section order.
func RenderAgentMD(c AgentContent) string {
	var b strings.Builder
	b.WriteString("# Agent.md\n")
	writeSection(&b, "Facts", c.Facts)
	writeSection(&b, "Preferences", c.Preferences)
	writeSection(&b, "Context", c.Context)
	writeSection(&b, "Custom Instructions", c.CustomInstructions)
	return b.String()
}

// AddFact inserts a bullet into the Facts section without duplicating it.
func AddFact(agentMD, fact string) (string, bool, error) {
	fact = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(fact), "-"))
	if fact == "" {
		return "", false, fmt.Errorf("fact cannot be empty")
	}
	bullet := "- " + fact
	lines := splitLines(agentMD)
	if len(lines) == 0 {
		out := RenderAgentMD(AgentContent{Facts: []string{fact}})
		return out, true, checkAgentSize(out)
	}
	factsAt := sectionIndex(lines, "Facts")
	if factsAt == -1 {
		insertAt := 1
		if strings.TrimSpace(lines[0]) != "# Agent.md" {
			insertAt = 0
		}
		lines = insertLines(lines, insertAt, "", "## Facts", bullet)
		out := strings.Join(lines, "\n")
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out, true, checkAgentSize(out)
	}
	end := nextSectionIndex(lines, factsAt+1)
	for _, line := range lines[factsAt+1 : end] {
		if strings.TrimSpace(line) == bullet {
			return agentMD, false, nil
		}
	}
	insertAt := end
	lines = insertLines(lines, insertAt, bullet)
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, true, checkAgentSize(out)
}

func writeSection(b *strings.Builder, name string, items []string) {
	b.WriteString("\n## ")
	b.WriteString(name)
	b.WriteByte('\n')
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(item), "-"))
		if item == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteByte('\n')
	}
}

func checkAgentSize(md string) error {
	if len([]byte(md)) > MaxAgentMDBytes {
		return fmt.Errorf("Agent.md exceeds %d bytes", MaxAgentMDBytes)
	}
	return nil
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func insertLines(lines []string, at int, additions ...string) []string {
	out := make([]string, 0, len(lines)+len(additions))
	out = append(out, lines[:at]...)
	out = append(out, additions...)
	out = append(out, lines[at:]...)
	return out
}

func sectionIndex(lines []string, title string) int {
	want := "## " + title
	for i, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), want) {
			return i
		}
	}
	return -1
}

func nextSectionIndex(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			return i
		}
	}
	return len(lines)
}
