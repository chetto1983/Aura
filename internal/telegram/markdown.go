package telegram

import (
	"regexp"
	"strings"
)

// renderForTelegram converts LLM-emitted Markdown into the small subset
// of HTML that Telegram's "HTML" parse mode accepts. Telegram does not
// support headings, lists, or tables; we degrade headings to bold lines
// and bullets to a "•" prefix so the output is still readable.
//
// Supported (per https://core.telegram.org/bots/api#html-style):
//
//	<b> <strong> <i> <em> <u> <ins> <s> <strike> <del>
//	<a href="url"> <code> <pre> <blockquote>
//
// Conversion rules (applied in this order so escaping doesn't trample
// our own tags):
//  1. Extract fenced code blocks ``` and inline code `…` to placeholders.
//  2. Per line: convert headings (# / ## / ###) and bullets (- / *).
//  3. Convert inline runs: bold (** or __), italic (* or _),
//     strike (~~), link [text](url).
//  4. Escape remaining <, >, & in plain text.
//  5. Restore code placeholders with HTML-escaped contents wrapped in
//     <code> or <pre>.
//
// Best-effort: the converter favors not crashing over perfect parsing.
// Pathologically malformed input may emit visible markdown chars but
// never broken HTML that Telegram would reject.
func renderForTelegram(s string) string {
	if s == "" {
		return ""
	}

	// Step 1: extract code blocks first so their contents are immune to
	// the bold/italic/escaping passes below.
	type codeRun struct {
		body  string
		block bool // true => <pre>, false => <code>
	}
	var codes []codeRun
	placeholder := func(i int) string {
		// Two NULs make the marker unlikely to clash with model output.
		return "\x00CODE" + ltoa(i) + "\x00"
	}

	// Fenced ``` blocks: `\nfoo\n` style. Greedy across lines.
	fenced := regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\\n?(.*?)```")
	s = fenced.ReplaceAllStringFunc(s, func(match string) string {
		body := fenced.FindStringSubmatch(match)[1]
		body = strings.TrimRight(body, "\n")
		codes = append(codes, codeRun{body: body, block: true})
		return placeholder(len(codes) - 1)
	})

	// Inline `code`. Single backticks only, non-greedy.
	inline := regexp.MustCompile("`([^`\\n]+)`")
	s = inline.ReplaceAllStringFunc(s, func(match string) string {
		body := inline.FindStringSubmatch(match)[1]
		codes = append(codes, codeRun{body: body, block: false})
		return placeholder(len(codes) - 1)
	})

	// Step 2: per-line block-level conversion.
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		trim := strings.TrimLeft(line, " \t")
		switch {
		case strings.HasPrefix(trim, "### "):
			lines[i] = "<b>" + strings.TrimPrefix(trim, "### ") + "</b>"
		case strings.HasPrefix(trim, "## "):
			lines[i] = "<b>" + strings.TrimPrefix(trim, "## ") + "</b>"
		case strings.HasPrefix(trim, "# "):
			lines[i] = "<b>" + strings.TrimPrefix(trim, "# ") + "</b>"
		case strings.HasPrefix(trim, "- "):
			indent := line[:len(line)-len(trim)]
			lines[i] = indent + "• " + strings.TrimPrefix(trim, "- ")
		case strings.HasPrefix(trim, "* ") && !strings.HasPrefix(trim, "**"):
			indent := line[:len(line)-len(trim)]
			lines[i] = indent + "• " + strings.TrimPrefix(trim, "* ")
		case strings.HasPrefix(trim, "> "):
			// Telegram supports <blockquote>; convert per-line.
			lines[i] = "<blockquote>" + strings.TrimPrefix(trim, "> ") + "</blockquote>"
		}
	}
	s = strings.Join(lines, "\n")

	// Step 3: inline runs. Order matters — bold (**) before italic (*).
	s = boldRE.ReplaceAllString(s, "<b>$1</b>")
	s = boldUnderRE.ReplaceAllString(s, "<b>$1</b>")
	s = strikeRE.ReplaceAllString(s, "<s>$1</s>")
	s = italicStarRE.ReplaceAllString(s, "$1<i>$2</i>$3")
	s = italicUnderRE.ReplaceAllString(s, "$1<i>$2</i>$3")
	s = linkRE.ReplaceAllStringFunc(s, func(match string) string {
		parts := linkRE.FindStringSubmatch(match)
		// Only allow http(s)/tg URLs to avoid javascript:// injection if
		// the model misbehaves.
		url := parts[2]
		if !(strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "tg://")) {
			return match
		}
		return `<a href="` + escapeAttr(url) + `">` + parts[1] + `</a>`
	})

	// Step 4: escape remaining <, >, & in plain text. Our own tags use
	// only the limited Telegram set so a token-level pass that leaves
	// known tags alone is sufficient.
	s = escapeOutsideTags(s)

	// Step 5: restore code placeholders, escaping body chars.
	for i, code := range codes {
		body := htmlEscape(code.body)
		var rep string
		if code.block {
			rep = "<pre>" + body + "</pre>"
		} else {
			rep = "<code>" + body + "</code>"
		}
		s = strings.Replace(s, placeholder(i), rep, 1)
	}

	return s
}

var (
	boldRE        = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	boldUnderRE   = regexp.MustCompile(`__([^_\n]+?)__`)
	strikeRE      = regexp.MustCompile(`~~([^~\n]+?)~~`)
	italicStarRE  = regexp.MustCompile(`(^|[\s(])\*([^*\n]+?)\*([\s).,!?:;]|$)`)
	italicUnderRE = regexp.MustCompile(`(^|[\s(])_([^_\n]+?)_([\s).,!?:;]|$)`)
	linkRE        = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\n]+)\)`)
)

// htmlEscape escapes the three reserved chars Telegram HTML mode cares
// about. Single quotes are safe inside double-quoted attributes.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// escapeAttr escapes for inclusion in a double-quoted href attribute.
func escapeAttr(s string) string {
	s = htmlEscape(s)
	return strings.ReplaceAll(s, `"`, "&quot;")
}

// escapeOutsideTags walks the string and escapes <, >, & only when they
// are NOT part of one of our generated tags (<b>, </b>, <i>, </i>, <s>,
// </s>, <code>, </code>, <pre>, </pre>, <a href="…">, </a>,
// <blockquote>, </blockquote>). Anything else is treated as literal.
func escapeOutsideTags(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 16)
	for i := 0; i < len(s); {
		c := s[i]
		if c == '<' && matchesKnownTag(s[i:]) {
			// Find tag end and copy verbatim.
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				out.WriteString("&lt;")
				i++
				continue
			}
			out.WriteString(s[i : i+end+1])
			i += end + 1
			continue
		}
		switch c {
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '&':
			// Don't double-escape entities like &amp; that we wrote ourselves.
			if isEntityAt(s, i) {
				out.WriteByte('&')
			} else {
				out.WriteString("&amp;")
			}
		default:
			out.WriteByte(c)
		}
		i++
	}
	return out.String()
}

var knownTagPrefixes = []string{
	"<b>", "</b>",
	"<i>", "</i>",
	"<u>", "</u>",
	"<s>", "</s>",
	"<code>", "</code>",
	"<pre>", "</pre>",
	"<blockquote>", "</blockquote>",
	"<a href=\"", "</a>",
}

func matchesKnownTag(s string) bool {
	for _, p := range knownTagPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// isEntityAt detects whether the & at s[i] starts a named entity we
// produced earlier (so we don't escape it back to &amp;).
func isEntityAt(s string, i int) bool {
	if i+1 >= len(s) {
		return false
	}
	for _, name := range []string{"amp;", "lt;", "gt;", "quot;"} {
		if strings.HasPrefix(s[i+1:], name) {
			return true
		}
	}
	return false
}

// ltoa is a small itoa that avoids importing strconv just for placeholders.
func ltoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	bp := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		bp--
		b[bp] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		bp--
		b[bp] = '-'
	}
	return string(b[bp:])
}
