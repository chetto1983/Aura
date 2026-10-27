// Package asciifold makes a caption safe for the Telegram Bot API, which answers a
// document or voice caption carrying a byte above 0x7F with a 400 (Pitfall 4 /
// T-13-02-CaptionInject). send_file sanitizes on the substrate side and the Telegram channel
// re-applies it defensively; both used to carry their own copy of this table, to keep the
// substrate from importing the channel. A leaf package keeps them uncoupled without the copy.
package asciifold

import (
	"strings"
	"unicode"
)

// Caption folds an accented Latin rune to its base ASCII letter where trivially possible and
// drops any other non-ASCII rune, so the result contains only bytes <= 0x7F. It is
// best-effort by design: dropping a rune never fails, a 400 would.
func Caption(caption string) string {
	var b strings.Builder
	b.Grow(len(caption))
	for _, r := range caption {
		switch {
		case r <= unicode.MaxASCII:
			b.WriteRune(r)
		default:
			if base := foldLatin(r); base != 0 {
				b.WriteByte(base)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// foldLatin maps a common accented Latin rune to its base ASCII letter, or 0 when there is no
// trivial single-byte equivalent. It covers the Latin-1/Latin-Extended-A letters an
// Italian/European caption is most likely to carry; it is deliberately small, not a full
// Unicode transliteration table.
func foldLatin(r rune) byte {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å', 'ā', 'ă', 'ą':
		return 'a'
	case 'è', 'é', 'ê', 'ë', 'ē', 'ĕ', 'ė', 'ę', 'ě':
		return 'e'
	case 'ì', 'í', 'î', 'ï', 'ī', 'ĭ', 'į':
		return 'i'
	case 'ò', 'ó', 'ô', 'õ', 'ö', 'ø', 'ō', 'ŏ', 'ő':
		return 'o'
	case 'ù', 'ú', 'û', 'ü', 'ū', 'ŭ', 'ů', 'ű':
		return 'u'
	case 'ç', 'ć', 'č', 'ĉ', 'ċ':
		return 'c'
	case 'ñ', 'ń', 'ň':
		return 'n'
	case 'ÿ', 'ý':
		return 'y'
	case 'À', 'Á', 'Â', 'Ã', 'Ä', 'Å', 'Ā', 'Ă', 'Ą':
		return 'A'
	case 'È', 'É', 'Ê', 'Ë', 'Ē', 'Ĕ', 'Ė', 'Ę', 'Ě':
		return 'E'
	case 'Ì', 'Í', 'Î', 'Ï', 'Ī', 'Ĭ', 'Į':
		return 'I'
	case 'Ò', 'Ó', 'Ô', 'Õ', 'Ö', 'Ø', 'Ō', 'Ŏ', 'Ő':
		return 'O'
	case 'Ù', 'Ú', 'Û', 'Ü', 'Ū', 'Ŭ', 'Ů', 'Ű':
		return 'U'
	case 'Ç', 'Ć', 'Č', 'Ĉ', 'Ċ':
		return 'C'
	case 'Ñ', 'Ń', 'Ň':
		return 'N'
	default:
		return 0
	}
}
