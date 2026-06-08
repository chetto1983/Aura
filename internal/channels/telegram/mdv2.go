// Package telegram is the Telegram channel (Phase 13 / Slice 9b, UX-02). This
// file is the entity-aware MarkdownV2 escaper, locked in-tree by amendment #4
// (the supply-chain alternative telegramify-markdown-go was rejected). The spec
// is binding from the spike (.claude/skills/spike-findings-Aura/references/
// telegram-channel.md, "MarkdownV2 discipline", Pitfall #18), not a code analog.
package telegram

import "strings"

// reservedOutsideEntity is the MarkdownV2 reserved set that must be
// backslash-escaped when it appears OUTSIDE an intended entity (bold/italic/
// code/pre span). One naked occurrence anywhere 400s the whole send with
// "can't parse entities" — the rejection is all-or-nothing per message.
const reservedOutsideEntity = "_*[]()~`>#+-=|{}.!\\"

// EscapeMarkdownV2 escapes Telegram MarkdownV2 reserved characters OUTSIDE
// intended entities only — it NEVER whole-string escapes (that would neutralize
// the bot's own bold/italic/code formatting; the 017 throwaway escaper is the
// negative example). It tracks fence state so reserved characters are handled
// per region:
//
//   - Outside any fence: the full reserved set is escaped (a naked '-' / '.' /
//     '(' etc. gets a leading backslash).
//   - Inside a ```pre``` block or a `code` span: only backtick (the delimiter)
//     and backslash are reserved; pipes, dashes, dots flow through unescaped so
//     table payloads render intact (verified live, spike 018a/b).
//
// The fence delimiters themselves (``` and `) pass through verbatim. The output
// is always a well-formed MarkdownV2 entity stream: even when the input has an
// unterminated fence, the in-fence content is emitted with only backtick/
// backslash escaped, so the stream cannot 400. The renderer (plan 13-05) owns
// the plain-text fallback: if a MarkdownV2 send still 400s, it resends WITHOUT
// ParseMode (see PlainTextFallback for the contract helper).
func EscapeMarkdownV2(s string) string {
	type mode int
	const (
		outside mode = iota
		inPre
		inInline
	)

	var b strings.Builder
	b.Grow(len(s) + len(s)/8)

	runes := []rune(s)
	m := outside
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch m {
		case outside:
			switch {
			case c == '`' && i+2 < len(runes) && runes[i+1] == '`' && runes[i+2] == '`':
				b.WriteString("```")
				i += 2
				m = inPre
			case c == '`':
				b.WriteByte('`')
				m = inInline
			case strings.ContainsRune(reservedOutsideEntity, c):
				b.WriteByte('\\')
				b.WriteRune(c)
			default:
				b.WriteRune(c)
			}
		case inPre:
			switch {
			case c == '`' && i+2 < len(runes) && runes[i+1] == '`' && runes[i+2] == '`':
				b.WriteString("```")
				i += 2
				m = outside
			case c == '\\':
				b.WriteString("\\\\")
			case c == '`':
				// A lone backtick inside a pre block is reserved — escape it so
				// it does not prematurely close (or corrupt) the entity stream.
				b.WriteString("\\`")
			default:
				b.WriteRune(c)
			}
		case inInline:
			switch c {
			case '`':
				b.WriteByte('`')
				m = outside
			case '\\':
				b.WriteString("\\\\")
			default:
				b.WriteRune(c)
			}
		}
	}

	// An unterminated inline span or pre block leaves the parser expecting a
	// closer. Telegram treats an unclosed entity as a 400 the same as a naked
	// reserved char, so close it deterministically. The closer matches the open
	// delimiter the model emitted.
	switch m {
	case inInline:
		b.WriteByte('`')
	case inPre:
		b.WriteString("```")
	}

	return b.String()
}

// PlainTextFallback is the documented contract the renderer uses when a
// MarkdownV2 send 400s despite escaping: resend the ORIGINAL (un-escaped) text
// without ParseMode. It is the identity function here — the helper exists so the
// "fallback = original plain text, no escaping, no ParseMode" decision lives in
// this file next to the escaper it complements, rather than as an implicit rule
// scattered in the renderer (plan 13-05).
func PlainTextFallback(original string) string {
	return original
}
