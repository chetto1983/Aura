package telegram

import (
	"regexp"
	"strings"
	"unicode"
)

// TTS speech sanitization: the agent's answer is MarkdownV2-ish prose with emoji
// (the status pane + content use 😊/✅/💭 glyphs and **bold**/`code`/[links]).
// Kokoro would voice those literally ("asterisk asterisk", an emoji name, a raw
// URL), so before synthesis the markup is unwrapped to its inner text and emoji +
// other non-speech symbol runes are dropped. The on-screen text is untouched —
// only the spoken copy is cleaned.
var (
	ttsCodeFence    = regexp.MustCompile("(?s)```[a-zA-Z0-9]*\\n?(.*?)```")
	ttsInlineCode   = regexp.MustCompile("`([^`]*)`")
	ttsMdLink       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	ttsHeading      = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]*`)
	ttsBlockquote   = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)
	ttsListBullet   = regexp.MustCompile(`(?m)^[ \t]*[-*+][ \t]+`)
	ttsEmphasis     = regexp.MustCompile(`\*\*|__|~~|\*|_`)
	ttsMultiSpace   = regexp.MustCompile(`[ \t]{2,}`)
	ttsMultiNewline = regexp.MustCompile(`\n{3,}`)
)

// sanitizeForSpeech reduces an answer to clean spoken prose: markdown markup is
// unwrapped (code fences/inline → inner, [text](url) → text, heading/quote/list
// markers + emphasis runs dropped) and emoji / symbol runes (plus the ZWJ +
// variation-selector glue that binds emoji sequences) are removed. Whitespace is
// collapsed so the synth gets natural pauses, not a wall of blank lines. Returns
// "" when nothing speakable remains (e.g. an emoji-only reply) so the caller can
// skip the voice note rather than synthesize silence.
func sanitizeForSpeech(s string) string {
	s = ttsCodeFence.ReplaceAllString(s, "$1")
	s = ttsInlineCode.ReplaceAllString(s, "$1")
	s = ttsMdLink.ReplaceAllString(s, "$1")
	s = ttsHeading.ReplaceAllString(s, "")
	s = ttsBlockquote.ReplaceAllString(s, "")
	s = ttsListBullet.ReplaceAllString(s, "")
	s = ttsEmphasis.ReplaceAllString(s, "")
	s = stripNonSpeechRunes(s)
	s = ttsMultiSpace.ReplaceAllString(s, " ")
	// Trim per-line whitespace left where list/quote markers + glyphs were removed
	// (preserves blank lines so the paragraph collapse below still works).
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	s = strings.Join(lines, "\n")
	s = ttsMultiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// stripNonSpeechRunes drops emoji and symbol runes (categories So/Sk, which hold
// 😊/✅/🟡 and skin-tone modifiers) plus the ZWJ + variation selectors that glue
// emoji sequences, while keeping letters, digits, punctuation and whitespace.
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
