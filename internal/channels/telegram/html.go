package telegram

import "strings"

import tgmd2html "github.com/PaulSonOfLars/gotg_md2html"

// RenderTelegramHTML converts the model's Markdown-ish answer to the subset of
// HTML accepted by the Telegram Bot API. The converter escapes raw HTML before
// adding Telegram-safe entity tags, so user/model text like "<script>" remains
// text, not markup.
func RenderTelegramHTML(s string) string {
	return tgmd2html.MD2HTMLV2(normalizeTelegramBlockMarkdown(s))
}

func normalizeTelegramBlockMarkdown(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if isMarkdownThematicBreak(line) {
			lines[i] = ""
			continue
		}
		if heading, ok := normalizeMarkdownHeading(line); ok {
			lines[i] = heading
		}
	}
	return strings.Join(lines, "\n")
}

func normalizeMarkdownHeading(line string) (string, bool) {
	trimmedLeft := strings.TrimLeft(line, " ")
	if len(line)-len(trimmedLeft) > 3 || !strings.HasPrefix(trimmedLeft, "#") {
		return line, false
	}
	hashes := 0
	for hashes < len(trimmedLeft) && trimmedLeft[hashes] == '#' {
		hashes++
	}
	if hashes > 6 {
		return line, false
	}
	if hashes < len(trimmedLeft) && trimmedLeft[hashes] != ' ' && trimmedLeft[hashes] != '\t' {
		return line, false
	}
	text := stripClosingHeadingHashes(strings.TrimSpace(trimmedLeft[hashes:]))
	if text == "" {
		return "", true
	}
	return "**" + text + "**", true
}

func stripClosingHeadingHashes(s string) string {
	s = strings.TrimRight(s, " \t")
	last := len(s) - 1
	if last < 0 || s[last] != '#' {
		return s
	}
	start := last
	for start >= 0 && s[start] == '#' {
		start--
	}
	if start < 0 || (s[start] != ' ' && s[start] != '\t') {
		return s
	}
	return strings.TrimRight(s[:start], " \t")
}

func isMarkdownThematicBreak(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	marker := trimmed[0]
	if marker != '-' && marker != '*' && marker != '_' {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != marker {
			return false
		}
	}
	return true
}
