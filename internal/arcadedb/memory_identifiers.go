package arcadedb

import (
	"strings"
	"unicode"
)

// Similarity cannot establish an exact technical identity. Qualify the bounded
// hydrated candidates before admission, including when ranking fell back to
// lexical search. Ordinary words and purely numeric dates are not identifiers.
func memoryQueryIdentifiers(query string) []string {
	var identifiers []string
	for _, token := range memoryIdentifierTokens(query) {
		var letter, digit bool
		for _, symbol := range token {
			letter = letter || unicode.IsLetter(symbol)
			digit = digit || unicode.IsDigit(symbol)
		}
		if letter && (digit || strings.ContainsRune(token, '_')) {
			identifiers = append(identifiers, token)
		}
	}
	return identifiers
}

func memoryIdentifierTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(symbol rune) bool {
		return !unicode.IsLetter(symbol) && !unicode.IsDigit(symbol) &&
			!unicode.IsMark(symbol) && symbol != '_' && symbol != '-'
	})
}

func memoryIdentifiersMatch(identifiers []string, texts ...string) bool {
	if len(identifiers) == 0 {
		return true
	}
	available := make(map[string]struct{})
	for _, text := range texts {
		for _, token := range memoryIdentifierTokens(text) {
			available[token] = struct{}{}
		}
	}
	for _, identifier := range identifiers {
		if _, found := available[identifier]; !found {
			return false
		}
	}
	return true
}
