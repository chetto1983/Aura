package multimodal

// speech_text.go is the ONE place that turns an assistant answer into something a
// speech synthesizer should read. Every channel that speaks goes through it.
//
// It exists because there were two of these, and they had drifted. The web lane
// (internal/agui) stripped Markdown, tables, HTML and URLs but read emoji aloud; the
// Telegram lane (internal/channels/telegram) stripped emoji but read bare URLs aloud,
// kept code blocks, missed ordered lists, and removed every underscore — which also
// ate the one inside user_id. Same product, same models, two different answers to
// "what should the voice say", and no way to fix one without remembering the other.
// Measured 2026-09-19 by diffing the two implementations rule by rule.
//
// The union is what ships: the web lane's syntax coverage plus the Telegram lane's
// emoji stripping. Two behaviours therefore CHANGE for Telegram and are meant to:
// a bare URL is no longer read out, and a fenced code block is dropped whole rather
// than dictated line by line. Both were the web lane's deliberate choices (operator
// directive 2026-07-23: reading syntax aloud "sounds like a robot"), and a code block
// read aloud is noise in any channel.
//
// The stripping is purely mechanical: prose is preserved verbatim, only unspeakable
// machinery is removed. It lives here, beside the TTS client, because this package is
// already "the SINGLE shared client for Aura's media sidecars" (doc.go) and the text
// handed to the synthesizer is part of that wire contract.

import (
	"regexp"
	"strings"
	"unicode"
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

// SpeechText strips markup from text and returns clean speakable prose, or "" when
// nothing speakable remains (an emoji-only reply, a code-only answer) so the caller
// can skip synthesis rather than buy silence.
//
// Order is load-bearing: multiline constructs (fences, tables) before line markers,
// line markers before inline spans, non-speech runes before the whitespace collapse
// that closes the gaps they leave.
func SpeechText(text string) string {
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
	// `\b_(…)_\b` rather than a blanket underscore strip: the blanket form is what
	// turned user_id into userid in the Telegram lane.
	t = speechUnderWordRe.ReplaceAllString(t, "$1")
	t = speechHTMLTagRe.ReplaceAllString(t, "")
	// Table cell pipes read as prose pauses: "a | b" → "a, b"; leading/trailing pipes
	// vanish. Applied after the separator rows are gone.
	t = strings.ReplaceAll(t, " | ", ", ")
	t = strings.ReplaceAll(t, "|", " ")
	t = stripNonSpeechRunes(t)
	t = speechManySpaceRe.ReplaceAllString(t, " ")
	t = speechLineEdgeRe.ReplaceAllString(t, "")
	t = speechManyLineRe.ReplaceAllString(t, "\n\n")
	return strings.TrimSpace(t)
}

// PrepareSpeech is what a TTS caller actually wants: the speakable text, rune-capped,
// plus whether the cap bit. An empty result means there was nothing to say — the web
// lane answers 400, the Telegram lane sends no voice note. A non-positive maxChars
// disables the cap, which is how a channel with no ceiling of its own asks for one
// later in a single line instead of a new code path.
func PrepareSpeech(text string, maxChars int) (spoken string, truncated bool) {
	t := SpeechText(text)
	if t == "" || maxChars <= 0 {
		return t, false
	}
	// Runes, not bytes, so a multi-byte character is never split in half.
	runes := []rune(t)
	if len(runes) <= maxChars {
		return t, false
	}
	return string(runes[:maxChars]), true
}

// stripNonSpeechRunes drops emoji and symbol runes (categories So/Sk, which hold
// 😊/✅/🟡 and skin-tone modifiers) plus the ZWJ + variation selectors that glue emoji
// sequences, while keeping letters, digits, punctuation and whitespace. Math symbols
// (Sm) are deliberately NOT touched: that category holds +, <, = and ~, which are read
// as words a listener expects.
func stripNonSpeechRunes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == ' ':
			b.WriteRune(r)
		case r == 0x200D, r >= 0xFE00 && r <= 0xFE0F: // ZWJ + variation selectors
			continue
		case unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r): // emoji + symbol modifiers
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
