package agui

// voice_speech_text.go converts an assistant answer's Markdown into speakable plain
// text for POST /api/tts (operator directive 2026-07-23: reading raw Markdown syntax
// aloud — asterisks, pipes, fences, URLs — makes the TTS "sound like a robot").
// The stripping is purely mechanical syntax removal, applied server-side in handleTTS
// so every TTS consumer benefits and the D-05 rune cap counts CLEAN runes. Prose is
// preserved verbatim; only unreadable machinery is dropped: fenced code blocks
// (unspeakable — removed whole), bare URLs, image/link targets (labels are kept),
// emphasis/heading/quote/list/table markers, HTML tags.

import (
	"regexp"
	"strings"
)

var (
	speechFenceRe      = regexp.MustCompile("(?s)(?:```|~~~).*?(?:```|~~~)")
	speechInlineCodeRe = regexp.MustCompile("`([^`]*)`")
	speechImageRe      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	speechLinkRe       = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	speechAutoLinkRe   = regexp.MustCompile(`<https?://[^>]+>`)
	speechBareURLRe    = regexp.MustCompile(`https?://\S+`)
	speechHTMLTagRe    = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	speechHeadingRe    = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	speechQuoteRe      = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)
	speechBulletRe     = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|\d{1,3}[.)])[ \t]+(?:\[[ xX]\][ \t]+)?`)
	speechHRuleRe      = regexp.MustCompile(`(?m)^[ \t]*(?:-{3,}|\*{3,}|_{3,})[ \t]*$`)
	speechTableSepRe   = regexp.MustCompile(`(?m)^[ \t]*\|?[ \t]*:?-{2,}.*\|.*$`)
	speechBoldRe       = regexp.MustCompile(`\*\*|__|~~`)
	speechStarRe       = regexp.MustCompile(`\*`)
	speechUnderWordRe  = regexp.MustCompile(`\b_([^_\n]+)_\b`)
	speechManySpaceRe  = regexp.MustCompile(`[ \t]{2,}`)
	speechLineEdgeRe   = regexp.MustCompile(`(?m)^[ \t]+|[ \t]+$`)
	speechManyLineRe   = regexp.MustCompile(`\n{3,}`)
)

// speechText strips Markdown syntax from text, returning clean speakable prose.
// Order is load-bearing: multiline constructs (fences, tables) before line markers,
// line markers before inline spans, whitespace collapse last.
func speechText(text string) string {
	t := speechFenceRe.ReplaceAllString(text, " ")
	t = speechTableSepRe.ReplaceAllString(t, "")
	t = speechHRuleRe.ReplaceAllString(t, "")
	t = speechHeadingRe.ReplaceAllString(t, "")
	t = speechQuoteRe.ReplaceAllString(t, "")
	t = speechBulletRe.ReplaceAllString(t, "")
	t = speechImageRe.ReplaceAllString(t, "$1")
	t = speechLinkRe.ReplaceAllString(t, "$1")
	t = speechAutoLinkRe.ReplaceAllString(t, "")
	t = speechBareURLRe.ReplaceAllString(t, "")
	t = speechInlineCodeRe.ReplaceAllString(t, "$1")
	t = speechBoldRe.ReplaceAllString(t, "")
	t = speechStarRe.ReplaceAllString(t, "")
	t = speechUnderWordRe.ReplaceAllString(t, "$1")
	t = speechHTMLTagRe.ReplaceAllString(t, "")
	// Table cell pipes read as prose pauses: "a | b" → "a, b"; leading/trailing
	// pipes vanish. Applied after the separator rows are gone.
	t = strings.ReplaceAll(t, " | ", ", ")
	t = strings.ReplaceAll(t, "|", " ")
	t = speechManySpaceRe.ReplaceAllString(t, " ")
	t = speechLineEdgeRe.ReplaceAllString(t, "")
	t = speechManyLineRe.ReplaceAllString(t, "\n\n")
	return strings.TrimSpace(t)
}
