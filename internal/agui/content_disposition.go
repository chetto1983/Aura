package agui

import (
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// contentDisposition builds a safe RFC-6266 "attachment" Content-Disposition value for an
// arbitrary (agent/user-influenced) download filename (WEBART-03/D-11). It emits BOTH a quoted
// ASCII fallback (filename="...") for legacy agents and a percent-encoded UTF-8 extended param
// (the filename* param) that preserves unicode on modern browsers.
//
// Unlike the Telegram caption path it does NOT transliterate the name: D-11 preserves the unicode
// filename here (on the extended param) rather than ASCII-folding it. Every CR/LF/quote/backslash
// and control byte is stripped from the quoted fallback and percent-encoded in the extended param,
// so no filename can inject a header or a second param (T-HdrInj); an empty name collapses to
// "download". Ported from LibreChat api/server/utils/files.js:67-88.
func contentDisposition(filename string) string {
	fallback := asciiFallbackFilename(filename)
	// url.PathEscape percent-encodes every header-structural byte (CR %0D, LF %0A, ';' %3B, '"'
	// %22, space %20) while round-tripping UTF-8, so the extended param is injection-safe and
	// unicode-preserving.
	extended := url.PathEscape(filename)
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + extended
}

// asciiFallbackFilename folds diacritics (NFKD decompose, drop combining marks, recompose) so an
// accented name degrades to its base-Latin ASCII form ("relazióne.pdf" -> "relazione.pdf"), then
// replaces every remaining non-printable-ASCII byte and the quoted-string delimiters ('"' and
// '\\') with '_'. An empty result collapses to "download". The returned string therefore contains
// only printable ASCII with no '"', '\\', CR, or LF — safe to drop verbatim inside filename="...".
func asciiFallbackFilename(filename string) string {
	// A fresh transformer per call: transform.Transformer carries state and is not safe to share
	// across goroutines (a download handler may serve concurrently).
	folded, _, err := transform.String(
		transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC),
		filename,
	)
	if err != nil {
		folded = filename
	}
	var b strings.Builder
	b.Grow(len(folded))
	for _, r := range folded {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteByte(byte(r))
	}
	fallback := b.String()
	if fallback == "" {
		return "download"
	}
	return fallback
}
