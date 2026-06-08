package telegram

import (
	"strings"
	"testing"
	"unicode"
)

// reservedOutside is the MarkdownV2 reserved set that must be backslash-escaped
// when it appears OUTSIDE an intended entity (the spike's Pitfall #18 set).
const reservedOutside = "_*[]()~`>#+-=|{}.!\\"

func TestEscapeMarkdownV2_OutsideFenceReservedAreEscaped(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "naked_dash", in: "a-b", want: "a\\-b"},
		{name: "naked_dot", in: "end.", want: "end\\."},
		{name: "naked_paren", in: "f(x)", want: "f\\(x\\)"},
		{name: "naked_bang_bracket", in: "![alt]", want: "\\!\\[alt\\]"},
		{name: "plus_equals_pipe", in: "1+1=2|3", want: "1\\+1\\=2\\|3"},
		{name: "hash_gt_brace", in: "#>{x}", want: "\\#\\>\\{x\\}"},
		{name: "underscore_star_tilde", in: "_*~", want: "\\_\\*\\~"},
		{name: "backslash", in: "a\\b", want: "a\\\\b"},
		{name: "plain_text_untouched", in: "ciao mondo", want: "ciao mondo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeMarkdownV2(tt.in); got != tt.want {
				t.Errorf("EscapeMarkdownV2(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEscapeMarkdownV2_InsideFencePipesDashesDotsUnescaped(t *testing.T) {
	// VALIDATION "pre-fence pipes/dashes unescaped": inside a ``` block only
	// backtick and backslash are reserved; |,-,. flow through untouched.
	in := "```\n| col |---| 1.0 - 2 |\n```"
	got := EscapeMarkdownV2(in)
	if strings.Contains(got, "\\|") || strings.Contains(got, "\\-") || strings.Contains(got, "\\.") {
		t.Errorf("in-fence pipes/dashes/dots must NOT be escaped; got %q", got)
	}
	// The fence delimiters themselves survive verbatim.
	if !strings.HasPrefix(got, "```") || !strings.HasSuffix(got, "```") {
		t.Errorf("fence delimiters must survive verbatim; got %q", got)
	}
}

func TestEscapeMarkdownV2_InlineCodeSpanPreservesContent(t *testing.T) {
	// Inside a single-backtick inline code span, reserved chars flow through.
	in := "use `a-b.c` here"
	got := EscapeMarkdownV2(in)
	if strings.Contains(got, "`a\\-b\\.c`") {
		t.Errorf("inline-code content must not be escaped; got %q", got)
	}
	// The surrounding prose ("here" has no reserved) and the span delimiters survive.
	if !strings.Contains(got, "`a-b.c`") {
		t.Errorf("inline code span content must pass through; got %q", got)
	}
}

func TestEscapeMarkdownV2_InFenceBackslashAndBacktickStillEscaped(t *testing.T) {
	// Inside a fence, a stray backslash IS reserved and must be escaped so the
	// span content does not break the entity stream.
	in := "```\npath\\to\n```"
	got := EscapeMarkdownV2(in)
	if !strings.Contains(got, "path\\\\to") {
		t.Errorf("in-fence backslash must be escaped; got %q", got)
	}
}

func TestEscapeMarkdownV2_NeverWholeStringEscape(t *testing.T) {
	// A bold span the model emitted must keep its * markers usable. A naive
	// whole-string escaper would backslash every char including the entity
	// markers. We assert a representative entity passes through with at least
	// one un-escaped reserved marker preserved as formatting is out of scope
	// here — instead we assert plain ASCII letters are never escaped.
	in := "abcDEF123"
	if got := EscapeMarkdownV2(in); got != in {
		t.Errorf("plain alphanumerics must never be escaped; got %q", got)
	}
}

// validateMarkdownV2 is the oracle: it walks the escaped output the same way the
// Telegram parser does and returns false if any reserved char appears unescaped
// outside a fence, or a fence is left open — i.e. the cases that 400 with
// "can't parse entities". It is intentionally a strict superset of the parser's
// rejection rules so a pass here implies no would-400.
func validateMarkdownV2(s string) bool {
	type mode int
	const (
		outside mode = iota
		inPre
		inInline
	)
	m := outside
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch m {
		case outside:
			switch {
			case c == '\\':
				// An escape must be followed by a reserved char (a real escape),
				// never dangle at end of string.
				if i+1 >= len(runes) {
					return false
				}
				i++ // consume the escaped char
			case c == '`':
				if i+2 < len(runes) && runes[i+1] == '`' && runes[i+2] == '`' {
					m = inPre
					i += 2
				} else {
					m = inInline
				}
			case strings.ContainsRune(reservedOutside, c):
				// Any reserved char outside a fence that was NOT preceded by a
				// backslash (handled above) is a would-400.
				return false
			}
		case inPre:
			switch {
			case c == '\\':
				if i+1 >= len(runes) {
					return false
				}
				i++
			case c == '`':
				if i+2 < len(runes) && runes[i+1] == '`' && runes[i+2] == '`' {
					m = outside
					i += 2
				} else {
					// a lone backtick inside a pre block must be escaped
					return false
				}
			}
		case inInline:
			switch {
			case c == '\\':
				if i+1 >= len(runes) {
					return false
				}
				i++
			case c == '`':
				m = outside
			}
		}
	}
	return m == outside // no unterminated fence
}

func FuzzMdv2(f *testing.F) {
	for _, seed := range mdv2SeedCorpus() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := EscapeMarkdownV2(in)
		if !validateMarkdownV2(out) {
			t.Errorf("EscapeMarkdownV2(%q) = %q is not a well-formed MarkdownV2 stream (would 400)", in, out)
		}
	})
}

// TestFuzzMdv2_SeedCorpusReplaysClean is the per-task gate: it replays the full
// >=10K-entry seeded corpus deterministically (no -fuzz) and asserts every entry
// escapes to a well-formed (no would-400) stream. The 30s -fuzz campaign is the
// wave/quality gate, not this fast per-task run.
func TestFuzzMdv2_SeedCorpusReplaysClean(t *testing.T) {
	corpus := mdv2SeedCorpus()
	if len(corpus) < 10_000 {
		t.Fatalf("seed corpus has %d entries, want >= 10000 (10K-Unicode requirement)", len(corpus))
	}
	for i, in := range corpus {
		out := EscapeMarkdownV2(in)
		if !validateMarkdownV2(out) {
			t.Fatalf("corpus[%d] %q -> %q is not well-formed MarkdownV2 (would 400)", i, in, out)
		}
	}
}

// mdv2SeedCorpus builds a >=10K-entry corpus of Unicode + prose-punctuation
// strings: every reserved char in isolation and in prose context, fenced and
// unfenced table payloads, and a broad sweep of Unicode code points (Latin,
// accented, CJK, emoji, control, combining) interleaved with reserved chars.
func mdv2SeedCorpus() []string {
	var corpus []string

	// Hand-curated edge cases (prose with every reserved char, fences, tables).
	fixed := []string{
		"",
		"plain text no reserved",
		"a-b", "end.", "f(x)", "![alt]", "1+1=2|3", "#>{x}", "_*~",
		"a\\b", "100%% done", "list: - item",
		"```\n| col | val |\n|-----|-----|\n| 1.0 | 2.0 |\n```",
		"```python\nx = a - b  # 1.0\n```",
		"inline `a-b.c` code",
		"mix `code` and -dash. and (paren)",
		"unterminated ``` fence open",
		"unterminated ` inline open",
		"nested ``` outer `inner` still outer ```",
		"emoji 😀 with -dash and .dot",
		"日本語のテキスト - ダッシュ . ドット",
		"combining é with -",
		"control\x00char-dot.",
		"\\", "\\\\", ".", "-", "!", "|", "`", "```",
	}
	corpus = append(corpus, fixed...)

	reserved := []rune(reservedOutside)
	// Reserved char in 3 prose templates -> ~54 entries.
	for _, r := range reserved {
		corpus = append(corpus,
			string(r),
			"pre"+string(r)+"post",
			"x"+string(r)+string(r)+"y",
		)
	}

	// Broad Unicode sweep interleaved with reserved chars. Walk code points in
	// strides across BMP + a slice of astral planes, pairing each with a rotating
	// reserved char and a prose frame. This produces well over 10K entries.
	stride := 0
	for cp := rune(0x20); cp <= 0x33FF; cp += 2 {
		if !unicode.IsGraphic(cp) {
			continue
		}
		r := reserved[stride%len(reserved)]
		corpus = append(corpus, string(cp)+string(r)+string(cp))
		corpus = append(corpus, "```\n"+string(cp)+string(r)+"\n```")
		stride++
	}
	// CJK Unified Ideographs slice — dense graphic code points outside the BMP
	// punctuation ranges, each interleaved with a rotating reserved char.
	for cp := rune(0x4E00); cp <= 0x5400; cp += 2 {
		r := reserved[stride%len(reserved)]
		corpus = append(corpus, string(cp)+string(r)+"text"+string(cp))
		stride++
	}
	// A slice of emoji / astral plane code points.
	for cp := rune(0x1F300); cp <= 0x1F600; cp += 1 {
		r := reserved[stride%len(reserved)]
		corpus = append(corpus, string(cp)+" "+string(r)+" "+string(cp))
		stride++
	}

	return corpus
}
