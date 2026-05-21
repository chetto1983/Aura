package stringx

import (
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// StripDiacritics removes combining diacritical marks (Unicode Mn category)
// from s via NFD decomposition. "è" → "e", "ñ" → "n", "Ü" → "U".
// NFC and pre-composed input is handled transparently.
func StripDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)))
	result, _, _ := transform.String(t, s)
	return result
}
